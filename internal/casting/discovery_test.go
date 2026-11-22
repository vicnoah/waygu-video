package casting

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestRendererDiscoveryPinsDescriptionAndControlToSender(t *testing.T) {
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `<root><URLBase>http://192.0.2.1/</URLBase><device><deviceType>urn:schemas-upnp-org:device:Basic:1</deviceType>
<deviceList><device><deviceType>urn:schemas-upnp-org:device:MediaRenderer:2</deviceType><friendlyName>Test TV</friendlyName>
<UDN>uuid:test</UDN><serviceList>
<service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/transport</controlURL></service>
<service><serviceType>urn:schemas-upnp-org:service:AVTransport:99</serviceType><controlURL>http://192.0.2.1/transport</controlURL></service>
</serviceList></device></deviceList></device></root>`)
	}))
	defer host.Close()
	socket, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	query := make(chan string, 1)
	go func() {
		raw := make([]byte, 1024)
		n, sender, err := socket.ReadFromUDPAddrPort(raw)
		if err != nil {
			return
		}
		query <- string(raw[:n])
		for _, location := range []string{"http://192.0.2.1/description.xml", host.URL + "/description.xml", host.URL + "/description.xml"} {
			response := fmt.Sprintf("HTTP/1.1 200 OK\r\nLOCATION: %s\r\nST: %s2\r\n\r\n", location, rendererType)
			_, _ = socket.WriteToUDPAddrPort([]byte(response), sender)
		}
	}()
	found := search(ctx, socket.LocalAddr().(*net.UDPAddr).AddrPort())
	if len(found) != 1 || found[0].Name != "Test TV" || found[0].transport.control.String() != host.URL+"/transport" {
		t.Fatalf("unexpected DLNA discovery result: %+v", found)
	}
	select {
	case message := <-query:
		if !strings.Contains(message, "ST: "+rendererType+"1") {
			t.Fatalf("unexpected SSDP query: %q", message)
		}
	default:
		t.Fatal("renderer did not receive a discovery query")
	}
}
