//go:build !darwin

package media

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDiscoverFlymooDNS(t *testing.T) {
	const instance = `Waygu\ NAS\ v1\.2._trim_media._tcp.local.`
	const host = "waygu-nas.local."
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	client, err := net.ListenUDP("udp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	responder := make(chan struct{})
	go func() {
		defer close(responder)
		_ = receiver.SetReadDeadline(time.Now().Add(2 * time.Second))
		var packet [1024]byte
		for {
			size, sender, err := receiver.ReadFromUDPAddrPort(packet[:])
			if err != nil {
				return
			}
			var request dns.Msg
			if request.Unpack(packet[:size]) != nil || len(request.Question) != 1 {
				t.Error("invalid DNS-SD query")
				return
			}
			q := request.Question[0]
			response := new(dns.Msg).SetReply(&request)
			header := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Ttl: 10, Rrtype: q.Qtype}
			switch {
			case q.Name == flymooServiceDomain && q.Qtype == dns.TypePTR:
				// Reject an unrelated service, even when carried in the packet.
				response.Answer = []dns.RR{&dns.PTR{Hdr: header, Ptr: "Other._http._tcp.local."}, &dns.PTR{Hdr: header, Ptr: instance}}
			case q.Name == instance && q.Qtype == dns.TypeSRV:
				response.Answer = []dns.RR{&dns.SRV{Hdr: header, Target: host, Port: 5667}}
			case q.Name == host && q.Qtype == dns.TypeA:
				response.Answer = []dns.RR{&dns.A{Hdr: header, A: net.IPv4(192, 0, 2, 80)}}
			case q.Name == host && q.Qtype == dns.TypeAAAA:
				continue
			default:
				t.Errorf("unexpected DNS-SD question %v", q)
				return
			}
			data, err := response.Pack()
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = receiver.WriteToUDPAddrPort(data, sender)
		}
	}()
	var results []DiscoveredServer
	discoverFlymooDNS(ctx, client, receiver.LocalAddr().(*net.UDPAddr).AddrPort(), func(server DiscoveredServer) bool {
		results = append(results, server)
		return false
	})
	receiver.Close()
	<-responder
	want := DiscoveredServer{Provider: Flymoo, Name: "Waygu NAS v1.2", Address: "https://192.0.2.80:5667/v"}
	if len(results) != 1 || results[0] != want {
		t.Fatalf("discovered %+v, want %+v", results, want)
	}
}

func TestFlymooDNSIPv6LinkLocal(t *testing.T) {
	const host = "waygu-nas.local."
	const instance = "Waygu._trim_media._tcp.local."
	scan := flymooDNSScan{
		destination: netip.MustParseAddrPort("[ff02::fb%eth5]:5353"),
		names:       make(map[string]*flymooDNSInstance),
		addresses:   make(map[string][]netip.Addr),
	}
	header := func(name string, kind uint16) dns.RR_Header {
		return dns.RR_Header{Name: name, Class: dns.ClassINET, Ttl: 10, Rrtype: kind}
	}
	scan.update([]dns.RR{&dns.PTR{Hdr: header(flymooServiceDomain, dns.TypePTR), Ptr: instance}}, []dns.RR{
		&dns.AAAA{Hdr: header(host, dns.TypeAAAA), AAAA: net.ParseIP("fe80::246")},
		&dns.SRV{Hdr: header(instance, dns.TypeSRV), Target: host, Port: 5666},
	})
	count := 0
	scan.results(func(server DiscoveredServer) bool {
		count++
		if server.Address != "http://[fe80::246%25eth5]:5666/v" {
			t.Errorf("link-local media endpoint = %q", server.Address)
		}
		return true
	})
	scan.results(func(DiscoveredServer) bool {
		count++
		return true
	})
	if count != 1 {
		t.Fatalf("discovered %d times, want one", count)
	}
}
