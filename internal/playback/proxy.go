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
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxPlaylist = 4 << 20

// Open starts a loopback proxy for a player that needs authenticated headers.
// It forwards byte ranges and keeps the credentials out of the player's URL.
func Open(address string, headers http.Header) (string, func(), error) {
	origin, err := url.Parse(address)
	if err != nil || origin.User != nil || origin.Hostname() == "" || (origin.Scheme != "http" && origin.Scheme != "https") {
		return "", nil, errors.New("invalid playback URL")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 20 * time.Second}
	proxy := &streamProxy{origin: origin, headers: headers.Clone(), prefix: "/" + rand.Text() + "/"}
	proxy.client = &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, prior []*http.Request) error {
		if len(prior) >= 5 || !proxy.sameServer(req.URL) {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
	local := "http://" + listener.Addr().String() + proxy.add(origin)
	go func() { _ = server.Serve(listener) }()
	return local, func() {
		_ = server.Close()
		transport.CloseIdleConnections()
	}, nil
}

type streamProxy struct {
	origin  *url.URL
	client  *http.Client
	headers http.Header
	prefix  string
	routes  sync.Map
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
	request.Header = p.headers.Clone()
	for _, name := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
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
	contentType := response.Header.Get("Content-Type")
	if r.Method == http.MethodGet && response.StatusCode == http.StatusOK &&
		(strings.HasSuffix(r.URL.Path, ".m3u8") || strings.Contains(strings.ToLower(contentType), "mpegurl")) {
		data, err := p.playlist(response.Request.URL, response.Body)
		if err != nil {
			http.Error(w, "Invalid stream playlist", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
		return
	}
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := response.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
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
