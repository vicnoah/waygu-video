package media

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
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
	Hub        []struct {
		Metadata, Directory []plexMetadata
	}
}

type plexMetadata struct {
	RatingKey, Type, Title, Summary, Thumb, Art string
	Reason, ReasonTitle, Subtype                string
	OriginalTitle, ContentRating                string
	OriginallyAvailableAt                       string
	ParentRatingKey, GrandparentRatingKey       string
	GrandparentTitle                            string
	Year, Index, ParentIndex, LibrarySectionID  int
	Rating                                      float64
	Duration, ViewOffset                        int64
	AddedAt                                     int64
	LastViewedAt                                int64
	ViewCount                                   int
	ChildCount, LeafCount, ViewedLeafCount      int
	Genre                                       []struct{ Tag string }
	Director, Writer, Role                      []struct{ Tag, Role, Thumb string }
	Country                                     []struct{ Tag string }
	PrimaryGuid                                 string                `json:"guid"`
	Guid                                        []struct{ ID string } `json:"Guid"`
	Media                                       []plexMedia
	Chapter                                     []plexChapter
	Marker                                      []plexMarker
}

type plexMedia struct {
	ID                                int
	Bitrate                           int
	Width, Height                     int
	Container, VideoCodec, AudioCodec string
	Part                              []plexPart
}

type plexPart struct {
	ID       int
	Key      string
	File     string
	Duration int64
	Size     int64
	Stream   []plexTrack
	Chapter  []plexChapter
}

type plexTrack struct {
	ID           int
	Index        *int
	StreamType   int
	Codec        string
	Language     string
	LanguageCode string
	Title        string
	DisplayTitle string
	ColorTrc     string
	FrameRate    json.Number
	Key          string
	Default      bool
	Forced       bool
	Selected     bool
}

func (p *plexClient) auth() http.Header {
	return plexHeaders(p.device, p.session.Token)
}

