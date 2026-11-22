package danmaku

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	commentCacheAge  = 24 * time.Hour
	commentCacheSize = 128 << 20
)

func (c *DandanClient) CachedComments(ctx context.Context, cacheDir string, episode int64) ([]Comment, error) {
	if cacheDir == "" {
		return c.Comments(ctx, episode)
	}
	dir := filepath.Join(cacheDir, "dandanplay")
	key := sha256.Sum256(fmt.Appendf(nil, "v2:%s:%d", c.credentials.AppID, episode))
	path := filepath.Join(dir, fmt.Sprintf("%x.json.gz", key))
	if comments, ok := readCommentCache(path); ok {
		return comments, nil
	}
	comments, err := c.Comments(ctx, episode)
	if err != nil {
		return nil, err
	}
	if ctx.Err() == nil && os.MkdirAll(dir, 0700) == nil {
		writeCommentCache(path, comments)
		pruneCommentCache(dir)
	}
	return comments, nil
}

func readCommentCache(path string) ([]Comment, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || time.Since(info.ModTime()) >= commentCacheAge {
		return nil, false
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, false
	}
	defer reader.Close()
	var comments []Comment
	if err := decodeDanmakuJSON(reader, &comments); err != nil || len(comments) > maxEntries {
		return nil, false
	}
	return comments, true
}

func writeCommentCache(path string, comments []Comment) {
	file, err := os.CreateTemp(filepath.Dir(path), ".comments-*")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	writer := gzip.NewWriter(file)
	writeErr := json.NewEncoder(writer).Encode(comments)
	compressionErr := writer.Close()
	closeErr := file.Close()
	if writeErr == nil && compressionErr == nil && closeErr == nil {
		_ = os.Rename(file.Name(), path)
	}
}

func pruneCommentCache(dir string) {
	entries, _ := os.ReadDir(dir)
	var files []os.FileInfo
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json.gz") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			files = append(files, info)
		}
	}
	slices.SortFunc(files, func(a, b os.FileInfo) int { return b.ModTime().Compare(a.ModTime()) })
	var used int64
	for _, file := range files {
		if time.Since(file.ModTime()) >= commentCacheAge || used+file.Size() > commentCacheSize {
			_ = os.Remove(filepath.Join(dir, file.Name()))
		} else {
			used += file.Size()
		}
	}
}

// ClearCachedComments removes files written by CachedComments.
func ClearCachedComments(cacheDir string) error {
	return visitCachedComments(cacheDir, func(root *os.Root, entry fs.DirEntry) error {
		return root.Remove(entry.Name())
	})
}

// CachedCommentBytes reports the bytes occupied by comments on disk.
func CachedCommentBytes(cacheDir string) (int64, error) {
	var size int64
	err := visitCachedComments(cacheDir, func(root *os.Root, entry fs.DirEntry) error {
		info, err := root.Lstat(entry.Name())
		if err == nil && info.Mode().IsRegular() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

func visitCachedComments(cacheDir string, visit func(*os.Root, fs.DirEntry) error) error {
	if cacheDir == "" {
		return nil
	}
	dir := filepath.Join(cacheDir, "dandanplay")
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fs.ErrInvalid
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		hash, ok := strings.CutSuffix(entry.Name(), ".json.gz")
		if !ok || len(hash) != 2*sha256.Size || !entry.Type().IsRegular() {
			continue
		}
		if _, err := hex.DecodeString(hash); err != nil {
			continue
		}
		if err := visit(root, entry); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
