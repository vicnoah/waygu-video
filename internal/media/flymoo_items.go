package media

import (
	"math"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

func flymooObject(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	if entries, ok := value.([]any); ok && len(entries) > 0 {
		object, _ := entries[0].(map[string]any)
		return object
	}
	return nil
}

func flymooRows(value any, wrappers ...string) []map[string]any {
	switch value := value.(type) {
	case []any:
		rows := make([]map[string]any, 0, len(value))
		for _, entry := range value {
			if object := flymooObject(entry); object != nil {
				rows = append(rows, object)
			}
		}
		return rows
	case map[string]any:
		for _, key := range wrappers {
			if rows := flymooRows(value[key], wrappers...); rows != nil {
				return rows
			}
		}
		if flymooText(value, "guid", "id", "title", "name") != "" {
			return []map[string]any{value}
		}
	}
	return nil
}

func flymooString(value any) string {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	return ""
}

func flymooText(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := flymooString(object[key]); value != "" {
			return value
		}
	}
	return ""
}

func flymooNumber(object map[string]any, keys ...string) float64 {
	number, err := strconv.ParseFloat(flymooText(object, keys...), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0
	}
	return number
}

func flymooInt(object map[string]any, keys ...string) int {
	number, _ := strconv.Atoi(flymooText(object, keys...))
	return number
}

func flymooBool(value any) bool {
	switch value := value.(type) {
	case bool:
		return value
	case float64:
		return value != 0
	case string:
		return value == "1" || strings.EqualFold(value, "true")
	}
	return false
}

