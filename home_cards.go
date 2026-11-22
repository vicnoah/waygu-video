package main

import (
	"cmp"
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

func recommendHomeTitles(recent []catalogItem, limit int) []catalogItem {
	featured := make([]catalogItem, 0, 12)
	for _, entry := range recent[:min(len(recent), 12)] {
		if playable(entry.item) {
			featured = append(featured, entry)
		}
	}
	if len(featured) == 0 {
		featured = slices.Clone(recent[:min(len(recent), limit)])
	}
	slices.SortStableFunc(featured, func(a, b catalogItem) int { return cmp.Compare(b.item.Rating, a.item.Rating) })
	return featured[:min(len(featured), limit)]
}

var homeHorizontalPan = core.PanActivation{ActiveOffsetX: 12, FailOffsetY: 12}

func (a *application) homeHeroCarousel(cx *core.Scope, titles []catalogItem, tablet bool) core.Node {
	index := core.NewSignal(cx, 0)
	if !tablet {
		cx.Effect(func() {
			cursor := index.Get()
			if len(titles) > 1 && a.tab.Get() == 0 && !cx.Env().ReduceMotion() {
				cx.AfterFunc(8*time.Second, func() { index.Set((cursor + 1) % len(titles)) })
			}
		})
	}
	style := "bg-zinc-900/20 dark:bg-zinc-50/20 selected:bg-violet-500"
	rowClass := "flex-row justify-center gap-1.5"
	if tablet {
		style = "bg-white/40 selected:bg-white"
		rowClass = "absolute right-6 bottom-6 flex-row gap-1.5"
	}
	dots := make([]core.Node, 0, len(titles))
	for i, title := range titles {
		dots = append(dots, base.Button{Label: title.item.Title, TestID: "home-hero-dot-" + strconv.Itoa(i),
			Class:   "h-1.5 w-1.5 rounded-full selected:w-5 " + style,
			HitSlop: core.All(4), Selected: func() bool { return index.Get() == i },
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { index.Set(i) },
		})
	}
	dotRow := core.View{Class: rowClass, Children: dots}
	if tablet {
		card := core.View{Class: "relative mx-6 h-[260px] overflow-hidden rounded-3xl shadow-2xl shadow-black/25", TestID: "home-hero",
			Children: []core.Node{
				core.Dynamic(func(cx *core.Scope) core.Node { return a.homeHero(cx, titles[index.Get()], true) }), dotRow,
			}}
		if len(titles) > 1 {
			card.PanActivation = homeHorizontalPan
			card.OnPan = func(event core.PanEvent) {
				if event.Phase != core.GestureEnded {
					return
				}
				if event.Translation.X <= -44 {
					index.Update(func(i int) int { return (i + 1) % len(titles) })
				} else if event.Translation.X >= 44 {
					index.Update(func(i int) int { return (i + len(titles) - 1) % len(titles) })
				}
			}
		}
		return card
	}
	return core.View{Class: "gap-3", TestID: "home-hero-carousel", Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node { return a.homeHero(cx, titles[index.Get()], false) }),
		dotRow,
	}}
}

