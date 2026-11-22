package main

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

const seriesSpecials = "specials"

type seriesViewState struct {
	tab                   string
	copy                  copyKey
	reversed, hideWatched bool
}

type seriesEpisodePage struct {
	tab     string
	items   []media.Item
	failure error
}

func (a *application) phoneSeriesDetail(cx *core.Scope, current route, copies func() []ui.Option[copyKey], selectCopy func(copyKey)) core.Node {
	detail := a.detailResource(cx, current)
	personal := a.detailPersonal(cx, current, detail)
	selected := core.NewSignal(cx, current.seriesView.tab)
	reversed := core.NewSignal(cx, current.seriesView.reversed)
	hideWatched := core.NewSignal(cx, current.seriesView.hideWatched || a.appearancePrefs.GetUntracked().HideFinished)
	menuEpisode := core.NewSignalFunc(cx, catalogItem{}, nil)
	menuOpen := core.NewSignal(cx, false)
	seasons := core.NewMemoFunc(cx, func() []media.Item {
		result, _ := detail.Get()
		children := slices.DeleteFunc(slices.Clone(result.children), func(item media.Item) bool {
			return item.ID == "" || item.Kind != media.Season
		})
		slices.SortStableFunc(children, func(a, b media.Item) int { return cmp.Compare(a.Season, b.Season) })
		return children
	}, nil)
	bookmarks := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) ([]media.Item, error) {
		client, err := a.client(current.source)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var items []media.Item
		if scoped, ok := client.(media.LibraryResumeClient); ok && current.item.LibraryID != "" {
			items, err = scoped.ResumeLibrary(ctx, current.item.LibraryID)
		} else {
			items, err = client.Resume(ctx)
		}
		return slices.DeleteFunc(items, func(item media.Item) bool {
			return item.Kind != media.Episode || item.SeriesID != current.item.ID || !canResume(item)
		}), err
	})
	pending := func(items []catalogItem) []catalogItem {
		query := catalogRequest{}
		if current.item.LibraryID != "" {
			query.library = current.source.ID + "/" + current.item.LibraryID
		}
		rows := a.progressResumeShelf(items, []source{current.source}, query)
		return slices.DeleteFunc(slices.Clone(rows), func(entry catalogItem) bool {
			return entry.item.SeriesID != current.item.ID
		})
	}
	bookmarked := core.NewMemoFunc(cx, func() catalogItem {
		items, _ := bookmarks.Get()
		rows := make([]catalogItem, 0, len(items))
		for _, item := range items {
			rows = append(rows, catalogItem{source: current.source, item: inheritSeriesEpisode(item, current.item)})
		}
		return latestSeriesBookmark(a.parentalRules().catalog(pending(rows)))
	}, sameSeriesBookmark)

	activeTab := core.NewMemo(cx, func() string {
		available := seasons.Get()
		if tab := selected.Get(); tab != "" && (tab == seriesSpecials || slices.ContainsFunc(available, func(season media.Item) bool {
			return season.ID == tab && season.Season > 0
		})) {
			return tab
		}
		resume := bookmarked.Get().item
		for _, season := range available {
			if resume.ID != "" && season.Season == resume.Season {
				if season.Season == 0 {
					return seriesSpecials
				}
				return season.ID
			}
		}
		for _, season := range available {
			if season.Season > 0 {
				return season.ID
			}
		}
		if result, ready := detail.Get(); ready && result.failure == nil && !detail.Loading() {
			return seriesSpecials
		}
		return ""
	})
	type query struct {
		tab     string
		seasons []media.Item
	}
	episodes := core.NewResource(cx, func() (query, bool) {
		tab := activeTab.Get()
		return query{tab, seasons.GetUntracked()}, tab != ""
	}, func(ctx context.Context, q query, _ core.ResourceInfo) (seriesEpisodePage, error) {
		return a.loadSeriesEpisodes(ctx, current, q.tab, q.seasons), nil
	})
	rows := core.NewMemoFunc(cx, func() []catalogItem {
		tab := activeTab.Get()
		page, _ := episodes.Get()
		if tab == "" || page.tab != tab {
			return nil
		}
		items := make([]catalogItem, 0, len(page.items))
		ids := make(map[string]bool, len(page.items))
		for _, item := range page.items {
			if !a.parentalRules().visible(current.source.ID, item) {
				continue
			}
			items = append(items, catalogItem{source: current.source, item: item})
			ids[item.ID] = true
		}
		if resume := bookmarked.Get(); resume.item.ID != "" {
			if i := slices.IndexFunc(items, func(item catalogItem) bool { return item.item.ID == resume.item.ID }); i >= 0 {
				if resume.pending || items[i].item.LastPlayed.Before(resume.item.LastPlayed) {
					items[i].item.Position, items[i].item.LastPlayed = resume.item.Position, resume.item.LastPlayed
					items[i].pending = resume.pending
				}
			}
		}
		return slices.DeleteFunc(pending(items), func(entry catalogItem) bool { return !ids[entry.item.ID] })
	}, nil)
	target := core.NewMemoFunc(cx, func() catalogItem {
		resume := bookmarked.Get()
		items := rows.Get()
		if local := latestSeriesBookmark(items); local.item.ID != "" && (resume.item.ID == "" || resume.item.LastPlayed.Before(local.item.LastPlayed)) {
			resume = local
		}
		if resume.item.ID != "" {
			return resume
		}
		for _, entry := range items {
			if !entry.item.Watched {
				return entry
			}
		}
		if len(items) > 0 {
			return items[0]
		}
		return catalogItem{}
	}, sameSeriesBookmark)
	episodeID := core.NewMemo(cx, func() string { return target.Get().item.ID })
	type versionQuery struct {
		item    media.Item
		options media.PlaybackOptions
	}
	version := core.NewResource(cx, func() (versionQuery, bool) {
		id := episodeID.Get()
		item := target.GetUntracked().item
		options, _ := a.initialPlaybackOptions(route{source: current.source, item: item})
		return versionQuery{item, options}, id != ""
	}, func(ctx context.Context, q versionQuery, _ core.ResourceInfo) (seriesVersion, error) {
		return a.seriesVersion(ctx, current.source, q.item, q.options)
	})
	open := func(entry catalogItem, resume, details bool) {
		view := seriesViewState{
			tab: activeTab.GetUntracked(), copy: (catalogCopy{current.source, current.item}).key(),
			reversed: reversed.GetUntracked(), hideWatched: hideWatched.GetUntracked(),
		}
		a.openSeriesEpisode(view, entry, resume, details)
	}
	refresh := func() {
		detail.Refetch()
		bookmarks.Refetch()
		episodes.Refetch()
	}
	hero := core.Dynamic(func(cx *core.Scope) core.Node {
		result, ready := detail.Get()
		if !ready {
			result.item = current.item
		}
		badges, _ := version.Get()
		if badges.itemID != episodeID.Get() {
			badges = seriesVersion{}
		}
		return a.phoneSeriesHero(cx, current, result.item, seasons.Get(), badges.version,
			personal.buttons(cx, result, phoneHeroAction), copies, selectCopy, refresh)
	})
	summary := core.Dynamic(func(cx *core.Scope) core.Node {
		result, ready := detail.Get()
		item := current.item
		if ready {
			item = result.item
		}
		entry := target.Get()
		heading := a.seriesPlaybackSummary(cx, item, entry, rows.Get(), func() { open(entry, true, false) }, func() {
			menuEpisode.Set(entry)
			menuOpen.Set(true)
		})
		return core.View{Class: "gap-3 px-5", Children: []core.Node{heading, personal.error}}
	})
	episodeList := core.Dynamic(func(cx *core.Scope) core.Node {
		tab := activeTab.Get()
		page, ready := episodes.Get()
		result, detailReady := detail.Get()
		switch {
		case detailReady && result.failure != nil && tab == "":
			return seriesEmpty(cx, lucide.CircleAlert(), errorText(cx, result.failure), refresh)
		case !ready || page.tab != tab || (episodes.Loading() && len(page.items) == 0):
			return seriesEmpty(cx, lucide.LoaderCircle(), locales.Loading(cx), nil)
		case len(page.items) == 0 && page.failure != nil:
			return seriesEmpty(cx, lucide.CircleAlert(), errorText(cx, page.failure), episodes.Refetch)
		}
		items := rows.Get()
		active := target.Get().item.ID
		list := make([]core.Node, 0, len(items))
		appendRow := func(entry catalogItem) {
			if hideWatched.Get() && entry.item.Watched && !canResume(entry.item) {
				return
			}
			play := func() { open(entry, true, false) }
			more := func() { menuEpisode.Set(entry); menuOpen.Set(true) }
			list = append(list, a.phoneEpisodeRow(cx, current, entry.item, active, play, more))
		}
		if reversed.Get() {
			for _, item := range slices.Backward(items) {
				appendRow(item)
			}
		} else {
			for _, item := range items {
				appendRow(item)
			}
		}
		if len(list) == 0 {
			icon, message := lucide.CalendarClock(), locales.SeriesNoEpisodes(cx)
			if tab == seriesSpecials && len(items) == 0 {
				icon, message = lucide.Clapperboard(), locales.SeriesNoSpecials(cx)
			}
			return seriesEmpty(cx, icon, message, nil)
		}
		if page.failure != nil {
			list = append(list, notice(errorText(cx, page.failure)), ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: episodes.Refetch})
		}
		return core.View{Class: "mx-4 gap-1 rounded-2xl bg-white p-2 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder,
			TestID: "series-episode-list", Role: core.RoleGroup, Children: list}
	})
	return core.View{Class: "relative flex-1 bg-waygu-page", Children: []core.Node{
		core.Scroll{Class: "flex-1", TestID: "phone-series-scroll", Children: []core.Node{
			core.View{Class: "relative", Style: core.Style{PaddingEnv: core.EnvEdges{
				Left: core.EnvSafeArea, Right: core.EnvSafeArea, Bottom: core.EnvSafeArea,
			}}, Children: []core.Node{
				core.SVGView{Class: "absolute inset-x-0 top-[360px] h-[1100px] w-full dark:hidden", SVG: welcomeAmbientLight},
				core.SVGView{Class: "absolute inset-x-0 top-[360px] hidden h-[1100px] w-full dark:flex", SVG: welcomeAmbientDark},
				hero,
				core.View{Class: "gap-6 pt-5 pb-10", Children: []core.Node{
					summary,
					core.Dynamic(func(cx *core.Scope) core.Node {
						return phoneSeasonTabs(cx, seasons.Get(), activeTab.Get, selected.Set, reversed, hideWatched)
					}),
					episodeList,
				}},
			}},
		}},
		ui.Dialog{
			TitleFunc: func() string { return menuEpisode.Get().item.Title }, Open: menuOpen.Get,
			TestID: "series-episode-menu", OnOpenChange: func(open bool, _ base.Reason) { menuOpen.Set(open) },
			Children: []core.Node{
				ui.Button{Text: locales.Play(cx), Icon: lucide.Play, OnPress: func() {
					menuOpen.Set(false)
					open(menuEpisode.GetUntracked(), true, false)
				}},
				ui.Button{Text: locales.FromStart(cx), Icon: lucide.RotateCcw, TestID: "series-episode-start", Variant: ui.ButtonOutline,
					OnPress: func() { menuOpen.Set(false); open(menuEpisode.GetUntracked(), false, false) }},
				ui.Button{Text: locales.Details(cx), Icon: lucide.Info, TestID: "series-episode-details", Variant: ui.ButtonOutline,
					OnPress: func() { menuOpen.Set(false); open(menuEpisode.GetUntracked(), true, true) }},
			}},
	}}
}

