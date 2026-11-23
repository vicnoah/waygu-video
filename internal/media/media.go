// Package media connects Waygu Video to media servers.
package media

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"time"

	"waygu-video/internal/mediahttp"
	"waygu-video/internal/mediatls"
)

type Provider string

const (
	Emby     Provider = "emby"
	Jellyfin Provider = "jellyfin"
	Plex     Provider = "plex"
	Flymoo   Provider = "flymoo"
)

var (
	ErrInvalidAddress = errors.New("invalid media server address")
	ErrAuthentication = errors.New("media server authentication failed")
	ErrResponse       = errors.New("invalid media server response")
	ErrNoStream       = errors.New("media server returned no playable stream")
	ErrProvider       = errors.New("unsupported media server type")
)

type Device struct {
	ID   string
	Name string
}

func NewDevice(name string) Device { return Device{ID: rand.Text(), Name: name} }

type Credentials struct {
	Username string
	Password string
	Token    string
}

type Session struct {
	UserID string
	Token  string `json:"-"`
}

type ServerInfo struct {
	ID      string
	Name    string
	Version string
}

type Library struct {
	ID   string
	Name string
	Kind string
}

type Kind string

const (
	Movie   Kind = "movie"
	Series  Kind = "series"
	Season  Kind = "season"
	Episode Kind = "episode"
	Folder  Kind = "folder"
	Video   Kind = "video"
)

type Person struct {
	Name, Role, Portrait string
	Director             bool
	Kind                 string
}

// VideoProfile describes the picture format exposed in catalog metadata.
type VideoProfile struct {
	Width, Height int
	Range         string
}

// TitleRatings holds scores with a known source, each on a scale of ten.
type TitleRatings struct {
	IMDb, TMDB float64
}

type Item struct {
	ID                string
	LibraryID         string
	Kind              Kind
	Title             string
	OriginalTitle     string
	ContentRating     string
	SeriesID          string
	SeriesTitle       string
	Overview          string
	Year              int
	Season            int
	Episode           int
	Rating            float64
	Ratings           TitleRatings `json:",omitzero"`
	Genres            []string
	Countries         []string
	Writers           []string
	VideoProfiles     []VideoProfile
	DolbyAtmos        bool
	SeasonCount       int
	EpisodeCount      int
	UnplayedCount     *int
	People            []Person
	Logo              string
	Trailer           bool
	Poster            string
	Backdrop          string
	ExternalIDs       map[string]string
	SeriesExternalIDs map[string]string
	Duration          time.Duration
	FileSize          int64
	ReleasedAt        time.Time
	Position          time.Duration
	AddedAt           time.Time
	LastPlayed        time.Time
	Watched           bool
	Favorite          bool
}

type Query struct {
	LibraryID    string
	Search       string
	Offset       int
	Limit        int
	CatalogFacts bool
}

func (q Query) bounds() (int, int) {
	if q.Limit <= 0 {
		q.Limit = 48
	}
	return max(0, q.Offset), min(q.Limit, 200)
}

type Page struct {
	Items []Item
	Total int
}

type Track struct {
	ID            string
	Type          string
	Language      string
	Title         string
	Codec         string
	URL           string
	Default       bool
	Forced        bool
	Selected      bool
	External      bool
	FileSubtitle  *FileSubtitle
	SavedSubtitle *string `json:"-"` // Local WebVTT from a completed download.
}

// FileSubtitle locates a text track in the original version, even during
// server remuxing or transcoding. Index is the zero-based stream index.
type FileSubtitle struct {
	Container string
	Index     int
}

// ReadableSubtitle reports whether the app can read the subtitle as timed text.
func (t Track) ReadableSubtitle() bool {
	return t.Type == "subtitle" && (t.URL != "" || t.FileSubtitle != nil || t.SavedSubtitle != nil)
}

type PlayMethod string

const (
	DirectPlay   PlayMethod = "DirectPlay"
	DirectStream PlayMethod = "DirectStream"
	Transcode    PlayMethod = "Transcode"
)

type PlaybackOptions struct {
	SourceID      string
	VersionPolicy VersionPolicy
	MaxBitrate    int
	// MaxHeight requests a server-encoded stream no taller than this many pixels.
	MaxHeight      int
	ForceTranscode bool
	AudioIndex     *int
	SubtitleIndex  *int
	Direct         []DirectProfile
	download       bool
}

type DirectProfile struct {
	Container  string
	VideoCodec string
	AudioCodec string
}

type Stream struct {
	ItemID          string
	SourceID        string
	SessionID       string
	URL             string
	Headers         http.Header
	Method          PlayMethod
	Position        time.Duration
	Duration        time.Duration
	Tracks          []Track
	Rendition       Version
	File            SourceFile
	Trust           mediatls.Trust
	SubtitleHeaders http.Header
	authorize       mediahttp.Authorize
}

// SourceFile is the original file of the selected version, even when the
// player opens a re-encoded or remuxed stream.
type SourceFile struct {
	Name    string
	Size    int64
	URL     string
	Headers http.Header
}

// Resource is the HTTP stream selected for playback.
func (s Stream) Resource() mediahttp.Resource {
	return mediahttp.Resource{URL: s.URL, Headers: s.Headers, Trust: s.Trust, Authorize: s.authorize}
}

// Original is the selected version's original file.
func (s Stream) Original() mediahttp.Resource {
	return mediahttp.Resource{URL: s.File.URL, Headers: s.File.Headers, Trust: s.Trust, Authorize: s.authorize}
}

// VersionID identifies the selection. SourceID remains the server's media ID for progress reports.
func (s Stream) VersionID() string {
	if s.Rendition.ID != "" {
		return s.Rendition.ID
	}
	return s.SourceID
}

type PlaybackEvent string

const (
	Started  PlaybackEvent = "started"
	Progress PlaybackEvent = "progress"
	Stopped  PlaybackEvent = "stopped"
)

type PlaybackState struct {
	Event    PlaybackEvent
	Stream   Stream
	Position time.Duration
	Paused   bool
}

type Client interface {
	Probe(context.Context) (ServerInfo, error)
	SignIn(context.Context, Credentials) (Session, error)
	Libraries(context.Context) ([]Library, error)
	Catalog(context.Context, Query) (Page, error)
	Detail(context.Context, Item) (Item, error)
	Children(context.Context, Item) ([]Item, error)
	Resume(context.Context) ([]Item, error)
	Playback(context.Context, Item, PlaybackOptions) (Stream, error)
	Report(context.Context, PlaybackState) error
}

// LibraryResumeClient supports asking the server for resume items within one library.
type LibraryResumeClient interface {
	ResumeLibrary(context.Context, string) ([]Item, error)
}

func NewClient(kind Provider, address string, device Device, session Session) (Client, error) {
	return NewObservedClient(kind, address, device, session, nil)
}

// NewObservedClient reports timing and status for media server API requests.
func NewObservedClient(kind Provider, address string, device Device, session Session, observe func(RequestMetric)) (Client, error) {
	base, err := NormalizeAddress(address)
	if err != nil {
		return nil, err
	}
	api := newAPI(base)
	api.observe = observe
	switch kind {
	case Emby, Jellyfin:
		return newMediaBrowser(api, kind, device, session), nil
	case Plex:
		return &plexClient{api: api, device: device, session: session}, nil
	case Flymoo:
		return newFlymoo(api, session), nil
	default:
		return nil, ErrProvider
	}
}
