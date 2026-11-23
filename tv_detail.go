package main

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) tvMediaDetail(cx *core.Scope, current route, copies func() []ui.Option[copyKey], selectCopy func(copyKey)) core.Node {
	l := tvLayoutFor(cx)
	state := a.movieDetailState(cx, current, true)
	trailer := func() media.Item { return media.Item{} }
	var related *core.Resource[[]media.Item]
	if current.item.Kind == media.Movie {
		extras := a.movieFeatures(cx, current, false)
		related = a.movieFeatures(cx, current, true)
		trailer = func() media.Item {
			items, _ := extras.Get()
			if idx := slices.IndexFunc(items, func(item media.Item) bool { return item.Trailer && a.parentalRules().visible(current.source.ID, item) }); idx >= 0 {
				return items[idx]
			}
			return media.Item{}
		}
	}
	chooseDownload, downloadDialog := a.seasonDownload(cx, current, func() []media.Item {
		result, _ := state.loaded.Get()
		return result.children
	}, nil)
	hero := a.tvDetailHero(cx, l, current, state, trailer, copies, selectCopy, chooseDownload)
	content := []core.Node{homeAmbient(false, true), homeAmbient(true, true), state.personal.error}
	content = append(content, core.Dynamic(func(cx *core.Scope) core.Node {
		result, ready := state.loaded.Get()
		if !ready || result.failure == nil {
			return nil
		}
		return core.View{Class: "gap-2", Style: core.Style{Padding: l.padding(96, 0)}, Children: []core.Node{
			notice(errorText(cx, result.failure)), ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: state.loaded.Refetch},
		}}
	}), core.Dynamic(func(cx *core.Scope) core.Node {
		people := state.item().People
		if len(people) == 0 {
			return nil
		}
		return tvDetailCast(cx, l, people)
	}), core.Dynamic(func(cx *core.Scope) core.Node {
		title, id := locales.Episodes(cx), "tv-detail-episodes"
		var items []media.Item
		if related != nil {
			title, id = locales.RelatedMovies(cx), "tv-detail-related"
			items, _ = related.Get()
		} else if result, ready := state.loaded.Get(); ready {
			items = result.children
			if current.item.Kind == media.Series && slices.ContainsFunc(items, func(x media.Item) bool { return x.Kind == media.Season }) {
				title = locales.Seasons(cx)
			}
		}
		entries := make([]catalogItem, 0, len(items))
		seen := map[string]bool{current.item.ID: true}
		for _, item := range items {
			if item.ID == "" || seen[item.ID] || (related != nil && item.Kind != media.Movie) {
				continue
			}
			seen[item.ID] = true
			item = attachSeriesParent(current.item, item)
			if !a.parentalRules().visible(current.source.ID, item) {
				continue
			}
			entry := catalogItem{source: current.source, item: item}
			if item.Kind == media.Episode {
				if saved, ok := a.seriesDownload(current.source, item, a.downloads.entries.Get()); ok && saved.State == downloadComplete {
					entry.downloadID = saved.ID
				}
			}
			entries = append(entries, entry)
		}
		if len(entries) == 0 {
			return nil
		}
		return a.tvPosters(cx, l, id, title, "bg-violet-500", nil, nil, entries)
	}))
	return core.Scroll{Class: "flex-1 " + tvBackground, HideIndicator: true, TestID: "tv-detail-scroll", Children: []core.Node{
		hero,
		core.View{Class: "relative", Style: core.Style{Gap: l.points(56), Padding: core.Edges{Top: l.points(16), Bottom: l.points(96)}}, Children: content},
		downloadDialog,
	}}
}