func (a *application) phoneSeriesHero(cx *core.Scope, current route, item media.Item, seasons []media.Item, version media.Version, tools []core.Node,
	copies func() []ui.Option[copyKey], selectCopy func(copyKey), refresh func()) core.Node {
	var facts []string
	if item.Year > 0 {
		facts = append(facts, strconv.Itoa(item.Year))
	}
	count := 0
	for _, season := range seasons {
		if season.Season > 0 {
			count++
		}
	}
	if count > 0 {
		facts = append(facts, locales.SeriesSeasonCount(cx, strconv.Itoa(count)))
	}
	if len(item.Genres) > 0 {
		facts = append(facts, strings.Join(item.Genres, " / "))
	}
	if item.Rating > 0 {
		facts = append(facts, "★ "+strconv.FormatFloat(item.Rating, 'f', 1, 64))
	}
	title := []core.Node{core.Text{Content: item.Title, Role: core.RoleHeader, TestID: "phone-series-title",
		Class: "text-4xl font-bold tracking-tight text-white", MaxLines: 2}}
	if len(facts) != 0 {
		title = append(title, core.Text{Content: strings.Join(facts, " · "), Class: "text-xs text-white/80", MaxLines: 2})
	}
	const badgeStyle = "rounded-md px-1.5 py-0.5 text-[10px] font-semibold text-white"
	var badges []core.Node
	for _, label := range []string{detailResolution(version), detailVideoRange(version)} {
		if label != "" {
			badges = append(badges, core.Text{Content: label, Class: "bg-white/15 " + badgeStyle})
		}
	}
	badges = append(badges, core.Text{
		Content: strings.Join([]string{current.source.Name, providerName(cx, current.source.Provider)}, " · "),
		TestID:  "series-source", Class: "bg-sky-500/30 " + badgeStyle, MaxLines: 1,
	})
	title = append(title, core.View{Class: "flex-row flex-wrap gap-1.5", Children: badges})
	menu := []core.Node{ui.MenuItem{Text: locales.ReloadMovieDetail(cx), TestID: "series-reload", OnSelect: refresh}}
	if copies != nil && selectCopy != nil {
		active := (catalogCopy{current.source, current.item}).key()
		for _, option := range copies() {
			menu = append(menu, ui.MenuItem{Text: option.Label, Checked: func() bool { return option.Value == active },
				OnSelect: func() { selectCopy(option.Value) }})
		}
	}
	tools = append(tools, base.Menu{Label: locales.DetailMore(cx), TestID: "detail-hero-more",
		Class: "size-9 rounded-full bg-black/40", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, PopupClass: tabletDetailPopup,
		Trigger: core.View{Class: "flex-1 items-center justify-center", Children: []core.Node{
			core.Icon{SVG: lucide.Ellipsis(), Class: "size-4 text-white"},
		}}, Children: menu})
	return a.phoneMediaHero(cx, item, "h-[420px]", "phone-series-hero", title, tools...)
}

