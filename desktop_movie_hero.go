package main

import (
	"cmp"
	"math"
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

const desktopMovieHeroUtility = "size-10 items-center justify-center rounded-full border border-white/15 bg-black/40"
const desktopMovieHeroAction = "rounded-full border border-white/20 bg-white/15"

func (a *application) desktopMovieHero(cx *core.Scope, current route, state movieDetailState, narrow bool, reload, trailer func()) core.Node {
	item, version := state.item(), state.active()
	result, _ := state.loaded.Get()
	back := desktopMovieIcon(cx, "back", locales.Back(cx), lucide.ChevronLeft(), "absolute top-6 left-6 "+desktopMovieHeroUtility,
		"size-5", false, nil, a.back)
	var shortcuts []core.Node
	if result.canFavorite {
		label := locales.Favorite(cx)
		if item.Favorite {
			label = locales.RemoveFavorite(cx)
		}
		shortcuts = append(shortcuts, desktopMovieIcon(cx, "detail-favorite", label, lucide.Heart(), desktopMovieHeroUtility,
			"size-4", item.Favorite, state.personal.busy, state.personal.favorite))
	}
	shortcuts = append(shortcuts,
		desktopMovieIcon(cx, "detail-share", locales.ShareTitle(cx), lucide.Share2(), desktopMovieHeroUtility, "size-4", false, nil, func() {
			cx.Share(core.ShareOptions{Text: item.Title}, func(shown bool, err error) {
				if !shown && err != nil {
					cx.SetClipboard(item.Title)
				}
			})
		}))
	more := []core.Node{}
	if result.canWatch {
		label := locales.MarkWatched(cx)
		if item.Watched {
			label = locales.MarkUnwatched(cx)
		}
		more = append(more, ui.MenuItem{Text: label, Disabled: state.personal.busy, TestID: "desktop-movie-menu-watched", OnSelect: state.personal.watch})
	}
	if current.downloadID == "" {
		more = append(more, ui.MenuItem{Text: locales.ReloadMovieDetail(cx), TestID: "desktop-movie-menu-refresh", OnSelect: reload})
		if canCast(cx) {
			more = append(more, ui.MenuItem{Text: locales.PhonePlayerCast(cx), TestID: "desktop-movie-menu-cast", OnSelect: state.cast})
		}
	}
	if len(more) > 0 {
		shortcuts = append(shortcuts, base.Menu{Label: locales.DetailMore(cx), TestID: "detail-more", Class: desktopMovieHeroUtility,
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, PopupClass: tabletDetailPopup, Children: more,
			Trigger: core.Icon{SVG: lucide.Ellipsis(), Class: "size-4 text-white"}})
	}

	playLabel := locales.Play(cx)
	if item.Position > 0 && !item.Watched {
		playLabel = locales.DetailResumeAt(cx, playbackClock(item.Position))
	}
	actions := []core.Node{desktopMovieAction(cx, "detail-play", playLabel, lucide.Play(), true, func() { state.play(true) })}
	if item.Position > 0 {
		actions = append(actions, desktopMovieAction(cx, "detail-from-start", locales.FromStart(cx), lucide.RotateCcw(), false,
			func() { state.play(false) }))
	}
	if trailer != nil {
		actions = append(actions, desktopMovieAction(cx, "detail-trailer", locales.TvTrailer(cx), lucide.Clapperboard(), false, trailer))
	}
	if result.canWatch {
		label := locales.MarkWatched(cx)
		if item.Watched {
			label = locales.MarkUnwatched(cx)
		}
		actions = append(actions, desktopMovieRoundAction(cx, "detail-watched", label, lucide.CircleCheck(), item.Watched,
			state.personal.busy, state.personal.watch))
	}
	if download := a.downloadAction(cx, current, desktopMovieRoundAction, state.download); download != nil {
		actions = append(actions, download)
	}
	titleClass, contentClass := "text-5xl", "gap-8 px-10"
	if narrow {
		titleClass, contentClass = "text-4xl", "gap-5 px-6"
	}
	text := []core.Node{core.Text{Content: item.Title, Class: titleClass + " font-bold text-white", MaxLines: 2,
		Role: core.RoleHeader, TestID: "desktop-movie-title"}}
	if item.OriginalTitle != "" && item.OriginalTitle != item.Title {
		text = append(text, core.Text{Content: item.OriginalTitle, Class: "text-lg text-zinc-400", MaxLines: 1})
	}
	if facts := desktopMovieHeroFacts(cx, item); facts != nil {
		text = append(text, facts)
	}
	if badges := desktopMovieBadges(cx, item, version); badges != nil {
		text = append(text, badges)
	}
	text = append(text, core.View{Class: "flex-row flex-wrap items-center gap-3 pt-2", Children: actions})
	if item.Position > 0 && item.Duration > 0 && !item.Watched {
		text = append(text, homeProgress(cx, "detail-progress", "w-80 max-w-full overflow-hidden rounded-full", item))
	}

	var content []core.Node
	if !narrow {
		content = append(content, core.View{
			Class:  "relative h-[300px] w-52 shrink-0 rounded-2xl shadow-2xl ring-1 ring-white/20",
			TestID: "desktop-movie-poster", Children: []core.Node{desktopMovieArtwork(item.Poster, "absolute inset-0 overflow-hidden rounded-2xl")},
		})
	}
	content = append(content, core.View{Class: "min-w-0 flex-1 gap-3", Children: text})
	art := cmp.Or(item.Backdrop, item.Poster)
	backdrop := desktopMovieArtwork(art, "absolute inset-0")
	if art == "" {
		backdrop = core.View{Class: "absolute inset-x-0 top-0 h-40 bg-[#f5f5f5] dark:bg-[#262626]", Children: []core.Node{
			core.View{Class: "absolute inset-x-0 top-[68px] items-center", Children: []core.Node{
				core.Icon{SVG: lucide.Image(), Class: "size-7 text-[#737373] dark:text-[#a3a3a3]"},
			}},
		}}
	}
	return core.View{Class: "relative mx-8 mt-6 h-[500px] shrink-0 overflow-hidden rounded-3xl shadow-2xl shadow-black/25",
		TestID: "desktop-movie-hero", Children: []core.Node{
			backdrop,
			core.View{Class: "absolute inset-0 bg-linear-to-t from-black/90 via-black/45 to-black/10"},
			core.View{Class: "absolute inset-0 bg-linear-to-r from-black/70 via-black/20 to-transparent"},
			back,
			core.View{Class: "absolute top-6 right-6 flex-row gap-2", Children: shortcuts},
			core.View{Class: "absolute inset-x-0 bottom-0 flex-row items-end pb-8 " + contentClass, Children: content},
		}}
}

func desktopMovieArtwork(url, class string) core.Node {
	if url != "" {
		return homeArtwork(url, class)
	}
	return core.View{Class: class + " items-center justify-center bg-[#f5f5f5] dark:bg-[#262626]", Children: []core.Node{
		core.Icon{SVG: lucide.Image(), Class: "size-7 text-[#737373] dark:text-[#a3a3a3]"},
	}}
}

func desktopMovieHeroFacts(cx *core.Scope, item media.Item) core.Node {
	var facts []core.Node
	add := func(text, class string) {
		if len(facts) > 0 {
			facts = append(facts, core.Text{Content: "·", Class: "text-sm text-zinc-500"})
		}
		facts = append(facts, core.Text{Content: text, Class: class, MaxLines: 1})
	}
	const standard = "text-sm text-zinc-300"
	if item.Year > 0 {
		add(strconv.Itoa(item.Year), standard)
	}
	if len(item.Genres) > 0 {
		add(strings.Join(item.Genres, " / "), standard)
	}
	if item.Duration > 0 {
		add(desktopMediaRuntime(cx, item.Duration), standard)
	}
	if item.ContentRating != "" {
		add(item.ContentRating, "rounded border border-white/30 px-1.5 text-[11px] text-zinc-300")
	}
	if len(facts) == 0 {
		return nil
	}
	return core.View{Class: "flex-row flex-wrap items-center gap-3", Children: facts}
}

func desktopMediaRuntime(cx *core.Scope, duration time.Duration) string {
	if hours := duration / time.Hour; hours > 0 {
		minutes := duration % time.Hour / time.Minute
		return locales.DesktopMovieRuntime(cx, strconv.Itoa(int(hours)), strconv.Itoa(int(minutes)))
	}
	return seriesDuration(cx, duration)
}

func desktopMovieBadges(cx *core.Scope, item media.Item, version media.Version) core.Node {
	var nodes []core.Node
	for _, rating := range []struct {
		label, value, background, tint string
	}{
		{"IMDb", movieRatingDecimal(item.Ratings.IMDb), "bg-amber-400/15", "text-amber-300"},
		{"TMDB", movieRatingPercent(item.Ratings.TMDB), "bg-sky-500/15", "text-sky-300"},
	} {
		if rating.value != "" {
			nodes = append(nodes, core.View{Class: "flex-row items-center gap-1.5 rounded-md px-2 py-1 " + rating.background,
				Children: []core.Node{
					core.Text{Content: rating.label, Class: "text-xs font-bold " + rating.tint},
					core.Text{Content: rating.value, Class: "text-xs font-bold text-white"},
				}})
		}
	}
	if len(nodes) == 0 && item.Rating > 0 {
		nodes = append(nodes, core.View{Class: "flex-row items-center gap-1.5 rounded-md bg-amber-400/15 px-2 py-1", Children: []core.Node{
			core.Icon{SVG: lucide.Star(), Class: "size-3 text-amber-300"},
			core.Text{Content: strconv.FormatFloat(item.Rating, 'f', 1, 64), Class: "text-xs font-bold text-white"},
		}})
	}
	atmos, chinese := "", ""
	for _, track := range version.Tracks {
		if track.HasAtmos() {
			atmos = "Atmos"
		}
		if track.Type == "subtitle" {
			language, _, _ := strings.Cut(strings.ReplaceAll(strings.ToLower(track.Language), "_", "-"), "-")
			switch language {
			case "zh", "zho", "chi":
				chinese = locales.DesktopMovieChineseSubtitles(cx)
			}
		}
	}
	var tech []core.Node
	for _, badge := range []string{detailResolution(version), desktopVideoRange(cx, version), atmos, strings.ToUpper(version.VideoCodec), chinese} {
		if badge != "" {
			tech = append(tech, core.Text{Content: badge,
				Class: "rounded border border-white/30 px-1.5 py-0.5 text-[11px] font-semibold text-white"})
		}
	}
	if len(tech) > 0 {
		nodes = append(nodes, core.View{Class: "ml-2 flex-row flex-wrap gap-1.5", Children: tech})
	}
	if len(nodes) == 0 {
		return nil
	}
	return core.View{Class: "flex-row flex-wrap items-center gap-2", Children: nodes}
}

func movieRatingDecimal(score float64) string {
	if !(score > 0 && score <= 10) {
		return ""
	}
	return strconv.FormatFloat(score, 'f', 1, 64)
}

func movieRatingPercent(score float64) string {
	if !(score > 0 && score <= 10) {
		return ""
	}
	return strconv.Itoa(int(math.Round(score*10))) + "%"
}

func desktopMovieAction(cx *core.Scope, id, label string, icon *core.SVG, primary bool, action func()) core.Node {
	class, text, glyph := "px-5 "+desktopMovieHeroAction, "text-sm font-medium", "size-4"
	if primary {
		class, text, glyph = "rounded-full bg-violet-500 px-7 shadow-lg shadow-violet-500/40", "font-semibold", "size-5"
	}
	return base.Button{Label: label, TestID: id, Class: "h-12 shrink-0 flex-row items-center justify-center gap-2 " + class,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { action() },
		Children: []core.Node{core.Icon{SVG: icon, Class: glyph + " text-white"}, core.Text{Content: label, Class: "text-white " + text}}}
}

func desktopMovieRoundAction(cx *core.Scope, id, label string, icon *core.SVG, active bool, disabled func() bool, action func()) core.Node {
	return desktopMovieIcon(cx, id, label, icon, "size-12 items-center justify-center "+desktopMovieHeroAction, "size-5", active, disabled, action)
}

func desktopMovieIcon(cx *core.Scope, id, label string, icon *core.SVG, class, size string, active bool, disabled func() bool, action func()) core.Node {
	tint := "text-white"
	if active {
		tint = "text-violet-400"
	}
	return base.Button{Label: label, TestID: id, Class: "shrink-0 disabled:opacity-50 " + class,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, Selected: func() bool { return active }, Disabled: disabled,
		OnPress: func(core.PressEvent) { action() }, Children: []core.Node{core.Icon{SVG: icon, Class: size + " " + tint}}}
}
