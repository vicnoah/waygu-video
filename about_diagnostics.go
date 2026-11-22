package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

const diagnosticPreferencesKey = "diagnostic-preferences-v1"

type diagnosticPreferences struct {
	Network, Overlay bool
}

func readDiagnosticPreferences(storage core.Storage) diagnosticPreferences {
	var prefs diagnosticPreferences
	if raw, ok := storage.GetItem(diagnosticPreferencesKey); ok {
		_ = json.Unmarshal([]byte(raw), &prefs)
	}
	return prefs
}

func (a *application) saveDiagnosticPreferences(edit func(*diagnosticPreferences)) error {
	prefs := a.diagnosticPrefs.GetUntracked()
	edit(&prefs)
	if err := storeSettings(a.storage, diagnosticPreferencesKey, a.diagnosticPrefs, prefs); err != nil {
		return err
	}
	a.diagnostics.enabled.Store(prefs.Network)
	return nil
}

const networkLogSize = 256 << 10

type networkDiagnostics struct {
	dir     string
	enabled atomic.Bool
	mu      sync.Mutex
	day     string
}

type networkLogEntry struct {
	At         time.Time      `json:"time_utc"`
	Provider   media.Provider `json:"provider"`
	Method     string         `json:"method"`
	Status     int            `json:"http_status"`
	DurationMS int64          `json:"duration_ms"`
	Failed     bool           `json:"failed"`
}

func (e networkLogEntry) valid() bool {
	switch e.Provider {
	case media.Emby, media.Jellyfin, media.Plex, media.Flymoo:
	default:
		return false
	}
	switch e.Method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return (e.Status == 0 || (e.Status >= 100 && e.Status < 600)) && e.DurationMS >= 0 && !e.At.IsZero()
	}
	return false
}

func networkLogName(day time.Time) string {
	return "waygu-network-" + day.UTC().Format(time.DateOnly) + ".jsonl"
}

func (n *networkDiagnostics) record(now time.Time, provider media.Provider, request media.RequestMetric) {
	if !n.enabled.Load() || n.dir == "" {
		return
	}
	entry := networkLogEntry{now.UTC(), provider, request.Method, request.Status, request.Duration.Milliseconds(), request.Failed}
	if !entry.valid() {
		return
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.enabled.Load() {
		return
	}
	if err := os.MkdirAll(n.dir, 0700); err != nil {
		return
	}
	root, err := os.OpenRoot(n.dir)
	if err != nil {
		return
	}
	defer root.Close()
	name := networkLogName(now)
	if n.day != name {
		oldest := networkLogName(now.UTC().AddDate(0, 0, -6))
		entries, _ := fs.ReadDir(root.FS(), ".")
		for _, entry := range entries {
			day, ok := strings.CutPrefix(entry.Name(), "waygu-network-")
			day, okSuffix := strings.CutSuffix(day, ".jsonl")
			if _, err := time.Parse(time.DateOnly, day); ok && okSuffix && err == nil &&
				entry.Name() < oldest && entry.Type().IsRegular() {
				_ = root.Remove(entry.Name())
			}
		}
		n.day = name
	}
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Mode().IsRegular() && info.Size()+int64(len(encoded)+1) <= networkLogSize {
		_, _ = file.Write(append(encoded, '\n'))
	}
}

// archive exports only the app's typed network events for the last seven UTC dates.
func (n *networkDiagnostics) archive(now time.Time) ([]byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var data bytes.Buffer
	bundle := zip.NewWriter(&data)
	added := false
	if n.dir != "" {
		root, err := os.OpenRoot(n.dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if root != nil {
			defer root.Close()
			for i := 6; i >= 0; i-- {
				day := now.UTC().AddDate(0, 0, -i)
				written, err := addNetworkLog(bundle, root, day)
				if err != nil {
					return nil, err
				}
				added = added || written
			}
		}
	}
	if !added {
		if _, err := bundle.Create("network.jsonl"); err != nil {
			return nil, err
		}
	}
	if err := bundle.Close(); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

func addNetworkLog(bundle *zip.Writer, root *os.Root, day time.Time) (bool, error) {
	name := networkLogName(day)
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	file, err := root.Open(name)
	if err != nil {
		return false, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, networkLogSize))
	var entries bytes.Buffer
	encoder := json.NewEncoder(&entries)
	for decoder.More() {
		var entry networkLogEntry
		if err := decoder.Decode(&entry); err != nil {
			break
		}
		if entry.valid() && networkLogName(entry.At) == name {
			if err := encoder.Encode(entry); err != nil {
				return false, err
			}
		}
	}
	if entries.Len() == 0 {
		return false, nil
	}
	writer, err := bundle.Create(name)
	if err != nil {
		return false, err
	}
	_, err = writer.Write(entries.Bytes())
	return err == nil, err
}
