package main

import (
	"cmp"
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

const (
	tvMuted     = "text-zinc-500 dark:text-zinc-400"
	tvCardTitle = "text-zinc-900/70 dark:text-white/70"
)

type tvActionKind uint8

const (
	tvPlayAction tvActionKind = iota
	tvDetailAction
	tvIconAction
	tvShelfAction
)

func (l tvLayout) action(id, label string, icon *core.SVG, kind tvActionKind, press func()) core.View {
	class := "flex-row items-center justify-center rounded-full"
	style := core.Style{Height: l.length(80), Gap: l.points(12), Padding: l.padding(40, 0), FocusVisible: l.focusRing()}
	tint, size, font := "text-white", core.Points(28), core.Points(30)
	switch kind {
	case tvPlayAction:
		class += " bg-white shadow-2xl shadow-violet-500/40 motion-safe:focus-visible:scale-[1.02]"
		style.Padding = l.padding(48, 0)
		tint, size = "text-black", 32
	case tvDetailAction, tvIconAction:
		class += " border border-white/20 bg-white/15"
		if kind == tvIconAction {
			style.Width, style.Padding, size = l.length(80), core.Edges{}, 32
		}
	case tvShelfAction:
		class += " border border-zinc-900/8 bg-zinc-900/5 dark:border-white/10 dark:bg-white/10"
		style.Height, style.Gap, style.Padding = l.length(44), l.points(4), l.padding(20, 0)
		tint, size, font = tvMuted, 20, 20
	}

	children := []core.Node{l.icon(icon, size, tint)}
	if kind != tvIconAction {
		weight := "font-medium "
		if kind == tvPlayAction {
			weight = "font-semibold "
		}
		labelNode := l.text(label, font, weight+tint)
		if kind == tvShelfAction {
			children = append([]core.Node{labelNode}, children...)
		} else {
			children = append(children, labelNode)
		}
	}
	return core.View{TestID: id, Role: core.RoleButton, Label: label, Class: class, Style: style,
		OnPress: func(core.PressEvent) { press() }, Children: children}
}

func (a *application) tvFeatured(cx *core.Scope, l tvLayout, titles []catalogItem, nav core.Node) core.Node {
	index := core.NewSignal(cx, 0)
	focused := core.NewSignal(cx, false)
	firstAction := &core.Ref{}
	cx.Post(firstAction.Focus)
	carouselActive := func() bool {
		return focused.Get() && !cx.Env().ReduceMotion() && cx.Env().Lifecycle() == core.LifecycleActive
	}
	cx.Effect(func() {
		current := index.Get()
		if len(titles) > 1 && carouselActive() {
			stop := cx.AfterFunc(6*time.Second, func() {
				if focused.GetUntracked() && current == index.GetUntracked() {
					index.Set((current + 1) % len(titles))
				}
			})
			cx.Cleanup(func() { stop() })
		}
	})
	current := func() catalogItem { return titles[index.GetUntracked()] }
	detail := l.action("home-hero-details", locales.Details(cx), lucide.Info(), tvDetailAction, func() { a.openCatalogEntry(current()) })
	var actions []core.Node
	// Recommendation updates leave the primary actions mounted so the remote keeps its focus.
	if playable(titles[0].item) {
		play := l.action("home-hero-play", locales.Play(cx), lucide.Play(), tvPlayAction, func() { a.playCatalogEntry(current()) })
		play.Ref = firstAction
		actions = append(actions, play)
	} else {
		detail.Ref = firstAction
	}
	actions = append(actions, detail, core.Dynamic(func(cx *core.Scope) core.Node {
		return a.tvHeroFavorite(cx, l, titles[index.Get()])
	}))

	return core.View{TestID: "tv-featured", Class: "relative bg-[#171720]",
		Style: core.Style{Height: l.length(900)}, Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				return a.tvFeaturedBackdrop(cx, titles[index.Get()], carouselActive)
			}),
			core.View{Class: "absolute inset-y-0 left-0 bg-linear-to-r from-black/85 via-black/55 to-transparent", Style: core.Style{Width: l.length(1360)}},
			core.View{Class: "absolute inset-x-0 top-0 bg-linear-to-b from-black/60 to-transparent", Style: core.Style{Height: l.length(224)}},
			core.View{Class: "absolute inset-x-0 bottom-0 bg-linear-to-b from-transparent to-waygu-page", Style: core.Style{Height: l.length(96)}},
			core.View{Class: "absolute inset-x-0 top-0", Children: []core.Node{nav}},
			core.View{Class: "absolute", Style: core.Style{Top: l.length(230), Left: l.length(96), Width: l.length(820), Gap: l.points(28)}, Children: []core.Node{
				core.Dynamic(func(cx *core.Scope) core.Node { return tvFeaturedFacts(cx, l, titles[index.Get()]) }),
				core.View{Class: "flex-row items-center", Style: core.Style{Gap: l.points(20), Padding: core.Edges{Top: l.points(8)}},
					OnFocusIn: func() { focused.Set(true) }, OnFocusOut: func() { focused.Set(false) },
					Children: actions},

				core.Dynamic(func(*core.Scope) core.Node {
					dots := make([]core.Node, len(titles))
					selected := index.Get()
					for i := range dots {
						width, tint := core.Points(8), "bg-white/40"
						if i == selected {
							width, tint = 48, "bg-white"
						}
						dots[i] = core.View{TestID: "tv-featured-dot-" + strconv.Itoa(i), Class: "rounded-full " + tint,
							Selected: func() bool { return i == selected }, Style: core.Style{Width: l.length(width), Height: l.length(8)}}
					}
					return core.View{TestID: "tv-featured-dots", Class: "flex-row", Style: core.Style{Gap: l.points(12), Padding: core.Edges{Top: l.points(16)}}, Children: dots}
				}),
			}},
		}}
}

