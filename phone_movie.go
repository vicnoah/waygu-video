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

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) phoneMovieDetail(cx *core.Scope, current route, copies func() []ui.Option[copyKey], selectCopy func(copyKey)) core.Node {
	state := a.movieDetailState(cx, current, false)
	castOpen := core.NewSignal(cx, false)
	playMenu := core.NewSignal(cx, false)
	matchOpen := core.NewSignal(cx, false)
	match := core.Dynamic(func(cx *core.Scope) core.Node {
		return a.phoneMovieDanmaku(cx, current, func() { matchOpen.Set(true) })
	})
	extras := a.movieFeatures(cx, current, false)
	related := a.movieFeatures(cx, current, true)
	reload := func() {
		state.loaded.Refetch()
		state.available.Refetch()
		extras.Refetch()
		related.Refetch()
	}
	hero := core.Dynamic(func(cx *core.Scope) core.Node {
		var cast func()
		if canCast(cx) && current.downloadID == "" {
			cast = state.cast
		}
		return a.phoneMovieHero(cx, state.item(), state.active(), reload, cast)
	})
	actions := core.Dynamic(func(cx *core.Scope) core.Node {
		item := state.item()
		result, ready := state.loaded.Get()
		start := locales.Play(cx)
		var hold func(core.PressEvent)
		if item.Position > 0 {
			start = locales.PhoneMovieResumeAt(cx, playbackClock(item.Position))
			hold = func(core.PressEvent) { playMenu.Set(true) }
		}
		button := base.Button{Label: start, TestID: "detail-play",
			Class: "h-[52px] flex-row items-center justify-center gap-2 rounded-2xl bg-linear-to-r from-violet-500 to-fuchsia-500 shadow-lg shadow-violet-500/30",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { state.play(true) }, OnLongPress: hold,
			Children: []core.Node{
				core.Icon{SVG: lucide.Play(), Class: "size-5 text-white"},
				core.Text{Content: start, Class: "text-base font-semibold text-white"},
			}}
		var tools []core.Node
		if ready {
			tools = append(tools, a.downloadAction(cx, current, phoneMovieAction, func() route {
				download := current
				download.item, download.versionID = item, state.selected.GetUntracked()
				return download
			}))
		}
		tools = append(tools, state.personal.buttons(cx, result, phoneMovieAction)...)
		tools = append(tools, phoneMovieAction(cx, "detail-share", locales.ShareTitle(cx), lucide.Share2(), false, nil, func() {
			cx.Share(core.ShareOptions{Text: item.Title}, func(shown bool, err error) {
				if !shown && err != nil {
					cx.SetClipboard(item.Title)
				}
			})
		}), phoneMovieMore(cx, false, reload, item.Position > 0, func() { state.play(false) }))
		return core.View{Class: "gap-4 px-5", Children: []core.Node{
			button,
			core.View{Class: "flex-row rounded-2xl bg-white py-3 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder, Children: tools},
		}}
	})
	moreDetails := core.Dynamic(func(cx *core.Scope) core.Node {
		item, version := state.item(), state.active()
		result, ready := state.loaded.Get()
		var children []core.Node
		if len(item.People) != 0 {
			children = append(children, phoneMovieCast(cx, item.People, func() { castOpen.Set(true) }))
		}
		if clips, _ := extras.Get(); len(clips) != 0 {
			clips = a.parentalVisibleItems(current.source.ID, clips)
			if len(clips) != 0 {
				children = append(children, a.phoneMovieExtras(cx, current.source, clips))
			}
		}
		if matches, _ := related.Get(); len(matches) != 0 {
			var cards []core.Node
			seen := map[string]bool{item.ID: true}
			for _, match := range matches {
				if match.Kind == media.Movie && !seen[match.ID] && a.parentalRules().visible(current.source.ID, match) {
					seen[match.ID] = true
					cards = append(cards, a.homePosterCard(cx, catalogItem{source: current.source, item: match}))
				}
			}
			if len(cards) > 0 {
				children = append(children, core.View{Class: "gap-3", Children: []core.Node{
					homeSectionTitle(cx, locales.RelatedMovies(cx), "", nil, "", "px-5"),
					homeRail("detail-related", "gap-3", "px-5", cards),
				}})
			}
		}
		if version.ID != "" {
			children = append(children, phoneMovieFile(cx, version))
		}
		if ready && result.failure != nil && current.downloadID == "" {
			children = append(children, core.View{Class: "gap-2 px-5", Children: []core.Node{
				notice(errorText(cx, result.failure)),
				ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: reload},
			}})
		}
		if state.available.Err() != nil && current.downloadID == "" {
			children = append(children, core.View{Class: "px-5", Children: []core.Node{
				ui.Button{Text: locales.ReloadPlaybackVersions(cx), Variant: ui.ButtonOutline, OnPress: state.available.Refetch},
			}})
		}
		return core.Group(children)
	})
	body := core.View{Class: "gap-7", Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node { return phoneMovieSynopsis(cx, state.item()) }),
		phoneMovieOptions(cx, current, state, copies, selectCopy, match),
		moreDetails,
	}}
	return core.View{Class: "relative flex-1 bg-waygu-page", Children: []core.Node{
		core.Scroll{Class: "flex-1", TestID: "phone-movie-scroll", Children: []core.Node{
			core.View{Class: "relative", Style: core.Style{PaddingEnv: core.EnvEdges{
				Left: core.EnvSafeArea, Right: core.EnvSafeArea, Bottom: core.EnvSafeArea,
			}}, Children: []core.Node{
				core.SVGView{Class: "absolute inset-x-0 top-[400px] h-[1100px] w-full dark:hidden", SVG: welcomeAmbientLight},
				core.SVGView{Class: "absolute inset-x-0 top-[400px] hidden h-[1100px] w-full dark:flex", SVG: welcomeAmbientDark},
				hero,
				core.View{Class: "gap-7 pt-5 pb-10", Children: []core.Node{actions, state.personal.error, body}},
			}},
		}},
		ui.Dialog{Title: locales.CastAndCrew(cx), TestID: "detail-cast-list", Open: castOpen.Get,
			OnOpenChange: func(open bool, _ base.Reason) { castOpen.Set(open) },
			Children: []core.Node{core.Show{When: castOpen.Get, Then: func(cx *core.Scope) core.Node {
				return phoneMovieCastList(cx, state.item().People)
			}}}, Footer: []core.Node{ui.Button{
				Text: locales.Back(cx), TestID: "detail-close-cast", OnPress: func() { castOpen.Set(false) },
			}}},
		ui.Dialog{Title: locales.PlaybackOptions(cx), TestID: "detail-play-menu", Open: playMenu.Get,
			OnOpenChange: func(open bool, _ base.Reason) { playMenu.Set(open) }, Children: []core.Node{
				ui.Button{Text: locales.Resume(cx), Icon: lucide.Play, OnPress: func() { playMenu.Set(false); state.play(true) }},
				ui.Button{Text: locales.FromStart(cx), Icon: lucide.RotateCcw, TestID: "detail-from-start",
					Variant: ui.ButtonOutline, OnPress: func() { playMenu.Set(false); state.play(false) }},
			}},
		ui.Dialog{Title: locales.Danmaku(cx), TestID: "detail-danmaku-dialog", Open: matchOpen.Get,
			OnOpenChange: func(open bool, _ base.Reason) { matchOpen.Set(open) },
			Children: []core.Node{core.Show{When: matchOpen.Get, Then: func(cx *core.Scope) core.Node {
				return core.Scroll{Class: "max-h-[350px]", Children: []core.Node{
					a.onlineDanmaku(cx, current, media.Stream{}, nil, func() { matchOpen.Set(false) }, nil),
				}}
			}}}, Footer: []core.Node{ui.Button{Text: locales.Back(cx), TestID: "detail-close-danmaku",
				OnPress: func() { matchOpen.Set(false) }}}},
	}}
}