func (a *application) tvDetailHero(cx *core.Scope, l tvLayout, current route, state movieDetailState, trailer func() media.Item,
	copies func() []ui.Option[copyKey], selectCopy func(copyKey), chooseDownload func()) core.Node {
	back := l.action("back", locales.Back(cx), lucide.ArrowLeft(), tvDetailAction, a.back)
	back.Class = "flex-row items-center justify-center rounded-full border border-white/20 bg-black/35"
	back.Style.Height, back.Style.Padding, back.Style.Gap = l.length(42), l.padding(16, 0), l.points(8)
	back.Children = []core.Node{l.icon(lucide.ArrowLeft(), 20, "text-white"), l.text(locales.Back(cx), 18, "font-medium text-white")}
	whenPlayable := playable(current.item)
	focus := &core.Ref{}
	if !whenPlayable {
		back.Ref = focus
	}
	cx.Post(focus.Focus)

	actions := tvDetailActions(cx, l, a, current, state, trailer, copies, selectCopy, focus, chooseDownload)
	return core.View{Class: "relative bg-[#171720]", TestID: "tv-detail-hero", Style: core.Style{Height: l.length(980)}, Children: []core.Node{
		core.Dynamic(func(*core.Scope) core.Node {
			item := state.item()
			return homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0")
		}),
		core.View{Class: "absolute inset-y-0 left-0 bg-linear-to-r from-black/85 via-black/60 to-transparent", Style: core.Style{Width: l.length(1440)}},
		core.View{Class: "absolute inset-x-0 top-0 bg-linear-to-b from-black/55 to-transparent", Style: core.Style{Height: l.length(192)}},
		core.View{Class: "absolute inset-x-0 bottom-0 bg-linear-to-b from-transparent to-waygu-page", Style: core.Style{Height: l.length(80)}},
		core.View{Class: "absolute flex-row items-center justify-between",
			Style: core.Style{Top: l.length(48), Left: l.length(96), Right: l.length(96)}, Children: []core.Node{
				core.View{Class: "flex-row items-center", Style: core.Style{Gap: l.points(12)}, Children: []core.Node{
					back, l.text(locales.TvBackHint(cx), 20, "text-white/60"),
				}},
				tvClock(cx, l),
			}},
		core.View{Class: "absolute", Style: core.Style{Top: l.length(170), Left: l.length(96), Width: l.length(900), Gap: l.points(28)}, Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				return tvDetailFacts(cx, l, current.source, state.item(), state.active())
			}),
			actions,
		}},
	}}
}

func tvDetailFacts(cx *core.Scope, l tvLayout, src source, item media.Item, version media.Version) core.Node {
	headline := l.text(item.Title, 72, "font-bold tracking-tight text-white leading-none")
	headline.TestID, headline.Role = "tv-detail-title", core.RoleHeader
	var logo core.Node = headline
	if item.Logo != "" {
		failed := core.NewSignal(cx, false)
		logo = core.Dynamic(func(*core.Scope) core.Node {
			if failed.Get() {
				return headline
			}
			return core.Image{Source: core.Src(item.Logo), ResizeMode: core.Contain, Label: item.Title, TestID: "tv-detail-logo",
				Class: "object-left", Style: core.Style{Width: l.length(480), Height: l.length(112)},
				OnError: func(error) { failed.Set(true) }}
		})
	}
	subtitle := item.OriginalTitle
	if item.Logo != "" {
		subtitle = item.Title
		if item.OriginalTitle != "" && item.OriginalTitle != item.Title {
			subtitle += " · " + item.OriginalTitle
		}
	} else if subtitle == item.Title {
		subtitle = ""
	}
	var meta []string
	if item.Year > 0 {
		meta = append(meta, strconv.Itoa(item.Year))
	}
	if len(item.Genres) > 0 {
		meta = append(meta, strings.Join(item.Genres[:min(len(item.Genres), 2)], " / "))
	}
	if item.Duration > 0 {
		meta = append(meta, seriesDuration(cx, item.Duration))
	}
	if item.ContentRating != "" {
		meta = append(meta, item.ContentRating)
	}
	detailMeta := l.text(strings.Join(meta, " · "), 24, "text-white/80 min-w-0 shrink")
	facts := core.View{Class: "flex-row items-center", Style: core.Style{Gap: l.points(20)}, Children: []core.Node{detailMeta}}
	if item.Rating > 0 {
		rating := core.View{Class: "flex-row items-center", Style: core.Style{Gap: l.points(8)}, Children: []core.Node{
			l.text(locales.TvRating(cx), 16, "rounded bg-emerald-600 px-2 py-0.5 font-bold text-white"),
			l.text(strconv.FormatFloat(item.Rating, 'f', 1, 64), 24, "font-semibold text-white"),
		}}
		facts.Children = append([]core.Node{rating}, facts.Children...)
	}
	dotClass, _ := tvProviderStyle(src.Provider)
	tag := core.View{Class: "flex-row items-center rounded-md border border-emerald-400/30 bg-emerald-500/25",
		Style: core.Style{Gap: l.points(8), Padding: l.padding(10, 4)}, Children: []core.Node{
			core.View{Class: "rounded-full " + dotClass, Style: core.Style{Width: l.length(8), Height: l.length(8)}},
			l.text(src.Name, 18, "font-medium text-emerald-200"),
		}}
	badges := core.View{Class: "flex-row flex-wrap items-center", Style: core.Style{Gap: l.points(12)}, TestID: "tv-detail-formats"}
	for _, format := range tvDetailFormats(cx, version) {
		badge := l.text(format, 18, "rounded-md border border-white/40 font-bold text-white")
		badge.Style.Padding = l.padding(10, 4)
		badges.Children = append(badges.Children, badge)
	}
	badges.Children = append(badges.Children, tag)
	synopsis := l.text(cmp.Or(item.Overview, locales.NoOverview(cx)), 24, "leading-relaxed text-white/80")
	synopsis.MaxLines, synopsis.TestID = 2, "detail-synopsis"
	credits := tvDetailByline(cx, l, item.People)
	return core.View{Style: core.Style{Gap: l.points(28)}, Children: []core.Node{
		core.View{Class: "justify-center", Style: core.Style{Height: l.length(112)}, Children: []core.Node{logo}},
		core.View{Style: core.Style{MinHeight: l.length(36)}, Children: []core.Node{l.text(subtitle, 30, "font-semibold text-white")}},
		facts, badges, synopsis, credits,
	}}
}