func (a *application) homeHero(cx *core.Scope, entry catalogItem, tablet bool) core.Node {
	item := entry.item
	var meta []string
	if item.Year > 0 {
		meta = append(meta, strconv.Itoa(item.Year))
	}
	if tablet && len(item.Genres) > 0 {
		meta = append(meta, strings.Join(item.Genres[:min(2, len(item.Genres))], " / "))
	}
	if minutes := int(item.Duration / time.Minute); minutes > 0 {
		meta = append(meta, locales.HomeDuration(cx, strconv.Itoa(minutes)))
	}
	if item.Rating > 0 {
		meta = append(meta, strconv.FormatFloat(item.Rating, 'f', 1, 64))
	}
	genreNames := item.Genres
	if len(genreNames) == 0 {
		genreNames = []string{locales.MovieType(cx)}
		if item.Kind == media.Series {
			genreNames[0] = locales.SeriesType(cx)
		}
	}
	genres := make([]core.Node, 0, min(len(genreNames), 3))
	for _, genre := range genreNames[:min(len(genreNames), 3)] {
		genres = append(genres, core.Text{Content: genre, Class: "rounded-md bg-white/15 px-1.5 py-0.5 text-[10px] font-semibold text-white", MaxLines: 1})
	}
	var byline []core.Node
	if homeIsFresh(cx, item) {
		byline = append(byline, core.Text{Content: locales.HomeNewRelease(cx), Class: "rounded-md bg-violet-500 px-1.5 py-0.5 text-[10px] font-bold text-white"})
	}
	byline = append(byline, core.Text{Content: entry.source.Name, TestID: "home-hero-source", Class: "text-xs text-white/80", MaxLines: 1})
	actions := []core.Node{
		homeHeroAction(cx, "home-hero-details", locales.Details(cx), lucide.Info(), false, tablet, func() { a.openCatalogEntry(entry) }),
	}
	if playable(item) {
		actions = append([]core.Node{
			homeHeroAction(cx, "home-hero-play", locales.Play(cx), lucide.Play(), true, tablet, func() { a.playCatalogEntry(entry) }),
		}, actions...)
	}
	if tablet {
		return core.View{Class: "absolute inset-0", Children: []core.Node{
			homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0"),
			core.View{Class: "absolute inset-0 bg-linear-to-r from-black/85 via-black/45 to-transparent"},
			core.View{Class: "absolute bottom-0 left-0 w-[460px] max-w-full gap-3 p-8", Children: []core.Node{
				core.View{Class: "flex-row items-center gap-2", Children: byline},
				core.Text{Content: item.Title, TestID: "home-hero-title", Class: "text-4xl font-bold tracking-tight text-white", MaxLines: 2},
				core.Text{Content: strings.Join(meta, " · "), Class: "text-sm text-white/80", MaxLines: 1},
				core.View{Class: "flex-row gap-3 pt-1", Children: actions},
			}},
		}}
	}
	return core.View{TestID: "home-hero", Class: "relative mx-5 h-[440px] overflow-hidden rounded-3xl shadow-xl shadow-black/20", Children: []core.Node{
		homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0 pt-16"),
		core.View{Class: "absolute inset-0 bg-linear-to-t from-black/85 via-black/25 to-transparent"},
		core.View{Class: "absolute top-4 left-4 flex-row items-center gap-1.5 rounded-full bg-black/40 px-2.5 py-1", Children: []core.Node{
			core.Icon{SVG: lucide.Sparkles(), Class: "size-3 text-amber-300"},
			core.Text{Content: locales.HomeFeatured(cx), Class: "text-[11px] font-medium text-white"},
		}},
		core.View{Class: "absolute inset-x-0 bottom-0 gap-3 p-5", Children: []core.Node{
			core.View{Class: "flex-row items-center gap-2", Children: byline},
			core.Text{Content: item.Title, TestID: "home-hero-title", Class: "text-4xl font-bold tracking-tight text-white", MaxLines: 2},
			core.Text{Content: strings.Join(meta, " · "), Class: "text-xs text-white/75", MaxLines: 1},
			core.View{Class: "flex-row flex-wrap gap-1.5", Children: genres},
			core.View{Class: "flex-row gap-3 pt-1", Children: actions},
		}},
	}}
}

func homeHeroAction(cx *core.Scope, id, label string, icon *core.SVG, primary, tablet bool, press func()) core.Node {
	fill, tint := "bg-white/20", "text-white"
	if primary {
		fill, tint = "bg-white", "text-black"
	}
	shape := "h-11 flex-1 rounded-xl"
	if tablet {
		shape = "h-10 rounded-full px-5"
		if primary {
			shape += " shadow-lg"
		}
	}
	return base.Button{TestID: id, Label: label,
		Class: "flex-row items-center justify-center gap-2 " + shape + " " + fill, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress: func(core.PressEvent) { press() }, Children: []core.Node{
			core.Icon{SVG: icon, Class: "size-4 " + tint},
			core.Text{Content: label, Class: "text-sm font-semibold " + tint},
		}}
}

func homeArtwork(url, class string) core.Node {
	if url != "" {
		return core.Image{Class: class + " bg-zinc-100 dark:bg-[#272831]", Source: core.Src(url), ResizeMode: core.Cover, ElementsHidden: true}
	}
	return core.View{Class: class + " items-center bg-zinc-100 dark:bg-[#272831]", Children: []core.Node{
		core.Icon{SVG: lucide.Image(), Class: "m-auto size-7 text-zinc-500 dark:text-zinc-400"},
	}}
}

func homeIsFresh(cx *core.Scope, item media.Item) bool {
	age := cx.Now().Sub(item.AddedAt)
	return !item.AddedAt.IsZero() && age >= 0 && age < 7*24*time.Hour
}

