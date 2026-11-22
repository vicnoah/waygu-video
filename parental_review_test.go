package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

type parentalFixture struct{ fixtureServer }

func (p parentalFixture) Catalog(_ context.Context, query media.Query) (media.Page, error) {
	items := []media.Item{
		{ID: "family", Kind: media.Movie, Title: "Family fixture", ContentRating: "G", Genres: []string{"Family"}},
		{ID: "adult", Kind: media.Movie, Title: "Adult fixture", ContentRating: "R"},
		{ID: "unrated", Kind: media.Movie, Title: "Unrated fixture"},
	}
	if query.LibraryID == "private" {
		items = []media.Item{{ID: "private", Kind: media.Movie, Title: "Private fixture", ContentRating: "G"}}
	}
	if query.Search != "" {
		items = slices.DeleteFunc(items, func(item media.Item) bool {
			return !strings.Contains(strings.ToLower(item.Title), strings.ToLower(query.Search))
		})
	}
	return media.Page{Items: items, Total: len(items)}, nil
}

func (p parentalFixture) Playback(_ context.Context, item media.Item, _ media.PlaybackOptions) (media.Stream, error) {
	return media.Stream{ItemID: item.ID, URL: "http://127.0.0.1:1/fixture.mp4",
		Method: media.DirectPlay, Duration: 5 * time.Minute}, nil
}

func TestPhoneParentalControlsAndPlaybackLimit(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses the deterministic playback clock and simulated video events")
	}
	when := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return parentalFixture{fixtureServer{session: session}}, nil
		}, nil)
	}, nsgotest.Size(390, 1100), nsgotest.WallTime(when),
		nsgotest.Storage(map[string]string{
			sourcesKey:          `[{"ID":"family","Name":"Family Jellyfin","Provider":"jellyfin","Address":"http://127.0.0.1:1","Libraries":[{"ID":"movies","Name":"Movies"},{"ID":"private","Name":"Private"}]}]`,
			parentalSettingsKey: `{"TimeLimit":true,"Weekday":1,"Weekend":2}`,
			parentalUsageKey:    `{"Day":"2026-11-23","Used":58000000000}`,
		}), nsgotest.Secrets(map[string]string{"family": "fixture-token"}))
	app.Press("id:tab-settings")
	app.Press("id:open-parental-settings")
	app.Press("id:parental-enabled")
	app.Type("id:parental-pin-input", "2468")
	app.Type("id:parental-pin-repeat", "2468")
	app.Press("id:parental-pin-confirm")
	app.WaitUntil(3*time.Second, func() bool { return readParentalPolicy(app.Storage()).Enabled })
	raw, _ := app.Storage().GetItem(parentalSettingsKey)
	if strings.Contains(raw, "2468") {
		t.Fatal("the PIN was written to preferences")
	}
	app.Press("id:parental-hidden-libraries")
	app.Press("id:parental-hide-library-family-private")
	app.Press("id:parental-choices-done")
	app.Press("id:back")
	app.Press("id:tab-home")
	app.WaitFor("id:title-family", 3*time.Second)
	for _, id := range []string{"adult", "private", "unrated"} {
		app.WaitForGone("id:title-"+id, time.Second)
	}
	app.Press("id:tab-search")
	app.Type("id:search-query", "Unrated fixture")
	app.Press("id:search-submit")
	app.WaitFor("id:parental-unrated-result-1", 3*time.Second)
	app.WaitForGone("id:search-result-unrated", time.Second)
	app.Press("id:parental-unrated-result-1")
	app.WaitFor("id:parental-blocked-media", time.Second)
	app.Press("id:parental-approve-title")
	app.Type("id:parental-pin-input", "2468")
	app.Press("id:parental-pin-confirm")
	app.WaitFor("id:phone-movie-title", 3*time.Second)
	app.Press("id:back")
	app.Press("id:tab-home")
	app.WaitFor("id:title-family", 3*time.Second)
	app.Press("id:title-family")
	app.WaitFor("id:detail-play", 3*time.Second)
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", 3*time.Second)
	app.Play("id:video-player", core.VideoInfo{Duration: 5 * time.Minute})
	app.FastForward(3 * time.Second)
	app.WaitFor("id:parental-playback-lock", time.Second)
	app.Press("id:parental-extend-time")
	app.Type("id:parental-pin-input", "0000")
	app.Press("id:parental-pin-confirm")
	app.WaitFor("id:parental-pin-error", 3*time.Second)
	app.Type("id:parental-pin-input", "2468")
	app.Press("id:parental-pin-confirm")
	app.WaitForGone("id:parental-playback-lock", 3*time.Second)
	if used := readParentalUsage(app.Storage()); used.Used < time.Minute || used.Bonus != 30*time.Minute {
		t.Fatalf("saved usage = %+v", used)
	}
	app.Press("id:close-player")
	app.Press("id:back")
	app.Press("id:tab-settings")
	app.Press("id:open-parental-settings")
	app.Press("id:parental-enabled")
	app.WaitFor("id:parental-pin-dialog", time.Second)
	if !readParentalPolicy(app.Storage()).Enabled {
		t.Fatal("reopening Settings carried over the PIN authorization")
	}
	app.Key("Escape")
	app.Press("id:back")
	app.Press("id:settings-add-source")
	app.WaitFor("id:parental-source-lock", time.Second)
	app.Press("id:parental-authorize-source")
	app.Type("id:parental-pin-input", "2468")
	app.Press("id:parental-pin-confirm")
	app.WaitFor("id:choose-flymoo", 3*time.Second)
}

