package main

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"waygu-video/internal/media"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestSkipOnboarding(t *testing.T) {
	app := nsgotest.Start(t, Root)
	app.Press("id:onboarding-later")
	app.Find("id:empty-add-source")
}

func TestTVNavigation(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Idiom(core.IdiomTV), nsgotest.Size(1280, 720))
	app.Press("id:onboarding-later")
	app.Press("id:tv-tab-settings")
	app.Press("id:settings-add-source")
	app.Find("id:choose-flymoo")
	app.Press("id:back")
	app.Find("id:settings-add-source")
}

func TestChineseAppName(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetLocale("zh-Hans")
		return Root(cx)
	})
	app.Find("text:肥牛视频")
}

func TestConnectLibraryAndOpenPlayer(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return appRoot(cx, func(kind media.Provider, address string, device media.Device, session media.Session) (media.Client, error) {
			if kind != media.Jellyfin || address != "http://nas.example.test:8096" || device.ID == "" {
				return nil, media.ErrInvalidAddress
			}
			return fixtureServer{session: session}, nil
		})
	})
	app.Press("id:add-source")
	app.Press("id:choose-jellyfin")
	app.Type("id:server-address", "nas.example.test:8096")
	app.Type("id:server-username", "viewer")
	app.Type("id:server-password", "fixture-password")
	app.Press("id:sign-in")
	app.WaitFor("id:import-library-test-library", time.Second)
	app.Press("id:import-libraries")
	app.WaitFor("id:title-test-film", time.Second)
	if config, _ := app.Storage().GetItem(sourcesKey); strings.Contains(config, "fixture-token") || strings.Contains(config, "fixture-password") {
		t.Fatal("source configuration exposed credentials")
	}
	app.Press("id:title-test-film")
	app.WaitFor("text:Fixture synopsis", time.Second)
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", time.Second)
	app.PickFiles = func(nsgotest.FilePick) []core.File {
		return []core.File{{Name: "fixture.xml", Opener: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(`<i><d p="0,1,25,16777215">Fixture comment</d></i>`)), nil
		}}}
	}
	app.Press("id:load-danmaku")
	app.WaitFor("id:danmaku-canvas", time.Second)
	app.Press("id:toggle-danmaku")
	app.WaitForGone("id:danmaku-canvas", time.Second)
	app.Press("id:close-player")
	app.WaitFor("id:detail-play", time.Second)
}

type fixtureServer struct {
	session media.Session
}

func (f fixtureServer) Probe(context.Context) (media.ServerInfo, error) {
	return media.ServerInfo{ID: "fixture-1", Name: "Family Jellyfin", Version: "12.4.0"}, nil
}

func (f fixtureServer) SignIn(_ context.Context, credentials media.Credentials) (media.Session, error) {
	if credentials.Username != "viewer" || credentials.Password != "fixture-password" {
		return media.Session{}, media.ErrAuthentication
	}
	return media.Session{UserID: "viewer", Token: "fixture-token"}, nil
}

func (f fixtureServer) Libraries(context.Context) ([]media.Library, error) {
	if f.session.Token != "fixture-token" {
		return nil, media.ErrAuthentication
	}
	return []media.Library{{ID: "test-library", Name: "Movies"}}, nil
}

func (f fixtureServer) Catalog(context.Context, media.Query) (media.Page, error) {
	if f.session.Token != "fixture-token" {
		return media.Page{}, media.ErrAuthentication
	}
	return media.Page{Items: []media.Item{{ID: "test-film", Kind: media.Movie, Title: "Fixture Film"}}, Total: 1}, nil
}

func (f fixtureServer) Detail(_ context.Context, item media.Item) (media.Item, error) {
	item.Overview = "Fixture synopsis"
	return item, nil
}

func (f fixtureServer) Children(context.Context, media.Item) ([]media.Item, error) { return nil, nil }
func (f fixtureServer) Resume(context.Context) ([]media.Item, error)               { return nil, nil }
func (f fixtureServer) Report(context.Context, media.PlaybackState) error          { return nil }

func (f fixtureServer) Playback(_ context.Context, item media.Item, _ media.PlaybackOptions) (media.Stream, error) {
	return media.Stream{ItemID: item.ID, URL: "http://nas.example.test:8096/fixture.mp4", Method: media.DirectPlay,
		Duration: 18 * time.Second}, nil
}
