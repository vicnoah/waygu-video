package media

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestDiscoverUDP(t *testing.T) {
	for _, kind := range []Provider{Emby, Jellyfin} {
		t.Run(string(kind), func(t *testing.T) {
			listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			query := "who is " + string(kind) + "Server?"
			want := DiscoveredServer{Provider: kind, Address: "https://server.example.test:8096/media", Name: "Fixture NAS"}
			reply := make(chan error, 1)
			go func() {
				var buf [128]byte
				n, from, err := listener.ReadFromUDP(buf[:])
				if err != nil {
					reply <- err
					return
				}
				if got := string(buf[:n]); got != query {
					reply <- fmt.Errorf("discovery query = %q, want %q", got, query)
					return
				}
				for _, packet := range []string{`{"Address":"file:///invalid"}`, `{"Address":"https://server.example.test:8096/media/","Name":" Fixture NAS "}`} {
					if _, err = listener.WriteToUDP([]byte(packet), from); err != nil {
						reply <- err
						return
					}
				}
				reply <- nil
			}()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			var discovered DiscoveredServer
			for found := range discoverUDP(ctx, kind, query, []netip.AddrPort{listener.LocalAddr().(*net.UDPAddr).AddrPort()}) {
				discovered = found
				break
			}
			if discovered != want {
				t.Fatalf("discovered %+v, want %+v", discovered, want)
			}
			select {
			case err := <-reply:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
