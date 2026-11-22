package main

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/mozillazg/go-pinyin"
	"github.com/nsgo-dev/nsgo/core"
	"golang.org/x/text/unicode/norm"

	"waygu-video/internal/media"
)

const desktopCollectionsKey = "desktop-smart-collections-v1"

type desktopLibraryFilters struct {
	Genre, Source, Library   string
	Year, Resolution         int
	HDR, Unwatched, Favorite bool
}

func (f desktopLibraryFilters) count() int {
	count := 0
	for _, active := range []bool{f.Genre != "", f.Source != "", f.Library != "", f.Year != 0,
		f.Resolution != 0, f.HDR, f.Unwatched, f.Favorite} {
		if active {
			count++
		}
	}
	return count
}

type desktopLibraryMode string

const (
	desktopGrid desktopLibraryMode = "grid"
	desktopList desktopLibraryMode = "list"
	desktopWall desktopLibraryMode = "wall"
)

const (
	libraryDuration librarySort = libraryRating + 1 + iota
	libraryFileSize
	libraryLastPlayed
)

type desktopLibraryState struct {
	filters    desktopLibraryFilters
	sort       librarySort
	mode       desktopLibraryMode
	collection string
}

type desktopCollection struct {
	ID, Name string
	Category libraryCategory
	Filters  desktopLibraryFilters
	Sort     librarySort
}

func readDesktopCollections(storage core.Storage) []desktopCollection {
	var collections []desktopCollection
	if data, ok := storage.GetItem(desktopCollectionsKey); ok && json.Unmarshal([]byte(data), &collections) == nil {
		return collections
	}
	return nil
}

func (a *application) writeDesktopCollections(collections []desktopCollection) error {
	data, err := json.Marshal(collections)
	if err == nil {
		err = a.storage.SetItem(desktopCollectionsKey, string(data))
	}
	if err == nil {
		a.desktopCollections.Set(collections)
	}
	return err
}

func desktopLibraryMatches(entry catalogItem, filters desktopLibraryFilters) bool {
	item := entry.item
	if filters.Unwatched && catalogWatched(entry) || filters.Year > 0 && item.Year != filters.Year {
		return false
	}
	if filters.Genre != "" && !slices.ContainsFunc(item.Genres, func(genre string) bool {
		return strings.EqualFold(strings.TrimSpace(genre), filters.Genre)
	}) {
		return false
	}
	if filters.Favorite && !item.Favorite && (entry.match == nil || !slices.ContainsFunc(entry.match.copies, func(copy catalogCopy) bool {
		return copy.item.Favorite
	})) {
		return false
	}
	if filters.Resolution > 0 || filters.HDR {
		return slices.ContainsFunc(catalogVideoProfiles(entry), func(profile media.VideoProfile) bool {
			return (filters.Resolution == 0 || libraryResolution(profile) == filters.Resolution) &&
				(!filters.HDR || detailVideoRange(media.Version{VideoRange: profile.Range}) != "")
		})
	}
	return true
}

type desktopLibraryRow struct {
	entry    catalogItem
	titleKey string
	size     int64
	played   time.Time
}

func desktopLibraryEntries(items []catalogItem) []desktopLibraryRow {
	rows := make([]desktopLibraryRow, 0, len(items))
	for _, entry := range items {
		row := desktopLibraryRow{entry: entry, titleKey: desktopTitleKey(entry.item.Title), size: entry.item.FileSize, played: entry.item.LastPlayed}
		if entry.match != nil {
			for _, copy := range entry.match.copies {
				row.size = max(row.size, copy.item.FileSize)
				if copy.item.LastPlayed.After(row.played) {
					row.played = copy.item.LastPlayed
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func desktopLibraryRows(entries []desktopLibraryRow, filters desktopLibraryFilters, sort librarySort) []desktopLibraryRow {
	rows := make([]desktopLibraryRow, 0, len(entries))
	for _, row := range entries {
		if desktopLibraryMatches(row.entry, filters) {
			rows = append(rows, row)
		}
	}
	slices.SortStableFunc(rows, func(a, b desktopLibraryRow) int {
		left, right := a.entry.item, b.entry.item
		var order int
		switch sort {
		case libraryTitle:
			order = strings.Compare(a.titleKey, b.titleKey)
		case libraryYear:
			order = desktopReleaseDate(right).Compare(desktopReleaseDate(left))
		case libraryRating:
			order = cmp.Compare(right.Rating, left.Rating)
		case libraryDuration:
			order = cmp.Compare(right.Duration, left.Duration)
		case libraryFileSize:
			order = cmp.Compare(b.size, a.size)
		case libraryLastPlayed:
			order = b.played.Compare(a.played)
		default:
			order = right.AddedAt.Compare(left.AddedAt)
		}
		if order != 0 {
			return order
		}
		return strings.Compare(a.titleKey, b.titleKey)
	})
	return rows
}

func desktopReleaseDate(item media.Item) time.Time {
	if !item.ReleasedAt.IsZero() || item.Year <= 0 {
		return item.ReleasedAt
	}
	return time.Date(item.Year, time.January, 1, 0, 0, 0, 0, time.UTC)
}

var desktopPinyin = func() pinyin.Args {
	args := pinyin.NewArgs()
	args.Fallback = func(char rune, _ pinyin.Args) []string { return []string{string(char)} }
	return args
}()

func desktopTitleKey(title string) string {
	title = strings.TrimFunc(title, func(char rune) bool { return !unicode.IsLetter(char) && !unicode.IsNumber(char) })
	parts := pinyin.LazyPinyin(title, desktopPinyin)
	var folded strings.Builder
	for _, char := range norm.NFD.String(strings.ToLower(strings.Join(parts, ""))) {
		if !unicode.Is(unicode.Mn, char) {
			folded.WriteRune(char)
		}
	}
	return folded.String()
}

func desktopTitleLetter(key string) string {
	if len(key) > 0 && key[0] >= 'a' && key[0] <= 'z' {
		return strings.ToUpper(key[:1])
	}
	return "#"
}
