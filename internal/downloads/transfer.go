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

	"waygu-video/internal/mediahttp"
	"waygu-video/internal/mediatls"
)

type Source struct {
	mediahttp.Resource
	Name, MediaID string
	Size          int64
	// MaxHeight marks a newly encoded MP4 whose length may be unknown.
	MaxHeight int
	Duration  time.Duration
	Playlist  bool
	// Tracks fetches optional subtitles and comments for the verified copy.
	Tracks func(context.Context) Tracks
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
	if err := os.Remove(filepath.Join(dir, tracksFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return File{}, err
	}
	resumePath, part := filepath.Join(dir, "resume.json"), filepath.Join(dir, "video.part")
	prepared := filepath.Join(dir, "complete.pending")
	var finished File
	if readJSON(prepared, &finished) == nil && finished.valid() && finished.MediaID == src.MediaID &&
		fileSize(part) == finished.Size && matchesPreparedVideo(finished, src) {
		release, err := s.allocate(id, finished.Size, limits)
		if err != nil {
			return File{}, err
		}
		defer release()
		extras, stopTracks := startTransferTracks(ctx, src.Tracks)
		defer stopTracks()
		return s.finishTransfer(ctx, dir, id, finished, extras, limits)
	}
	if err := os.Remove(prepared); err != nil && !errors.Is(err, os.ErrNotExist) {
		return File{}, err
	}
	if src.MaxHeight > 0 {
		if err := os.Remove(part); err != nil && !errors.Is(err, os.ErrNotExist) {
			return File{}, err
		}
	}
	var previous revision
	position := fileSize(part)
	if src.MaxHeight > 0 || readJSON(resumePath, &previous) != nil || previous.Validator == "" ||
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
	if src.MaxHeight <= 0 {
		request.Header.Set("Range", "bytes="+strconv.FormatInt(position, 10)+"-")
		if position > 0 {
			request.Header.Set("If-Range", previous.Validator)
		}
	}
	client := http.Client{CheckRedirect: func(next *http.Request, history []*http.Request) error {
		if len(history) >= 5 || next.URL.Host != request.URL.Host || next.URL.Scheme != request.URL.Scheme || next.URL.User != nil {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	if transport, err := mediatls.Transport(src.URL, src.Trust); err != nil {
		return File{}, err
	} else if transport != nil {
		defer transport.CloseIdleConnections()
		client.Transport = transport
	}
	client.Transport = src.Authorize.Transport(client.Transport)
	var response *http.Response
	if src.Playlist {
		response, err = openMP4Playlist(&client, request)
	} else {
		response, err = client.Do(request)
	}
	if err != nil {
		if errors.Is(err, ErrInvalidFile) {
			return File{}, err
		}
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
		if current.Total < 0 && src.MaxHeight <= 0 {
			current.Total = src.Size
		}
	case http.StatusPartialContent:
		if src.MaxHeight > 0 {
			return File{}, ErrInvalidFile
		}
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
	unknownSize := current.Total < 0 && src.MaxHeight > 0
	if !unknownSize && current.Total <= position {
		return File{}, ErrInvalidFile
	}
	reserved := max(0, current.Total)
	release, err := s.allocate(id, reserved, limits)
	if err != nil {
		return File{}, err
	}
	defer release()
	extras, stopTracks := startTransferTracks(ctx, src.Tracks)
	defer stopTracks()

	file, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return File{}, err
	}
	defer file.Close()
	if err := file.Truncate(position); err != nil {
		return File{}, err
	}
	if src.MaxHeight <= 0 {
		if err := writeJSON(resumePath, current); err != nil {
			return File{}, err
		}
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
	written := position
	for {
		if err := requestCtx.Err(); err != nil {
			_ = file.Sync()
			return File{}, err
		}
		var n int
		var readErr error
		if !unknownSize && written < current.Total {
			n, readErr = io.ReadFull(response.Body, buffer[:min(int64(len(buffer)), current.Total-written)])
		} else {
			n, readErr = response.Body.Read(buffer)
		}
		if n > 0 {
			next := written + int64(n)
			if !unknownSize && next > current.Total {
				_ = file.Truncate(position)
				return File{}, ErrInvalidFile
			}
			if unknownSize && next > reserved {
				const room = 4 << 20
				reserved = next + room
				if err := s.reserveSize(id, reserved, limits); errors.Is(err, ErrNoSpace) {
					reserved = next
					if err := s.reserveSize(id, reserved, limits); err != nil {
						return File{}, err
					}
				} else if err != nil {
					return File{}, err
				}
			}
			deadline.Reset(idle)
			if _, err := output.Write(buffer[:n]); err != nil {
				return File{}, err
			}
			written = next
			if progress != nil {
				progress(written, current.Total)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = file.Sync()
			return File{}, readErr
		}
	}
	if written == 0 || (!unknownSize && written != current.Total) {
		return File{}, ErrInvalidFile
	}
	if checksum := response.Header.Get("Content-MD5"); checksum != "" && checksum != base64.StdEncoding.EncodeToString(transferMD5.Sum(nil)) {
		_ = file.Truncate(position)
		return File{}, ErrInvalidFile
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	height := 0
	if src.MaxHeight > 0 {
		if extension != ".mp4" {
			_ = file.Truncate(0)
			return File{}, ErrInvalidFile
		}
		var indexOffset int64
		var indexSize uint64
		if src.Playlist {
			fragments := response.Body.(*downloadFragments)
			indexOffset, indexSize = fragments.indexOffset, fragments.indexSize
		}
		written, err = indexMP4Timeline(ctx, file, written, indexOffset, indexSize,
			func(bytes int64) error { return s.reserveSize(id, bytes, limits) })
		if err != nil {
			_ = file.Truncate(0)
			return File{}, err
		}
		height = transcodedMP4Height(file, written, src.MaxHeight, src.Duration)
		if height == 0 {
			_ = file.Truncate(0)
			return File{}, ErrInvalidFile
		}
		digest.Reset()
		if _, err := io.Copy(digest, io.NewSectionReader(file, 0, written)); err != nil {
			return File{}, err
		}
	}
	if err := file.Sync(); err != nil {
		return File{}, err
	}
	if err := file.Close(); err != nil {
		return File{}, err
	}
	result := File{Extension: extension, Size: written, SHA256: hex.EncodeToString(digest.Sum(nil)), MediaID: src.MediaID, Height: height}
	if err := writeJSON(prepared, result); err != nil {
		return File{}, err
	}
	return s.finishTransfer(ctx, dir, id, result, extras, limits)
}

func matchesPreparedVideo(file File, source Source) bool {
	if source.MaxHeight > 0 {
		return file.Height > 0 && file.Height <= source.MaxHeight
	}
	return file.Height == 0 && (source.Size <= 0 || source.Size == file.Size)
}

func startTransferTracks(ctx context.Context, fetch func(context.Context) Tracks) (<-chan Tracks, context.CancelFunc) {
	if fetch == nil {
		return nil, func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	result := make(chan Tracks, 1)
	go func() { result <- fetch(ctx) }()
	return result, cancel
}

func (s *Store) finishTransfer(ctx context.Context, dir, id string, result File, extras <-chan Tracks, limits Limits) (File, error) {
	if extras != nil {
		select {
		case tracks := <-extras:
			// The video is still useful when a provider or the available space
			// cannot supply every optional track.
			if ctx.Err() == nil {
				_ = s.writeTracks(dir, id, result.Size, tracks, limits)
			}
		case <-ctx.Done():
			return File{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if err := os.Rename(filepath.Join(dir, "video.part"), filepath.Join(dir, "video"+result.Extension)); err != nil {
		return File{}, err
	}
	// Open accepts the prepared receipt alongside the completed media,
	// making this final rename recoverable after process termination.
	_ = os.Rename(filepath.Join(dir, "complete.pending"), filepath.Join(dir, "complete.json"))
	_ = os.Remove(filepath.Join(dir, "resume.json"))
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