func plexHeaders(device Device, token string) http.Header {
	headers := make(http.Header)
	headers.Set("X-Plex-Product", "Waygu Video")
	headers.Set("X-Plex-Version", "0.1.0")
	headers.Set("X-Plex-Client-Identifier", device.ID)
	if token != "" {
		headers.Set("X-Plex-Token", token)
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
	info, err := p.identity(ctx)
	if err != nil {
		return ServerInfo{}, err
	}
	if root, err := p.get(ctx, "/", nil); err == nil && root.Name != "" {
		info.Name = root.Name
	}
	return info, nil
}

func (p *plexClient) identity(ctx context.Context) (ServerInfo, error) {
	info, err := p.get(ctx, "/identity", nil)
	if err != nil {
		return ServerInfo{}, err
	}
	if info.Identifier == "" {
		return ServerInfo{}, ErrResponse
	}
	return ServerInfo{ID: info.Identifier, Name: "Plex", Version: info.Version}, nil
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
	if q.CatalogFacts {
		optional := "Stream"
		if search != "" {
			optional += ",Director,Role"
		}
		params.Set("includeOptionalElements", optional)
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
	container, err := p.get(ctx, "/hubs/home/continueWatching", url.Values{"count": {"24"}, "includeGuids": {"1"}})
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
		Trailer:       strings.EqualFold(entry.Subtype, "trailer") || strings.EqualFold(entry.Type, "trailer"),
		OriginalTitle: entry.OriginalTitle, ContentRating: entry.ContentRating,
		Position: millis(entry.ViewOffset), Watched: entry.ViewCount > 0, SeriesTitle: entry.GrandparentTitle,
		Poster: p.artworkAddress(p.mediaURL(entry.Thumb, nil)), Backdrop: p.artworkAddress(p.mediaURL(entry.Art, nil)),
		ExternalIDs: make(map[string]string, len(entry.Guid))}
	item.ReleasedAt, _ = time.Parse("2006-01-02", entry.OriginallyAvailableAt)
	if entry.LastViewedAt > 0 {
		item.LastPlayed = time.Unix(entry.LastViewedAt, 0).UTC()
	}
	if entry.AddedAt > 0 {
		item.AddedAt = time.Unix(entry.AddedAt, 0).UTC()
	}
	for _, genre := range entry.Genre {
		item.Genres = append(item.Genres, genre.Tag)
	}
	for _, director := range entry.Director {
		if director.Tag != "" {
			item.People = append(item.People, Person{Name: director.Tag, Director: true, Kind: "director", Portrait: p.artworkAddress(p.mediaURL(director.Thumb, nil))})
		}
	}
	for _, country := range entry.Country {
		if country.Tag != "" {
			item.Countries = append(item.Countries, country.Tag)
		}
	}
	for _, writer := range entry.Writer {
		if writer.Tag != "" {
			item.Writers = append(item.Writers, writer.Tag)
		}
	}
	for _, actor := range entry.Role {
		if actor.Tag != "" {
			item.People = append(item.People, Person{Name: actor.Tag, Role: actor.Role, Kind: "actor", Portrait: p.artworkAddress(p.mediaURL(actor.Thumb, nil))})
		}
	}
	if entry.LibrarySectionID > 0 {
		item.LibraryID = strconv.Itoa(entry.LibrarySectionID)
	}
	for _, guid := range entry.Guid {
		if key, id, ok := strings.Cut(guid.ID, "://"); ok {
			item.ExternalIDs[key] = id
		}
	}
	if key, id := plexPrimaryExternalID(entry.PrimaryGuid); key != "" && item.ExternalIDs[key] == "" {
		item.ExternalIDs[key] = id
	}
	switch entry.Type {
	case "show":
		item.Kind = Series
		item.SeasonCount, item.EpisodeCount = entry.ChildCount, entry.LeafCount
		if entry.LeafCount > 0 {
			unplayed := max(0, entry.LeafCount-entry.ViewedLeafCount)
			item.UnplayedCount = &unplayed
			item.Watched = unplayed == 0
		}
	case "season":
		item.Season, item.SeriesID = entry.Index, entry.ParentRatingKey
	case "episode":
		item.Episode, item.Season, item.SeriesID = entry.Index, entry.ParentIndex, entry.GrandparentRatingKey
	}
	for _, medium := range entry.Media {
		size, videoRange, _ := medium.videoFacts()
		item.FileSize = max(item.FileSize, size)
		if medium.Width > 0 || medium.Height > 0 {
			item.addVideoProfile(VideoProfile{Width: medium.Width, Height: medium.Height, Range: videoRange})
		}
	}
	return item
}

func plexPrimaryExternalID(guid string) (string, string) {
	scheme, value, ok := strings.Cut(guid, "://")
	if !ok {
		return "", ""
	}
	id, _, _ := strings.Cut(value, "?")
	switch scheme {
	case "imdb", "com.plexapp.agents.imdb":
		return "imdb", id
	case "tmdb", "com.plexapp.agents.tmdb", "com.plexapp.agents.themoviedb":
		return "tmdb", id
	case "tvdb", "com.plexapp.agents.thetvdb":
		return "tvdb", id
	default:
		return "", ""
	}
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

func (p *plexClient) Versions(ctx context.Context, item Item) ([]Version, error) {
	entry, err := p.metadata(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	var versions []Version
	for _, medium := range entry.Media {
		if medium.ID > 0 && len(medium.Part) > 0 {
			version := medium.version()
			version.Tracks = p.partTracks(medium.Part[0], nil, medium.Container)
			versions = append(versions, version)
		}
	}
	return versions, nil
}

func (m plexMedia) version() Version {
	version := Version{ID: strconv.Itoa(m.ID), Width: m.Width, Height: m.Height, Bitrate: m.Bitrate * 1000,
		VideoCodec: m.VideoCodec, Container: m.Container}
	version.Size, version.VideoRange, version.FrameRate = m.videoFacts()
	if len(m.Part) == 1 {
		version.FileName = videoFileName(m.Part[0].File)
		version.FilePath = localMediaPath(m.Part[0].File)
	}
	return version
}

func (m plexMedia) videoFacts() (size int64, videoRange string, fps float64) {
	for _, part := range m.Part {
		size += max(0, part.Size)
		for _, stream := range part.Stream {
			if stream.StreamType != 1 {
				continue
			}
			if fps == 0 {
				fps, _ = stream.FrameRate.Float64()
			}
			title := strings.ToLower(stream.DisplayTitle)
			switch {
			case strings.Contains(title, "dolby vision") || strings.Contains(title, "dovi"):
				videoRange = "DOVI"
			case videoRange == "" && stream.ColorTrc == "smpte2084":
				videoRange = "HDR10"
			case videoRange == "" && stream.ColorTrc == "arib-std-b67":
				videoRange = "HLG"
			}
		}
	}
	return
}

func (p *plexClient) Playback(ctx context.Context, item Item, options PlaybackOptions) (Stream, error) {
	entry, err := p.metadata(ctx, item.ID)
	if err != nil {
		return Stream{}, err
	}
	versions := make([]Version, len(entry.Media))
	for i, medium := range entry.Media {
		versions[i] = medium.version()
	}
	for _, index := range versionOrder(versions, options) {
		version := entry.Media[index]
		sourceID := strconv.Itoa(version.ID)
		if len(version.Part) == 0 || (options.SourceID != "" && options.SourceID != sourceID) {
			continue
		}
		part := version.Part[0]
		audio := options.AudioIndex
		if audio != nil && (part.ID <= 0 || !slices.ContainsFunc(part.Stream, func(track plexTrack) bool {
			return track.StreamType == 2 && track.ID == *audio
		})) {
			continue
		}
		original := p.mediaURL(part.Key, nil)
		stream := Stream{ItemID: item.ID, SourceID: sourceID, SessionID: rand.Text(), Trust: p.trust, SubtitleHeaders: p.headers,
			Position: millis(entry.ViewOffset), Duration: millis(max(entry.Duration, part.Duration)),
			URL: original, Method: DirectPlay,
			File: SourceFile{Name: videoFileName(part.File), Size: part.Size, URL: original, Headers: p.headers}}
		var transcodeParams url.Values
		switchAudio := audio != nil || plexAlternativeAudio(part.Stream)
		transcode := options.ForceTranscode || options.MaxHeight > 0
		if switchAudio || transcode || !plexDirect(version, options) {
			params := url.Values{"path": {plexItemRoute(item.ID)}, "mediaIndex": {strconv.Itoa(index)}, "partIndex": {"0"},
				"protocol": {"hls"}, "directPlay": {"0"}, "directStream": {"1"}, "session": {stream.SessionID},
				"X-Plex-Client-Identifier": {p.device.ID}, "X-Plex-Session-Identifier": {stream.SessionID},
				"X-Plex-Product": {"Waygu Video"}, "X-Plex-Platform": {"Chrome"}}
			if options.SubtitleIndex != nil && *options.SubtitleIndex < 0 {
				params.Set("subtitles", "none")
			}
			if options.MaxBitrate > 0 || switchAudio || transcode {
				params.Set("directStream", "0")
			}
			if options.MaxBitrate > 0 {
				params.Set("maxVideoBitrate", strconv.Itoa(options.MaxBitrate/1000))
			}
			if height := options.MaxHeight; height > 0 {
				width := (height*16/9 + 1) &^ 1
				params.Set("videoResolution", fmt.Sprintf("%dx%d", width, height))
			}
			if options.download {
				params.Set("hasMDE", "1")
				params.Set("X-Plex-Client-Profile-Name", "Generic")
				params.Set("X-Plex-Client-Profile-Extra", "add-transcode-target(type=videoProfile&context=streaming&protocol=hls&container=mp4&videoCodec=h264&audioCodec=aac&replace=true)")
			}
			stream.URL, stream.Method = p.mediaURL("/video/:/transcode/universal/start.m3u8", params), Transcode
			stream.Headers = p.auth()
			transcodeParams = params
		}
		if stream.URL == "" {
			continue
		}
		if audio != nil {
			// Plex stores the user's selected track on the part. Its
			// transcoder uses that selection for the next playback session.
			params := url.Values{"audioStreamID": {strconv.Itoa(*audio)}}
			if err := p.request(ctx, http.MethodPut, "/library/parts/"+strconv.Itoa(part.ID), params, p.auth(), nil, nil); err != nil {
				return Stream{}, err
			}
		}
		if transcodeParams != nil {
			// Plex's transcoder can reject a playlist until this session has a
			// decision. Request it after applying the selected audio stream.
			err := p.request(ctx, http.MethodGet, "/video/:/transcode/universal/decision", transcodeParams, p.auth(), nil, nil)
			if err != nil && !errors.Is(err, StatusError(http.StatusNotFound)) && !errors.Is(err, StatusError(http.StatusMethodNotAllowed)) {
				return Stream{}, err
			}
		}
		stream.Headers = p.withEndpointHeaders(stream.Headers)
		stream.Tracks = p.partTracks(part, audio, version.Container)
		if stream.Method == DirectPlay {
			stream.Rendition = version.version()
		}
		return stream, nil
	}
	return Stream{}, ErrNoStream
}

func (p *plexClient) partTracks(part plexPart, audio *int, container string) []Track {
	var tracks []Track
	for _, track := range part.Stream {
		kind := "audio"
		switch track.StreamType {
		case 2:
		case 3:
			kind = "subtitle"
		default:
			continue
		}
		title, language := track.Title, track.LanguageCode
		if title == "" {
			title = track.DisplayTitle
		}
		if language == "" {
			language = track.Language
		}
		current := Track{ID: strconv.Itoa(track.ID), Type: kind, Title: title,
			Language: language, Codec: track.Codec, Default: track.Default, Forced: track.Forced, Selected: track.Selected}
		if kind == "audio" && audio != nil {
			current.Selected = track.ID == *audio
		}
		if kind == "subtitle" && track.Key != "" {
			current.External = true
			switch strings.ToLower(track.Codec) {
			case "srt", "subrip", "vtt", "webvtt":
				current.URL = p.mediaURL(track.Key, nil)
			}
		}
		if kind == "subtitle" && track.Key == "" && track.Index != nil && *track.Index >= 0 && part.Size > 0 && part.Key != "" {
			switch {
			case includesCodec("mp4,m4v,mov", container) && includesCodec("mov_text,tx3g,webvtt,wvtt,vtt", track.Codec):
				current.FileSubtitle = &FileSubtitle{Container: "mp4", Index: *track.Index}
			case includesCodec("mkv,matroska,webm", container) && includesCodec("srt,subrip,utf8,webvtt,vtt", track.Codec):
				current.FileSubtitle = &FileSubtitle{Container: "matroska", Index: *track.Index}
			}
		}
		tracks = append(tracks, current)
	}
	return tracks
}

func plexAlternativeAudio(tracks []plexTrack) bool {
	index := 0
	for _, track := range tracks {
		if track.StreamType != 2 {
			continue
		}
		if track.Selected {
			return index > 0
		}
		index++
	}
	return false
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
	return p.reportTimeline(ctx, params, headers)
}

func (p *plexClient) reportTimeline(ctx context.Context, params url.Values, headers http.Header) error {
	err := p.request(ctx, http.MethodPost, "/:/timeline", params, headers, nil, nil)
	if errors.Is(err, StatusError(http.StatusMethodNotAllowed)) {
		return p.request(ctx, http.MethodGet, "/:/timeline", params, headers, nil, nil)
	}
	return err
}