func tvFeaturedFacts(cx *core.Scope, l tvLayout, entry catalogItem) core.Node {
	item := entry.item
	dot, _ := tvProviderStyle(entry.source.Provider)
	source := core.View{Class: "flex-row items-center rounded-full border border-emerald-400/30 bg-emerald-500/25",
		Style: core.Style{Gap: l.points(8), Padding: l.padding(16, 6)}, Children: []core.Node{
			core.View{Class: "rounded-full " + dot, Style: core.Style{Width: l.length(10), Height: l.length(10)}},
			l.text(entry.source.Name, 18, "font-medium text-emerald-200"),
		}}
	byline := core.View{Class: "flex-row items-center", Style: core.Style{Gap: l.points(12)}, Children: []core.Node{source}}
	if homeIsFresh(cx, item) {
		fresh := l.text(locales.TvAddedThisWeek(cx), 18, "rounded-full bg-white/15 font-medium text-white")
		fresh.Style.Padding = l.padding(16, 6)
		byline.Children = append(byline.Children, fresh)
	}
	title := l.text(item.Title, 96, "font-bold tracking-tight text-white leading-none")
	title.TestID = "home-hero-title"
	var facts []string
	if item.Year > 0 {
		facts = append(facts, strconv.Itoa(item.Year))
	}
	if len(item.Genres) > 0 {
		facts = append(facts, strings.Join(item.Genres[:min(len(item.Genres), 2)], " / "))
	}
	if item.Duration > 0 {
		facts = append(facts, seriesDuration(cx, item.Duration))
	}
	meta := core.View{Class: "flex-row items-center flex-wrap", Style: core.Style{Gap: l.points(16)}}
	if item.Rating > 0 {
		meta.Children = append(meta.Children, l.text(strconv.FormatFloat(item.Rating, 'f', 1, 64), 24, "font-semibold text-amber-300"))
	}
	if len(facts) > 0 {
		meta.Children = append(meta.Children, l.text(strings.Join(facts, " · "), 24, "text-white/80"))
	}
	if quality := tvQuality(entry); quality != "" {
		meta.Children = append(meta.Children, l.text(quality, 18, "rounded-md border border-white/40 px-2 py-0.5 font-bold text-white"))
	}
	overview := l.text(item.Overview, 24, "leading-relaxed text-white/80")
	overview.MaxLines = 3
	return core.View{Style: core.Style{Gap: l.points(28)}, Children: []core.Node{byline, title, meta, overview}}
}

