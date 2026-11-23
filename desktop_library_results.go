package main

import (
	"slices"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) desktopLibraryResults(cx *core.Scope, rows *core.Memo[[]desktopLibraryRow], bookmarks *core.Memo[map[copyKey]media.Item],
	state *core.Signal[desktopLibraryState], collapsed, waiting func() bool, failure func() error, retry func(), sort func(librarySort)) core.Node {
	ref := &core.Ref{}
	letter := core.NewSignal(cx, "")
	mode := core.NewMemo(cx, func() desktopLibraryMode { return state.Get().mode })
	columns := core.NewMemo(cx, func() int {
		width := cx.Env().Window().Width - 96
		if !collapsed() {
			width -= 240
		}
		switch mode.Get() {
		case desktopList:
			return 1
		case desktopWall:
			return max(1, min(10, int((width+8)/112)))
		default:
			if a.appearancePrefs.Get().Poster == "landscape" {
				return max(1, min(5, int((width+16)/240)))
			}
			return max(1, min(7, int((width+16)/160)))
		}
	})
	results := core.Dynamic(func(cx *core.Scope) core.Node {
		layout, columns := mode.Get(), columns.Get()
		landscape := a.appearancePrefs.Get().Poster == "landscape"
		estimated, rowGap, gap := core.Points(252), core.Points(33), core.Points(16)
		switch layout {
		case desktopList:
			estimated, rowGap, gap = 64, 1, 0
		case desktopWall:
			estimated, rowGap, gap = 156, 8, 8
		case desktopGrid:
			if landscape {
				estimated = 168
			}
		}
		return core.List{
			Class: "flex-1 min-w-0 pb-8", TestID: "desktop-library-" + string(layout), Ref: ref,
			NumColumns: columns, EstimatedSize: estimated, Gap: rowGap, ColumnGap: gap,
			Count: func() int { return len(rows.Get()) },
			Key:   func(i int) any { return desktopRowKey(rows.Get()[i]) },
			Row: func(cx *core.Scope, row core.ListRow) core.Node {
				return core.Dynamic(func(cx *core.Scope) core.Node {
					items := rows.Get()
					index := row.Index()
					if index < 0 || index >= len(items) {
						return nil
					}
					item := items[index]
					entry := item.entry
					quality := desktopLibraryBadge(cx, item.picture, state.Get().filters.Resolution > 0)
					switch layout {
					case desktopList:
						return a.desktopLibraryListCard(cx, item, bookmarks.Get())
					case desktopWall:
						return a.desktopLibraryWallCard(cx, entry, quality, bookmarks.Get())
					default:
						shape, cover := "w-36 max-w-full min-w-0 gap-1.5 rounded-xl", "h-52 rounded-xl shadow-lg shadow-black/15"
						if landscape {
							shape, cover = "w-56 max-w-full min-w-0 gap-1.5 rounded-xl", "w-full rounded-xl shadow-lg shadow-black/15"
						}
						return a.compactMediaCard(cx, entry, bookmarks.Get(), quality,
							shape, cover, true, landscape, func() { a.openCatalogEntry(entry) })
					}
				})
			},
			Empty: core.Dynamic(func(cx *core.Scope) core.Node {
				if len(a.sources.Get()) == 0 {
					return compactLibraryEmpty(cx, lucide.LibraryBig(), locales.NoSources(cx), locales.AddSource(cx),
						func() { a.push(route{screen: screenSources}) })
				}
				if waiting() {
					return compactLibraryEmpty(cx, lucide.LoaderCircle(), locales.Loading(cx), "", nil)
				}
				if err := failure(); err != nil {
					return compactLibraryEmpty(cx, lucide.CircleAlert(), errorText(cx, err), locales.Retry(cx), retry)
				}
				return compactLibraryEmpty(cx, lucide.Film(), locales.EmptyLibrary(cx), "", nil)
			}),
		}
	})
	jump := func(target string) {
		letter.Set(target)
		// The alphabet index is also a shortcut to title order.
		sort(libraryTitle)
		cx.Post(func() {
			i := slices.IndexFunc(rows.GetUntracked(), func(row desktopLibraryRow) bool { return desktopTitleLetter(row.titleKey) == target })
			if i >= 0 {
				ref.ScrollToIndex(i, false)
			}
		})
	}
	index := core.Dynamic(func(cx *core.Scope) core.Node {
		available := make(map[string]bool)
		for _, row := range rows.Get() {
			available[desktopTitleLetter(row.titleKey)] = true
		}
		entries := make([]core.Node, 0, len(available))
		for _, char := range "#ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
			key := string(char)
			if !available[key] {
				continue
			}
			entries = append(entries, base.Button{Class: "group w-full items-center justify-center rounded",
				Label: key, TestID: "desktop-index-" + key, Style: core.Style{FocusVisible: ui.FocusRing(cx, 1)},
				Selected: func() bool { return letter.Get() == key && state.Get().sort == libraryTitle },
				OnPress:  func(core.PressEvent) { jump(key) }, Children: []core.Node{
					core.Text{Content: key, Class: "text-[10px] " + setupMuted + " group-selected:font-bold group-selected:text-violet-500"},
				}})
		}
		return core.View{Class: "gap-1 items-center", Children: entries}
	})
	partialFailure := core.Dynamic(func(cx *core.Scope) core.Node {
		err := failure()
		if err == nil || waiting() || len(rows.Get()) == 0 {
			return nil
		}
		return core.View{Class: "flex-row items-center justify-between gap-3 p-3 rounded-xl " + desktopLibrarySurface, Children: []core.Node{
			core.Text{Content: errorText(cx, err), Class: "min-w-0 flex-1 text-sm " + setupMuted},
			ui.Button{Text: locales.Retry(cx), TestID: "desktop-library-retry", Variant: ui.ButtonOutline, OnPress: retry},
		}}
	})
	return core.View{Class: "min-h-0 flex-1 gap-3", Children: []core.Node{
		partialFailure,
		core.View{Class: "min-h-0 flex-1 flex-row gap-3", Children: []core.Node{
			results,
			core.Scroll{Class: "w-5 shrink-0", HideIndicator: true, TestID: "desktop-library-index", Children: []core.Node{
				core.View{Class: "pt-2", Children: []core.Node{index}},
			}},
		}},
	}}
}