func (a *application) movieFeatures(cx *core.Scope, current route, recommendations bool) *core.Resource[[]media.Item] {
	return core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) ([]media.Item, error) {
		if current.downloadID != "" {
			return nil, nil
		}
		client, err := a.client(ctx, current.source)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		if !recommendations {
			if features, ok := client.(media.MovieFeaturesClient); ok {
				items, err := features.Extras(ctx, current.item)
				if len(items) != 0 {
					return items, nil
				}
				return nil, err
			}
			return nil, nil
		}
		return a.movieRecommendations(ctx, current, client)
	})
}

func (a *application) movieRecommendations(ctx context.Context, current route, client media.Client) ([]media.Item, error) {
	movie := current.item
	if features, ok := client.(media.MovieFeaturesClient); ok {
		items, err := features.RelatedMovies(ctx, movie)
		if err == nil && slices.ContainsFunc(items, func(item media.Item) bool { return item.Kind == media.Movie && item.ID != movie.ID }) {
			return items, nil
		}
	}
	if len(movie.Genres) == 0 || movie.LibraryID == "" {
		return nil, nil
	}
	page, err := client.Catalog(ctx, media.Query{LibraryID: movie.LibraryID, Limit: 48})
	if err != nil {
		return nil, err
	}
	var matches []media.Item
	for _, item := range page.Items {
		if item.Kind == media.Movie && item.ID != movie.ID && slices.ContainsFunc(item.Genres, func(genre string) bool {
			return slices.ContainsFunc(movie.Genres, func(candidate string) bool { return strings.EqualFold(candidate, genre) })
		}) {
			item.LibraryID = movie.LibraryID
			matches = append(matches, item)
			if len(matches) == 12 {
				break
			}
		}
	}
	return matches, nil
}

