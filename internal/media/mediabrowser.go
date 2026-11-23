package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const browserItemFields = "Overview,Genres,ProviderIds,PrimaryImageAspectRatio,DateCreated"

type mediaBrowser struct {
	api
	kind    Provider
	device  Device
	session Session
}

func newMediaBrowser(a api, kind Provider, device Device, session Session) *mediaBrowser {
	return &mediaBrowser{api: a, kind: kind, device: device, session: session}
}

func (m *mediaBrowser) auth() http.Header {
	field := func(key, value string) string {
		return key + `="` + strings.ReplaceAll(url.QueryEscape(value), "+", "%20") + `"`
	}
	name := m.device.Name
	if name == "" {
		name = "Waygu Video"
	}
	value := "MediaBrowser " + strings.Join([]string{
		field("Client", "Waygu Video"), field("Device", name),
		field("DeviceId", m.device.ID), field("Version", "0.1.0"),
	}, ", ")
	h := make(http.Header)
	if m.session.Token != "" {
		value += ", " + field("Token", m.session.Token)
		if m.kind == Emby {
			h.Set("X-Emby-Token", m.session.Token)
		}
	}
	if m.kind == Jellyfin {
		h.Set("Authorization", value)
	} else {
		h.Set("X-Emby-Authorization", value)
	}
	return h
}

func (m *mediaBrowser) get(ctx context.Context, route string, query url.Values, result any) error {
	return m.request(ctx, http.MethodGet, route, query, m.auth(), nil, result)
}

func (m *mediaBrowser) post(ctx context.Context, route string, query url.Values, body, result any) error {
	return m.request(ctx, http.MethodPost, route, query, m.auth(), body, result)
}

func (m *mediaBrowser) Probe(ctx context.Context) (ServerInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var dto struct{ ID, ServerName, Version string }
	if err := m.get(ctx, "System/Info/Public", nil, &dto); err != nil {
		return ServerInfo{}, err
	}
	if dto.ID == "" || dto.ServerName == "" {
		return ServerInfo{}, ErrResponse
	}
	return ServerInfo{ID: dto.ID, Name: dto.ServerName, Version: dto.Version}, nil
}

type browserLogin struct {
	AccessToken string
	User        struct{ ID string }
}

func (m *mediaBrowser) SignIn(ctx context.Context, credentials Credentials) (Session, error) {
	if m.device.ID == "" || strings.TrimSpace(credentials.Username) == "" {
		return Session{}, ErrAuthentication
	}
	var dto browserLogin
	body := struct{ Username, Pw string }{strings.TrimSpace(credentials.Username), credentials.Password}
	if err := m.post(ctx, "Users/AuthenticateByName", nil, body, &dto); err != nil {
		return Session{}, err
	}
	return dto.session()
}

func (l browserLogin) session() (Session, error) {
	if l.User.ID == "" || l.AccessToken == "" {
		return Session{}, ErrResponse
	}
	return Session{UserID: l.User.ID, Token: l.AccessToken}, nil
}

func (m *mediaBrowser) Libraries(ctx context.Context) ([]Library, error) {
	var dto browserList
	var err error
	if m.kind == Jellyfin {
		err = m.get(ctx, "UserViews", m.userQuery(), &dto)
	} else {
		err = m.get(ctx, m.userRoute()+"/Views", nil, &dto)
	}
	if errors.Is(err, StatusError(http.StatusNotFound)) && m.kind == Jellyfin {
		err = m.get(ctx, m.userRoute()+"/Views", nil, &dto)
	}
	if err != nil {
		return nil, err
	}
	libraries := make([]Library, 0, len(dto.Items))
	for _, entry := range dto.Items {
		if entry.ID != "" {
			libraries = append(libraries, Library{ID: entry.ID, Name: entry.Name, Kind: entry.CollectionType})
		}
	}
	return libraries, nil
}

func (m *mediaBrowser) userRoute() string { return "Users/" + url.PathEscape(m.session.UserID) }

func (m *mediaBrowser) userQuery() url.Values {
	return url.Values{"UserId": {m.session.UserID}}
}

type browserList struct {
	Items            []browserItem
	TotalRecordCount int
}

type browserItem struct {
	ID                             string
	Name                           string
	ExtraType                      string
	OriginalTitle                  string
	OfficialRating                 string
	Overview                       string
	Type                           string
	CollectionType                 string
	SeriesID                       string
	SeriesName                     string
	ParentIndexNumber              int
	IndexNumber                    int
	ProductionYear                 int
	CommunityRating                float64
	ChildCount, RecursiveItemCount int
	MediaStreams                   []browserStream
	MediaSources                   []struct {
		Size         int64
		MediaStreams []browserStream
	}
	RunTimeTicks        int64
	DateCreated         string
	PremiereDate        string
	Genres              []string
	ProductionLocations []string
	ProviderIds         map[string]string
	ImageTags           map[string]string
	BackdropImageTags   []string
	People              []struct {
		ID, Name, Role, Type, PrimaryImageTag string
	}
	UserData struct {
		PlaybackPositionTicks int64
		LastPlayedDate        string
		Played                bool
		IsFavorite            bool
		UnplayedItemCount     *int
	}
}

