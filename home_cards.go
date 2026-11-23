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

func (a *application) homeHeroFavorite(cx *core.Scope, entry catalogItem,
	render func(label string, selected bool, disabled func() bool, press func()) core.Node) core.Node {
	placeholder := func() core.Node {
		return render(locales.Favorite(cx), false, func() bool { return true }, func() {})
	}
	if entry.source.Provider == media.Plex {
		return placeholder()
	}
	current := route{source: entry.source, item: entry.item}
	loaded := a.detailResource(cx, current)
	personal := a.detailPersonal(cx, current, loaded)
	return core.Dynamic(func(cx *core.Scope) core.Node {
		result, ready := loaded.Get()
		if !ready || !result.canFavorite || (!playable(result.item) && result.item.Kind != media.Series) {
			return placeholder()
		}
		label := locales.Favorite(cx)
		if result.item.Favorite {
			label = locales.RemoveFavorite(cx)
		}
		return core.View{Class: "relative shrink-0", Children: []core.Node{
			render(label, result.item.Favorite, personal.busy, personal.favorite),
			core.View{Class: "absolute top-full left-0 w-[245px] pt-2", Children: []core.Node{personal.error}},
		}}
	})
}

func (a *application) homeHeroCarousel(cx *core.Scope, titles []catalogItem, tablet bool, changed func(catalogItem)) core.Node {
	index := core.NewSignal(cx, 0)
	cx.Effect(func() { changed(titles[index.Get()]) })
	cx.Cleanup(func() { changed(catalogItem{}) })
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
	genre := strings.Join(item.Genres[:min(2, len(item.Genres))], " / ")
	if tablet {
		if genre != "" {
			meta = append(meta, genre)
		}
		if item.Duration > 0 {
			meta = append(meta, detailMediaRuntime(cx, item.Duration))
		}
		if quality := catalogVideoFormat(cx, highestVideoProfile(item.VideoProfiles)); quality != "" {
			meta = append(meta, quality)
		}
	} else if minutes := int(item.Duration / time.Minute); minutes > 0 {
		meta = append(meta, locales.HomeDuration(cx, strconv.Itoa(minutes)))
	}
	if !tablet {
		if genre != "" {
			meta = append(meta, genre)
		}
		if score := movieRatingDecimal(item.Ratings.IMDb); score != "" {
			meta = append(meta, "IMDb "+score)
		} else if score := movieRatingPercent(item.Ratings.TMDB); score != "" {
			meta = append(meta, "TMDB "+score)
		} else if score := movieRatingDecimal(item.Rating); score != "" {
			meta = append(meta, score)
		}
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
				core.Text{Content: strings.Join(meta, " · "), TestID: "tablet-hero-facts", Class: "text-sm text-white/80", MaxLines: 1},
				core.View{Class: "flex-row gap-3 pt-1", Children: actions},
			}},
		}}
	}
	return core.View{TestID: "home-hero", Class: "relative mx-5 h-[440px] overflow-hidden rounded-3xl shadow-xl shadow-black/20", Children: []core.Node{
		homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0"),
		core.View{Class: "absolute inset-0 bg-linear-to-t from-black/85 via-black/25 to-transparent"},
		core.View{Class: "absolute top-4 left-4 flex-row items-center gap-1.5 rounded-full bg-black/40 px-2.5 py-1", Children: []core.Node{
			core.Icon{SVG: lucide.Sparkles(), Class: "size-3 text-amber-300"},
			core.Text{Content: locales.HomeFeatured(cx), Class: "text-[11px] font-medium text-white"},
		}},
		core.View{Class: "absolute inset-x-0 bottom-0 gap-3 p-5", Children: []core.Node{
			core.View{Class: "flex-row items-center gap-2", Children: byline},
			core.Text{Content: item.Title, TestID: "home-hero-title", Class: "text-4xl font-bold tracking-tight text-white", MaxLines: 2},
			core.Text{Content: strings.Join(meta, " · "), TestID: "phone-hero-facts", Class: "text-xs text-white/75", MaxLines: 1},
			phoneHomeFormatBadges(item, a.appearancePrefs.Get().ShowQuality),
			core.View{Class: "flex-row gap-3 pt-1", Children: actions},
		}},
	}}
}

func phoneHomeFormatBadges(item media.Item, showQuality bool) core.Node {
	if !showQuality {
		return nil
	}
	profile := highestVideoProfile(item.VideoProfiles)
	var resolution, audio string
	if height := libraryResolution(profile); height > 0 {
		resolution = libraryResolutionName(height)
	}
	if item.DolbyAtmos {
		audio = "Atmos"
	}
	var badges []core.Node
	for _, label := range []string{resolution, detailVideoRange(media.Version{VideoRange: profile.Range}), audio} {
		if label != "" {
			badges = append(badges, core.Text{Content: label,
				Class: "rounded-md bg-white/15 px-1.5 py-0.5 text-[10px] font-semibold text-white"})
		}
	}
	if len(badges) == 0 {
		return nil
	}
	return core.View{Class: "flex-row gap-1.5", TestID: "phone-hero-formats", Children: badges}
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
	return artworkWithIconSize(url, class, 28)
}