func (a *application) phoneMovieHero(cx *core.Scope, item media.Item, version media.Version, reload, cast func()) core.Node {
	title := []core.Node{core.Text{Content: item.Title, Role: core.RoleHeader, TestID: "phone-movie-title",
		Class: "text-4xl font-bold tracking-tight text-white", MaxLines: 2}}
	if item.OriginalTitle != "" && item.OriginalTitle != item.Title {
		title = append(title, core.Text{Content: item.OriginalTitle, Class: "text-sm text-white/80", MaxLines: 1})
	}
	var facts []string
	if item.Year > 0 {
		facts = append(facts, strconv.Itoa(item.Year))
	}
	if item.Duration >= time.Minute {
		facts = append(facts, locales.HomeDuration(cx, strconv.Itoa(int(item.Duration/time.Minute))))
	}
	if len(item.Genres) > 0 {
		facts = append(facts, strings.Join(item.Genres, " / "))
	}
	if item.ContentRating != "" {
		facts = append(facts, item.ContentRating)
	}
	if len(facts) != 0 {
		title = append(title, core.Text{Content: strings.Join(facts, " · "), Class: "text-xs text-white/80", MaxLines: 2})
	}
	if item.Rating > 0 {
		title = append(title, core.View{Class: "flex-row items-center gap-1", Children: []core.Node{
			core.Icon{SVG: lucide.Star(), Class: "size-4 text-amber-400"},
			core.Text{Content: strconv.FormatFloat(item.Rating, 'f', 1, 64), Class: "text-sm font-semibold text-white"},
		}})
	}
	var badges []core.Node
	for _, label := range []string{detailResolution(version), detailVideoRange(version)} {
		if label != "" {
			badges = append(badges, core.Text{Content: label, Class: "rounded-md bg-white/15 px-1.5 py-0.5 text-[10px] font-semibold text-white"})
		}
	}
	if len(badges) != 0 {
		title = append(title, core.View{Class: "flex-row flex-wrap gap-1.5", Children: badges})
	}
	var tools []core.Node
	if cast != nil {
		tools = append(tools, phoneHeroAction(cx, "detail-cast-device", locales.PhonePlayerCast(cx), lucide.Cast(), false, nil, cast))
	}
	return a.phoneMediaHero(cx, item, "h-[460px]", "phone-movie-hero", title, append(tools, phoneMovieMore(cx, true, reload, false, nil))...)
}

