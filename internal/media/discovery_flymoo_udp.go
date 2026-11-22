//go:build !darwin

package media

import (
	"context"
	"iter"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const mdnsPort = 5353

const flymooServiceDomain = flymooServiceType + ".local."

func discoverFlymooLAN(ctx context.Context) iter.Seq[DiscoveredServer] {
	return func(yield func(DiscoveredServer) bool) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		results := make(chan DiscoveredServer)
		var workers sync.WaitGroup
		interfaces, _ := net.Interfaces()
		for _, link := range interfaces {
			if link.Flags&(net.FlagUp|net.FlagMulticast|net.FlagLoopback) != net.FlagUp|net.FlagMulticast {
				continue
			}
			for _, network := range []string{"udp4", "udp6"} {
				workers.Go(func() {
					conn, multicast, err := flymooMulticastSocket(network, &link)
					if err != nil {
						return
					}
					defer conn.Close()
					discoverFlymooDNS(ctx, conn, multicast, func(server DiscoveredServer) bool {
						select {
						case results <- server:
							return true
						case <-ctx.Done():
							return false
						}
					})
				})
			}
		}
		go func() {
			workers.Wait()
			close(results)
		}()
		for server := range results {
			if !yield(server) {
				return
			}
		}
	}
}

func flymooMulticastSocket(network string, link *net.Interface) (*net.UDPConn, netip.AddrPort, error) {
	conn, err := net.ListenUDP(network, nil)
	if err != nil {
		return nil, netip.AddrPort{}, err
	}
	var group netip.Addr
	if network == "udp4" {
		err = ipv4.NewPacketConn(conn).SetMulticastInterface(link)
		group = netip.MustParseAddr("224.0.0.251")
	} else {
		err = ipv6.NewPacketConn(conn).SetMulticastInterface(link)
		group = netip.MustParseAddr("ff02::fb").WithZone(link.Name)
	}
	if err != nil {
		conn.Close()
		return nil, netip.AddrPort{}, err
	}
	return conn, netip.AddrPortFrom(group, mdnsPort), nil
}

type flymooDNSInstance struct {
	name   string
	label  string
	record dns.SRV
	sent   bool
}

type flymooDNSScan struct {
	socket      *net.UDPConn
	destination netip.AddrPort
	names       map[string]*flymooDNSInstance
	addresses   map[string][]netip.Addr
	questions   map[uint16]dns.Question
	sent        map[dns.Question]bool
}

// Query from an ephemeral port. DNS-SD responders send a legacy unicast
// response back to it, so the scan does not own the host's multicast port.
func discoverFlymooDNS(ctx context.Context, conn *net.UDPConn, destination netip.AddrPort, yield func(DiscoveredServer) bool) {
	stop := context.AfterFunc(ctx, func() { conn.SetReadDeadline(time.Now()) })
	defer stop()
	scan := flymooDNSScan{
		socket: conn, destination: destination, names: make(map[string]*flymooDNSInstance),
		addresses: make(map[string][]netip.Addr), questions: make(map[uint16]dns.Question),
		sent: make(map[dns.Question]bool),
	}
	browse := scan.query(flymooServiceDomain, dns.TypePTR)
	if browse == nil {
		return
	}
	retry := time.AfterFunc(750*time.Millisecond, func() {
		if ctx.Err() == nil {
			_, _ = conn.WriteToUDPAddrPort(browse, destination)
		}
	})
	defer retry.Stop()
	var packet [9000]byte
	for ctx.Err() == nil {
		n, from, err := conn.ReadFromUDPAddrPort(packet[:])
		if err != nil {
			return
		}
		if from.Port() != destination.Port() {
			continue
		}
		var message dns.Msg
		if message.Unpack(packet[:n]) != nil || !scan.accepts(message) {
			continue
		}
		scan.update(message.Answer, message.Extra)
		if !scan.results(yield) {
			return
		}
	}
}

func (scan *flymooDNSScan) query(name string, kind uint16) []byte {
	question := dns.Question{Name: name, Qtype: kind, Qclass: dns.ClassINET}
	if scan.sent[question] || len(scan.questions) >= 100 {
		return nil
	}
	id := dns.Id()
	for id == 0 || scan.questions[id].Qtype != 0 {
		id = dns.Id()
	}
	message := dns.Msg{MsgHdr: dns.MsgHdr{Id: id}, Question: []dns.Question{question}}
	if packet, err := message.Pack(); err == nil {
		if _, err := scan.socket.WriteToUDPAddrPort(packet, scan.destination); err == nil {
			scan.questions[id], scan.sent[question] = question, true
			return packet
		}
	}
	return nil
}

