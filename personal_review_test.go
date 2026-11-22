package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerPersonalLibraryReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	client, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Resume Test")
	favorites, watched := client.(media.FavoriteClient), client.(media.WatchedClient)
	setFlags := func(t *testing.T, favorite, seen bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := favorites.SetFavorite(ctx, movie, favorite); err != nil {
			t.Fatal(err)
		}
		if err := watched.SetWatched(ctx, movie, seen); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { setFlags(t, movie.Favorite, movie.Watched) })
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			setFlags(t, false, false)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			setDockerCopyProgress(t, ctx, client, movie, 80*time.Second)
			cancel()

			height := core.Points(820)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				shellIdiom.Provide(cx, core.IdiomTablet)
				return Root(cx)
			}, nsgotest.Size(1180, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			item := "id:title-emby-" + movie.ID
			app.Press("id:tablet-source-emby")
			openMovie := func() {
				app.WaitFor(item, 5*time.Second)
				app.Press(item)
				app.WaitFor("id:detail-favorite", 5*time.Second)
				app.WaitFor("id:detail-watched", 5*time.Second)
			}
			waitSelected := func(id string, selected bool) {
				app.WaitUntil(5*time.Second, func() bool {
					return app.Find("id:"+id).Props.Selected == selected
				})
			}
			openMovie()
			app.WaitFor("id:detail-progress", 5*time.Second)
			app.Press("id:detail-favorite")
			waitSelected("detail-favorite", true)
			if nsgotest.Target() != "" {
				app.Advance(500 * time.Millisecond)
				app.Screenshot("tablet-detail-personal-" + variant.name)
			}
			app.Press("id:back")
			app.Press("id:tab-favorites")
			app.WaitFor(item, 5*time.Second)
			if nsgotest.Target() != "" {
				app.Advance(500 * time.Millisecond)
				app.Screenshot("favorites-" + variant.name)
			}
			openMovie()
			waitSelected("detail-favorite", true)
			app.Press("id:detail-watched")
			waitSelected("detail-watched", true)
			app.WaitForGone("id:detail-progress", 5*time.Second)

			app.Press("id:back")
			app.Press("id:tab-history")
			app.WaitFor(item, 5*time.Second)
			if nsgotest.Target() != "" {
				app.Advance(500 * time.Millisecond)
				app.Screenshot("watch-history-" + variant.name)
			}
			openMovie()
			waitSelected("detail-watched", true)
			app.Press("id:detail-watched")
			waitSelected("detail-watched", false)
			app.Press("id:detail-favorite")
			waitSelected("detail-favorite", false)
			app.Press("id:back")
			app.Press("id:tab-favorites")
			app.WaitFor("text:所选媒体库暂无收藏。", 5*time.Second)
			app.WaitForGone(item, 5*time.Second)
		})
	}
}