func (a *application) phoneMediaHero(cx *core.Scope, item media.Item, height, id string, title []core.Node, tools ...core.Node) core.Node {
	art := homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0")
	return a.phoneMediaHeroFrame(cx, art, height, id, title, tools...)
}

func (a *application) phoneMediaHeroFrame(cx *core.Scope, art core.Node, height, id string, title []core.Node, tools ...core.Node) core.Node {
	return core.View{Class: "relative shrink-0 overflow-hidden " + height, TestID: id, Children: []core.Node{
		art,
		core.View{Class: "absolute inset-0 bg-linear-to-t from-black/90 via-black/35 to-black/45"},
		core.View{Class: "absolute inset-x-0 bottom-0 h-12 bg-linear-to-b from-transparent to-waygu-page"},
		core.View{Class: "absolute inset-x-0 top-0 flex-row items-center justify-between px-4 pt-1", Style: core.Style{
			PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea},
		}, Children: []core.Node{
			base.Button{Label: locales.Back(cx), TestID: "back", Class: "size-9 items-center justify-center rounded-full bg-black/40",
				Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { a.back() },
				Children: []core.Node{core.Icon{SVG: lucide.ChevronLeft(), Class: "size-5 text-white"}}},
			core.View{Class: "flex-row gap-2", Children: tools},
		}},
		core.View{Class: "absolute inset-x-0 bottom-0 gap-2.5 px-5 pb-12", Children: title},
	}}
}

func phoneMovieAction(cx *core.Scope, id, label string, icon *core.SVG, selected bool, disabled func() bool, press func()) core.Node {
	return base.Button{Label: label, TestID: id, Selected: func() bool { return selected }, Disabled: disabled,
		Style:   core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		Class:   "min-w-0 flex-1 basis-0 items-center justify-start gap-1.5 rounded-lg selected:bg-violet-500/10 disabled:opacity-50",
		OnPress: func(core.PressEvent) { press() }, Children: []core.Node{
			core.Icon{SVG: icon, Class: "size-5 " + setupAccent},
			core.Text{Content: label, Class: "text-center text-[11px] " + setupMuted, MaxLines: 2},
		}}
}

func phoneMovieMore(cx *core.Scope, hero bool, reload func(), restart bool, playFromStart func()) core.Node {
	class, iconClass, id := "min-w-0 flex-1 basis-0 rounded-lg", "size-5 "+setupAccent, "detail-more"
	if hero {
		class, iconClass, id = "size-9 rounded-full bg-black/40", "size-4 text-white", "detail-hero-more"
	}
	content := []core.Node{core.Icon{SVG: lucide.Ellipsis(), Class: iconClass}}
	if !hero {
		content = append(content, core.Text{Content: locales.DetailMore(cx), Class: "text-[11px] " + setupMuted})
	}
	menu := []core.Node{ui.MenuItem{Text: locales.ReloadMovieDetail(cx), OnSelect: reload}}
	if restart {
		menu = append(menu, ui.MenuItem{Text: locales.FromStart(cx), OnSelect: playFromStart})
	}
	return base.Menu{Class: class, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		Label: locales.DetailMore(cx), TestID: id, PopupClass: tabletDetailPopup,
		Trigger:  core.View{Class: "flex-1 items-center justify-center gap-1.5", Children: content},
		Children: menu}
}

