// Package casting discovers and controls DLNA MediaRenderers.
package casting

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const rendererType = "urn:schemas-upnp-org:device:MediaRenderer:"

var (
	ErrDevice  = errors.New("no supported DLNA renderer found")
	ErrFormat  = errors.New("the receiver does not support the media format")
	ErrChanged = errors.New("the receiver switched to another stream")
)

var lanClient = &http.Client{
	Transport:     &http.Transport{ResponseHeaderTimeout: 3 * time.Second, IdleConnTimeout: 20 * time.Second},
	Timeout:       4 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type Renderer struct {
	Name                             string
	ID                               string
	Address                          netip.Addr
	transport, connection, rendering service
}

type service struct {
	kind    string
	control *url.URL
}

type deviceDescription struct {
	Type     string              `xml:"deviceType"`
	Name     string              `xml:"friendlyName"`
	ID       string              `xml:"UDN"`
	Devices  []deviceDescription `xml:"deviceList>device"`
	Services []struct {
		Kind string `xml:"serviceType"`
		Path string `xml:"controlURL"`
	} `xml:"serviceList>service"`
}

// Discover searches IPv4 SSDP on the default network interface. A manual
// address can select a receiver on a different interface.
func Discover(ctx context.Context) []Renderer {
	return search(ctx, netip.MustParseAddrPort("239.255.255.250:1900"))
}

// Resolve accepts a LAN IP for unicast SSDP or an HTTP device description URL.
func Resolve(ctx context.Context, input string) ([]Renderer, error) {
	input = strings.TrimSpace(input)
	if ip, err := netip.ParseAddr(input); err == nil && localAddress(ip) {
		if devices := search(ctx, netip.AddrPortFrom(ip.Unmap(), 1900)); len(devices) > 0 {
			return devices, nil
		}
		return nil, ErrDevice
	}
	endpoint, err := url.Parse(input)
	if err != nil || !validEndpoint(endpoint, netip.Addr{}) {
		return nil, ErrDevice
	}
	peer, _ := netip.ParseAddr(endpoint.Hostname())
	device, err := describe(ctx, endpoint, peer.Unmap())
	if err != nil {
		return nil, err
	}
	return []Renderer{device}, nil
}

func search(ctx context.Context, destination netip.AddrPort) []Renderer {
	addresses := ssdpLocations(ctx, destination)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var devices []Renderer
	for location, ip := range addresses {
		wg.Go(func() {
			if device, err := describe(ctx, location, ip); err == nil {
				mu.Lock()
				devices = append(devices, device)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return devices
}

func ssdpLocations(ctx context.Context, destination netip.AddrPort) map[*url.URL]netip.Addr {
	network := "udp4"
	if destination.Addr().Is6() {
		network = "udp6"
	}
	conn, err := net.ListenUDP(network, nil)
	if err != nil {
		return nil
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetReadDeadline(time.Now().Add(2100 * time.Millisecond))
	message := "M-SEARCH * HTTP/1.1\r\nHOST: " + destination.String() + "\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\nST: " + rendererType + "1\r\n\r\n"
	if _, err := conn.WriteToUDPAddrPort([]byte(message), destination); err != nil {
		return nil
	}
	addresses := make(map[*url.URL]netip.Addr)
	seen := make(map[string]bool)
	var raw [8192]byte
	for len(addresses) < 32 {
		n, peer, err := conn.ReadFromUDPAddrPort(raw[:])
		if err != nil {
			break
		}
		ip := peer.Addr().Unmap()
		if !destination.Addr().IsMulticast() && ip != destination.Addr() {
			continue
		}
		response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw[:n])), nil)
		if err != nil {
			continue
		}
		_ = response.Body.Close()
		location, err := url.Parse(response.Header.Get("Location"))
		if response.StatusCode != http.StatusOK || err != nil || !validEndpoint(location, ip) || seen[location.String()] {
			continue
		}
		seen[location.String()] = true
		addresses[location] = ip
	}
	return addresses
}

func localAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLoopback()
}

func validEndpoint(endpoint *url.URL, peer netip.Addr) bool {
	if endpoint == nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.Fragment != "" {
		return false
	}
	ip, err := netip.ParseAddr(endpoint.Hostname())
	return err == nil && localAddress(ip) && (!peer.IsValid() || ip.Unmap() == peer)
}

func describe(ctx context.Context, location *url.URL, peer netip.Addr) (Renderer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return Renderer{}, ErrDevice
	}
	resp, err := lanClient.Do(req)
	if err != nil {
		return Renderer{}, err
	}
	defer resp.Body.Close()
	var doc struct {
		Base   string            `xml:"URLBase"`
		Device deviceDescription `xml:"device"`
	}
	if resp.StatusCode != http.StatusOK || xml.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&doc) != nil {
		return Renderer{}, ErrDevice
	}
	base := location
	if alternate, err := url.Parse(strings.TrimSpace(doc.Base)); err == nil && validEndpoint(alternate, peer) {
		base = alternate
	}
	if device := doc.Device.renderer(base, peer); device.transport.control != nil {
		return device, nil
	}
	return Renderer{}, ErrDevice
}

func (device deviceDescription) renderer(base *url.URL, peer netip.Addr) Renderer {
	if _, version := serviceVersion(device.Type, rendererType); version {
		r := Renderer{Name: strings.TrimSpace(device.Name), ID: strings.TrimSpace(device.ID), Address: peer}
		if r.Name == "" {
			r.Name = peer.String()
		}
		for _, entry := range device.Services {
			if strings.TrimSpace(entry.Path) == "" {
				continue
			}
			control, err := base.Parse(strings.TrimSpace(entry.Path))
			if err != nil || !validEndpoint(control, peer) {
				continue
			}
			for prefix, destination := range map[string]*service{
				transportType: &r.transport, connectionType: &r.connection, renderingType: &r.rendering,
			} {
				if version, ok := serviceVersion(entry.Kind, prefix); ok {
					previous, _ := serviceVersion(destination.kind, prefix)
					if version > previous {
						*destination = service{entry.Kind, control}
					}
				}
			}
		}
		if r.transport.control != nil {
			if r.ID == "" {
				r.ID = r.transport.control.String()
			}
			return r
		}
	}
	for _, child := range device.Devices {
		if r := child.renderer(base, peer); r.transport.control != nil {
			return r
		}
	}
	return Renderer{}
}

func serviceVersion(kind, prefix string) (int, bool) {
	suffix, ok := strings.CutPrefix(kind, prefix)
	version, err := strconv.Atoi(suffix)
	return version, ok && err == nil && version > 0
}
