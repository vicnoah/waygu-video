package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerHomeReview(t *testing.T) {
	if nsgotest.Target() == "" || os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires a native screenshot target and the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			height := core.Points(1480)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				return Root(cx)
			}, nsgotest.Idiom(core.IdiomPhone), nsgotest.Size(390, height),
				nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.WaitFor("text:最近添加", 10*time.Second)
			app.WaitFor("text:继续观看", 5*time.Second)
			app.Advance(500 * time.Millisecond)
			app.Screenshot("home-" + variant.name)
			if height := app.Find("id:home-hero").Frame.Size.Height; height != 440 {
				t.Errorf("home hero height = %v, want 440", height)
			}
			if height := app.Find("id:tab-bar").Frame.Size.Height; height != 64 {
				t.Errorf("tab bar height = %v, want 64", height)
			}
			app.Press("id:home-hero-dot-1")
			if !app.Find("id:home-hero-dot-1").Props.Selected {
				t.Fatal("pressing a dot did not select the next feature")
			}
			app.Advance(8 * time.Second)
			if !app.Find("id:home-hero-dot-2").Props.Selected {
				t.Fatal("the carousel did not advance after eight seconds")
			}
			app.Press("id:home-recent-all")
			app.WaitFor("text:收起", time.Second)
			app.Press("id:home-recent-all")
			app.WaitFor("id:home-recent-rail", time.Second)
			app.Scroll("id:home-scroll", 500)
			app.WaitFor("id:home-source-emby", time.Second)
			app.Screenshot("home-" + variant.name + "-sources")
			app.Press("id:home-source-emby")
			app.WaitFor("id:home-hero-source", 5*time.Second)
			if name := app.Find("id:home-hero-source").Text; name != "Emby 影视库" {
				t.Fatalf("featured source after selecting Emby = %q", name)
			}
			app.Press("id:home-source-filter")
			app.Press("role:option 全部媒体源")
			app.WaitUntil(5*time.Second, func() bool { return strings.HasPrefix(app.Find("id:home-summary").Text, "3 个媒体源在线") })
			app.Press("id:home-hero-details")
			app.WaitFor("id:detail-play", 5*time.Second)
			app.Press("id:back")
			app.WaitFor("id:home-hero-play", 5*time.Second)
			app.Press("id:home-hero-play")
			app.WaitFor("id:video-player", 5*time.Second)
			app.Press("id:close-player")
		})
	}
}

func homeReviewSources(t *testing.T) (map[string]string, map[string]string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	password := os.Getenv("WAYGU_TEST_PASSWORD")
	if password == "" {
		password = "waygu-fixture-local"
	}
	device := media.NewDevice("Waygu Home Review")
	sources := []source{
		{ID: "emby", Name: "Emby 影视库", Provider: media.Emby, Address: "http://127.0.0.1:28096", Username: "waygu-test"},
		{ID: "jellyfin", Name: "Jellyfin 影视库", Provider: media.Jellyfin, Address: "http://127.0.0.1:18096"},
		{ID: "plex", Name: "Plex 影视库", Provider: media.Plex, Address: "http://127.0.0.1:32400"},
	}
	secrets := make(map[string]string, len(sources))
	for i := range sources {
		entry := &sources[i]
		guest, err := media.NewClient(entry.Provider, entry.Address, device, media.Session{})
		if err != nil {
			t.Fatal(err)
		}
		session, err := guest.SignIn(ctx, media.Credentials{Username: "waygu-test", Password: password})
		if err != nil {
			t.Fatal(err)
		}
		secrets[entry.ID], entry.AccountID = session.Token, session.UserID
		client, err := media.NewClient(entry.Provider, entry.Address, device, session)
		if err != nil {
			t.Fatal(err)
		}
		libraries, err := client.Libraries(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, library := range libraries {
			entry.Libraries = append(entry.Libraries, sourceLibrary{Library: library})
			if library.Name == "Movies" && entry.Provider != media.Plex {
				page, err := client.Catalog(ctx, media.Query{LibraryID: library.ID, Search: "Waygu Resume Test"})
				if err != nil || len(page.Items) != 1 {
					t.Fatalf("read resume fixture: %v, %d items", err, len(page.Items))
				}
				setDockerCopyProgress(t, ctx, client, page.Items[0], 80*time.Second)
			}
		}
	}
	data, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{sourcesKey: string(data)}, secrets
}