func phoneHeroAction(cx *core.Scope, id, label string, icon *core.SVG, selected bool, disabled func() bool, press func()) core.Node {
	return base.Button{Label: label, TestID: id, Selected: func() bool { return selected }, Disabled: disabled,
		Class: "size-9 items-center justify-center rounded-full bg-black/40 selected:bg-violet-500/65 disabled:opacity-50",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { press() }, Children: []core.Node{
			core.Icon{SVG: icon, Class: "size-4 text-white"},
		}}
}

func (a *application) seriesPlaybackSummary(cx *core.Scope, series media.Item, entry catalogItem, items []catalogItem, play, more func()) core.Node {
	episode := entry.item
	parts := []core.Node{}
	if episode.ID != "" {
		label := locales.Play(cx)
		if canResume(episode) {
			label = locales.SeriesResumeEpisode(cx, strconv.Itoa(episode.Season), strconv.Itoa(episode.Episode))
		}
		parts = append(parts, base.Button{Label: label, TestID: "detail-play",
			Class:   "h-[52px] flex-row items-center justify-center gap-2 rounded-2xl bg-linear-to-r from-violet-500 to-fuchsia-500 shadow-lg shadow-violet-500/30",
			Style:   core.Style{FocusVisible: ui.FocusRing(cx, 2)},
			OnPress: func(core.PressEvent) { play() }, OnLongPress: func(core.PressEvent) { more() }, Children: []core.Node{
				core.Icon{SVG: lucide.Play(), Class: "size-5 text-white"},
				core.Text{Content: label, Class: "text-base font-semibold text-white"},
			}})
	}
	if canResume(episode) {
		if episode.Duration > 0 {
			value := episodeProgress(episode)
			parts = append(parts, core.View{Class: "overflow-hidden rounded-full", Children: []core.Node{
				base.Progress{TestID: "series-resume-progress", Label: locales.ResumePercent(cx, strconv.Itoa(int(value))),
					Value: func() float32 { return value }, TrackClass: "h-1 bg-zinc-900/10 dark:bg-zinc-50/10", IndicatorClass: "h-1 bg-violet-500"},
			}})
		}
		spent := seriesDuration(cx, episode.Position)
		label := locales.SeriesWatchedTime(cx, strconv.Itoa(episode.Episode), spent)
		if slices.ContainsFunc(items, func(entry catalogItem) bool { return entry.item.ID == episode.ID }) {
			remaining := 0
			for _, row := range items {
				if row.item.Kind == media.Episode && row.item.Season == episode.Season && row.item.Episode > episode.Episode && !row.item.Watched {
					remaining++
				}
			}
			label += " · " + locales.SeriesEpisodesLeft(cx, strconv.Itoa(remaining))
		}
		parts = append(parts, core.Text{Content: label, TestID: "series-resume-caption", Class: "text-xs " + setupMuted})
	}
	parts = append(parts, core.Text{Content: cmp.Or(series.Overview, locales.NoOverview(cx)), TestID: "detail-synopsis",
		Class: "text-sm leading-relaxed text-zinc-900/80 dark:text-zinc-50/80", MaxLines: 2})
	return core.View{Class: "gap-3", Children: parts}
}