func (m *mediaBrowser) itemFields() string {
	if m.kind == Emby {
		return browserItemFields + ",OfficialRating,UserDataLastPlayedDate"
	}
	return browserItemFields
}

func (m *mediaBrowser) Catalog(ctx context.Context, q Query) (Page, error) {
	return m.catalog(ctx, q, nil)
}

func (m *mediaBrowser) catalog(ctx context.Context, q Query, people []string) (Page, error) {
	offset, limit := q.bounds()
	params := m.userQuery()
	fields := m.itemFields()
	if q.CatalogFacts {
		fields += ",MediaStreams,MediaSources,ChildCount,RecursiveItemCount,PremiereDate"
		if q.Search != "" || len(people) > 0 {
			fields += ",People"
		}
		if m.kind == Emby {
			fields += ",ProductionYear,CommunityRating"
		}
	}
	params.Set("Fields", fields)
	params.Set("IncludeItemTypes", "Movie,Series,Video")
	params.Set("Recursive", "true")
	params.Set("SortBy", "DateCreated,SortName")
	params.Set("SortOrder", "Descending")
	params.Set("StartIndex", strconv.Itoa(offset))
	params.Set("Limit", strconv.Itoa(limit))
	if q.LibraryID != "" {
		params.Set("ParentId", q.LibraryID)
	}
	if len(people) > 0 {
		params.Set("PersonIds", strings.Join(people, ","))
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		params.Set("SearchTerm", search)
		params.Del("SortBy")
		params.Del("SortOrder")
	}
	var dto browserList
	if err := m.get(ctx, "Items", params, &dto); err != nil {
		return Page{}, err
	}
	return Page{Items: m.items(dto.Items), Total: dto.TotalRecordCount}, nil
}

func (m *mediaBrowser) Detail(ctx context.Context, item Item) (Item, error) {
	route := "Items/" + url.PathEscape(item.ID)
	if m.kind == Emby {
		route = m.userRoute() + "/" + route
	}
	var dto browserItem
	params := m.userQuery()
	params.Set("Fields", m.itemFields()+",OriginalTitle,People,ProductionLocations")
	err := m.get(ctx, route, params, &dto)
	if errors.Is(err, StatusError(http.StatusNotFound)) && m.kind == Jellyfin {
		err = m.get(ctx, m.userRoute()+"/"+route, params, &dto)
	}
	if err != nil {
		return Item{}, err
	}
	if dto.ID == "" {
		return Item{}, ErrResponse
	}
	return m.item(dto), nil
}

func (m *mediaBrowser) Children(ctx context.Context, parent Item) ([]Item, error) {
	params := m.userQuery()
	params.Set("Fields", m.itemFields())
	route := "Items"
	switch parent.Kind {
	case Series:
		route = "Shows/" + url.PathEscape(parent.ID) + "/Seasons"
	case Season:
		if parent.SeriesID != "" {
			route = "Shows/" + url.PathEscape(parent.SeriesID) + "/Episodes"
			params.Set("SeasonId", parent.ID)
			break
		}
		params.Set("ParentId", parent.ID)
		params.Set("IncludeItemTypes", "Episode")
	default:
		params.Set("ParentId", parent.ID)
	}
	var dto browserList
	if err := m.get(ctx, route, params, &dto); err != nil {
		return nil, err
	}
	return m.items(dto.Items), nil
}

func (m *mediaBrowser) Resume(ctx context.Context) ([]Item, error) {
	return m.ResumeLibrary(ctx, "")
}

func (m *mediaBrowser) ResumeLibrary(ctx context.Context, libraryID string) ([]Item, error) {
	params := m.userQuery()
	params.Set("Fields", m.itemFields())
	params.Set("Limit", "24")
	params.Set("MediaTypes", "Video")
	if libraryID != "" {
		params.Set("ParentId", libraryID)
	}
	route := "UserItems/Resume"
	if m.kind == Emby {
		route = m.userRoute() + "/Items/Resume"
	}
	var dto browserList
	if err := m.get(ctx, route, params, &dto); err != nil {
		return nil, err
	}
	return m.items(dto.Items), nil
}

func (m *mediaBrowser) items(entries []browserItem) []Item {
	items := make([]Item, 0, len(entries))
	for _, entry := range entries {
		if entry.ID != "" {
			items = append(items, m.item(entry))
		}
	}
	return items
}