func (a *application) homePosterCard(cx *core.Scope, entry catalogItem) core.Node {
	item := entry.item
	desktop := appLayoutIdiom(cx) == core.IdiomDesktop
	width, height := "w-28", "h-40"
	if desktop {
		width, height = "w-36", "h-52"
	}
	cover := core.View{Class: "relative " + height + " overflow-hidden rounded-xl shadow-lg shadow-black/15", Children: []core.Node{
		homeArtwork(item.Poster, "absolute inset-0 justify-center"),
		a.downloadBadge(entry),
	}}
	if item.Watched {
		cover.Children = append(cover.Children, core.View{Class: "absolute top-1.5 right-1.5 size-5 items-center justify-center rounded-full bg-violet-500 shadow-md",
			Children: []core.Node{core.Icon{SVG: lucide.Check(), Class: "size-3 text-white"}}})
	}
	if quality := tvQuality(entry); desktop && quality != "" {
		cover.Children = append(cover.Children, core.Text{Content: quality, Class: "absolute top-2 left-2 rounded-md bg-black/55 px-1.5 py-0.5 text-[10px] font-semibold text-white"})
	} else if homeIsFresh(cx, item) {
		cover.Children = append(cover.Children, core.Text{Content: locales.HomeNewShort(cx), Class: "absolute top-1.5 left-1.5 rounded-md bg-violet-500 px-1.5 py-0.5 text-[9px] font-bold text-white"})
	}
	if item.Duration > 0 && item.Position > 0 && !item.Watched {
		cover.Children = append(cover.Children, homeProgress(cx, "", "absolute inset-x-2 bottom-2", item))
	}
	var meta []string
	if item.Year > 0 {
		meta = append(meta, strconv.Itoa(item.Year))
	}
	if item.Rating > 0 {
		meta = append(meta, strconv.FormatFloat(item.Rating, 'f', 1, 64))
	}
	return base.Button{Label: item.Title, Hint: entry.source.Name, TestID: a.catalogEntryID("title-", entry),
		Class: width + " shrink-0 gap-1.5 rounded-xl", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress: func(core.PressEvent) { a.openCatalogEntry(entry) }, Children: []core.Node{
			cover,
			core.Text{Content: item.Title, Class: "text-[13px] font-medium " + setupTitle, MaxLines: 1},
			core.Text{Content: strings.Join(meta, " · "), Class: "text-[11px] " + setupMuted, MaxLines: 1},
		}}
}

func (a *application) homeResumeCard(cx *core.Scope, entry catalogItem) core.Node {
	id, item := a.catalogEntryID("resume-", entry), entry.item
	title, detail := item.Title, itemMetadata(cx, item)
	if item.Kind == media.Episode && item.SeriesTitle != "" {
		title, detail = item.SeriesTitle, item.Title
		if item.Season > 0 && item.Episode > 0 {
			detail = locales.HomeEpisode(cx, strconv.Itoa(item.Season), strconv.Itoa(item.Episode))
		}
	}
	width, height := "w-64", "h-36"
	if appLayoutIdiom(cx) == core.IdiomDesktop {
		width, height = "w-72", "h-40"
	}
	art := core.View{Class: "relative " + height + " overflow-hidden rounded-2xl shadow-lg shadow-black/15", Children: []core.Node{
		homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0 justify-center"),
		core.View{Class: "absolute inset-x-0 bottom-0 h-16 bg-linear-to-t from-black/60 to-transparent"},
	}}
	if item.Duration > 0 {
		remaining := locales.ResumeRemaining(cx, playbackClock(max(0, item.Duration-item.Position)))
		art.Children = append(art.Children,
			core.View{Class: "absolute bottom-3 left-2.5 flex-row items-center gap-1 rounded-full bg-black/55 px-2 py-1", Children: []core.Node{
				core.Icon{SVG: lucide.Play(), Class: "size-3 text-white"},
				core.Text{Content: remaining, Class: "text-[10px] font-medium text-white"},
			}},
			homeProgress(cx, id+"-progress", "absolute inset-x-0 bottom-0", item))
	}
	return base.Button{Label: locales.ResumeCardLabel(cx, item.Title), TestID: id,
		Class: width + " shrink-0 gap-2 rounded-2xl", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress: func(core.PressEvent) { a.playResumeEntry(entry) }, Children: []core.Node{
			art,
			core.View{Class: "gap-0.5 px-0.5", Children: []core.Node{
				core.Text{Content: title, Class: "text-sm font-medium " + setupTitle, MaxLines: 1},
				core.Text{Content: detail, Class: "text-xs " + setupMuted, MaxLines: 1},
			}},
		}}
}

func homeProgress(cx *core.Scope, id, class string, item media.Item) core.Node {
	value := float32(min(100, max(0, 100*float64(item.Position)/float64(item.Duration))))
	return core.View{Class: class, Children: []core.Node{
		base.Progress{TestID: id, Value: func() float32 { return value }, Label: locales.ResumePercent(cx, strconv.Itoa(int(value))),
			TrackClass: "h-1 bg-white/30", IndicatorClass: "h-1 bg-violet-500"},
	}}
}