func phoneSeasonTabs(cx *core.Scope, seasons []media.Item, active func() string, choose func(string),
	reversed, hideWatched *core.Signal[bool]) core.Node {
	tabs := make([]core.Node, 0, len(seasons)+1)
	add := func(id, label string) {
		tabs = append(tabs, base.Tab{Label: label, TestID: "series-tab-" + id,
			Class:    "shrink-0 border-b-2 border-transparent pb-2.5 selected:border-violet-500",
			Selected: func() bool { return active() == id }, Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
			OnPress: func(core.PressEvent) { choose(id) }, Children: []core.Node{
				core.Text{Content: label, ClassFunc: func() string {
					tint := setupMuted
					if active() == id {
						tint = setupTitle
					}
					return "text-[15px] font-semibold " + tint
				}},
			}})
	}
	for _, season := range seasons {
		if season.Season > 0 {
			add(season.ID, locales.SeasonLabel(cx, strconv.Itoa(season.Season)))
		}
	}
	add(seriesSpecials, locales.SeriesSpecials(cx))
	return core.View{Class: "flex-row items-center gap-4 border-b border-zinc-900/8 px-5 dark:border-white/8", Children: []core.Node{
		core.Scroll{Horizontal: true, Class: "min-w-0 flex-1", Children: []core.Node{
			base.Tabs{Label: locales.Seasons(cx), Class: "flex-row items-center gap-5", Children: tabs},
		}},
		base.Menu{Label: locales.SeriesSort(cx), TestID: "series-sort", Class: "shrink-0 rounded",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, PopupClass: tabletDetailPopup,
			Trigger: core.View{Class: "flex-row items-center gap-1 pb-2.5", Children: []core.Node{
				core.Icon{SVG: lucide.ArrowUpDown(), Class: "size-3.5 " + setupMuted},
				core.Text{Class: "text-xs " + setupMuted, ContentFunc: func() string {
					if reversed.Get() {
						return locales.SeriesSortDesc(cx)
					}
					return locales.SeriesSortAsc(cx)
				}},
			}}, Children: []core.Node{
				ui.MenuItem{Text: locales.SeriesSortAsc(cx), TestID: "series-ascending", Checked: func() bool { return !reversed.Get() },
					OnSelect: func() { reversed.Set(false) }},
				ui.MenuItem{Text: locales.SeriesSortDesc(cx), TestID: "series-descending", Checked: reversed.Get, OnSelect: func() { reversed.Set(true) }},
				ui.MenuItem{Text: locales.SeriesHideWatched(cx), TestID: "series-hide-watched", Checked: hideWatched.Get,
					OnSelect: func() { hideWatched.Update(func(value bool) bool { return !value }) }},
			}},
	}}
}

