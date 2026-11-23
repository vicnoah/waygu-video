package downloads

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
)

// openMP4Playlist opens one finite HLS rendition with an MP4 initialization
// segment. Appending its fragments yields a local fragmented MP4.
func openMP4Playlist(client *http.Client, request *http.Request) (*http.Response, error) {
	base, lines, err := readDownloadPlaylist(client, request)
	if err != nil {
		return nil, err
	}
	if len(lines) == 2 && strings.HasPrefix(lines[0], "#EXT-X-STREAM-INF:") {
		child, err := downloadReference(base, lines[1])
		if err != nil {
			return nil, err
		}
		next := request.Clone(request.Context())
		next.URL = child
		base, lines, err = readDownloadPlaylist(client, next)
		if err != nil {
			return nil, err
		}
	}
	var resources []*url.URL
	var media, ended bool
	for _, line := range lines {
		switch {
		case ended:
			return nil, ErrInvalidFile
		case strings.HasPrefix(line, "#EXT-X-MAP:URI=\"") && strings.HasSuffix(line, "\""):
			if len(resources) != 0 || media {
				return nil, ErrInvalidFile
			}
			ref := strings.TrimSuffix(strings.TrimPrefix(line, "#EXT-X-MAP:URI=\""), "\"")
			if strings.ContainsRune(ref, '"') {
				return nil, ErrInvalidFile
			}
			address, err := downloadReference(base, ref)
			if err != nil {
				return nil, err
			}
			resources = append(resources, address)
		case strings.HasPrefix(line, "#EXTINF:"):
			if media || len(resources) == 0 {
				return nil, ErrInvalidFile
			}
			media = true
		case line == "#EXT-X-ENDLIST":
			ended = true
		case !strings.HasPrefix(line, "#"):
			if !media {
				return nil, ErrInvalidFile
			}
			address, err := downloadReference(base, line)
			if err != nil {
				return nil, err
			}
			resources = append(resources, address)
			media = false
		case strings.HasPrefix(line, "#EXT-X-KEY"), strings.HasPrefix(line, "#EXT-X-BYTERANGE"),
			strings.HasPrefix(line, "#EXT-X-DISCONTINUITY"), strings.HasPrefix(line, "#EXT-X-MAP"):
			return nil, ErrInvalidFile
		}
	}
	if !ended || media || len(resources) < 2 {
		return nil, ErrInvalidFile
	}
	segmentCount := len(resources) - 1
	if segmentCount > 65535 {
		return nil, ErrInvalidFile
	}
	index := mp4.SidxBox{SidxRefs: make([]mp4.SidxRef, segmentCount)}
	var padding bytes.Buffer
	if err := mp4.NewFreeBox(make([]byte, index.Size()-8)).Encode(&padding); err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Header: http.Header{"Content-Type": {"video/mp4"}},
		Body: &downloadFragments{client: client, request: request, remaining: resources,
			segmentCount: segmentCount, indexSize: index.Size(), padding: &padding}}, nil
}

func readDownloadPlaylist(client *http.Client, request *http.Request) (*url.URL, []string, error) {
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, ErrInvalidFile
	}
	const limit = 1 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(body) > limit {
		return nil, nil, ErrInvalidFile
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "#EXTM3U" {
		return nil, nil, ErrInvalidFile
	}
	content := lines[:0]
	for _, line := range lines[1:] {
		if line = strings.TrimSpace(line); line != "" {
			content = append(content, line)
		}
	}
	return response.Request.URL, content, nil
}

func downloadReference(base *url.URL, ref string) (*url.URL, error) {
	link, err := url.Parse(ref)
	if err != nil || link.Path == "" || link.Fragment != "" {
		return nil, ErrInvalidFile
	}
	address := base.ResolveReference(link)
	if address.Scheme != base.Scheme || address.Host != base.Host || address.User != nil {
		return nil, ErrInvalidFile
	}
	return address, nil
}

type downloadFragments struct {
	client       *http.Client
	request      *http.Request
	remaining    []*url.URL
	body         io.ReadCloser
	expected     string
	checksum     hash.Hash
	read         bool
	segmentCount int
	indexSize    uint64
	indexOffset  int64
	padding      *bytes.Buffer
}

func (d *downloadFragments) Read(p []byte) (int, error) {
	for d.body == nil {
		if len(d.remaining) == d.segmentCount && d.padding.Len() > 0 {
			return d.padding.Read(p)
		}
		if len(d.remaining) == 0 {
			return 0, io.EOF
		}
		request := d.request.Clone(d.request.Context())
		request.URL = d.remaining[0]
		d.remaining = d.remaining[1:]
		response, err := d.client.Do(request)
		if err != nil {
			return 0, ErrInvalidFile
		}
		if response.StatusCode != http.StatusOK || response.ContentLength == 0 ||
			(response.Header.Get("Content-Encoding") != "" && !strings.EqualFold(response.Header.Get("Content-Encoding"), "identity")) {
			response.Body.Close()
			return 0, ErrInvalidFile
		}
		d.body, d.read = response.Body, false
		d.expected = response.Header.Get("Content-MD5")
		if d.expected != "" {
			d.checksum = md5.New()
		} else {
			d.checksum = nil
		}
	}
	n, err := d.body.Read(p)
	if n > 0 {
		d.read = true
		if len(d.remaining) == d.segmentCount {
			d.indexOffset += int64(n)
		}
		if d.checksum != nil {
			_, _ = d.checksum.Write(p[:n])
		}
	}
	if err != io.EOF {
		return n, err
	}
	_ = d.Close()
	if !d.read || (d.checksum != nil && d.expected != base64.StdEncoding.EncodeToString(d.checksum.Sum(nil))) {
		return n, ErrInvalidFile
	}
	return n, nil
}

func (d *downloadFragments) Close() error {
	if d.body == nil {
		return nil
	}
	err := d.body.Close()
	d.body = nil
	return err
}
