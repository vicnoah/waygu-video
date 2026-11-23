package downloads

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"waygu-video/internal/danmaku"
)

const tracksFile = "tracks.json"
const maxTracksBytes = 32 << 20

type Subtitle struct {
	ID, Title, Language, Text string
	Default, Forced, External bool
}

type CommentTrack struct {
	Comments []danmaku.Comment
	Shift    float64
}

// Tracks contains only local payloads and display metadata. Remote locations,
// tokens, and session headers never enter the download directory.
type Tracks struct {
	Subtitles     []Subtitle    `json:",omitempty"`
	Danmaku       *CommentTrack `json:",omitempty"`
	AudioLanguage string        `json:",omitempty"`
}

func (s *Store) writeTracks(dir, id string, videoBytes int64, tracks Tracks, limits Limits) error {
	if len(tracks.Subtitles) == 0 && tracks.Danmaku == nil && tracks.AudioLanguage == "" {
		return nil
	}
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(tracks); err != nil {
		return err
	}
	if data.Len() > maxTracksBytes {
		return ErrInvalidFile
	}
	if err := s.reserveSize(id, videoBytes+int64(data.Len()), limits); err != nil {
		return err
	}
	return writeReceipt(filepath.Join(dir, tracksFile), data.Bytes())
}

func (s *Store) ReadTracks(id string) (Tracks, error) {
	dir, err := s.directory(id)
	if err != nil {
		return Tracks{}, err
	}
	f, err := os.Open(filepath.Join(dir, tracksFile))
	if err != nil {
		return Tracks{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxTracksBytes+1))
	if err != nil {
		return Tracks{}, err
	}
	if len(data) > maxTracksBytes {
		return Tracks{}, ErrInvalidFile
	}
	var tracks Tracks
	if err := json.Unmarshal(data, &tracks); err != nil {
		return Tracks{}, err
	}
	return tracks, nil
}
