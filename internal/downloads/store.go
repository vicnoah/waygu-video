// Package downloads manages verified copies of offline media files.
package downloads

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var ErrNoSpace = errors.New("not enough space for the download")
var ErrInvalidFile = errors.New("the server did not provide a complete media file")
var ErrBusy = errors.New("the download is busy")

// File describes a completed, verified copy without exposing its server URL.
type File struct {
	Extension string
	Size      int64
	SHA256    string
	MediaID   string
	Height    int `json:",omitempty"`
}

type Limits struct {
	Quota int64
	Free  *int64
}

type Store struct {
	Dir     string
	mu      sync.Mutex
	reserve map[string]int64
	active  map[string]bool
}

func (s *Store) directory(id string) (string, error) {
	if err := validateID(id); err != nil || s.Dir == "" {
		return "", os.ErrInvalid
	}
	return filepath.Join(s.Dir, id), nil
}

func validateID(id string) error {
	if len(id) != sha256.Size*2 {
		return os.ErrInvalid
	}
	_, err := hex.DecodeString(id)
	return err
}

func (s *Store) Open(id string) (string, File, error) {
	dir, err := s.directory(id)
	if err != nil {
		return "", File{}, err
	}
	var file File
	if err := readJSON(filepath.Join(dir, "complete.json"), &file); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", File{}, err
		}
		// A crash between the two final renames can leave a verified
		// receipt here. The final video itself must already exist.
		if err := readJSON(filepath.Join(dir, "complete.pending"), &file); err != nil {
			return "", File{}, err
		}
	}
	if !videoExtensions[file.Extension] || file.Size <= 0 || validateID(file.SHA256) != nil {
		return "", File{}, ErrInvalidFile
	}
	path := filepath.Join(dir, "video"+file.Extension)
	info, err := os.Stat(path)
	if err != nil {
		return "", File{}, err
	}
	if !info.Mode().IsRegular() || info.Size() != file.Size {
		return "", File{}, ErrInvalidFile
	}
	return path, file, nil
}

// Bytes is the payload held on disk for a queued or completed copy.
func (s *Store) Bytes(id string) int64 {
	dir, err := s.directory(id)
	if err != nil {
		return 0
	}
	if _, file, err := s.Open(id); err == nil {
		return file.Size
	}
	return fileSize(filepath.Join(dir, "video.part"))
}

func (s *Store) claim(id string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[id] {
		return nil, ErrBusy
	}
	if s.active == nil {
		s.active = make(map[string]bool)
	}
	s.active[id] = true
	return func() {
		s.mu.Lock()
		delete(s.active, id)
		s.mu.Unlock()
	}, nil
}

func (s *Store) Remove(id string) error {
	dir, err := s.directory(id)
	if err != nil {
		return err
	}
	release, err := s.claim(id)
	if err != nil {
		return err
	}
	defer release()
	return os.RemoveAll(dir)
}

func (s *Store) allocate(id string, total int64, limits Limits) (func(), error) {
	if err := s.reserveSize(id, total, limits); err != nil {
		return nil, err
	}
	return func() {
		s.mu.Lock()
		delete(s.reserve, id)
		s.mu.Unlock()
	}, nil
}

func (s *Store) reserveSize(id string, total int64, limits Limits) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return err
	}
	var totalUsed, remaining uint64
	for _, entry := range entries {
		if !entry.IsDir() || validateID(entry.Name()) != nil {
			continue
		}
		name := entry.Name()
		dir := filepath.Join(s.Dir, name)
		actual := fileSize(filepath.Join(dir, "video.part"))
		if _, receipt, err := s.Open(name); err == nil {
			actual += receipt.Size
		}
		reserved := s.reserve[name]
		if name == id {
			reserved = total
		}
		totalUsed += uint64(max(reserved, actual))
		remaining += uint64(max(0, reserved-actual))
	}
	if limits.Free == nil {
		limits.Free = s.Available()
	}
	if (limits.Quota > 0 && totalUsed > uint64(limits.Quota)) ||
		(limits.Free != nil && remaining > uint64(max(0, *limits.Free))) {
		return ErrNoSpace
	}
	if s.reserve == nil {
		s.reserve = make(map[string]int64)
	}
	s.reserve[id] = total
	return nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err == nil && info.Mode().IsRegular() {
		return info.Size()
	}
	return 0
}

func readJSON(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// Available reports space available to the current user if the platform exposes it.
func (s *Store) Available() *int64 {
	if s.Dir == "" {
		return nil
	}
	if free := availableBytes(s.Dir); free != nil {
		return free
	}
	return availableBytes(filepath.Dir(s.Dir))
}