func flymooDuration(seconds float64) time.Duration {
	if seconds <= 0 || seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func flymooLibraryID(row map[string]any) (string, bool) {
	keys := []string{"mdb_guid", "mdbGuid", "mdb_id", "mdbId", "mediadb_guid", "mediadbGuid", "mediadb_id", "mediadbId",
		"library_guid", "libraryGuid", "library_id", "libraryId", "ancestor_guid", "ancestorGuid"}
	if id := flymooText(row, keys...); id != "" {
		return id, true
	}
	for _, key := range []string{"mdb", "mediadb", "library", "ancestor"} {
		if id := flymooText(flymooObject(row[key]), "guid", "id"); id != "" {
			return id, true
		}
	}
	for _, key := range keys {
		if _, present := row[key]; present {
			return "", true
		}
	}
	return "", false
}

func flymooKind(row map[string]any, series string, season, episode int) Kind {
	for _, value := range []string{flymooText(row, "type"), flymooText(row, "media_type", "mediaType"),
		flymooText(row, "category"), flymooText(row, "item_type", "itemType")} {
		value = strings.ToLower(value)
		switch {
		case strings.Contains(value, "directory"):
			return Folder
		case strings.Contains(value, "livechannel") || strings.Contains(value, "live channel"):
			return Kind("livechannel")
		case strings.Contains(value, "person") || strings.Contains(value, "actor") || strings.Contains(value, "cast"):
			return Kind("person")
		case strings.Contains(value, "episode"):
			return Episode
		case strings.Contains(value, "season"):
			return Season
		case strings.Contains(value, "tv") || strings.Contains(value, "series") || strings.Contains(value, "show"):
			return Series
		case strings.Contains(value, "movie") || value == "film":
			return Movie
		}
	}
	if flymooText(row, "season_guid", "seasonGuid", "season_id", "seasonId") != "" || (season > 0 && episode == 0) {
		return Season
	}
	if series != "" || episode > 0 || flymooText(row, "tv_title", "series_title", "show_title") != "" {
		return Episode
	}
	return Movie
}

func (f *flymooClient) item(row map[string]any) Item {
	series := flymooText(row, "tv_guid", "tvGuid", "series_guid", "seriesGuid", "tv_id", "tvId", "series_id", "seriesId")
	season := flymooInt(row, "season_number", "seasonNumber")
	episode := flymooInt(row, "episode_number", "episodeNumber")
	year := flymooText(row, "year", "release_date", "air_date", "first_air_date", "last_air_date")
	yearNumber, _ := strconv.Atoi(year[:min(len(year), 4)])
	item := Item{
		ID: flymooText(row, "guid", "id", "media_id", "mediaId"), Kind: flymooKind(row, series, season, episode),
		Title:    flymooText(row, "title", "name", "original_title", "display_name", "file_name"),
		SeriesID: series, SeriesTitle: flymooText(row, "tv_title", "tvTitle", "series_title", "seriesTitle", "tv_name", "show_title", "showTitle"),
		Season: season, Episode: episode, Year: yearNumber,
		OriginalTitle: flymooText(row, "original_title", "originalTitle"),
		Rating:        flymooNumber(row, "rating", "score", "vote_average"),
		Overview:      flymooText(row, "overview", "description", "summary", "plot"),
		Duration:      flymooDuration(flymooNumber(row, "duration")),
		Watched:       flymooBool(row["is_watched"]) || flymooBool(row["watched"]),
		Favorite:      flymooBool(row["favorite"]) || flymooBool(row["is_favorite"]) || flymooBool(row["isFavorite"]),
		ExternalIDs:   make(map[string]string),
	}
	item.LibraryID, _ = flymooLibraryID(row)
	item.Poster = f.artwork(row, "poster", "posters", "poster_list", "poster_url", "posterUrl", "image", "image_url", "cover", "poster_path", "posterPath", "thumb")
	item.Backdrop = f.artwork(row, "backdrops", "backdrop", "backdrop_path", "backdrop_url")
	if genres, ok := row["genres"].([]any); ok {
		for _, value := range genres {
			name := flymooString(value)
			if name == "" {
				name = flymooText(flymooObject(value), "name", "title")
			}
			if name != "" {
				item.Genres = append(item.Genres, name)
			}
		}
	}
	for _, provider := range []string{"imdb", "tmdb", "tvdb"} {
		if id := flymooText(row, provider+"_id", provider+"Id"); id != "" {
			item.ExternalIDs[provider] = id
		}
	}
	return item
}

func (f *flymooClient) artwork(row map[string]any, keys ...string) string {
	for _, key := range keys {
		link := flymooImagePath(row[key])
		if link == "" {
			continue
		}
		parsed, err := url.Parse(link)
		if err != nil || parsed.User != nil {
			continue
		}
		if parsed.Host != "" && parsed.Scheme == "" {
			parsed.Scheme = f.base.Scheme
		}
		if isHTTP(parsed) {
			return f.artworkAddress(parsed.String())
		}
		cleaned := strings.TrimPrefix(path.Clean("/"+parsed.Path), "/")
		switch {
		case strings.HasPrefix(cleaned, "api/"):
		case strings.HasPrefix(cleaned, "sys/img/"):
			cleaned = "api/v1/" + cleaned
		default:
			cleaned = "api/v1/sys/img/" + cleaned
		}
		if resolved, err := f.streamURL("/"+cleaned, parsed.Query()); err == nil {
			return f.artworkAddress(resolved.String())
		}
	}
	return ""
}

func flymooImagePath(value any) string {
	if direct := flymooString(value); direct != "" {
		return direct
	}
	if entries, ok := value.([]any); ok {
		for _, entry := range entries {
			if link := flymooImagePath(entry); link != "" {
				return link
			}
		}
		return ""
	}
	return flymooText(flymooObject(value), "url", "path", "src", "image", "poster_path", "profile_path", "file")
}

func mergeFlymooDetail(item *Item, detail Item) {
	for _, field := range []struct {
		target *string
		value  string
	}{
		{&item.ID, detail.ID}, {&item.Title, detail.Title}, {&item.OriginalTitle, detail.OriginalTitle}, {&item.Overview, detail.Overview},
		{&item.Poster, detail.Poster}, {&item.Backdrop, detail.Backdrop}, {&item.SeriesID, detail.SeriesID},
		{&item.SeriesTitle, detail.SeriesTitle},
	} {
		if field.value != "" {
			*field.target = field.value
		}
	}
	if detail.Kind != Movie || item.Kind == "" {
		item.Kind = detail.Kind
	}
	if detail.Duration > 0 {
		item.Duration = detail.Duration
	}
	if detail.Year > 0 {
		item.Year = detail.Year
	}
	if detail.Rating > 0 {
		item.Rating = detail.Rating
	}
	if len(detail.Genres) > 0 {
		item.Genres = detail.Genres
	}
	if len(detail.ExternalIDs) > 0 {
		item.ExternalIDs = detail.ExternalIDs
	}
	item.Favorite = item.Favorite || detail.Favorite
	item.Watched = item.Watched || detail.Watched
}

func (f *flymooClient) childItems(data any, parent Item) []Item {
	rows := flymooRows(data, "list", "items", "data", "results", "medias", "rows")
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		item := f.item(row)
		if item.ID == "" {
			continue
		}
		if parent.Kind == Series {
			item.Kind, item.SeriesID = Season, parent.ID
			item.Season = flymooInt(row, "season_number", "number", "seasonNumber")
		} else {
			item.Kind, item.SeriesID = Episode, parent.SeriesID
			item.Season = parent.Season
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if parent.Kind == Series {
			return a.Season < b.Season
		}
		if a.Episode != b.Episode {
			return a.Episode < b.Episode
		}
		return a.Title < b.Title
	})
	return items
}