func artworkWithIconSize(url, class string, iconSize core.Points) core.Node {
	if url != "" {
		return core.Image{Class: class + " bg-zinc-100 dark:bg-[#272831]", Source: core.Src(url), ResizeMode: core.Cover, ElementsHidden: true}
	}
	return core.View{Class: class + " items-center bg-zinc-100 dark:bg-[#272831]", Children: []core.Node{
		core.Icon{SVG: lucide.Image(), Class: "m-auto text-zinc-500 dark:text-zinc-400",
			Style: core.Style{Width: core.Pt(iconSize), Height: core.Pt(iconSize)}},
	}}
}

func homeIsFresh(cx *core.Scope, item media.Item) bool {
	age := cx.WallTime().Sub(item.AddedAt)
	return !item.AddedAt.IsZero() && age >= 0 && age < 7*24*time.Hour
}

func (a *application) homePosterCard(cx *core.Scope, entry catalogItem) core.Node {
	return a.homePoster(cx, entry, "title-")
}

func (a *application) homePoster(cx *core.Scope, entry catalogItem, prefix string) core.Node {
	item := entry.item
	idiom := appLayoutIdiom(cx)
	desktop, tablet := idiom == core.IdiomDesktop, idiom == core.IdiomTablet
	prefs := a.appearancePrefs.Get()
	width, height := "w-28", "h-40"
	if desktop {
		width, height = "w-36", "h-52"
	}
	cover := core.View{Class: "relative " + height + " overflow-hidden rounded-xl shadow-lg shadow-black/15", Children: []core.Node{
		homeArtwork(item.Poster, "absolute inset-0 justify-center"),
		a.downloadBadge(entry),
	}}
	if item.Watched && prefs.ShowWatched {
		cover.Children = append(cover.Children, core.View{Class: "absolute top-1.5 right-1.5 size-5 items-center justify-center rounded-full bg-waygu-control shadow-md",
			Children: []core.Node{core.Icon{SVG: lucide.Check(), Class: "size-3 text-white"}}})
	}
	qualityItem := entry
	today := false
	if entry.latest != nil {
		qualityItem = catalogItem{item: *entry.latest}
		now := cx.WallTime()
		day := calendarHomeDay(now)
		today = !entry.latest.AddedAt.Before(day.start) && !entry.latest.AddedAt.After(now)
	}
	quality := tvQuality(qualityItem)
	if tablet {
		quality = ""
		if resolution := libraryResolution(highestVideoProfile(qualityItem.item.VideoProfiles)); resolution > 0 {
			quality = libraryResolutionName(resolution)
		}
	}
	showFormat := quality != "" && prefs.ShowQuality
	legacyFresh := entry.latest == nil && !tablet && (desktop || item.Kind == media.Series) && homeIsFresh(cx, item)
	if today || legacyFresh && !showFormat {
		cover.Children = append(cover.Children, core.Text{Content: locales.HomeNewShort(cx), TestID: "poster-new",
			Class: "absolute top-1.5 left-1.5 rounded-md bg-violet-500 px-1.5 py-0.5 text-[9px] font-bold text-white"})
	} else if showFormat {
		class := "absolute top-2 left-2 rounded-md bg-black/55 px-1.5 py-0.5 text-[10px] font-semibold text-white"
		if !desktop {
			class = "absolute top-1.5 left-1.5 rounded-md bg-black/55 px-1.5 py-0.5 text-[9px] font-bold text-white"
		}
		cover.Children = append(cover.Children, core.Text{Content: quality, TestID: "poster-quality", Class: class})
	}
	if item.Duration > 0 && item.Position > 0 && !item.Watched {
		cover.Children = append(cover.Children, homeProgress(cx, "", "absolute inset-x-2 bottom-2", item))
	}
	var meta []string
	if item.Year > 0 {
		meta = append(meta, strconv.Itoa(item.Year))
	}
	if tablet && len(item.Genres) > 0 {
		meta = append(meta, item.Genres[0])
	} else if !tablet && item.Rating > 0 {
		meta = append(meta, strconv.FormatFloat(item.Rating, 'f', 1, 64))
	}
	caption := core.Text{Content: strings.Join(meta, " · "), Class: "text-[11px] " + setupMuted, MaxLines: 1}
	accessibilityHint := entry.source.Name
	if entry.latest != nil {
		caption.Content = seriesUpdateCaption(cx, *entry.latest, today)
		caption.TestID = "series-update-caption"
		accessibilityHint = desktopMovieFacts(caption.Content, accessibilityHint)
	}
	return base.Button{Label: item.Title, Hint: accessibilityHint, TestID: a.catalogEntryID(prefix, entry),
		Class: width + " shrink-0 gap-1.5 rounded-xl", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress: func(core.PressEvent) { a.openCatalogEntry(entry) }, Children: []core.Node{
			cover,
			core.Text{Content: item.Title, Class: "text-[13px] font-medium " + setupTitle, MaxLines: 1},
			caption,
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
		remaining := locales.ResumeRemaining(cx, detailMediaRuntime(cx, max(0, item.Duration-item.Position)))
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
