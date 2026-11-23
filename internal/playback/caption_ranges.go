package playback

import (
	"context"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"

	"waygu-video/internal/mediahttp"
)

type captionFile struct {
	resource mediahttp.Resource
	client   *http.Client
	size     int64
}

func openCaptionFile(resource mediahttp.Resource, size int64) (captionFile, func(), error) {
	client, err := newCaptionClient(resource.URL, resource.Trust)
	if err != nil {
		return captionFile{}, nil, err
	}
	closeIdle := func() {}
	if transport, ok := client.Transport.(*http.Transport); ok {
		closeIdle = transport.CloseIdleConnections
	}
	client.Transport = resource.Authorize.Transport(client.Transport)
	return captionFile{resource, client, size}, closeIdle, nil
}

func (file captionFile) read(ctx context.Context, offset, length int64) ([]byte, error) {
	if offset < 0 || length <= 0 || offset > file.size-length {
		return nil, errCaptions
	}
	first := strconv.FormatInt(offset, 10)
	last := strconv.FormatInt(offset+length-1, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.resource.URL, nil)
	if err != nil {
		return nil, errCaptions
	}
	maps.Copy(req.Header, file.resource.Headers)
	req.Header.Set("Range", "bytes="+first+"-"+last)
	req.Header.Set("Accept-Encoding", "identity")
	res, err := file.client.Do(req)
	if err != nil {
		return nil, errCaptions
	}
	defer res.Body.Close()
	// Validate the entire range before reading. Some upstreams respond with
	// the full movie, even with a partial content status.
	rangeHeader := "bytes " + first + "-" + last + "/" + strconv.FormatInt(file.size, 10)
	encoding := res.Header.Get("Content-Encoding")
	if res.StatusCode != http.StatusPartialContent || res.Header.Get("Content-Range") != rangeHeader ||
		(res.ContentLength >= 0 && res.ContentLength != length) || (encoding != "" && !strings.EqualFold(encoding, "identity")) {
		return nil, errCaptions
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(res.Body, data); err != nil {
		return nil, errCaptions
	}
	return data, nil
}
