// Package playback prepares authenticated media for the native player.
package playback

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"waygu-video/internal/mediahttp"
	"waygu-video/internal/mediatls"
)

const maxPlaylist = 4 << 20

// Open starts a loopback proxy for a player that needs authenticated headers.
// It forwards byte ranges and keeps the credentials out of the player's URL.
func Open(source mediahttp.Resource) (string, func(), error) {
	return openProxy(source, "127.0.0.1:0", netip.Addr{}, "", 0)
}

// OpenWithReadLimit stops a decoder from reading beyond one shared byte budget.
func OpenWithReadLimit(source mediahttp.Resource, limit int64) (string, func(), error) {
	return openProxy(source, "127.0.0.1:0", netip.Addr{}, "", limit)
}

// OpenForRenderer publishes a single authenticated stream on the interface
// that reaches the receiver. Only that receiver may fetch its opaque URLs.
func OpenForRenderer(source mediahttp.Resource, receiver netip.Addr, contentType string) (string, func(), error) {
	if !receiver.IsValid() || receiver.IsUnspecified() || receiver.IsMulticast() {
		return "", nil, errors.New("invalid receiver address")
	}
	conn, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(receiver, 1900)))
	if err != nil {
		return "", nil, err
	}
	local := conn.LocalAddr().(*net.UDPAddr).AddrPort().Addr()
	_ = conn.Close()
	return openProxy(source, netip.AddrPortFrom(local, 0).String(), receiver.Unmap(), contentType, 0)
}

type ArtworkProxy struct {
	Address string
	Resolve func(string) string
	Close   func()
}

// OpenArtwork supplies server artwork to native image views that use their own
// HTTP client. Each allowed media URL has its own opaque request path.
func OpenArtwork(source mediahttp.Resource) (ArtworkProxy, error) {
	proxy, local, stop, err := startProxy(source, "127.0.0.1:0", netip.Addr{}, "", 0)
	if err != nil {
		return ArtworkProxy{}, err
	}
	resolve := func(raw string) string {
		endpoint, err := url.Parse(raw)
		if err != nil || !proxy.sameServer(endpoint) {
			return raw
		}
		return local + proxy.add(endpoint)
	}
	return ArtworkProxy{Address: local, Resolve: resolve, Close: stop}, nil
}

func openProxy(source mediahttp.Resource, bind string, receiver netip.Addr, contentType string, limit int64) (string, func(), error) {
	proxy, local, stop, err := startProxy(source, bind, receiver, contentType, limit)
	if err != nil {
		return "", nil, err
	}
	return local + proxy.add(proxy.origin), stop, nil
}