func (a *application) tvHeroFavorite(cx *core.Scope, l tvLayout, entry catalogItem) core.Node {
	if entry.source.Provider == media.Plex {
		button := l.action("home-hero-favorite", locales.Favorite(cx), lucide.Heart(), tvIconAction, func() {})
		button.Disabled = func() bool { return true }
		return button
	}
	current := route{source: entry.source, item: entry.item}
	loaded := a.detailResource(cx, current)
	personal := a.detailPersonal(cx, current, loaded)
	return core.Dynamic(func(cx *core.Scope) core.Node {
		result, ready := loaded.Get()
		placeholder := l.action("home-hero-favorite", locales.Favorite(cx), lucide.Heart(), tvIconAction, func() {})
		placeholder.Disabled = func() bool { return true }
		if !ready || !result.canFavorite {
			return placeholder
		}
		buttons := personal.buttons(cx, result, func(_ *core.Scope, id, label string, icon *core.SVG, selected bool, disabled func() bool, press func()) core.Node {
			if id != "detail-favorite" {
				return nil
			}
			button := l.action("home-hero-favorite", label, icon, tvIconAction, press)
			button.Disabled = disabled
			button.Selected = func() bool { return selected }
			button.Class += " selected:bg-violet-500/50"
			return button
		})
		if len(buttons) == 0 {
			return placeholder
		}
		return core.View{Class: "relative", Children: []core.Node{buttons[0], core.View{Class: "absolute top-full left-0 w-[245px] pt-2", Children: []core.Node{personal.error}}}}
	})
}

func tvShelf(l tvLayout, id, title, stripe string, badge, all core.Node, cardGap core.Points, cards []core.Node) core.Node {
	label := l.text(title, 30, "font-bold tracking-tight text-zinc-900 dark:text-zinc-100")
	return core.View{TestID: id, Style: core.Style{Gap: l.points(24)}, Children: []core.Node{
		core.View{Class: "flex-row items-center justify-between", Style: core.Style{Padding: l.padding(96, 0)}, Children: []core.Node{
			core.View{Class: "flex-row items-center", Style: core.Style{Gap: l.points(16)}, Children: []core.Node{
				core.View{Class: "rounded-full " + stripe, Style: core.Style{Width: l.length(6), Height: l.length(32)}},
				label, badge,
			}},
			all,
		}},
		core.Scroll{Horizontal: true, HideIndicator: true, TestID: id + "-rail",
			Style: core.Style{Margin: core.Edges{Left: l.points(90), Right: l.points(96)}}, Children: []core.Node{
				core.View{Class: "flex-row", Style: core.Style{Gap: l.points(cardGap), Padding: l.padding(6, 0)}, Children: cards},
			}},
	}}
}

func (a *application) tvPosters(cx *core.Scope, l tvLayout, id, title, stripe string, badge, all core.Node, entries []catalogItem) core.Node {
	cards := make([]core.Node, 0, len(entries))
	for _, entry := range entries {
		item := entry.item
		var details []string
		if item.Year > 0 {
			details = append(details, strconv.Itoa(item.Year))
		}
		if item.Kind == media.Series {
			details = append(details, locales.SeriesType(cx))
			if item.EpisodeCount > 0 {
				details = append(details, locales.LibraryEpisodeCount(cx, strconv.Itoa(item.EpisodeCount)))
			}
		} else if item.Rating > 0 {
			details = append(details, strconv.FormatFloat(item.Rating, 'f', 1, 64))
		}
		cover := []core.Node{homeArtwork(item.Poster, "absolute inset-0")}
		if quality := tvQuality(entry); quality != "" && a.appearancePrefs.Get().ShowQuality {
			tag := l.text(quality, 14, "absolute top-3 left-3 rounded-md bg-black/55 px-2 py-1 font-bold text-white")
			cover = append(cover, tag)
		}
		cardID := a.catalogEntryID("title-", entry)
		if id != "tv-recent" {
			cardID = id + "-" + cardID
		}
		card := core.View{Class: "shrink-0", Style: core.Style{Width: l.length(220), Gap: l.points(12)},
			Role: core.RoleButton, Label: item.Title, TestID: cardID,
			OnPress: func(core.PressEvent) { a.openCatalogEntry(entry) }}
		art := core.View{Class: "relative overflow-hidden rounded-2xl",
			Style: core.Style{Height: l.length(330)}, Children: cover}
		cards = append(cards, l.cardWithFocus(cx, card, art,
			l.text(item.Title, 20, "font-medium"), l.text(strings.Join(details, " · "), 16, tvMuted)))
	}
	return tvShelf(l, id, title, stripe, badge, all, 32, cards)
}

func tvQuality(entry catalogItem) string {
	var best media.VideoProfile
	for _, profile := range catalogVideoProfiles(entry) {
		if libraryResolution(profile) > libraryResolution(best) {
			best = profile
		}
	}
	if height := libraryResolution(best); height > 0 {
		if dynamicRange := detailVideoRange(media.Version{VideoRange: best.Range}); dynamicRange != "" {
			return dynamicRange
		}
		return libraryResolutionName(height)
	}
	return ""
}

