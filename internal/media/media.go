// Package media connects Waygu Video to media servers.
package media

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"time"
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

type Item struct {
	ID          string
	LibraryID   string
	Kind        Kind
	Title       string
	SeriesID    string
	SeriesTitle string
	Overview    string
	Year        int
	Season      int
	Episode     int
	Rating      float64
	Genres      []string
	Poster      string
	Backdrop    string
	ExternalIDs map[string]string
	Duration    time.Duration
	Position    time.Duration
	Watched     bool
	Favorite    bool
}

type Query struct {
	LibraryID string
	Search    string
	Offset    int
	Limit     int
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
	ID       string
	Type     string
	Language string
	Title    string
	Codec    string
	URL      string
	Default  bool
	Forced   bool
}

type PlayMethod string

const (
	DirectPlay   PlayMethod = "DirectPlay"
	DirectStream PlayMethod = "DirectStream"
	Transcode    PlayMethod = "Transcode"
)

type PlaybackOptions struct {
	SourceID      string
	MaxBitrate    int
	AudioIndex    *int
	SubtitleIndex *int
	Direct        []DirectProfile
}

type DirectProfile struct {
	Container  string
	VideoCodec string
	AudioCodec string
}

type Stream struct {
	ItemID    string
	SourceID  string
	SessionID string
	URL       string
	Headers   http.Header
	Method    PlayMethod
	Position  time.Duration
	Duration  time.Duration
	Tracks    []Track
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
	base, err := NormalizeAddress(address)
	if err != nil {
		return nil, err
	}
	api := newAPI(base)
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