func tvDetailFormats(cx *core.Scope, version media.Version) []string {
	formats := []string{detailResolution(version), detailVideoRange(version)}
	hasChinese := false
	for _, track := range version.Tracks {
		if track.HasAtmos() && !slices.Contains(formats, "Dolby Atmos") {
			formats = append(formats, "Dolby Atmos")
		}
		if track.Type == "subtitle" {
			language := strings.ToLower(track.Language)
			hasChinese = hasChinese || language == "chi" || language == "zho" || language == "cmn" || language == "zh" || strings.HasPrefix(language, "zh-")
		}
	}
	if hasChinese {
		formats = append(formats, locales.TvChineseSubtitles(cx))
	}
	return slices.DeleteFunc(formats, func(value string) bool { return value == "" })
}

func tvDetailByline(cx *core.Scope, l tvLayout, people []media.Person) core.Node {
	var directors, actors []string
	for _, person := range people {
		switch {
		case person.Director:
			directors = append(directors, person.Name)
		case person.Kind == "actor" && len(actors) < 3:
			actors = append(actors, person.Name)
		}
	}
	var details []string
	if len(directors) > 0 {
		details = append(details, locales.MovieDirectorLine(cx, strings.Join(directors, " / ")))
	}
	if len(actors) > 0 {
		details = append(details, locales.TvStarring(cx, strings.Join(actors, " / ")))
	}
	if len(details) == 0 {
		return nil
	}
	return l.text(strings.Join(details, " · "), 20, "text-white/60")
}