func desktopLibraryBadge(cx *core.Scope, profile media.VideoProfile, resolutionOnly bool) string {
	if !resolutionOnly {
		if value := localizedDetailVideoRange(cx, media.Version{VideoRange: profile.Range}); value != "" {
			return value
		}
	}
	return libraryPosterQuality(profile, true)
}

func (a *application) desktopLibraryListCard(cx *core.Scope, row desktopLibraryRow, bookmarks map[copyKey]media.Item) core.Node {
	entry, item := row.entry, row.entry.item
	description, progress := libraryCardDetails(cx, entry, bookmarks)
	metadata := []string{entry.source.Name}
	prefs := a.appearancePrefs.Get()
	if quality := desktopVideoFormat(cx, row.picture); quality != "" && prefs.ShowQuality {
		metadata = append(metadata, quality)
	}
	if row.size > 0 {
		metadata = append(metadata, mediaBytes(row.size))
	}
	status := []core.Node{}
	watched := catalogWatched(entry)
	if watched && prefs.ShowWatched {
		status = append(status, core.Icon{SVG: lucide.Check(), Class: "size-4 " + setupAccent})
	} else if !watched && progress.Duration > 0 && canResume(progress) {
		status = append(status, core.View{Class: "w-20", Children: []core.Node{homeProgress(cx, "", "", progress)}})
	}
	if item.Favorite {
		status = append(status, core.Icon{SVG: lucide.Heart(), Class: "size-4 " + setupAccent})
	}
	if item.Duration > 0 {
		status = append(status, core.Text{Content: playbackClock(item.Duration), Class: "text-sm " + setupMuted})
	}
	return base.Button{Label: item.Title, Hint: entry.source.Name, TestID: a.catalogEntryID("title-", entry),
		Class: "h-16 flex-row items-center gap-4 rounded-lg px-3 " + desktopLibrarySurface,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { a.openCatalogEntry(entry) },
		Children: []core.Node{
			homeArtwork(item.Poster, "h-[50px] w-9 shrink-0 overflow-hidden rounded-md"),
			core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
				core.Text{Content: item.Title, Class: "text-sm font-medium " + setupTitle, MaxLines: 1},
				core.Text{Content: description, Class: "text-xs " + setupMuted, MaxLines: 1},
			}},
			core.Text{Content: strings.Join(metadata, " · "), Class: "min-w-0 shrink text-sm " + setupMuted, MaxLines: 1},
			core.View{Class: "shrink-0 flex-row items-center gap-3", Children: status},
		}}
}

func (a *application) desktopLibraryWallCard(cx *core.Scope, entry catalogItem, quality string, bookmarks map[copyKey]media.Item) core.Node {
	item := entry.item
	_, progress := libraryCardDetails(cx, entry, bookmarks)
	caption := core.View{Class: "absolute inset-x-0 bottom-0 gap-1 bg-linear-to-t from-black/90 to-transparent px-2 pb-2 pt-6",
		Children: []core.Node{core.Text{Content: item.Title, Class: "text-xs font-semibold text-white", MaxLines: 2}}}
	if !catalogWatched(entry) && progress.Duration > 0 && canResume(progress) {
		caption.Children = append(caption.Children, homeProgress(cx, a.catalogEntryID("library-progress-", entry), "", progress))
	}
	children := []core.Node{
		desktopMovieArtwork(item.Poster, "absolute inset-0"),
		caption,
	}
	return base.Button{Label: item.Title, Hint: entry.source.Name, TestID: a.catalogEntryID("title-", entry),
		Class: "relative h-[156px] min-w-0 overflow-hidden rounded-lg", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress: func(core.PressEvent) { a.openCatalogEntry(entry) }, Children: append(children, a.libraryPosterBadges(cx, entry, quality)...)}
}
