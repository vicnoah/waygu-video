package main

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/tvpair"
)

func TestDockerTVSettingsReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker media fixtures")
	}
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("TV review capture runs on GTK")
	}
	config, secrets := homeReviewSources(t)
	config[danmakuPreferencesKey] = `{"Opacity":80,"Area":50,"Density":40,"Deduplicate":true}`
	for _, variant := range []struct {
		name       string
		appearance core.Appearance
	}{
		{"light", core.AppearanceLight}, {"dark", core.AppearanceDark},
	} {
		t.Run(variant.name, func(t *testing.T) {
			targets := make(chan tvpair.Target, 4)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomTV)
				cx.SetSubtreeAppearance(variant.appearance)
				tvPairListener.Provide(cx, func(ctx context.Context, apply func(context.Context, tvpair.Setup) error) (tvpair.Target, error) {
					target, err := tvpair.Listen(ctx, netip.MustParseAddr("127.0.0.1"), apply)
					if err == nil {
						targets <- target
					}
					return target, err
				})
				return Root(cx)
			}, nsgotest.Size(1920, 1080), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.WaitFor("id:tv-tab-settings", 8*time.Second)
			app.Press("id:tv-tab-settings")
			app.WaitFor("id:tv-pair-code", 4*time.Second)
			if width := app.Find("id:tv-settings-sidebar").Frame.Size.Width; width != 640 {
				t.Fatalf("sidebar is %v pt wide, want 640", width)
			}
			app.WaitUntil(time.Second, func() bool { return app.Find("id:tv-settings-section-sources").Focused })
			for _, next := range []string{"playback", "subtitles", "danmaku"} {
				app.Key("down")
				app.WaitUntil(time.Second, func() bool { return app.Find("id:tv-settings-section-" + next).Focused })
			}
			app.Key("right")
			app.WaitUntil(time.Second, func() bool { return app.Find("id:tv-danmaku-visible").Focused })
			for _, id := range []string{"sources", "opacity", "font-size", "speed", "area"} {
				app.Key("down")
				app.WaitUntil(time.Second, func() bool { return app.Find("id:tv-danmaku-" + id).Focused })
			}
			if width := app.Find("id:tv-danmaku-area").Frame.Size.Width; width != 1120 {
				t.Fatalf("options are %v pt wide, want 1120", width)
			}
			if nsgotest.Target() != "" {
				app.Screenshot("app-tv-settings-" + variant.name)
			}
			app.Key("right")
			if got := readDanmakuPreferences(app.Storage()).Area; got != 75 {
				t.Fatalf("area was saved as %d, want 75", got)
			}
			app.Key("left")
			if got := readDanmakuPreferences(app.Storage()).Area; got != 50 {
				t.Fatalf("area was saved as %d, want 50", got)
			}
			app.Key("up")
			app.Key("up")
			app.Key("up")
			app.Key("right")
			if got := readDanmakuPreferences(app.Storage()).Opacity; got != 85 {
				t.Fatalf("opacity was saved as %d, want 85", got)
			}
			if nsgotest.Target() == "" && variant.name == "light" {
				target := <-targets
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				result := make(chan error, 1)
				go func() {
					result <- target.Send(ctx, tvpair.Setup{AppID: "review-app", Secret: "review-key"})
				}()
				app.WaitFor("text:已收到设置", 8*time.Second)
				if err := <-result; err != nil {
					t.Fatal(err)
				}
				var paired bool
				for key, value := range app.Secrets.Items() {
					if key.Account == dandanCredentialKey {
						paired = value == `{"AppID":"review-app","Secret":"review-key"}`
					}
				}
				if !paired {
					t.Fatal("paired credential was not saved")
				}
				app.Press("id:tv-pair-card")
				app.WaitFor("id:tv-pair-code", 4*time.Second)
				target = <-targets
				password := os.Getenv("WAYGU_TEST_PASSWORD")
				if password == "" {
					password = "waygu-fixture-local"
				}
				go func() {
					result <- target.Send(ctx, tvpair.Setup{Provider: "emby", Address: "http://127.0.0.1:28096/emby", Username: "waygu-test", Password: password})
				}()
				app.WaitFor("text:已收到设置", 8*time.Second)
				if err := <-result; err != nil {
					t.Fatal(err)
				}
				stored, _ := app.Storage().GetItem(sourcesKey)
				var sources []source
				if err := json.Unmarshal([]byte(stored), &sources); err != nil || len(sources) != 4 ||
					sources[3].Address != "http://127.0.0.1:28096/emby" || len(sources[3].Libraries) == 0 {
					t.Fatal("the paired Emby library was not added")
				}
				paired = false
				for key, value := range app.Secrets.Items() {
					if key.Account == sources[3].credentialKey() && value != "" {
						paired = true
						break
					}
				}
				if !paired {
					t.Fatal("the paired Emby token was not staged in the credential store")
				}
			}
			app.Key("escape")
			app.WaitFor("id:tv-tab-settings", 4*time.Second)
		})
	}
}

func TestPhoneSendsTVSetup(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("the phone-to-TV transport test uses the local test host")
	}
	got := make(chan tvpair.Setup, 1)
	target, err := tvpair.Listen(t.Context(), netip.MustParseAddr("127.0.0.1"), func(_ context.Context, data tvpair.Setup) error {
		got <- data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Idiom(core.IdiomPhone))
	app.Press("id:onboarding-later")
	pressLibraryTestTab(app, screenSettings, "Settings")
	app.Press("id:open-phone-tv-pair")
	app.Type("id:phone-tv-pair-address", target.Link())
	app.Type("id:phone-tv-pair-server", "http://nas.example.test:8096")
	app.Type("id:phone-tv-pair-user", "viewer")
	app.Type("id:phone-tv-pair-password", "fixture-password")
	app.Press("id:phone-tv-pair-submit")
	app.WaitFor("text:Setup received", 8*time.Second)
	data := <-got
	if data.Provider != "emby" || data.Address != "http://nas.example.test:8096" ||
		data.Username != "viewer" || data.Password != "fixture-password" {
		t.Fatal("the phone sent different setup details")
	}
}
