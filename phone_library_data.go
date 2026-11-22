package main

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type phoneLibraryState struct {
	category         libraryCategory
	library          string
	genre            string
	year, resolution int
	sort             librarySort
	unwatched, list  bool
}

type librarySort uint8

const (
	libraryRecent librarySort = iota
	libraryTitle
	libraryYear
	libraryRating
)

var phoneLibraryCategories = []libraryCategory{libraryMovies, librarySeries, libraryAnime, libraryDocumentaries}

type phoneLibraryPage struct {
	query  *catalogRequest
	result catalogResult
}

func defaultPhoneLibrary(sources []source, filter string) libraryCategory {
	for _, category := range phoneLibraryCategories {
		for _, source := range sources {
			if filter != "" && source.ID != filter {
				continue
			}
			for _, library := range source.Libraries {
				if !library.Hidden && (library.category() == category || library.category() == "") {
					return category
				}
			}
		}
	}
	return libraryMovies
}

func scanCatalogPages(ctx context.Context, list func(context.Context, media.Query) (media.Page, error), query media.Query) (media.Page, error) {
	query.Limit = 100
	var result media.Page
	seen := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		batch, err := list(ctx, query)
		if err != nil {
			return result, err
		}
		result.Total = batch.Total
		added := false
		for _, item := range batch.Items {
			if item.ID != "" && !seen[item.ID] {
				seen[item.ID] = true
				result.Items = append(result.Items, item)
				added = true
			}
		}
		query.Offset += len(batch.Items)
		if query.Offset >= batch.Total {
			result.Total = len(result.Items)
			return result, nil
		}
		if !added {
			return result, media.ErrResponse
		}
	}
}

func (a *application) phoneLibraryPages(cx *core.Scope, request func() *catalogRequest) (*core.Resource[phoneLibraryPage], *core.Resource[phoneLibraryPage]) {
	preview := core.NewResource(cx, func() (*catalogRequest, bool) {
		q := request()
		return q, len(q.sources) != 0
	}, func(ctx context.Context, q *catalogRequest, _ core.ResourceInfo) (phoneLibraryPage, error) {
		return phoneLibraryPage{q, a.loadCatalog(ctx, *q)}, nil
	})
	full := core.NewResource(cx, func() (*catalogRequest, bool) {
		q := request()
		page, ready := preview.Get()
		return q, ready && !preview.Loading() && page.query == q && page.result.hasMore
	}, func(ctx context.Context, q *catalogRequest, _ core.ResourceInfo) (phoneLibraryPage, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		all := *q
		all.scan = true
		return phoneLibraryPage{q, a.loadCatalog(ctx, all)}, nil
	})
	return preview, full
}

type libraryFacets struct {
	genres             []string
	years, resolutions []int
}

