package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/publicsuffix"
)

const (
	fnConnectPortal = "https://fnos.net"
	fnConnectLookup = "/api/v1/fn/con"
)

var (
	ErrFNConnectID     = errors.New("invalid FN ID")
	ErrFNConnectLookup = errors.New("FN Connect lookup failed")
	ErrFNConnectAbsent = errors.New("FN ID was not found")
	ErrFNConnectRoute  = errors.New("no FN Connect media route is available")
	ErrFNConnectCode   = errors.New("the FN Connect gateway did not accept the access code")
)

type FNConnectClient struct {
	// Transport defaults to the system HTTPS transport.
	Transport http.RoundTripper
}

type FNConnectResult struct {
	Address       string
	BackupAddress string
	Name          string
	ServerID      string
	Cookie        string
}

func FNConnectID(raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if len(id) < 2 || len(id) > 63 || strings.HasPrefix(id, "-") || strings.HasSuffix(id, "-") {
		return ""
	}
	for _, ch := range id {
		if ch != '-' && !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') {
			return ""
		}
	}
	return id
}

func (f FNConnectClient) Resolve(ctx context.Context, id, code string) (FNConnectResult, error) {
	id = FNConnectID(id)
	if id == "" {
		return FNConnectResult{}, ErrFNConnectID
	}
	if len(code) > 128 || code == "." || code == ".." || strings.IndexFunc(code, unicode.IsControl) >= 0 {
		return FNConnectResult{}, ErrFNConnectCode
	}
	data, err := f.lookup(ctx, id)
	if err != nil {
		return FNConnectResult{}, err
	}
	groups := data.candidates()
	if code == "" {
		// A reachable relay provides the identity of the FN ID's NAS.
		// Private addresses can point to a different NAS on another LAN.
		last := len(groups) - 1
		relay, relayAvailable := f.selectRoute(ctx, groups[last], false, FNConnectResult{})
		for _, group := range groups[:last] {
			if result, ok := f.selectRoute(ctx, group, false, relay); ok {
				if relay.ServerID != "" && result.ServerID == relay.ServerID {
					result.BackupAddress = relay.Address
				}
				return result, nil
			}
		}
		if relayAvailable {
			return relay, nil
		}
		return FNConnectResult{}, ErrFNConnectRoute
	}
	for _, group := range groups {
		if result, ok := f.selectRoute(ctx, group, true, FNConnectResult{}); ok {
			// The access code is submitted once, to the selected HTTPS origin.
			return f.authorizeRoute(ctx, result.Address, code)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return FNConnectResult{}, ErrFNConnectRoute
}

type fnConnectData struct {
	IPv4       []string `json:"ipv4"`
	IPv6       []string `json:"ipv6"`
	PublicIPv4 []string `json:"publicIpv4"`
	PublicIPv6 []string `json:"publicIpv6"`
	DDNS       []string `json:"ddns"`
	Relay      []string `json:"fn"`
	Port       struct {
		HTTP  json.Number `json:"httpPort"`
		HTTPS json.Number `json:"httpsPort"`
	} `json:"port"`
}

func (f FNConnectClient) lookup(ctx context.Context, id string) (fnConnectData, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	body, _ := json.Marshal(struct {
		ID string `json:"fnId"`
	}{id})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, fnConnectPortal+fnConnectLookup, bytes.NewReader(body))
	if err != nil {
		return fnConnectData{}, ErrFNConnectLookup
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Origin", fnConnectPortal)
	request.Header.Set("Referer", fnConnectPortal+"/")
	signFNConnectLookup(request.Header, id, body, time.Now())

	client := http.Client{Transport: f.Transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(request)
	if err != nil {
		return fnConnectData{}, ErrFNConnectLookup
	}
	defer response.Body.Close()
	var result struct {
		Code *int          `json:"code"`
		Data fnConnectData `json:"data"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 32<<10)).Decode(&result) != nil || result.Code == nil {
		return fnConnectData{}, ErrFNConnectLookup
	}
	switch *result.Code {
	case 0:
		return result.Data, nil
	case 3000006, 3000037:
		return fnConnectData{}, ErrFNConnectAbsent
	default:
		return fnConnectData{}, ErrFNConnectLookup
	}
}

func signFNConnectLookup(headers http.Header, id string, payload []byte, now time.Time) {
	const clientSeed = "NDzZTVxnRKP8Z0jXg1VAMonaG8akvh"
	const portalKey = "zIGtkc3dqZnJpd29qZXJqa2w7c"
	stamp := strconv.FormatInt(now.UnixMilli(), 10)
	nonce := strconv.Itoa(100000 + now.Nanosecond()%900000)
	lookup := sha256.Sum256([]byte(strings.Join([]string{"trim_connect", id, stamp, "anna"}, "`")))
	auth := strings.Join([]string{clientSeed, fnConnectLookup, nonce, stamp, md5String(payload), portalKey}, "_")
	headers.Set("Fn-Sign", hex.EncodeToString(lookup[:]))
	headers.Set("Authx", "nonce="+nonce+"&timestamp="+stamp+"&sign="+md5String([]byte(auth)))
}

func (d fnConnectData) candidates() [][]string {
	httpPort := fnPort(d.Port.HTTP, "80")
	httpsPort := fnPort(d.Port.HTTPS, "443")
	var secureLocal, local, ddns, public, relay []string
	for _, list := range [][]string{d.IPv4, d.IPv6} {
		for _, raw := range list {
			if ip, err := netip.ParseAddr(raw); err == nil && ip.Zone() == "" &&
				(ip.IsPrivate() || ip.Is4() && ip.IsLinkLocalUnicast()) {
				secureLocal = append(secureLocal, fnMediaURL("https", ip.String(), httpsPort))
				local = append(local, fnMediaURL("http", ip.String(), httpPort))
			}
		}
	}
	for _, raw := range d.DDNS {
		if target := fnDNSMediaURL(raw, httpsPort); target != "" {
			ddns = append(ddns, target)
		}
	}
	for _, list := range [][]string{d.PublicIPv4, d.PublicIPv6} {
		for _, raw := range list {
			if ip, err := netip.ParseAddr(raw); err == nil && ip.Zone() == "" && ip.IsGlobalUnicast() && !ip.IsPrivate() {
				public = append(public, fnMediaURL("https", ip.String(), httpsPort))
			}
		}
	}
	for _, raw := range d.Relay {
		if target := fnDNSMediaURL(raw, "443"); target != "" {
			relay = append(relay, target)
		}
	}
	return [][]string{secureLocal, local, ddns, public, relay}
}

func fnPort(raw json.Number, fallback string) string {
	if raw == "" {
		return fallback
	}
	if port, err := strconv.ParseUint(raw.String(), 10, 16); err == nil && port > 0 {
		return strconv.FormatUint(port, 10)
	}
	return fallback
}

func fnMediaURL(scheme, host, port string) string {
	if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	} else {
		host = net.JoinHostPort(host, port)
	}
	endpoint := url.URL{Scheme: scheme, Host: host, Path: "/v"}
	return endpoint.String()
}

func fnDNSMediaURL(raw, port string) string {
	parsed, err := NormalizeAddress("https://" + raw)
	if err != nil || parsed.Path != "" || parsed.Port() != "" {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if !strings.Contains(host, ".") || len(host) > 253 {
		return ""
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return ""
		}
		for _, ch := range label {
			if ch != '-' && !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') {
				return ""
			}
		}
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return ""
	}
	return fnMediaURL("https", host, port)
}

func (f FNConnectClient) selectRoute(ctx context.Context, group []string, protected bool, known FNConnectResult) (FNConnectResult, bool) {
	return selectFNRoute(ctx, group, func(address string) bool {
		return address != known.Address && (!protected || strings.HasPrefix(address, "https://"))
	}, func(ctx context.Context, address string) (FNConnectResult, bool) {
		if protected {
			return FNConnectResult{Address: address}, f.gatewayAvailable(ctx, address)
		}
		info, err := f.probeMedia(ctx, address, "")
		return FNConnectResult{Address: address, Name: info.Name, ServerID: info.ID},
			err == nil && (known.ServerID == "" || info.ID == known.ServerID)
	})
}

func selectFNRoute(ctx context.Context, group []string, allowed func(string) bool, probe func(context.Context, string) (FNConnectResult, bool)) (FNConnectResult, bool) {
	if len(group) == 0 || ctx.Err() != nil {
		return FNConnectResult{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 1900*time.Millisecond)
	defer cancel()
	outcomes := make(chan FNConnectResult, 4)
	seen := make(map[string]bool)
	for _, address := range group {
		if len(seen) == cap(outcomes) {
			break
		}
		if seen[address] || !allowed(address) {
			continue
		}
		seen[address] = true
		go func() {
			if result, ok := probe(ctx, address); ok {
				outcomes <- result
			} else {
				outcomes <- FNConnectResult{}
			}
		}()
	}
	for range len(seen) {
		select {
		case <-ctx.Done():
			return FNConnectResult{}, false
		case outcome := <-outcomes:
			if outcome.Address != "" {
				return outcome, true
			}
		}
	}
	return FNConnectResult{}, false
}

// FNConnectRoute describes a media endpoint verified by the caller's public probe.
type FNConnectRoute struct {
	Address string
	Remote  bool
}

// Refresh asks the portal for updated addresses and races probes within each
// priority. Every accepted endpoint must report the previously saved NAS ID.
func (f FNConnectClient) Refresh(ctx context.Context, id, expected string, allowed func(string) bool, probe func(context.Context, string) (ServerInfo, error)) (FNConnectRoute, error) {
	id = FNConnectID(id)
	if id == "" {
		return FNConnectRoute{}, ErrFNConnectID
	}
	if expected == "" {
		return FNConnectRoute{}, ErrFNConnectRoute
	}
	data, err := f.lookup(ctx, id)
	if err != nil {
		return FNConnectRoute{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	groups := data.candidates()
	verified := make([]chan FNConnectResult, len(groups))
	for rank, group := range groups {
		verified[rank] = make(chan FNConnectResult, 1)
		go func(ch chan<- FNConnectResult) {
			result, _ := selectFNRoute(ctx, group, allowed, func(ctx context.Context, address string) (FNConnectResult, bool) {
				info, err := probe(ctx, address)
				return FNConnectResult{Address: address}, err == nil && info.ID == expected
			})
			ch <- result
		}(verified[rank])
	}
	for rank, candidates := range verified {
		select {
		case <-ctx.Done():
			return FNConnectRoute{}, ctx.Err()
		case result := <-candidates:
			if result.Address != "" {
				// The first two groups contain the LAN addresses.
				return FNConnectRoute{Address: result.Address, Remote: rank >= 2}, nil
			}
		}
	}
	return FNConnectRoute{}, ErrFNConnectRoute
}

func (f FNConnectClient) probeMedia(ctx context.Context, address, cookie string) (ServerInfo, error) {
	base, err := NormalizeAddress(address)
	if err != nil {
		return ServerInfo{}, err
	}
	client := newFlymoo(newAPI(base), Session{})
	client.client.Transport = f.Transport
	if cookie != "" {
		client.headers = http.Header{"Cookie": {cookie}}
	}
	return client.Probe(ctx)
}

func (f FNConnectClient) gatewayAvailable(ctx context.Context, address string) bool {
	target, _ := url.Parse(address)
	target.Path = "/trimcon"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return false
	}
	client := f.gatewayClient(target)
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK || response.StatusCode == http.StatusNoContent
}

func (f FNConnectClient) authorizeRoute(ctx context.Context, address, code string) (FNConnectResult, error) {
	target, _ := url.Parse(address)
	entry := *target
	entry.Path, entry.RawPath = "/c/"+code, "/c/"+url.PathEscape(code)
	client := f.gatewayClient(target)
	client.Jar, _ = cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.String(), nil)
	if err != nil {
		return FNConnectResult{}, ErrFNConnectCode
	}
	response, err := client.Do(request)
	if err != nil {
		return FNConnectResult{}, ErrFNConnectCode
	}
	response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return FNConnectResult{}, ErrFNConnectCode
	}
	mediaPath := *target
	mediaPath.Path += "/api/v1/sys/config"
	cookies := (&http.Request{Header: make(http.Header)})
	for _, cookie := range client.Jar.Cookies(&mediaPath) {
		cookies.AddCookie(cookie)
	}
	value := cookies.Header.Get("Cookie")
	if value == "" || len(value) > 4096 {
		return FNConnectResult{}, ErrFNConnectCode
	}
	info, err := f.probeMedia(ctx, address, value)
	if err != nil {
		return FNConnectResult{}, ErrFNConnectRoute
	}
	return FNConnectResult{Address: address, Name: info.Name, ServerID: info.ID, Cookie: value}, nil
}

func (f FNConnectClient) gatewayClient(target *url.URL) *http.Client {
	return &http.Client{Transport: f.Transport, CheckRedirect: func(next *http.Request, prior []*http.Request) error {
		if len(prior) >= 4 || next.URL.User != nil || !sameOrigin(target, next.URL) {
			return http.ErrUseLastResponse
		}
		next.Header.Del("Referer")
		return nil
	}}
}