func (a *application) tvResumeCard(cx *core.Scope, l tvLayout, entry catalogItem, dismiss func()) core.Node {
	item := entry.item
	title, details := item.Title, itemMetadata(cx, item)
	if item.Kind == media.Episode && item.SeriesTitle != "" {
		title, details = item.SeriesTitle, item.Title
		if item.Season > 0 && item.Episode > 0 {
			details = locales.HomeEpisode(cx, strconv.Itoa(item.Season), strconv.Itoa(item.Episode))
		}
	}
	if remaining := item.Duration - item.Position; remaining > 0 {
		details += " · " + locales.ResumeRemaining(cx, seriesDuration(cx, remaining))
	}
	dot, _ := tvProviderStyle(entry.source.Provider)
	id := a.catalogEntryID("resume-", entry)
	art := core.View{Class: "relative overflow-hidden rounded-2xl",
		Style: core.Style{Height: l.length(236)}, Children: []core.Node{
			homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0"),
			core.View{Class: "absolute left-3 top-3 flex-row items-center gap-1.5 rounded-md bg-black/55 px-2 py-1", Children: []core.Node{
				core.View{Class: "rounded-full " + dot, Style: core.Style{Width: l.length(8), Height: l.length(8)}},
				l.text(entry.source.Name, 14, "font-medium text-white"),
			}},
		}}
	if item.Duration > 0 {
		value := episodeProgress(item) / 100
		art.Children = append(art.Children, core.View{Class: "absolute inset-x-0 bottom-0 bg-white/25", TestID: id + "-progress",
			Style: core.Style{Height: l.length(6)}, Children: []core.Node{
				core.View{Class: "bg-violet-500", Style: core.Style{Width: core.Pct(value * 100), Height: core.Pct(100)}},
			}})
	}
	open := core.NewSignal(cx, false)
	button := core.View{Class: "shrink-0", Style: core.Style{Width: l.length(420), Gap: l.points(12)},
		TestID: id, Role: core.RoleButton, Label: locales.ResumeCardLabel(cx, item.Title),
		OnPress:     func(core.PressEvent) { a.playResumeEntry(entry) },
		OnLongPress: func(core.PressEvent) { open.Set(true) }}
	card := l.cardWithFocus(cx, button, art, l.text(title, 24, "font-semibold"), l.text(details, 18, tvMuted))
	return core.View{Children: []core.Node{card, core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node {
		current := route{source: entry.source, item: entry.item}
		loaded := a.detailResource(cx, current)
		actions := a.detailPersonal(cx, current, loaded)
		return ui.Dialog{Title: title, Open: open.Get, OnOpenChange: func(visible bool, _ base.Reason) { open.Set(visible) }, Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				result, ready := loaded.Get()
				if !ready {
					return hint(locales.Loading(cx))
				}
				buttons := actions.buttons(cx, result, func(_ *core.Scope, id, label string, icon *core.SVG, _ bool, disabled func() bool, press func()) core.Node {
					if id != "detail-watched" {
						return nil
					}
					return ui.Button{Text: label, Icon: func() *core.SVG { return icon }, TestID: "tv-resume-mark-watched",
						Disabled: disabled, OnPress: press}
				})
				if result.item.Watched {
					cx.Post(func() { open.Set(false) })
				}
				return core.View{Class: "gap-2", Children: append(buttons, actions.error)}
			}),
			ui.Button{Text: locales.TvRemoveFromContinueWatching(cx), Icon: lucide.X, TestID: "tv-resume-remove",
				Variant: ui.ButtonOutline, OnPress: func() { open.Set(false); dismiss() }},
		}}
	}}}}
}

func (l tvLayout) cardWithFocus(cx *core.Scope, card, art core.View, title, caption core.Text) core.View {
	focused := core.NewSignal(cx, false)
	card.OnFocus = func() { focused.Set(true) }
	card.OnBlur = func() { focused.Set(false) }
	title.ClassFunc = func() string {
		if focused.Get() {
			return "text-zinc-900 dark:text-white"
		}
		return tvCardTitle
	}
	frame := core.View{Class: "rounded-2xl shadow-lg shadow-black/20", Children: []core.Node{art}}
	frame.StyleFunc = func() core.Style {
		if focused.Get() {
			return *l.focusRing()
		}
		return core.Style{}
	}
	card.Children = []core.Node{frame, title, caption}
	return card
}
