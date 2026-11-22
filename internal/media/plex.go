package media

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type plexClient struct {
	api
	device  Device
	session Session
}

type plexEnvelope struct {
	Container *plexContainer `json:"MediaContainer"`
}

type plexContainer struct {
	Name       string `json:"friendlyName"`
	Identifier string `json:"machineIdentifier"`
	Version    string
	Size       int
	TotalSize  int
	Directory  []struct{ Key, Title, Type string }
	Metadata   []plexMetadata
	Hub        []struct{ Metadata []plexMetadata }
}

type plexMetadata struct {
	RatingKey, Type, Title, Summary, Thumb, Art string
	ParentRatingKey, GrandparentRatingKey       string
	GrandparentTitle                            string
	Year, Index, ParentIndex, LibrarySectionID  int
	Rating                                      float64
	Duration, ViewOffset                        int64
	ViewCount                                   int
	Genre                                       []struct{ Tag string }
	PrimaryGuid                                 string                `json:"guid"`
	Guid                                        []struct{ ID string } `json:"Guid"`
	Media                                       []plexMedia
}

type plexMedia struct {
	ID                                int
	Bitrate                           int
	Container, VideoCodec, AudioCodec string
	Part                              []plexPart
}

type plexPart struct {
	Key      string
	Duration int64
	Stream   []plexTrack
}

type plexTrack struct {
	ID         int
	StreamType int
	Codec      string
	Language   string
	Title      string
	Key        string
	Default    bool
	Forced     bool
}

func (p *plexClient) auth() http.Header {
	headers := make(http.Header)
	headers.Set("X-Plex-Product", "Waygu Video")
	headers.Set("X-Plex-Version", "0.1.0")
	headers.Set("X-Plex-Client-Identifier", p.device.ID)
	if p.session.Token != "" {
		headers.Set("X-Plex-Token", p.session.Token)
	}
	return headers
}

func (p *plexClient) get(ctx context.Context, route string, params url.Values) (*plexContainer, error) {
	var envelope plexEnvelope
	if err := p.request(ctx, http.MethodGet, route, params, p.auth(), nil, &envelope); err != nil {
		return nil, err
	}
	if envelope.Container == nil {
		return nil, ErrResponse
	}
	return envelope.Container, nil
}

func (p *plexClient) Probe(ctx context.Context) (ServerInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	info, err := p.get(ctx, "/identity", nil)
	if err != nil {
		return ServerInfo{}, err
	}
	if info.Identifier == "" {
		return ServerInfo{}, ErrResponse
	}
	name := "Plex"
	if root, err := p.get(ctx, "/", nil); err == nil && root.Name != "" {
		name = root.Name
	}
	return ServerInfo{ID: info.Identifier, Name: name, Version: info.Version}, nil
}