func (scan *flymooDNSScan) accepts(message dns.Msg) bool {
	question, ok := scan.questions[message.Id]
	if !message.Response || message.Opcode != dns.OpcodeQuery || message.Rcode != dns.RcodeSuccess || !ok || len(message.Question) != 1 {
		return false
	}
	reply := message.Question[0]
	return reply.Qtype == question.Qtype && reply.Qclass == question.Qclass && strings.EqualFold(reply.Name, question.Name)
}

func (scan *flymooDNSScan) update(answers, additional []dns.RR) {
	records := append(answers, additional...)
	for _, resource := range records {
		if ptr, ok := resource.(*dns.PTR); ok && usableDNSRecord(ptr) &&
			strings.EqualFold(ptr.Hdr.Name, flymooServiceDomain) && len(scan.names) < 32 {
			key := strings.ToLower(ptr.Ptr)
			if strings.HasSuffix(key, "."+flymooServiceDomain) && scan.names[key] == nil {
				if label := flymooInstanceName(ptr.Ptr); label != "" {
					scan.names[key] = &flymooDNSInstance{name: ptr.Ptr, label: label}
				}
			}
		}
	}
	for _, resource := range records {
		if srv, ok := resource.(*dns.SRV); ok && usableDNSRecord(srv) {
			instance := scan.names[strings.ToLower(srv.Hdr.Name)]
			if instance != nil && srv.Port != 0 && strings.HasSuffix(strings.ToLower(srv.Target), ".local.") {
				instance.record = *srv
			}
		}
	}
	for _, resource := range records {
		if !usableDNSRecord(resource) {
			continue
		}
		host := strings.ToLower(resource.Header().Name)
		for _, instance := range scan.names {
			if strings.EqualFold(instance.record.Target, host) {
				scan.addAddress(host, resource)
				break
			}
		}
	}
}

func usableDNSRecord(resource dns.RR) bool {
	header := resource.Header()
	return header.Ttl > 0 && header.Class&0x7fff == dns.ClassINET
}

func (scan *flymooDNSScan) addAddress(host string, resource dns.RR) {
	var ip netip.Addr
	switch record := resource.(type) {
	case *dns.A:
		ip, _ = netip.AddrFromSlice(record.A)
	case *dns.AAAA:
		ip, _ = netip.AddrFromSlice(record.AAAA)
	default:
		return
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
		return
	}
	if ip.Is6() && ip.IsLinkLocalUnicast() {
		if scan.destination.Addr().Zone() == "" {
			return
		}
		ip = ip.WithZone(scan.destination.Addr().Zone())
	}
	if len(scan.addresses[host]) < 8 {
		scan.addresses[host] = append(scan.addresses[host], ip)
	}
}

func (scan *flymooDNSScan) results(yield func(DiscoveredServer) bool) bool {
	for _, instance := range scan.names {
		if instance.sent {
			continue
		}
		if instance.record.Port == 0 {
			scan.query(instance.name, dns.TypeSRV)
			continue
		}
		ips := scan.addresses[strings.ToLower(instance.record.Target)]
		if len(ips) == 0 {
			scan.query(instance.record.Target, dns.TypeA)
			scan.query(instance.record.Target, dns.TypeAAAA)
			continue
		}
		target := ips[0]
		for _, ip := range ips {
			if ip.Is4() == scan.destination.Addr().Is4() {
				target = ip
				break
			}
		}
		instance.sent = true
		if !yield(flymooDiscoveredServer(target.String(), instance.record.Port, instance.label)) {
			return false
		}
	}
	return true
}

func flymooInstanceName(domain string) string {
	labels := dns.SplitDomainName(domain)
	if len(labels) != 4 {
		return ""
	}
	// Decode DNS presentation escapes, including literal dots and UTF-8 bytes.
	var wire [65]byte
	if _, err := dns.PackDomainName(labels[0]+".", wire[:], 0, nil, false); err == nil {
		return string(wire[1 : 1+int(wire[0])])
	}
	return ""
}