func (m *mediaBrowser) item(entry browserItem) Item {
	lastPlayed, _ := time.Parse(time.RFC3339Nano, entry.UserData.LastPlayedDate)
	addedAt, _ := time.Parse(time.RFC3339Nano, entry.DateCreated)
	releasedAt, _ := time.Parse(time.RFC3339Nano, entry.PremiereDate)
	item := Item{
		ID: entry.ID, Kind: Kind(strings.ToLower(entry.Type)), Title: entry.Name,
		SeriesID: entry.SeriesID, SeriesTitle: entry.SeriesName, Overview: entry.Overview,
		OriginalTitle: entry.OriginalTitle, ContentRating: entry.OfficialRating,
		Year: entry.ProductionYear, Rating: entry.CommunityRating, Genres: entry.Genres, Countries: entry.ProductionLocations,
		Duration: fromTicks(entry.RunTimeTicks), Position: fromTicks(entry.UserData.PlaybackPositionTicks),
		AddedAt: addedAt, LastPlayed: lastPlayed, ReleasedAt: releasedAt,
		Watched: entry.UserData.Played, Favorite: entry.UserData.IsFavorite,
		ExternalIDs: make(map[string]string, len(entry.ProviderIds)),
	}
	for _, person := range entry.People {
		kind := strings.ToLower(person.Type)
		if person.Name == "" || (kind != "director" && kind != "actor" && kind != "composer") {
			continue
		}
		credit := Person{Name: person.Name, Role: person.Role, Kind: kind, Director: kind == "director"}
		if person.ID != "" && person.PrimaryImageTag != "" {
			credit.Portrait = m.image(person.ID, "Primary", person.PrimaryImageTag)
		}
		item.People = append(item.People, credit)
	}
	for key, id := range entry.ProviderIds {
		item.ExternalIDs[strings.ToLower(key)] = id
	}
	switch item.Kind {
	case Series:
		item.SeasonCount, item.EpisodeCount, item.UnplayedCount = entry.ChildCount, entry.RecursiveItemCount, entry.UserData.UnplayedItemCount
	case Season:
		item.Season = entry.IndexNumber
	case Episode:
		item.Season, item.Episode = entry.ParentIndexNumber, entry.IndexNumber
	}
	addStreams := func(streams []browserStream) {
		for _, stream := range streams {
			if strings.EqualFold(stream.Type, "video") && (stream.Width > 0 || stream.Height > 0) {
				item.addVideoProfile(VideoProfile{Width: stream.Width, Height: stream.Height, Range: stream.videoRange().name})
			}
			if !item.DolbyAtmos && strings.EqualFold(stream.Type, "audio") {
				item.DolbyAtmos = atmosMetadata(stream.Codec, stream.Title, stream.DisplayTitle, stream.Profile)
			}
		}
	}
	addStreams(entry.MediaStreams)
	for _, source := range entry.MediaSources {
		item.FileSize = max(item.FileSize, source.Size)
		addStreams(source.MediaStreams)
	}
	if tag := entry.ImageTags["Logo"]; tag != "" {
		item.Logo = m.image(entry.ID, "Logo", tag)
	}
	item.Trailer = strings.EqualFold(entry.ExtraType, "Trailer") || strings.EqualFold(entry.Type, "Trailer")
	if tag := entry.ImageTags["Primary"]; tag != "" {
		item.Poster = m.image(entry.ID, "Primary", tag)
	}
	if len(entry.BackdropImageTags) > 0 {
		item.Backdrop = m.image(entry.ID, "Backdrop/0", entry.BackdropImageTags[0])
	}
	return item
}

func (m *mediaBrowser) image(id, kind, tag string) string {
	endpoint, err := m.resolve("Items/"+url.PathEscape(id)+"/Images/"+kind,
		url.Values{"tag": {tag}, "maxWidth": {"1200"}, m.tokenKey(): {m.session.Token}})
	if err != nil {
		return ""
	}
	return m.artworkAddress(endpoint.String())
}

func (m *mediaBrowser) tokenKey() string {
	if m.kind == Emby {
		return "api_key"
	}
	return "ApiKey"
}

func fromTicks(ticks int64) time.Duration {
	if ticks < 0 || ticks > (1<<63-1)/int64(100*time.Nanosecond) {
		return 0
	}
	return time.Duration(ticks) * 100 * time.Nanosecond
}

func toTicks(duration time.Duration) int64 { return int64(max(0, duration) / (100 * time.Nanosecond)) }

func (m *mediaBrowser) Report(ctx context.Context, event PlaybackState) error {
	const playing = "Sessions/Playing"
	var route string
	switch event.Event {
	case Started:
		route = playing
	case Progress:
		route = playing + "/Progress"
	case Stopped:
		route = playing + "/Stopped"
	default:
		return fmt.Errorf("unknown playback event %q", event.Event)
	}
	body := map[string]any{
		"ItemId": event.Stream.ItemID, "MediaSourceId": event.Stream.SourceID,
		"PlaySessionId": event.Stream.SessionID, "PlayMethod": event.Stream.Method,
		"PositionTicks": toTicks(event.Position), "IsPaused": event.Paused, "CanSeek": true,
	}
	return m.post(ctx, route, nil, body, nil)
}