func startProxy(source mediahttp.Resource, bind string, receiver netip.Addr, contentType string, limit int64) (*streamProxy, string, func(), error) {
	origin, err := url.Parse(source.URL)
	if err != nil || origin.User != nil || origin.Hostname() == "" || (origin.Scheme != "http" && origin.Scheme != "https") {
		return nil, "", nil, errors.New("invalid playback URL")
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 20 * time.Second}
	if err := mediatls.Configure(transport, source.URL, source.Trust); err != nil {
		return nil, "", nil, err
	}
	listener, err := net.Listen("tcp", bind)
	if err != nil {
		return nil, "", nil, err
	}
	proxy := &streamProxy{origin: origin, headers: source.Headers.Clone(), prefix: "/" + rand.Text() + "/",
		receiver: receiver, mediaType: contentType}
	if limit > 0 {
		proxy.budget = new(atomic.Int64)
		proxy.budget.Store(limit)
	}
	proxy.client = &http.Client{Transport: source.Authorize.Transport(transport), CheckRedirect: func(req *http.Request, prior []*http.Request) error {
		if len(prior) >= 5 || !proxy.sameServer(req.URL) {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
	local := (&url.URL{Scheme: "http", Host: listener.Addr().String()}).String()
	go func() { _ = server.Serve(listener) }()
	return proxy, local, func() {
		_ = server.Close()
		transport.CloseIdleConnections()
	}, nil
}

type streamProxy struct {
	origin      *url.URL
	client      *http.Client
	headers     http.Header
	prefix      string
	receiver    netip.Addr
	mediaType   string
	routes      sync.Map
	budget      *atomic.Int64
	captionMask mp4CaptionMask
}

func (p *streamProxy) sameServer(endpoint *url.URL) bool {
	return endpoint.User == nil && endpoint.Scheme == p.origin.Scheme && strings.EqualFold(endpoint.Host, p.origin.Host)
}

func (p *streamProxy) add(endpoint *url.URL) string {
	sum := sha256.Sum256([]byte(endpoint.String()))
	id := p.prefix + hex.EncodeToString(sum[:16])
	if strings.HasSuffix(strings.ToLower(endpoint.Path), ".m3u8") {
		id += ".m3u8"
	}
	p.routes.Store(id, endpoint)
	return id
}

func (p *streamProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.receiver.IsValid() {
		peer, err := netip.ParseAddrPort(r.RemoteAddr)
		if err != nil || peer.Addr().Unmap().WithZone("") != p.receiver.WithZone("") {
			http.NotFound(w, r)
			return
		}
	}
	value, found := p.routes.Load(r.URL.Path)
	if !found || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		http.NotFound(w, r)
		return
	}
	endpoint := value.(*url.URL)
	request, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint.String(), nil)
	if err != nil {
		http.Error(w, "Invalid upstream request", http.StatusBadGateway)
		return
	}
	for name, values := range p.headers {
		request.Header[name] = values
	}
	playlistPath := strings.HasSuffix(r.URL.Path, ".m3u8")
	maskCaptions := endpoint == p.origin && len(p.captionMask.offsets) > 0
	if maskCaptions {
		request.Header.Set("Accept-Encoding", "identity")
	}
	for _, name := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if playlistPath {
			// Playlist contents are rewritten in full. A 206 response can't
			// be passed through because its relative URLs bypass proxy routes.
			request.Header.Del(name)
			continue
		}
		if value := r.Header.Get(name); value != "" {
			request.Header.Set(name, value)
		}
	}
	response, err := p.client.Do(request)
	if err != nil {
		http.Error(w, "Stream unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 && response.StatusCode != http.StatusNotModified {
		http.Error(w, "Stream redirect was rejected", http.StatusBadGateway)
		return
	}
	var body io.Reader = response.Body
	if maskCaptions && r.Method == http.MethodGet && (response.StatusCode == http.StatusOK || response.StatusCode == http.StatusPartialContent) {
		start, ok := p.captionMask.responseStart(response)
		if !ok {
			http.Error(w, "Unexpected media range", http.StatusBadGateway)
			return
		}
		body = &maskedCaptionReader{source: body, pos: start, offsets: p.captionMask.offsets}
	}
	if p.budget != nil {
		body = &budgetReader{body, p.budget}
	}
	contentType := response.Header.Get("Content-Type")
	if r.Method == http.MethodGet && response.StatusCode == http.StatusOK &&
		(playlistPath || strings.Contains(strings.ToLower(contentType), "mpegurl")) {
		data, err := p.playlist(response.Request.URL, body)
		if err != nil {
			http.Error(w, "Invalid stream playlist", http.StatusBadGateway)
			return
		}
		mime := "application/vnd.apple.mpegurl"
		if endpoint == p.origin && p.mediaType != "" {
			mime = p.mediaType
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
		return
	}
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if maskCaptions && name == "ETag" {
			continue
		}
		if value := response.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	if p.mediaType != "" && endpoint == p.origin && (response.StatusCode == http.StatusOK || response.StatusCode == http.StatusPartialContent) {
		w.Header().Set("Content-Type", p.mediaType)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, body)
}

type budgetReader struct {
	source io.Reader
	left   *atomic.Int64
}

func (reader *budgetReader) Read(buffer []byte) (int, error) {
	for {
		left := reader.left.Load()
		if left <= 0 {
			return 0, io.EOF
		}
		size := min(int64(len(buffer)), left)
		if reader.left.CompareAndSwap(left, left-size) {
			n, err := reader.source.Read(buffer[:size])
			reader.left.Add(size - int64(n))
			return n, err
		}
	}
}

func (p *streamProxy) playlist(base *url.URL, reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxPlaylist+1))
	if err != nil || len(data) > maxPlaylist || !bytes.HasPrefix(data, []byte("#EXTM3U")) {
		return nil, errors.New("invalid HLS playlist")
	}
	var output bytes.Buffer
	lines := bufio.NewScanner(bytes.NewReader(data))
	for lines.Scan() {
		line := lines.Text()
		var err error
		if line != "" && !strings.HasPrefix(line, "#") {
			line, err = p.playlistLink(base, line)
		} else if before, after, found := strings.Cut(line, `URI="`); found {
			address, rest, closed := strings.Cut(after, `"`)
			if !closed {
				return nil, errors.New("unterminated HLS URI")
			}
			address, err = p.playlistLink(base, address)
			line = before + `URI="` + address + `"` + rest
		}
		if err != nil {
			return nil, err
		}
		output.WriteString(line)
		output.WriteByte('\n')
	}
	return output.Bytes(), lines.Err()
}

func (p *streamProxy) playlistLink(base *url.URL, address string) (string, error) {
	reference, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return "", err
	}
	resolved := base.ResolveReference(reference)
	if !p.sameServer(resolved) {
		return "", errors.New("HLS resource points to another server")
	}
	return p.add(resolved), nil
}