func tvDetailActions(cx *core.Scope, l tvLayout, a *application, current route, state movieDetailState, trailer func() media.Item,
	copies func() []ui.Option[copyKey], selectCopy func(copyKey), focus *core.Ref, chooseDownload func()) core.Node {
	var buttons []core.Node
	if playable(current.item) {
		text := func() string {
			if item := state.item(); canResume(item) {
				return locales.TvResumeAt(cx, playbackClock(item.Position))
			}
			return locales.Play(cx)
		}
		play := l.action("detail-play", locales.Play(cx), lucide.Play(), tvPlayAction, func() { state.play(true) })
		play.Ref, play.Label, play.LabelFunc = focus, "", text
		label := play.Children[1].(core.Text)
		label.Content, label.ContentFunc = "", text
		play.Children[1] = label
		buttons = append(buttons, play, core.Show{When: func() bool { return canResume(state.item()) }, Then: func(cx *core.Scope) core.Node {
			return tvDetailSecondary(l, "detail-from-start", locales.FromStart(cx), lucide.RotateCcw(), func() { state.play(false) })
		}})
		buttons = append(buttons, core.Show{When: func() bool { return trailer().ID != "" }, Then: func(cx *core.Scope) core.Node {
			return tvDetailSecondary(l, "tv-detail-trailer", locales.TvTrailer(cx), lucide.Clapperboard(), func() {
				a.push(route{screen: screenPlayer, source: current.source, item: core.Untracked(cx, trailer)})
			})
		}})
	}
	favorite := l.action("detail-favorite", locales.Favorite(cx), lucide.Heart(), tvIconAction, state.personal.favorite)
	favorite.Class += " selected:bg-violet-500/50 disabled:opacity-50"
	favorite.Selected = func() bool { return state.item().Favorite }
	favorite.Disabled = func() bool {
		result, ready := state.loaded.Get()
		return !ready || !result.canFavorite || state.personal.busy()
	}
	favorite.Label = ""
	favorite.LabelFunc = func() string {
		if state.item().Favorite {
			return locales.RemoveFavorite(cx)
		}
		return locales.Favorite(cx)
	}
	if playable(current.item) || current.item.Kind == media.Series {
		buttons = append(buttons, favorite)
	}
	if chooseDownload != nil {
		buttons = append(buttons, core.Show{When: func() bool {
			result, _ := state.loaded.Get()
			return current.item.Kind == media.Season || slices.ContainsFunc(result.children, func(child media.Item) bool { return child.Kind == media.Season })
		}, Then: func(cx *core.Scope) core.Node {
			return tvDetailSecondary(l, seasonDownloadID, locales.DownloadSeason(cx), lucide.Download(), chooseDownload)
		}})
	}
	if current.item.Kind == media.Episode {
		buttons = append(buttons, a.downloadAction(cx, current,
			func(cx *core.Scope, id, label string, icon *core.SVG, _ bool, _ func() bool, press func()) core.Node {
				return tvDetailSecondary(l, id, label, icon, press)
			}, state.download))
	}
	buttons = append(buttons, tvDetailVersionChooser(cx, l, current, state, copies, selectCopy))
	return core.View{Style: core.Style{Gap: l.points(16), Padding: core.Edges{Top: l.points(8)}}, Children: []core.Node{
		core.View{Class: "flex-row flex-wrap items-center", Style: core.Style{Gap: l.points(16), Width: l.length(1280)}, Children: buttons},
		core.Dynamic(func(cx *core.Scope) core.Node {
			item := state.item()
			if item.Duration <= 0 || !canResume(item) {
				return nil
			}
			label := locales.TvDetailProgress(cx, playbackClock(item.Position), seriesDuration(cx, item.Duration-item.Position))
			return core.View{Class: "flex-row items-center", Style: core.Style{Gap: l.points(16), Width: l.length(1200)}, Children: []core.Node{
				core.View{Class: "overflow-hidden rounded-full", Style: core.Style{Width: l.length(620)}, Children: []core.Node{
					base.Progress{TestID: "detail-progress", Label: label, Value: func() float32 { return episodeProgress(item) },
						TrackClass: "rounded-full bg-white/20", IndicatorClass: "rounded-full bg-violet-500",
						TrackStyle: core.Style{Height: l.length(8)}, IndicatorStyle: core.Style{Height: l.length(8)}},
				}},
				l.text(label, 18, "text-white/70"),
			}}
		}),
	}}
}

func tvDetailSecondary(l tvLayout, id, label string, icon *core.SVG, press func()) core.View {
	button := l.action(id, label, icon, tvDetailAction, press)
	button.Style.Padding = l.padding(36, 0)
	button.Children[1] = l.text(label, 24, "font-medium text-white")
	return button
}