func TestParentalRules(t *testing.T) {
	rules := parentalPolicy{Enabled: true, Kids: true, Rating: ratingPG13,
		RestrictSources: true, Sources: []string{"family"},
		Hidden:    []parentalLibrary{{"family", "private"}},
		TimeLimit: true, Weekday: 60, Weekend: 120, Quiet: true, QuietFrom: 21*60 + 30, QuietUntil: 7 * 60}
	for _, item := range []struct {
		source, library, rating, genre string
		want                           parentalDecision
	}{
		{"family", "shows", "us/TV-Y", "", parentalAllowed},
		{"family", "shows", "PG", "动画", parentalAllowed},
		{"family", "shows", "PG", "Horror", parentalBlocked},
		{"family", "shows", "PG-13", "Animation", parentalBlocked},
		{"family", "shows", "gb/15", "Animation", parentalUnrated},
		{"other", "shows", "G", "Family", parentalBlocked},
		{"family", "private", "G", "Family", parentalBlocked},
		{"family", "", "G", "Family", parentalBlocked},
	} {
		if got := rules.assess(item.source, media.Item{LibraryID: item.library, ContentRating: item.rating, Genres: []string{item.genre}}); got != item.want {
			t.Errorf("decision for %+v = %v", item, got)
		}
	}
	friday := time.Date(2026, time.November, 20, 21, 30, 0, 0, time.UTC)
	saturday := friday.Add(9*time.Hour + 30*time.Minute)
	if got, _ := rules.viewingLimit(friday, parentalUsage{}); got != parentalQuietHours {
		t.Fatalf("night lock = %v", got)
	}
	if got, _ := rules.viewingLimit(saturday.Add(-time.Minute), parentalUsage{Bonus: 30 * time.Minute}); got != parentalQuietHours {
		t.Fatalf("overnight lock = %v", got)
	}
	if got, remaining := rules.viewingLimit(saturday, parentalUsage{Day: friday.Format(time.DateOnly), Used: time.Hour}); got != parentalCanPlay || remaining != 2*time.Hour {
		t.Fatalf("new day allowance = %v, %v", got, remaining)
	}
}

func TestDockerPhoneParentalReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires local Docker fixtures")
	}
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("reference capture uses Linux GTK")
	}
	config, secrets := homeReviewSources(t)

	_, rated := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	if contentAgeRating(rated.ContentRating) != ratingG {
		t.Fatalf("Emby age rating for fixture = %q", rated.ContentRating)
	}
	_, jellyfinRated := reviewFixtureItem(t, config, secrets, "jellyfin", "Waygu Track Test")
	if contentAgeRating(jellyfinRated.ContentRating) != ratingG {
		t.Fatalf("Jellyfin age rating for fixture = %q", jellyfinRated.ContentRating)
	}
	_, unrated := reviewFixtureItem(t, config, secrets, "emby", "Waygu Test Movie")
	if contentAgeRating(unrated.ContentRating) != ratingUnknown {
		t.Fatalf("expected an unrated Emby movie, got %q", unrated.ContentRating)
	}
	var entries []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &entries); err != nil {
		t.Fatal(err)
	}
	rules := parentalPolicy{Enabled: true, PINSet: true, Rating: ratingPG13, RestrictSources: true, Sources: []string{"emby"},
		TimeLimit: true, Weekday: 60, Weekend: 120, Quiet: true, QuietFrom: 21*60 + 30, QuietUntil: 7 * 60}
	for _, entry := range entries {
		if entry.ID == "jellyfin" {
			for _, library := range entry.Libraries {
				rules.Hidden = append(rules.Hidden, parentalLibrary{entry.ID, library.ID})
			}
		}
	}
	prefs, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}
	config[parentalSettingsKey] = string(prefs)
	secrets[parentalPINKey], err = newParentalPIN("2468")
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			height := core.Points(1080)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(variant.appearance)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.Press("id:tab-settings")
			app.Press("id:open-parental-settings")
			app.WaitFor("id:phone-parental-settings", 2*time.Second)
			if nsgotest.Target() != "" {
				app.Advance(400 * time.Millisecond)
				app.Screenshot("settings-parental-" + variant.name)
			}
			if variant.name == "light" {
				app.Press("id:back")
				app.Press("id:tab-library")
				app.WaitFor("id:title-emby-"+rated.ID, 6*time.Second)
				app.WaitForGone("id:title-emby-"+unrated.ID, time.Second)
				app.Press("id:tab-settings")
				app.Press("id:open-parental-settings")
				app.Press("id:parental-allowed-sources")
				app.Type("id:parental-pin-input", "2468")
				app.Press("id:parental-pin-confirm")
				app.WaitFor("id:parental-allow-source-jellyfin", 3*time.Second)
				app.Press("id:parental-allow-source-jellyfin")
				if rules := readParentalPolicy(app.Storage()); !rules.sourceAllowed("jellyfin") || rules.sourceAllowed("plex") {
					t.Fatalf("saved allowed sources = %v", rules.Sources)
				}
				app.Press("id:parental-choices-done")
				app.Advance(350 * time.Millisecond)
				app.Press("id:parental-time-limit")
				app.Press("id:back")
				app.Press("id:tab-library")
				card := "id:title-emby-" + rated.ID
				app.WaitFor(card, 6*time.Second)
				app.Press(card)
				app.WaitFor("id:detail-play", 6*time.Second)
				app.Press("id:detail-play")
				app.WaitFor("id:video-player", 6*time.Second)
			}
		})
	}
}
