package media

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"strings"
)

type DiscoveredServer struct {
	Provider Provider
	Address  string
	Name     string
}

// DiscoverLocal sends the advertised Emby and Jellyfin discovery queries.
// Callers set the discovery window with ctx. No credentials are exchanged.
func DiscoverLocal(ctx context.Context, found func(DiscoveredServer)) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	destinations := discoveryDestinations()
	results := make(chan DiscoveredServer)
	finished := make(chan struct{}, 2)
	for _, probe := range []struct {
		provider Provider
		query    string
	}{{Emby, "who is EmbyServer?"}, {Jellyfin, "who is JellyfinServer?"}} {
		go func() {
			defer func() { finished <- struct{}{} }()
			for server := range discoverUDP(ctx, probe.provider, probe.query, destinations) {
				select {
				case results <- server:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	seen := make(map[DiscoveredServer]bool)
	for pending := 2; pending > 0; {
		select {
		case <-ctx.Done():
			return
		case <-finished:
			pending--
		case server := <-results:
			key := DiscoveredServer{Provider: server.Provider, Address: server.Address}
			if !seen[key] {
				seen[key] = true
				found(server)
			}
			if len(seen) >= 32 {
				return
			}
		}
	}
}

func discoverUDP(ctx context.Context, provider Provider, query string, destinations []netip.AddrPort) func(func(DiscoveredServer) bool) {
	return func(yield func(DiscoveredServer) bool) {
		conn, err := net.ListenUDP("udp4", nil)
		if err != nil {
			return
		}
		defer conn.Close()
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		sent := false
		for _, address := range destinations {
			if _, err := conn.WriteToUDPAddrPort([]byte(query), address); err == nil {
				sent = true
			}
		}
		if !sent {
			return
		}
		var reply [4096]byte
		for {
			n, _, err := conn.ReadFromUDPAddrPort(reply[:])
			if err != nil {
				return
			}
			var data struct{ Address, Name string }
			if json.Unmarshal(reply[:n], &data) != nil {
				continue
			}
			address, err := NormalizeAddress(data.Address)
			if err != nil {
				continue
			}
			if !yield(DiscoveredServer{Provider: provider, Address: address.String(), Name: strings.TrimSpace(data.Name)}) {
				return
			}
		}
	}
}

func discoveryDestinations() []netip.AddrPort {
	const port = 7359
	limited := netip.AddrPortFrom(netip.AddrFrom4([4]byte{255, 255, 255, 255}), port)
	destinations := []netip.AddrPort{limited}
	seen := map[netip.AddrPort]bool{limited: true}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&(net.FlagUp|net.FlagBroadcast) != net.FlagUp|net.FlagBroadcast {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			subnet, ok := address.(*net.IPNet)
			if !ok || len(subnet.Mask) != net.IPv4len || subnet.IP.To4() == nil {
				continue
			}
			ones, _ := subnet.Mask.Size()
			if ones == 0 || ones >= 31 {
				continue
			}
			ip := [4]byte(subnet.IP.To4())
			for i := range ip {
				ip[i] |= ^subnet.Mask[i]
			}
			destination := netip.AddrPortFrom(netip.AddrFrom4(ip), port)
			if !seen[destination] {
				seen[destination] = true
				destinations = append(destinations, destination)
			}
		}
	}
	return destinations
}