func (a *application) phoneEpisodeRow(cx *core.Scope, current route, item media.Item, active string, play, more func()) core.Node {
	resume := item.ID == active && canResume(item)
	title, muted := setupTitle, setupMuted
	if item.Watched && !resume {
		title, muted = setupMuted, "text-zinc-500/60 dark:text-zinc-400/60"
	}
	art := []core.Node{homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0")}
	if resume {
		art = append(art, core.View{Class: "absolute inset-0 items-center justify-center bg-black/30",
			Children: []core.Node{core.Icon{SVG: lucide.Play(), Class: "size-6 text-white"}}})
		if item.Duration > 0 {
			art = append(art, homeProgress(cx, "episode-progress-"+item.ID, "absolute inset-x-0 bottom-0", item))
		}
	} else if item.Watched {
		art = append(art, core.View{Class: "absolute top-1 right-1 size-4 items-center justify-center rounded-full bg-violet-500",
			Children: []core.Node{core.Icon{SVG: lucide.Check(), Class: "size-2.5 text-white"}}})
	}
	label := item.Title
	if item.Kind == media.Episode && item.Episode > 0 {
		label = strconv.Itoa(item.Episode) + ". " + label
	}
	var facts []string
	if item.Duration > 0 {
		facts = append(facts, seriesDuration(cx, item.Duration))
	}
	if resume && item.Duration > 0 {
		facts = append(facts, locales.ResumeRemaining(cx, seriesDuration(cx, item.Duration-item.Position)))
		muted = setupAccent
	} else if item.Watched {
		facts = append(facts, locales.SeriesFinished(cx))
	}
	if entry, ok := a.seriesDownload(current.source, item, a.downloads.entries.Get()); ok && entry.State == downloadComplete {
		facts = append(facts, locales.Downloaded(cx))
	}
	info := []core.Node{
		core.Text{Content: label, Class: "text-sm font-medium " + title, MaxLines: 2},
		core.Text{Content: strings.Join(facts, " · "), Class: "text-xs " + muted, MaxLines: 2},
	}
	if raw, found := a.storage.GetItem(danmakuBindingKey(route{source: current.source, item: item})); found {
		var binding danmakuBinding
		if json.Unmarshal([]byte(raw), &binding) == nil && binding.ID > 0 && binding.Count != nil {
			info = append(info, core.View{Class: "flex-row items-center gap-1", Children: []core.Node{
				core.Icon{SVG: lucide.MessageSquareText(), Class: "size-3 text-fuchsia-500"},
				core.Text{Content: locales.SeriesDanmakuCount(cx, strconv.Itoa(*binding.Count)), Class: "text-[11px] " + setupMuted},
			}})
		}
	}
	return base.Button{Label: label, TestID: "child-" + item.ID, Selected: func() bool { return resume },
		Class: "flex-row gap-3 rounded-xl p-2 selected:bg-violet-500/10", Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
		OnPress: func(core.PressEvent) { play() }, OnLongPress: func(core.PressEvent) { more() },
		Children: []core.Node{
			core.View{TestID: "episode-art-" + item.ID, Class: "relative h-[72px] w-32 shrink-0 overflow-hidden rounded-lg shadow-sm shadow-black/15", Children: art},
			core.View{Class: "min-w-0 flex-1 gap-1", Children: info},
		}}
}

func seriesEmpty(cx *core.Scope, icon *core.SVG, message string, retry func()) core.Node {
	children := []core.Node{
		core.Icon{SVG: icon, Class: "size-8 opacity-60 " + setupMuted},
		core.Text{Content: message, Class: "text-center text-sm " + setupMuted},
	}
	if retry != nil {
		children = append(children, ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: retry})
	}
	return core.View{Class: "mx-4 items-center gap-2 rounded-2xl bg-white px-5 py-10 dark:bg-[#1a1b25] " + setupBorder,
		TestID: "series-episode-empty", Role: core.RoleGroup, Children: children}
}

func seriesDuration(cx *core.Scope, duration time.Duration) string {
	if duration >= time.Minute {
		return locales.HomeDuration(cx, strconv.Itoa(int(duration/time.Minute)))
	}
	return playbackClock(duration)
}

func episodeProgress(item media.Item) float32 {
	return float32(min(100, max(0, 100*float64(item.Position)/float64(item.Duration))))
}