func phoneLibraryFacets(items []catalogItem) libraryFacets {
	var facets libraryFacets
	genres := make(map[string]string)
	years, resolutions := make(map[int]bool), make(map[int]bool)
	for _, entry := range items {
		for _, original := range entry.item.Genres {
			name := strings.TrimSpace(original)
			key := strings.ToLower(name)
			if _, ok := genres[key]; !ok && name != "" {
				genres[key] = name
				facets.genres = append(facets.genres, name)
			}
		}
		if year := entry.item.Year; year > 0 && !years[year] {
			years[year] = true
			facets.years = append(facets.years, year)
		}
		for _, profile := range catalogVideoProfiles(entry) {
			if pixels := libraryResolution(profile); pixels > 0 && !resolutions[pixels] {
				resolutions[pixels] = true
				facets.resolutions = append(facets.resolutions, pixels)
			}
		}
	}
	slices.SortFunc(facets.genres, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	slices.SortFunc(facets.years, func(a, b int) int { return cmp.Compare(b, a) })
	slices.SortFunc(facets.resolutions, func(a, b int) int { return cmp.Compare(b, a) })
	return facets
}

func libraryResolution(profile media.VideoProfile) int {
	switch {
	case profile.Height >= 4000 || profile.Width >= 7700:
		return 4320
	case profile.Height >= 2000 || profile.Width >= 3800:
		return 2160
	case profile.Height >= 1040 || profile.Width >= 1880:
		return 1080
	case profile.Height >= 700 || profile.Width >= 1240:
		return 720
	default:
		return profile.Height
	}
}

func libraryResolutionName(resolution int) string {
	switch resolution {
	case 4320:
		return "8K"
	case 2160:
		return "4K"
	default:
		return strconv.Itoa(resolution) + "p"
	}
}

func catalogVideoProfiles(entry catalogItem) []media.VideoProfile {
	profiles := entry.item.VideoProfiles
	if entry.match != nil {
		// Copies can contribute a format absent from the preferred version.
		profiles = slices.Clone(profiles)
		for _, copy := range entry.match.copies {
			profiles = append(profiles, copy.item.VideoProfiles...)
		}
	}
	return profiles
}

func catalogWatched(entry catalogItem) bool {
	if entry.item.Watched {
		return true
	}
	return entry.match != nil && slices.ContainsFunc(entry.match.copies, func(copy catalogCopy) bool { return copy.item.Watched })
}

func filterPhoneLibrary(items []catalogItem, settings phoneLibraryState) []catalogItem {
	matched := make([]catalogItem, 0, len(items))
	for _, item := range items {
		if settings.unwatched && catalogWatched(item) || settings.year > 0 && item.item.Year != settings.year {
			continue
		}
		if settings.genre != "" && !slices.ContainsFunc(item.item.Genres, func(genre string) bool {
			return strings.EqualFold(strings.TrimSpace(genre), settings.genre)
		}) {
			continue
		}
		if settings.resolution > 0 && !slices.ContainsFunc(catalogVideoProfiles(item), func(profile media.VideoProfile) bool {
			return libraryResolution(profile) == settings.resolution
		}) {
			continue
		}
		matched = append(matched, item)
	}
	slices.SortStableFunc(matched, func(a, b catalogItem) int {
		switch settings.sort {
		case libraryTitle:
			return strings.Compare(strings.ToLower(a.item.Title), strings.ToLower(b.item.Title))
		case libraryYear:
			return cmp.Compare(b.item.Year, a.item.Year)
		case libraryRating:
			return cmp.Compare(b.item.Rating, a.item.Rating)
		default:
			return b.item.AddedAt.Compare(a.item.AddedAt)
		}
	})
	return matched
}

func libraryChoices(cx *core.Scope, sources []source, filter string, category libraryCategory) []ui.Option[string] {
	choices := []ui.Option[string]{{Label: locales.AllLibraries(cx)}}
	q := catalogRequest{category: category}
	for _, source := range sources {
		if filter != "" && source.ID != filter {
			continue
		}
		for _, library := range source.Libraries {
			if q.includesLibrary(source.ID, library) {
				choices = append(choices, ui.Option[string]{Value: source.ID + "/" + library.ID, Label: library.Name + " · " + source.Name})
			}
		}
	}
	return choices
}

func libraryBookmarkMap(items []catalogItem) map[copyKey]media.Item {
	bookmarks := make(map[copyKey]media.Item)
	for _, entry := range items {
		item := entry.item
		if item.Kind != media.Episode || item.SeriesID == "" || !canResume(item) {
			continue
		}
		series := item
		series.ID = item.SeriesID
		key := (catalogCopy{entry.source, series}).key()
		if current := bookmarks[key]; current.ID == "" || item.LastPlayed.After(current.LastPlayed) {
			bookmarks[key] = item
		}
	}
	return bookmarks
}

func libraryCardDetails(cx *core.Scope, entry catalogItem, bookmarks map[copyKey]media.Item) (string, media.Item) {
	item := entry.item
	if item.Kind == media.Series {
		resume := bookmarks[(catalogCopy{entry.source, item}).key()]
		if entry.match != nil {
			for _, copy := range entry.match.copies {
				candidate := bookmarks[copy.key()]
				if candidate.ID != "" && (resume.ID == "" || candidate.LastPlayed.After(resume.LastPlayed)) {
					resume = candidate
				}
			}
		}
		if resume.ID != "" && resume.Episode > 0 {
			return locales.LibraryEpisodePosition(cx, strconv.Itoa(resume.Season), strconv.Itoa(resume.Episode)), resume
		}
		if item.EpisodeCount > 0 {
			label := locales.LibraryEpisodeCount(cx, strconv.Itoa(item.EpisodeCount))
			if item.UnplayedCount != nil && *item.UnplayedCount > 0 {
				label += " · " + locales.LibraryUnplayedEpisodes(cx, strconv.Itoa(*item.UnplayedCount))
			}
			return label, resume
		}
		if item.SeasonCount > 0 {
			return locales.SeriesSeasonCount(cx, strconv.Itoa(item.SeasonCount)), resume
		}
	}
	var details []string
	if item.Year > 0 {
		details = append(details, strconv.Itoa(item.Year))
	}
	if item.Rating > 0 {
		details = append(details, strconv.FormatFloat(item.Rating, 'f', 1, 64))
	}
	return strings.Join(details, " · "), item
}
