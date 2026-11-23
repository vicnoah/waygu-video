package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerCreditSearchScopesLibraries(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			client, movie := reviewFixtureItem(t, config, secrets, provider, "Waygu Track Test")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			libraries, err := client.Libraries(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, library := range libraries {
				if library.Name != "Movies" && library.Name != "Shows" {
					continue
				}
				page, err := client.(media.CreditSearchClient).SearchCreditTitles(ctx,
					media.Query{LibraryID: library.ID, Search: "Lin Chen", Limit: 24})
				if err != nil {
					t.Fatal(err)
				}
				if library.Name == "Movies" && provider != "plex" {
					if !slices.ContainsFunc(page.Items, func(item media.Item) bool {
						return item.ID == movie.ID && slices.ContainsFunc(item.People, func(person media.Person) bool {
							return person.Name == "Lin Chen" && person.Director
						})
					}) {
						t.Fatal("director search did not return its credited movie")
					}
				}
				if library.Name == "Shows" && len(page.Items) != 0 {
					t.Fatal("director search escaped the series library")
				}
			}
		})
	}
}

func TestDockerPhoneSearchReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	movie := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Track Test")
	plex := reviewFixtureTitle(t, config, secrets, "plex", "Waygu Track Test")
	_, _, show, _ := reviewSeriesFixture(t, config, secrets, "emby")
	config[phoneSearchHistoryKey] = `["Waygu Review Series","Lin Chen","Track Test","Preview"]`
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			height := core.Points(800)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				shellIdiom.Provide(cx, core.IdiomPhone)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			capture := func(suffix string) {
				if nsgotest.Target() != "" {
					app.Advance(760 * time.Millisecond)
					app.Screenshot("search-" + variant.name + suffix)
				}
			}
			assertHistory := func(want string) {
				t.Helper()
				raw, _ := app.Storage().GetItem(phoneSearchHistoryKey)
				var history []string
				if err := json.Unmarshal([]byte(raw), &history); err != nil || len(history) == 0 || history[0] != want {
					t.Fatalf("recent search = %q, want leading %q: %v", raw, want, err)
				}
			}
			app.Press("id:tab-search")
			app.WaitFor("id:search-history", time.Second)
			if !app.Find("id:search-source-all").Props.Selected {
				t.Fatal("new search did not start with all sources")
			}
			// Native views round fractional device pixels at the display scale.
			if height := app.Find("id:phone-search-field").Frame.Size.Height; height < 43.5 || height > 44.5 {
				t.Fatalf("search field height = %v, want 44", height)
			}
			capture("-idle")
			card := "id:title-emby-" + movie
			app.Type("id:search-query", "Waygu")
			app.Advance(450 * time.Millisecond)
			app.WaitFor(card, 8*time.Second)
			app.WaitFor("id:search-series-count", 5*time.Second)
			assertHistory("Waygu Review Series")
			app.Press("id:search-submit")
			assertHistory("Waygu")
			capture("")
			app.Press("id:search-source-plex")
			app.WaitFor("id:title-plex-"+plex, 8*time.Second)
			app.WaitForGone(card, 3*time.Second)
			if !app.Find("id:search-source-plex").Props.Selected {
				t.Fatal("scope change did not show the Plex copy")
			}
			app.Press("id:search-source-emby")
			app.Press("id:search-clear-query")
			app.WaitForGone("id:search-results", 3*time.Second)
			app.Type("id:search-query", "Lin Chen")
			app.Press("id:search-submit")
			app.WaitFor(card, 8*time.Second)
			app.WaitFor("id:search-people-heading", 5*time.Second)
			app.WaitFor("text:导演 · 1 部", time.Second)
			assertHistory("Lin Chen")
			capture("-credits")
			app.Press(card)
			app.WaitFor("id:phone-movie-hero", 5*time.Second)
			app.Press("id:back")
			app.WaitFor(card, 5*time.Second)
			if !app.Find("id:search-source-emby").Props.Selected {
				t.Fatal("returning from details lost the source filter")
			}
			app.Press("id:search-clear-query")
			app.Press("role:button Waygu Review Series")
			app.WaitFor("id:title-emby-"+show.ID, 5*time.Second)
			assertHistory("Waygu Review Series")
			app.Press("id:title-emby-" + show.ID)
			app.WaitFor("id:phone-series-hero", 5*time.Second)
			app.Press("id:back")
			app.WaitFor("id:search-clear-query", time.Second)
			app.Press("id:search-clear-query")
			app.Type("id:search-query", "zzzz-no-such-film-9999")
			app.Press("id:search-submit")
			app.WaitFor("id:search-empty", 8*time.Second)
			capture("-empty")
			app.Press("id:search-clear-history")
			app.WaitForGone("id:search-history", time.Second)
			if _, exists := app.Storage().GetItem(phoneSearchHistoryKey); exists {
				t.Fatal("cleared search history survived in storage")
			}
		})
	}
}