func phoneMovieSynopsis(cx *core.Scope, item media.Item) core.Node {
	var directors, cast []string
	for _, person := range item.People {
		if person.Director {
			directors = append(directors, person.Name)
		} else if person.Kind != "composer" && len(cast) < 4 {
			cast = append(cast, person.Name)
		}
	}
	var credits []string
	if len(directors) > 0 {
		credits = append(credits, locales.MovieDirectorLine(cx, strings.Join(directors, " / ")))
	}
	if len(cast) > 0 {
		credits = append(credits, locales.MovieCastLine(cx, strings.Join(cast, " / ")))
	}
	rows := []core.Node{core.Text{Content: cmp.Or(item.Overview, locales.NoOverview(cx)),
		TestID: "detail-synopsis", Class: "text-sm leading-relaxed text-zinc-900/80 dark:text-zinc-50/80", MaxLines: 3}}
	if len(credits) > 0 {
		rows = append(rows, core.Text{Content: strings.Join(credits, "   "), Class: "text-xs " + setupMuted, MaxLines: 2})
	}
	return core.View{Class: "gap-2 px-5", Children: rows}
}

func phoneMovieOptions(cx *core.Scope, current route, state movieDetailState,
	copies func() []ui.Option[copyKey], selectCopy func(copyKey), match core.Node) core.Node {
	versionChoice := core.Dynamic(func(cx *core.Scope) core.Node {
		versions, _ := state.available.Get()
		options := []ui.Option[string]{{Label: locales.AutomaticPlaybackVersion(cx)}}
		for i, candidate := range versions {
			options = append(options, ui.Option[string]{Label: versionLabel(cx, candidate, i+1), Value: candidate.ID})
		}
		label := locales.MetadataUnavailable(cx)
		if version := state.active(); version.ID != "" {
			label = versionLabel(cx, version, 1)
		}
		return phoneMovieChoice(cx, "playback-version", locales.VersionShort(cx), label, lucide.Layers(), "bg-violet-500",
			options, state.selected.Get(), state.chooseVersion)
	})
	// Source selection stays available while track details and extras load.
	sourceChoice := core.Dynamic(func(cx *core.Scope) core.Node {
		var options []ui.Option[copyKey]
		if copies != nil {
			options = copies()
		}
		return phoneMovieChoice(cx, "playback-source", locales.MovieSource(cx), current.source.Name, lucide.Server(), "bg-emerald-500",
			options, (catalogCopy{current.source, current.item}).key(), selectCopy)
	})
	tracks := core.Dynamic(func(cx *core.Scope) core.Node {
		version, choice := state.active(), state.tracks.Get()
		if choice.version != version.ID {
			choice = detailTrackChoice{version: version.ID}
		}
		audio, audioLabel, audioValue := detailTrackOptions(cx, version, choice, "audio")
		subtitles, subtitleLabel, subtitleValue := detailTrackOptions(cx, version, choice, "subtitle")
		return core.Group([]core.Node{
			phoneMovieChoice(cx, "detail-audio", locales.DetailAudio(cx), audioLabel, lucide.AudioLines(), "bg-sky-500",
				audio, audioValue, func(id string) { choice.audio = id; state.tracks.Set(choice) }),
			phoneMovieChoice(cx, "detail-subtitle", locales.Subtitles(cx), subtitleLabel, lucide.Captions(), "bg-amber-500",
				subtitles, subtitleValue, func(id string) { choice.subtitle = id; state.tracks.Set(choice) }),
		})
	})
	return setupGroup(locales.PlaybackOptions(cx), "", versionChoice, sourceChoice, tracks, match)
}

