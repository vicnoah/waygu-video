package downloads

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Source struct {
	URL, Name, MediaID string
	Size               int64
	Headers            http.Header
}

type revision struct {
	Validator string
	Total     int64
	MediaID   string
}

var videoExtensions = map[string]bool{
	".mp4": true, ".m4v": true, ".mkv": true, ".mov": true, ".webm": true, ".avi": true, ".ts": true,
	".m2ts": true, ".mpeg": true, ".mpg": true, ".wmv": true, ".flv": true, ".3gp": true, ".ogv": true,
}

// Transfer resumes an interrupted file when the origin still identifies the
// same representation. The final path becomes playable only after validation.
func (s *Store) Transfer(ctx context.Context, id string, src Source, limits Limits, progress func(bytes, total int64)) (File, error) {
	dir, err := s.directory(id)
	if err != nil {
		return File{}, err
	}
	unclaim, err := s.claim(id)
	if err != nil {
		return File{}, err
	}
	defer unclaim()
	if _, file, err := s.Open(id); err == nil {
		if file.MediaID == src.MediaID {
			return file, nil
		}
		return File{}, ErrInvalidFile
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return File{}, err
	}
	resumePath, part := filepath.Join(dir, "resume.json"), filepath.Join(dir, "video.part")
	var previous revision
	position := fileSize(part)
	if readJSON(resumePath, &previous) != nil || previous.Validator == "" ||
		previous.MediaID != src.MediaID || position >= previous.Total {
		position = 0
	}

	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	const idle = 35 * time.Second
	deadline := time.AfterFunc(idle, cancel)
	defer deadline.Stop()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, src.URL, nil)
	if err != nil || (request.URL.Scheme != "https" && request.URL.Scheme != "http") || request.URL.User != nil {
		return File{}, ErrInvalidFile
	}
	request.Header = src.Headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Range", "bytes="+strconv.FormatInt(position, 10)+"-")
	if position > 0 {
		request.Header.Set("If-Range", previous.Validator)
	}
	client := http.Client{CheckRedirect: func(next *http.Request, history []*http.Request) error {
		if len(history) >= 5 || next.URL.Host != request.URL.Host || next.URL.Scheme != request.URL.Scheme || next.URL.User != nil {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	response, err := client.Do(request)
	if err != nil {
		// The URL may carry a media server token.
		return File{}, errors.New("could not connect to the download source")
	}
	defer response.Body.Close()
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return File{}, ErrInvalidFile
	}
	extension := fileExtension(src.Name, response.Header.Get("Content-Type"))
	if extension == "" {
		return File{}, ErrInvalidFile
	}
	current := revision{Validator: validator(response.Header), Total: response.ContentLength, MediaID: src.MediaID}
	switch response.StatusCode {
	case http.StatusOK:
		position = 0
		if current.Total < 0 {
			current.Total = src.Size
		}
	case http.StatusPartialContent:
		var first, last int64
		count, _ := fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &first, &last, &current.Total)
		if count != 3 || first != position || last != current.Total-1 ||
			(position > 0 && (current.Total != previous.Total || current.Validator != previous.Validator)) ||
			(response.ContentLength >= 0 && response.ContentLength != current.Total-position) {
			_ = os.Remove(resumePath)
			return File{}, ErrInvalidFile
		}
	default:
		if response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			_ = os.Remove(resumePath)
		}
		return File{}, fmt.Errorf("download HTTP status %d", response.StatusCode)
	}
	if current.Total <= 0 || current.Total <= position {
		return File{}, ErrInvalidFile
	}
	release, err := s.allocate(id, current.Total, limits)
	if err != nil {
		return File{}, err
	}
	defer release()

	file, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return File{}, err
	}
	defer file.Close()
	if err := file.Truncate(position); err != nil {
		return File{}, err
	}
	if err := writeJSON(resumePath, current); err != nil {
		return File{}, err
	}
	digest := sha256.New()
	if position > 0 {
		if _, err := io.CopyN(digest, file, position); err != nil {
			return File{}, err
		}
	}
	transferMD5 := md5.New()
	output := io.MultiWriter(file, digest, transferMD5)
	if progress != nil {
		progress(position, current.Total)
	}
	buffer := make([]byte, 128<<10)
	length := current.Total - position
	for remaining := length; remaining > 0; {
		if err := requestCtx.Err(); err != nil {
			_ = file.Sync()
			return File{}, err
		}
		n, readErr := io.ReadFull(response.Body, buffer[:min(int64(len(buffer)), remaining)])
		if n > 0 {
			deadline.Reset(idle)
			if _, err := output.Write(buffer[:n]); err != nil {
				return File{}, err
			}
			remaining -= int64(n)
			if progress != nil {
				progress(current.Total-remaining, current.Total)
			}
		}
		if readErr != nil {
			_ = file.Sync()
			return File{}, readErr
		}
	}
	if n, err := io.Copy(io.Discard, io.LimitReader(response.Body, 1)); n != 0 || err != nil {
		_ = file.Truncate(position)
		return File{}, ErrInvalidFile
	}
	if checksum := response.Header.Get("Content-MD5"); checksum != "" && checksum != base64.StdEncoding.EncodeToString(transferMD5.Sum(nil)) {
		_ = file.Truncate(position)
		return File{}, ErrInvalidFile
	}
	if err := file.Sync(); err != nil {
		return File{}, err
	}
	if err := file.Close(); err != nil {
		return File{}, err
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	result := File{Extension: extension, Size: current.Total, SHA256: hex.EncodeToString(digest.Sum(nil)), MediaID: src.MediaID}
	prepared := filepath.Join(dir, "complete.pending")
	if err := writeJSON(prepared, result); err != nil {
		return File{}, err
	}
	if err := os.Rename(part, filepath.Join(dir, "video"+extension)); err != nil {
		return File{}, err
	}
	// Open accepts the prepared receipt alongside the completed media,
	// making this final rename recoverable after process termination.
	_ = os.Rename(prepared, filepath.Join(dir, "complete.json"))
	_ = os.Remove(resumePath)
	return result, nil
}

func validator(headers http.Header) string {
	if etag := strings.TrimSpace(headers.Get("ETag")); strings.HasPrefix(etag, "\"") && strings.HasSuffix(etag, "\"") {
		return etag
	}
	if modified := headers.Get("Last-Modified"); modified != "" {
		if _, err := http.ParseTime(modified); err == nil {
			return modified
		}
	}
	return ""
}

func fileExtension(name, contentType string) string {
	contentType, _, _ = mime.ParseMediaType(contentType)
	if strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "mpegurl") ||
		strings.Contains(contentType, "json") || strings.Contains(contentType, "xml") {
		return ""
	}
	extension := strings.ToLower(path.Ext(name))
	if videoExtensions[extension] {
		return extension
	}
	switch contentType {
	case "video/mp4", "application/mp4":
		return ".mp4"
	case "video/x-matroska":
		return ".mkv"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "video/mp2t":
		return ".ts"
	}
	return ""
}
