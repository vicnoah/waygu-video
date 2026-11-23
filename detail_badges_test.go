package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDetailBadges(t *testing.T) {
	target := nsgotest.Target()
	if target != "" && target != "linux" && target != "android" && target != "tvos_simulator" {
		t.Skip("detail badge captures run on GTK, Android and the Apple TV simulator")
	}
	config := movieDesignFixtureConfig(t, 0)
	for _, layout := range []struct {
		name          string
		idiom         core.Idiom
		width, height core.Points
	}{
		{"tablet", core.IdiomTablet, 1180, 820},
		{"tv", core.IdiomTV, 1920, 1640},
		{"tv-half-width", core.IdiomTV, 960, 540},
	} {
		if layout.name == "tv-half-width" && target != "" || target == "tvos_simulator" && layout.idiom != core.IdiomTV {
			continue
		}
		t.Run(layout.name, func(t *testing.T) {
			for _, theme := range reviewAppearances {
				t.Run(theme.name, func(t *testing.T) {
					height := layout.height
					if target == "linux" {
						height += 37
					}
					app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
						cx.SetSubtreeAppearance(theme.appearance)
						shellIdiom.Provide(cx, layout.idiom)
						return movieDesignFixtureRoot(cx)
					}, nsgotest.Size(layout.width, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
						nsgotest.Storage(config), nsgotest.Secrets(map[string]string{"capture": "fixture-token"}))
					if layout.idiom == core.IdiomTV {
						checkTVDetailBadges(t, app, theme.name)
					} else {
						checkTabletDetailBadges(t, app, theme.name)
					}
				})
			}
		})
	}
}

func checkTabletDetailBadges(t *testing.T, app *nsgotest.App, appearance string) {
	t.Helper()
	app.WaitFor("id:tablet-library-movies", 5*time.Second)
	app.Press("id:tablet-library-movies")
	app.WaitFor("id:title-test-film", 5*time.Second)
	app.Press("id:title-test-film")
	const badges = "id:tablet-detail-badges >> text:"
	for _, label := range []string{"IMDb", "8.5", "TMDB", "82%", "4K", "杜比视界", "Atmos", "中字"} {
		app.WaitFor(badges+label, 5*time.Second)
	}
	if app.Exists(badges + "9.6") {
		t.Fatal("tablet detail mixed the generic score with attributed ratings")
	}
	app.WaitFor("id:detail-file >> text:杜比视界 P7 + HDR10", 5*time.Second)
	if nsgotest.Target() != "" {
		app.Screenshot("tablet-detail-badges-" + appearance)
	}
	app.Press("id:playback-version")
	app.Press("role:option /1080p/")
	app.WaitFor(badges+"1080p", 5*time.Second)
	app.WaitForGone("id:detail-file >> text:杜比视界 P7 + HDR10", 5*time.Second)
	for _, label := range []string{"4K", "杜比视界", "Atmos", "中字"} {
		app.WaitForGone(badges+label, 5*time.Second)
	}
	app.WaitFor(badges+"TMDB", 5*time.Second)
}

func checkTVDetailBadges(t *testing.T, app *nsgotest.App, appearance string) {
	t.Helper()
	app.WaitFor("id:home-hero-details", 5*time.Second)
	app.Press("id:home-hero-details")
	const facts = "id:tv-detail-facts >> text:"
	for _, label := range []string{"IMDb", "8.5", "TMDB", "82%"} {
		app.WaitFor(facts+label, 5*time.Second)
	}
	if app.Exists(facts + "9.6") {
		t.Fatal("TV detail mixed the generic score with attributed ratings")
	}
	const formats = "id:tv-detail-formats >> text:"
	for _, label := range []string{"4K", "杜比视界", "HDR10", "Dolby Atmos", "中文字幕"} {
		app.WaitFor(formats+label, 5*time.Second)
	}
	app.WaitFor(facts+"2 小时 19 分", 5*time.Second)
	checkTVDetailDimensions(t, app, facts+"IMDb", formats+"HDR10")
	if nsgotest.Target() != "" {
		app.Screenshot("tv-detail-badges-" + appearance)
	}
	if nsgotest.Target() == "android" {
		const scroll = "id:tv-detail-scroll"
		amount := app.Find("id:tv-detail-hero").Frame.Size.Height / 2
		app.Scroll(scroll, amount)
		app.Advance(350 * time.Millisecond)
		app.Screenshot("tv-detail-credits-" + appearance)
		app.Scroll(scroll, -amount)
	}
	app.Press("id:playback-version")
	app.Press("role:radio /1080p/")
	app.WaitFor(formats+"1080p", 5*time.Second)
	for _, label := range []string{"4K", "杜比视界", "HDR10", "Dolby Atmos", "中文字幕"} {
		app.WaitForGone(formats+label, 5*time.Second)
	}
	app.WaitFor(facts+"TMDB", 5*time.Second)
}

func checkTVDetailDimensions(t *testing.T, app *nsgotest.App, rating, format string) {
	t.Helper()
	scale := min(app.Find("id:tv-detail-hero").Frame.Size.Width/1920, 1)
	check := func(name string, got, reference core.Points) {
		t.Helper()
		if want := reference * scale; got < want-0.25 || got > want+0.25 {
			t.Errorf("%s = %v pt, want %v pt at scale %v", name, got, want, scale)
		}
	}
	check("rating radius", app.Find(rating).Style.CornerRadius, 4)
	badge := app.Find(format)
	check("format radius", badge.Style.CornerRadius, 6)
	check("format border", badge.Style.BorderWidth, 1)
	check("source border", app.Find("id:tv-detail-formats >> text:电影档案").Parent.Style.BorderWidth, 1)
	for _, id := range []string{"back", "detail-from-start", "detail-favorite", "playback-version"} {
		check(id+" border", app.Find("id:"+id).Style.BorderWidth, 1)
	}
	progress := app.Find("id:detail-progress").Children[0]
	check("progress width", progress.Frame.Size.Width, 620)
	check("progress height", progress.Frame.Size.Height, 8)
	portrait := app.WaitFor("id:tv-cast-0", 5*time.Second).Children[0].Children[0]
	check("portrait border", portrait.Style.BorderWidth, 4)
	check("artwork width inside portrait border", portrait.Children[0].Frame.Size.Width, 136)
	check("empty portrait icon width", portrait.Children[0].Children[0].Frame.Size.Width, 28)
}
