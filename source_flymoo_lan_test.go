package main

import (
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestFlymooLANSourceReview(t *testing.T) {
	if nsgotest.Target() != "linux" || os.Getenv("WAYGU_TEST_FLYMOO_LAN") != "1" {
		t.Skip("requires native GTK and a multicast-capable local interface")
	}
	gateway := newFlymooGatewayFixture(t, false, "/v", nil, nil)
	address := gateway.server.Listener.Addr().(*net.TCPAddr)
	replies := serveFlymooLANFixture(t, address)
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			before, probes, logins := replies.Load(), gateway.public.Load(), gateway.logins.Load()
			app := startReview(t, theme.appearance, 1180)
			app.Press("id:add-source")
			app.WaitFor("id:local-flymoo", 3*time.Second)
			if replies.Load() <= before || gateway.public.Load() != probes || gateway.logins.Load() != logins {
				t.Fatal("discovery must use multicast and leave server authentication idle")
			}
			if os.Getenv("WAYGU_TEST_SERVERS") == "1" {
				app.WaitFor("id:local-jellyfin", 3*time.Second)
			}
			app.Screenshot("add-source-flymoo-" + theme.name)
			app.WaitFor("id:scan-again", 3*time.Second)
			before = replies.Load()
			app.Press("id:scan-again")
			app.WaitFor("id:local-flymoo", 3*time.Second)
			if replies.Load() <= before {
				t.Fatal("scan again did not issue a fresh browse")
			}
			app.Press("id:local-flymoo")
			if host := app.Find("id:server-address").Text; host != address.IP.String()+"/v" {
				t.Fatalf("discovery filled the host as %q", host)
			}
			if port := app.Find("id:server-port").Text; port != strconv.Itoa(address.Port) {
				t.Fatalf("discovery filled the port as %q", port)
			}
			if name := app.Find("id:server-name").Text; name != "Waygu Flymoo LAN v1.2" {
				t.Fatalf("discovery filled the name as %q", name)
			}
			app.Screenshot("flymoo-lan-form-" + theme.name)
			app.Press("id:test-connection")
			app.WaitFor("id:connection-verified", 5*time.Second)
			app.Type("id:server-username", "viewer")
			app.Type("id:server-password", "fixture-password")
			app.Press("id:sign-in")
			app.WaitFor("id:import-library-count-movies", 5*time.Second)
			app.Press("id:import-libraries")
			app.WaitFor("id:tab-search", 5*time.Second)
		})
	}
}

func serveFlymooLANFixture(t *testing.T, endpoint *net.TCPAddr) *atomic.Int64 {
	t.Helper()
	const service = "_trim_media._tcp.local."
	const instance = `Waygu\ Flymoo\ LAN\ v1\.2._trim_media._tcp.local.`
	const host = "waygu-lan-fixture.local."
	links, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	var socket *net.UDPConn
	for _, link := range links {
		if link.Flags&(net.FlagUp|net.FlagMulticast|net.FlagLoopback) != net.FlagUp|net.FlagMulticast {
			continue
		}
		socket, err = net.ListenMulticastUDP("udp4", &link, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
		if err == nil {
			break
		}
	}
	if socket == nil {
		t.Fatalf("could not listen on the local mDNS group: %v", err)
	}
	replies := new(atomic.Int64)
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = socket.Close()
		<-done
	})
	go func() {
		defer close(done)
		var packet [4096]byte
		for {
			size, sender, err := socket.ReadFromUDPAddrPort(packet[:])
			if err != nil {
				return
			}
			var request dns.Msg
			if request.Unpack(packet[:size]) != nil || request.Response || len(request.Question) != 1 {
				continue
			}
			query := request.Question[0]
			if !strings.EqualFold(query.Name, service) || query.Qtype != dns.TypePTR {
				continue
			}
			header := func(name string, kind uint16) dns.RR_Header {
				return dns.RR_Header{Name: name, Class: dns.ClassINET, Ttl: 10, Rrtype: kind}
			}
			response := new(dns.Msg).SetReply(&request)
			response.Compress = true
			response.Answer = []dns.RR{&dns.PTR{Hdr: header(service, dns.TypePTR), Ptr: instance}}
			response.Extra = []dns.RR{
				&dns.SRV{Hdr: header(instance, dns.TypeSRV), Target: host, Port: uint16(endpoint.Port)},
				&dns.A{Hdr: header(host, dns.TypeA), A: endpoint.IP},
			}
			wire, err := response.Pack()
			if err != nil {
				t.Error(err)
				return
			}
			replies.Add(1)
			_, _ = socket.WriteToUDPAddrPort(wire, sender)
		}
	}()
	return replies
}