func (p *plexClient) SignIn(ctx context.Context, input Credentials) (Session, error) {
	session := Session{Token: strings.TrimSpace(input.Token)}
	client := *p
	client.session = session
	if _, err := client.Libraries(ctx); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (p *plexClient) Libraries(ctx context.Context) ([]Library, error) {
	container, err := p.get(ctx, "/library/sections", nil)
	if err != nil {
		return nil, err
	}
	libraries := make([]Library, 0, len(container.Directory))
	for _, entry := range container.Directory {
		if entry.Key != "" && (entry.Type == "movie" || entry.Type == "show") {
			libraries = append(libraries, Library{ID: entry.Key, Name: entry.Title, Kind: entry.Type})
		}
	}
	return libraries, nil
}

func (p *plexClient) Catalog(ctx context.Context, q Query) (Page, error) {
	offset, limit := q.bounds()
	search := strings.TrimSpace(q.Search)
	if search != "" && q.LibraryID == "" {
		return p.search(ctx, search, offset, limit)
	}
	params := url.Values{"X-Plex-Container-Start": {strconv.Itoa(offset)}, "X-Plex-Container-Size": {strconv.Itoa(limit)},
		"includeGuids": {"1"}, "sort": {"addedAt:desc"}}
	route := "/library/all"
	if q.LibraryID != "" {
		route = "/library/sections/" + url.PathEscape(q.LibraryID) + "/all"
	} else {
		params.Set("type", "1,2")
	}
	if search != "" {
		params.Set("title", search)
	}
	container, err := p.get(ctx, route, params)
	if err != nil {
		return Page{}, err
	}
	return Page{Items: p.items(container.Metadata), Total: max(container.TotalSize, container.Size)}, nil
}

func (p *plexClient) search(ctx context.Context, term string, offset, limit int) (Page, error) {
	container, err := p.get(ctx, "/hubs/search", url.Values{"query": {term}, "limit": {strconv.Itoa(min(offset+limit, 200))}})
	if err != nil {
		return Page{}, err
	}
	seen := make(map[string]bool)
	var matches []plexMetadata
	for _, hub := range container.Hub {
		for _, item := range hub.Metadata {
			if item.RatingKey != "" && !seen[item.RatingKey] && (item.Type == "movie" || item.Type == "show") {
				matches = append(matches, item)
				seen[item.RatingKey] = true
			}
		}
	}
	start := min(offset, len(matches))
	return Page{Items: p.items(matches[start:min(start+limit, len(matches))]), Total: len(matches)}, nil
}

func plexItemRoute(id string) string { return "/library/metadata/" + url.PathEscape(id) }

func (p *plexClient) metadata(ctx context.Context, id string) (plexMetadata, error) {
	container, err := p.get(ctx, plexItemRoute(id), url.Values{"includeGuids": {"1"}})
	if err != nil {
		return plexMetadata{}, err
	}
	if len(container.Metadata) == 0 || container.Metadata[0].RatingKey == "" {
		return plexMetadata{}, ErrResponse
	}
	return container.Metadata[0], nil
}

func (p *plexClient) Detail(ctx context.Context, item Item) (Item, error) {
	entry, err := p.metadata(ctx, item.ID)
	if err != nil {
		return Item{}, err
	}
	return p.item(entry), nil
}

func (p *plexClient) Children(ctx context.Context, parent Item) ([]Item, error) {
	container, err := p.get(ctx, plexItemRoute(parent.ID)+"/children", url.Values{"includeGuids": {"1"}})
	if err != nil {
		return nil, err
	}
	return p.items(container.Metadata), nil
}

func (p *plexClient) Resume(ctx context.Context) ([]Item, error) {
	container, err := p.get(ctx, "/hubs/home/continueWatching", url.Values{"count": {"24"}})
	if err != nil {
		return nil, err
	}
	return p.items(container.Metadata), nil
}

func (p *plexClient) items(entries []plexMetadata) []Item {
	items := make([]Item, 0, len(entries))
	for _, entry := range entries {
		if entry.RatingKey != "" {
			items = append(items, p.item(entry))
		}
	}
	return items
}

func (p *plexClient) item(entry plexMetadata) Item {
	item := Item{ID: entry.RatingKey, Kind: Kind(entry.Type), Title: entry.Title, Year: entry.Year,
		Overview: entry.Summary, Rating: entry.Rating, Duration: millis(entry.Duration),
		Position: millis(entry.ViewOffset), Watched: entry.ViewCount > 0, SeriesTitle: entry.GrandparentTitle,
		Poster: p.mediaURL(entry.Thumb, nil), Backdrop: p.mediaURL(entry.Art, nil),
		ExternalIDs: make(map[string]string, len(entry.Guid))}
	for _, genre := range entry.Genre {
		item.Genres = append(item.Genres, genre.Tag)
	}
	for _, guid := range entry.Guid {
		if key, id, ok := strings.Cut(guid.ID, "://"); ok {
			item.ExternalIDs[key] = id
		}
	}
	if key, id, ok := strings.Cut(entry.PrimaryGuid, "://"); ok && (key == "tmdb" || key == "imdb" || key == "tvdb") {
		item.ExternalIDs[key] = id
	}
	switch entry.Type {
	case "show":
		item.Kind = Series
	case "season":
		item.Season, item.SeriesID = entry.Index, entry.ParentRatingKey
	case "episode":
		item.Episode, item.Season, item.SeriesID = entry.Index, entry.ParentIndex, entry.GrandparentRatingKey
	}
	return item
}

func millis(n int64) time.Duration {
	return time.Duration(max(0, n)) * time.Millisecond
}

func (p *plexClient) mediaURL(route string, params url.Values) string {
	if route == "" {
		return ""
	}
	link, err := p.streamURL(route, params)
	if err != nil {
		return ""
	}
	query := link.Query()
	if p.session.Token != "" {
		query.Set("X-Plex-Token", p.session.Token)
	}
	link.RawQuery = query.Encode()
	return link.String()
}

func (p *plexClient) Playback(ctx context.Context, item Item, options PlaybackOptions) (Stream, error) {
	entry, err := p.metadata(ctx, item.ID)
	if err != nil {
		return Stream{}, err
	}
	for index, version := range entry.Media {
		sourceID := strconv.Itoa(version.ID)
		if len(version.Part) == 0 || (options.SourceID != "" && options.SourceID != sourceID) {
			continue
		}
		part := version.Part[0]
		stream := Stream{ItemID: item.ID, SourceID: sourceID, SessionID: rand.Text(),
			Position: millis(entry.ViewOffset), Duration: millis(max(entry.Duration, part.Duration)),
			URL: p.mediaURL(part.Key, nil), Method: DirectPlay}
		if !plexDirect(version, options) {
			params := url.Values{"path": {plexItemRoute(item.ID)}, "mediaIndex": {strconv.Itoa(index)}, "partIndex": {"0"},
				"protocol": {"hls"}, "directPlay": {"0"}, "directStream": {"1"}, "session": {stream.SessionID},
				"X-Plex-Client-Identifier": {p.device.ID}, "X-Plex-Session-Identifier": {stream.SessionID},
				"X-Plex-Product": {"Waygu Video"}, "X-Plex-Platform": {"Chrome"}}
			if options.MaxBitrate > 0 {
				params.Set("maxVideoBitrate", strconv.Itoa(options.MaxBitrate/1000))
			}
			stream.URL, stream.Method = p.mediaURL("/video/:/transcode/universal/start.m3u8", params), Transcode
		}
		if stream.URL == "" {
			continue
		}
		for _, track := range part.Stream {
			kind := "audio"
			switch track.StreamType {
			case 2:
			case 3:
				kind = "subtitle"
			default:
				continue
			}
			stream.Tracks = append(stream.Tracks, Track{ID: strconv.Itoa(track.ID), Type: kind, Title: track.Title,
				Language: track.Language, Codec: track.Codec, Default: track.Default, Forced: track.Forced,
				URL: p.mediaURL(track.Key, nil)})
		}
		return stream, nil
	}
	return Stream{}, ErrNoStream
}

func plexDirect(version plexMedia, options PlaybackOptions) bool {
	if options.MaxBitrate > 0 && version.Bitrate > options.MaxBitrate/1000 {
		return false
	}
	if len(options.Direct) == 0 {
		return version.Container == "mp4" && version.VideoCodec == "h264" && version.AudioCodec == "aac"
	}
	for _, profile := range options.Direct {
		if includesCodec(profile.Container, version.Container) && includesCodec(profile.VideoCodec, version.VideoCodec) &&
			includesCodec(profile.AudioCodec, version.AudioCodec) {
			return true
		}
	}
	return false
}

func includesCodec(csv, value string) bool {
	for part := range strings.SplitSeq(csv, ",") {
		if strings.EqualFold(strings.TrimSpace(part), value) {
			return true
		}
	}
	return csv == ""
}

func (p *plexClient) Report(ctx context.Context, event PlaybackState) error {
	state := "playing"
	switch event.Event {
	case Stopped:
		state = "stopped"
	case Started, Progress:
		if event.Paused {
			state = "paused"
		}
	default:
		return fmt.Errorf("unknown playback event %q", event.Event)
	}
	params := url.Values{"ratingKey": {event.Stream.ItemID}, "key": {plexItemRoute(event.Stream.ItemID)},
		"time": {strconv.FormatInt(event.Position.Milliseconds(), 10)}, "state": {state},
		"duration": {strconv.FormatInt(event.Stream.Duration.Milliseconds(), 10)}}
	headers := p.auth()
	headers.Set("X-Plex-Session-Identifier", event.Stream.SessionID)
	err := p.request(ctx, http.MethodPost, "/:/timeline", params, headers, nil, nil)
	if errors.Is(err, StatusError(http.StatusMethodNotAllowed)) {
		return p.request(ctx, http.MethodGet, "/:/timeline", params, headers, nil, nil)
	}
	return err
}