func (a *application) phoneMovieDanmaku(cx *core.Scope, current route, open func()) core.Node {
	var bound danmaku.DandanEpisode
	if raw, found := a.storage.GetItem(danmakuBindingKey(current)); found {
		_ = json.Unmarshal([]byte(raw), &bound)
	}
	status := locales.MovieDanmakuUnmatched(cx)
	if bound.ID > 0 {
		status = bound.Label()
	}
	children := []core.Node{
		setupBadge(lucide.MessageSquareText(), "bg-fuchsia-500"),
		core.Text{Content: locales.Danmaku(cx), Class: "min-w-0 flex-1 text-[15px] " + setupTitle},
		core.Text{Content: status, Class: "max-w-[56%] shrink text-[15px] " + setupMuted, MaxLines: 1},
	}
	const row = "min-h-12 flex-row items-center gap-3 bg-white px-4 py-2.5 dark:bg-[#1a1b25]"
	if current.downloadID != "" {
		return core.View{Class: row, Children: children}
	}
	return base.Button{Class: row, Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, Label: locales.Danmaku(cx),
		TestID: "detail-danmaku", OnPress: func(core.PressEvent) { open() },
		Children: append(children, core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 shrink-0 opacity-60 " + setupMuted})}
}

func phoneMovieChoice[T comparable](cx *core.Scope, id, label, display string, icon *core.SVG, tint string,
	options []ui.Option[T], value T, change func(T)) core.Node {
	class := "min-h-12 flex-row items-center gap-3 rounded bg-white px-4 py-2.5 dark:bg-[#1a1b25]"
	children := []core.Node{
		setupBadge(icon, tint),
		core.Text{Content: label, Class: "min-w-0 flex-1 text-[15px] " + setupTitle, MaxLines: 1},
		core.Text{Content: display, TestID: id + "-value", Class: "max-w-[56%] shrink text-[15px] " + setupMuted, MaxLines: 1},
	}
	if len(options) <= 1 || change == nil {
		return core.View{Class: class, Children: children}
	}
	children = append(children, core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 shrink-0 opacity-60 " + setupMuted})
	items := make([]base.SelectItem, 0, len(options))
	selected := ""
	for i, option := range options {
		key := strconv.Itoa(i)
		items = append(items, base.SelectItem{Value: key, Title: option.Label})
		if option.Value == value {
			selected = key
		}
	}
	return base.Select{Label: label, TestID: id,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, PopupClass: tabletDetailPopup, ItemClass: tabletDetailOption,
		Items: func() []base.SelectItem { return items }, Value: func() *string { return &selected },
		RenderItem: func(row base.SelectEntry) core.Node {
			return core.Text{Content: row.Title, Class: "text-sm " + setupTitle, MaxLines: 1, TestID: id + "-" + strconv.Itoa(row.Index)}
		},
		OnChange: func(key *string) {
			if key != nil {
				if index, err := strconv.Atoi(*key); err == nil && index >= 0 && index < len(options) {
					change(options[index].Value)
				}
			}
		},
		Trigger: core.View{Class: class, Children: children},
	}
}

func phoneMovieCast(cx *core.Scope, people []media.Person, open func()) core.Node {
	cards := make([]core.Node, 0, len(people))
	for _, person := range people {
		role := person.Role
		if person.Director {
			role = locales.Director(cx)
		}
		cards = append(cards, core.View{Class: "w-16 shrink-0 items-center gap-1.5", Children: []core.Node{
			core.View{Class: "relative size-16 overflow-hidden rounded-full border-2 border-white shadow-md shadow-black/10 dark:border-white/10",
				Children: []core.Node{homeArtwork(person.Portrait, "absolute inset-0")}},
			core.Text{Content: person.Name, Class: "text-[11px] " + setupTitle, MaxLines: 1},
			core.Text{Content: role, Class: "text-[10px] " + setupMuted, MaxLines: 1},
		}})
	}
	return core.View{Class: "gap-3", Children: []core.Node{
		homeSectionTitle(cx, locales.CastAndCrew(cx), locales.MovieViewAll(cx, strconv.Itoa(len(cards))),
			open, "detail-cast-all", "px-5"),
		homeRail("detail-cast", "gap-4", "px-5", cards),
	}}
}

func phoneMovieCastList(cx *core.Scope, people []media.Person) core.Node {
	items := make([]core.Node, 0, len(people))
	for _, person := range people {
		role := person.Role
		if person.Director {
			role = locales.Director(cx)
		}
		items = append(items, core.View{Class: "flex-row items-center justify-between gap-3 py-2", Children: []core.Node{
			core.Text{Content: person.Name, Class: "text-sm font-medium " + setupTitle},
			core.Text{Content: role, Class: "text-xs " + setupMuted},
		}})
	}
	return core.Scroll{Class: "max-h-[280px]", Children: []core.Node{core.View{Children: items}}}
}

func (a *application) phoneMovieExtras(cx *core.Scope, source source, clips []media.Item) core.Node {
	cards := make([]core.Node, 0, len(clips))
	for _, clip := range clips {
		title := clip.Title
		if clip.Duration > 0 {
			title += " · " + playbackClock(clip.Duration)
		}
		cards = append(cards, base.Button{Label: title, TestID: "detail-extra-" + clip.ID, Class: "w-56 shrink-0 gap-1.5 rounded-2xl",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) {
				a.push(route{screen: screenPlayer, source: source, item: clip})
			}, Children: []core.Node{
				core.View{Class: "relative h-32 overflow-hidden rounded-2xl shadow-lg shadow-black/15", Children: []core.Node{
					homeArtwork(cmp.Or(clip.Backdrop, clip.Poster), "absolute inset-0"),
					core.View{Class: "absolute inset-0 items-center justify-center bg-black/30",
						Children: []core.Node{core.Icon{SVG: lucide.CirclePlay(), Class: "size-9 text-white"}}},
				}},
				core.Text{Content: title, Class: "text-xs text-zinc-900/80 dark:text-zinc-50/80", MaxLines: 1},
			}})
	}
	return core.View{Class: "gap-3", Children: []core.Node{
		homeSectionTitle(cx, locales.MovieExtras(cx), "", nil, "", "px-5"),
		homeRail("detail-extras", "gap-3", "px-5", cards),
	}}
}

func phoneMovieFile(cx *core.Scope, version media.Version) core.Node {
	row := func(label string, values ...string) core.Node {
		values = slices.DeleteFunc(values, func(value string) bool { return value == "" })
		if len(values) == 0 {
			return nil
		}
		return core.View{Class: "flex-row", Children: []core.Node{
			core.Text{Content: label, Class: "w-16 shrink-0 text-xs " + setupMuted},
			core.Text{Content: strings.Join(values, " · "), Class: "min-w-0 flex-1 text-xs text-zinc-900/80 dark:text-zinc-50/80"},
		}}
	}
	var size, bitrate, dimensions string
	if version.Size > 0 {
		size = versionSize(version.Size)
	}
	if version.Bitrate > 0 {
		precision := 1
		if version.Bitrate < 1_000_000 {
			precision = 2
		}
		bitrate = locales.BitrateMegabits(cx, strconv.FormatFloat(float64(version.Bitrate)/1_000_000, 'f', precision, 64))
	}
	if version.Width > 0 && version.Height > 0 {
		dimensions = strconv.Itoa(version.Width) + "×" + strconv.Itoa(version.Height)
	}
	var audio, subtitles []string
	for _, track := range version.Tracks {
		switch track.Type {
		case "audio":
			audio = append(audio, trackLabel(cx, track))
		case "subtitle":
			subtitles = append(subtitles, trackLabel(cx, track))
		}
	}
	return core.View{Class: "mx-4 gap-3 rounded-2xl bg-white p-4 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder, TestID: "detail-file",
		Children: []core.Node{
			core.Text{Content: locales.FileInfo(cx), Class: "text-sm font-semibold " + setupTitle},
			row(locales.MovieFileVideo(cx), strings.ToUpper(version.VideoCodec), dimensions, detailVideoRange(version)),
			row(locales.DetailAudio(cx), audio...),
			row(locales.Subtitles(cx), subtitles...),
			row(locales.MovieFileBitrate(cx), bitrate, size, strings.ToUpper(version.Container)),
			row(locales.MovieFileName(cx), version.FileName),
		}}
}
