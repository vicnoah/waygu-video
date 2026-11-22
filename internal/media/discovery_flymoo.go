package media

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Flymoo advertises its media service as _trim_media._tcp over DNS-SD.
const flymooServiceType = "_trim_media._tcp"

func flymooDiscoveredServer(host string, port uint16, name string) DiscoveredServer {
	scheme := "http"
	if port == 5667 || port == 8001 {
		scheme = "https"
	}
	address := url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(int(port))), Path: "/v"}
	return DiscoveredServer{Provider: Flymoo, Address: address.String(), Name: strings.TrimSpace(name)}
}
