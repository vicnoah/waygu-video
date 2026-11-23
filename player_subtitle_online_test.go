package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

type onlineSubtitleTestTransport func(*http.Request) (*http.Response, error)

func (transport onlineSubtitleTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return transport(r)
}

func TestDockerOnlineSubtitlePlayback(t *testing.T) {
	target := nsgotest.Target()
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || (target != "" && target != "linux" && target != "android") {
		t.Skip("requires the local Docker fixture servers")
	}
	// The native results share the phone controls. Headless runs also
	// exercise each server's original-file authorization and range headers.
	if target != "" {
		testDockerOnlineSubtitlePlayback(t, "plex", target)
		return
	}
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) { testDockerOnlineSubtitlePlayback(t, provider, target) })
	}
}

func testDockerOnlineSubtitlePlayback(t *testing.T, provider, target string) {
	t.Helper()
	config, secrets := homeReviewSources(t)
	const fixture = "Waygu Portable Captions Test"
	_, movie := reviewFixtureItem(t, config, secrets, provider, fixture)
	config[subtitlePreferencesKey] = `{"Languages":["en"],"Mode":"off","DisableShooter":true}`
	config[playbackPreferencesKey] = `{"DisableProgressRecording":true}`
	secrets[openSubtitleSecret] = `{"Key":"fixture-key"}`
	var searches, downloads atomic.Int64
	transport := onlineSubtitleTestTransport(func(r *http.Request) (*http.Response, error) {
		var body string
		status := http.StatusOK
		switch r.URL.Path {
		case "/api/v1/subtitles":
			searches.Add(1)
			if r.Header.Get("Api-Key") != "fixture-key" || r.URL.Query().Get("moviehash") == "" || r.URL.Query().Get("languages") != "en" {
				t.Errorf("OpenSubtitles search missing the original-file fingerprint or language filter: %v", r.URL.Query())
			}
			body = `{"data":[{"attributes":{"language":"en","files":[{"file_id":29,"file_name":"Waygu Online Captions.en.srt"}]}}]}`
		case "/api/v1/download":
			body = `{"link":"https://www.opensubtitles.com/download/waygu-fixture"}`
		case "/download/waygu-fixture":
			if r.Header.Get("Api-Key") != "" {
				t.Error("file request leaked the API key")
			}
			downloads.Add(1)
			body = "1\n00:00:00,750 --> 00:00:15,000\nLoaded from the subtitle service.\n"
		default:
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Request: r, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	window := nsgotest.Size(844, 390)
	if target == "linux" {
		window = nsgotest.Size(390, 837)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		onlineSubtitleTransport.Provide(cx, transport)
		return Root(cx)
	}, window, nsgotest.Offscreen(), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.Press("id:tab-search")
	app.Type("id:search-query", fixture)
	app.Press("id:search-source-" + provider)
	title := "id:title-" + provider + "-" + movie.ID
	app.WaitFor(title, 10*time.Second)
	app.Press(title)

	const play = "id:phone-player-play-small"
	reveal := func() {
		if !app.Exists(play) {
			app.Press("id:phone-player-surface")
		}
		app.WaitFor(play, 4*time.Second)
	}
	pauseAtCue := func() {
		app.WaitFor("id:detail-play", 10*time.Second)
		app.Press("id:detail-play")
		if target == "linux" {
			app.Resize(844, 427)
		}
		app.WaitFor("id:video-player", 15*time.Second)
		reveal()
		if target == "" {
			app.Play("id:video-player", core.VideoInfo{Duration: 18 * time.Second})
			app.Progress("id:video-player", 3*time.Second, 18*time.Second)
			app.Press(play)
		} else {
			app.WaitFor("id:phone-player-resolution", 10*time.Second)
			reveal()
			if app.Find(play).Props.Label == "暂停" {
				app.Press(play)
			}
			const rail = "id:phone-player-position-rail"
			app.PressAt(rail, core.Point{X: app.Find(rail).Frame.Size.Width / 6, Y: 8})
		}
		reveal()
		app.WaitUntil(4*time.Second, func() bool { return app.Exists(play) && app.Find(play).Props.Label == "播放" })
	}
	pauseAtCue()
	position := app.Find("id:player-position").Text
	app.Press("id:player-tracks")
	app.WaitFor("id:online-subtitle-search", 2*time.Second)
	if target != "" {
		app.Screenshot("online-subtitle-entry")
	}
	app.Press("id:online-subtitle-search")
	app.WaitFor("id:online-subtitle-result-0", 10*time.Second)
	if target != "" {
		app.Screenshot("online-subtitle-results")
	}
	app.Press("id:online-subtitle-result-0")
	app.WaitFor("text:Loaded from the subtitle service.", 5*time.Second)
	app.WaitUntil(3*time.Second, func() bool { return app.Find("id:cached-online-subtitle").Props.Checked })
	app.Press("id:subtitle-later")
	app.WaitFor("text:+0.5s", 2*time.Second)
	if target != "" {
		app.Screenshot("online-subtitle-selected")
	}
	app.Press("id:close-player-tracks")
	reveal()
	if app.Find("id:player-position").Text != position || app.Find(play).Props.Label != "播放" {
		t.Fatal("the online subtitle changed the paused playhead")
	}
	app.Press("id:close-player")

	pauseAtCue()
	app.WaitFor("text:Loaded from the subtitle service.", 5*time.Second)
	app.Press("id:player-tracks")
	app.WaitFor("text:+0.5s", 2*time.Second)
	if searches.Load() != 1 || downloads.Load() != 1 {
		t.Fatalf("cache replay made another provider request: searches %d, downloads %d", searches.Load(), downloads.Load())
	}
}

func TestPhoneOpenSubtitleAccountSettings(t *testing.T) {
	var logins atomic.Int64
	transport := onlineSubtitleTestTransport(func(r *http.Request) (*http.Response, error) {
		status := http.StatusBadRequest
		var body string
		if r.URL.Path == "/api/v1/login" && r.Header.Get("Api-Key") == "fixture-key" {
			logins.Add(1)
			status = http.StatusOK
			body = `{"token":"fixture-session"}`
		}
		return &http.Response{StatusCode: status, Request: r, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	height := core.Points(1780)
	if nsgotest.Target() == "linux" {
		height += 37
	}
	var secretStore core.Secrets
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		secretStore = cx.Secrets()
		shellIdiom.Provide(cx, core.IdiomPhone)
		onlineSubtitleTransport.Provide(cx, transport)
		cx.SetSubtreeAppearance(core.AppearanceLight)
		return Root(cx)
	}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Storage(map[string]string{seenKey: "true"}))
	app.Press("id:tab-settings")
	app.Press("id:open-phone-subtitle-settings")
	app.Press("id:configure-opensubtitles")
	app.Type("id:online-subtitle-api-key", "fixture-key")
	app.Type("id:online-subtitle-user", "fixture")
	app.Type("id:online-subtitle-password", "fixture-password")
	app.Press("id:online-subtitle-save")
	app.WaitFor("text:已登录 · fixture", 3*time.Second)
	creds, err := readOpenSubtitleCredentials(secretStore)
	if err != nil || creds.Key != "fixture-key" || creds.Username != "fixture" || logins.Load() != 1 {
		t.Fatal("successful account settings did not persist the verified credentials in the secret store")
	}
	if raw, _ := app.Storage().GetItem(subtitlePreferencesKey); strings.Contains(raw, "fixture-key") || strings.Contains(raw, "fixture-password") {
		t.Fatal("subtitle preferences exposed durable API credentials")
	}
	app.Press("id:online-subtitle-automatic")
	app.Press("id:online-subtitle-shooter")
	app.Press("id:online-subtitle-shooter-enabled")
	prefs := readSubtitlePreferences(app.Storage())
	if !prefs.DownloadMissing || !prefs.DisableShooter {
		t.Fatal("online subtitle preferences did not persist")
	}
	app.Press("id:online-subtitle-shooter-enabled")
	app.Press("id:back")
	app.WaitFor("id:online-subtitle-shooter >> text:已启用", 3*time.Second)
	if nsgotest.Target() != "" {
		app.Screenshot("online-subtitle-settings-configured")
	}
	app.Press("id:configure-opensubtitles")
	app.WaitFor("id:online-subtitle-remove", 3*time.Second)
	app.Press("id:online-subtitle-remove")
	app.WaitFor("id:configure-opensubtitles >> text:未启用", 3*time.Second)
	if _, found, err := secretStore.Get(openSubtitleSecret); found || err != nil {
		t.Fatalf("removing credentials left the secret in the store: %v", err)
	}
}

func TestAutomaticOnlineSubtitleRequiresFileMatch(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses a simulated playback clock")
	}
	const fileSize = 3 * 65536
	fixture := make([]byte, fileSize)
	mediaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "Automatic.mp4", time.Time{}, bytes.NewReader(fixture))
	}))
	defer mediaServer.Close()
	for _, exact := range []bool{false, true} {
		name := "no-file-match"
		if exact {
			name = "exact-file-match"
		}
		t.Run(name, func(t *testing.T) {
			var searches, downloads atomic.Int64
			transport := onlineSubtitleTestTransport(func(r *http.Request) (*http.Response, error) {
				status := http.StatusOK
				var body string
				switch r.URL.Path {
				case "/api/v1/subtitles":
					searches.Add(1)
					if r.URL.Query().Get("moviehash") == "" {
						t.Error("automatically downloading subtitles must only search by file fingerprint")
					}
					body = `{"data":[]}`
					if exact {
						body = `{"data":[{"attributes":{"language":"en","files":[{"file_id":17,"file_name":"Automatic.srt"}]}}]}`
					}
				case "/api/v1/download":
					downloads.Add(1)
					body = `{"link":"https://www.opensubtitles.com/download/automatic"}`
				case "/download/automatic":
					body = "1\n00:00:01,000 --> 00:00:04,000\nAutomatic online caption.\n"
				default:
					status = http.StatusNotFound
				}
				return &http.Response{StatusCode: status, Request: r, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				online := &onlineSubtitlePlayback{item: media.Item{ID: "automatic", Title: "Automatic"},
					sourceID: "fixture", storage: cx.Storage(), secrets: cx.Secrets(), transport: transport, allowSearch: true}
				prefs := func() subtitlePreferences {
					return subtitlePreferences{Mode: "always", Languages: []string{"en"}, DownloadMissing: true, DisableShooter: true}
				}
				stream := func() media.Stream {
					return media.Stream{File: media.SourceFile{URL: mediaServer.URL, Name: "Automatic.mp4", Size: fileSize}}
				}
				tracks := playerTracks(cx, stream, func() *desktopVideo { return nil }, nil, "", prefs, core.NewSignal(cx, 2*time.Second),
					core.NewSignal(cx, float32(1)), core.NewSignal(cx, true), core.NewSignal(cx, false), core.NewSignal(cx, false),
					func(media.Track) {}, nil, nil, online)
				return core.View{Class: "flex-1", Children: []core.Node{tracks.button, tracks.dialog, tracks.captions}}
			}, nsgotest.Size(844, 390), nsgotest.Secrets(map[string]string{openSubtitleSecret: `{"Key":"fixture-key"}`}))
			app.WaitUntil(time.Second, func() bool { return searches.Load() == 1 })
			if exact {
				app.WaitFor("text:Automatic online caption.", time.Second)
				if downloads.Load() != 1 {
					t.Fatal("the exact match was not downloaded")
				}
			} else {
				app.Advance(500 * time.Millisecond)
				if downloads.Load() != 0 || app.Exists("id:subtitle-caption") {
					t.Fatal("an unmatched file triggered a subtitle download")
				}
			}
		})
	}
}
