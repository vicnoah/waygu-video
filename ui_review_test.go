package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

var reviewAppearances = []struct {
	name       string
	appearance core.Appearance
}{{"light", core.AppearanceLight}, {"dark", core.AppearanceDark}}

func startReview(t *testing.T, appearance core.Appearance, height core.Points) *nsgotest.App {
	t.Helper()
	if nsgotest.Target() == "linux" {
		height += 37
	}
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetSubtreeAppearance(appearance)
		return Root(cx)
	}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}))
}

func TestWelcomeReview(t *testing.T) {
	if nsgotest.Target() == "" {
		t.Skip("requires a screenshot target")
	}
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			app := startReview(t, variant.appearance, 800)
			app.WaitFor("id:add-source", 5*time.Second)
			app.Advance(300 * time.Millisecond)
			if height := app.Find("id:welcome-hero").Frame.Size.Height; height != 250 {
				t.Errorf("poster wall height = %v pt, want 250 pt", height)
			}
			if height := app.Find("id:add-source").Frame.Size.Height; height != 52 {
				t.Errorf("primary action height = %v pt, want 52 pt", height)
			}
			if rows := len(app.Find("id:welcome-features").Children); rows != 4 {
				t.Errorf("feature rows = %d, want 4", rows)
			}
			app.Screenshot("welcome-" + variant.name)
			app.Press("id:add-source")
			app.WaitFor("id:choose-emby", time.Second)
			app.Press("id:back")
			app.WaitFor("id:onboarding-later", time.Second)
			app.Press("id:onboarding-later")
			app.WaitFor("id:empty-add-source", time.Second)
		})
	}
}

func TestSourceReview(t *testing.T) {
	if nsgotest.Target() == "" {
		t.Skip("requires a screenshot target")
	}
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			for _, design := range []struct {
				name   string
				height core.Points
			}{{"add-source", 1180}, {"source-login", 1240}} {
				t.Run(design.name, func(t *testing.T) {
					app := startReview(t, variant.appearance, design.height)
					app.WaitFor("id:add-source", 5*time.Second)
					app.Press("id:add-source")
					app.WaitFor("id:choose-emby", time.Second)
					if design.name == "source-login" {
						app.Press("id:choose-emby")
						app.WaitFor("id:server-address", time.Second)
						app.Find("id:server-port")
						app.Press("id:server-https")
						if !app.Find("id:server-https").Props.Checked {
							t.Fatal("HTTPS switch did not turn on")
						}
						app.Press("id:server-https")
					}
					app.Advance(300 * time.Millisecond)
					app.Screenshot(design.name + "-" + variant.name)
					if design.name == "source-login" {
						for _, kind := range []string{"jellyfin", "flymoo", "plex"} {
							app.Press("id:connect-" + kind)
							app.WaitFor("id:server-password", time.Second)
							if !app.Find("id:connect-" + kind).Props.Selected {
								t.Fatalf("%s tab is not selected", kind)
							}
							switch kind {
							case "jellyfin":
								app.Find("id:quick-connect")
							case "plex":
								app.Find("id:link-plex-account")
							}
							app.Screenshot(design.name + "-" + variant.name + "-" + kind)
						}
					}
					app.Press("id:back")
					if design.name == "add-source" {
						app.WaitFor("id:onboarding-later", time.Second)
					} else {
						app.WaitFor("id:choose-emby", time.Second)
					}
				})
			}
		})
	}
}

func TestDockerSourceReview(t *testing.T) {
	if nsgotest.Target() == "" || os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires a native screenshot target and the Docker fixtures")
	}
	password := os.Getenv("WAYGU_TEST_PASSWORD")
	if password == "" {
		password = "waygu-fixture-local"
	}
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			app := startReview(t, variant.appearance, 1240)
			app.Press("id:add-source")
			app.WaitFor("id:local-emby", 2*time.Second)
			app.WaitFor("id:local-jellyfin", 2*time.Second)
			app.Press("id:local-emby")
			app.WaitFor("id:server-address", time.Second)
			if address := app.Find("id:server-address").Text; address == "" {
				t.Fatal("discovery did not prefill the address")
			}
			if port := app.Find("id:server-port").Text; port != "8096" {
				t.Fatalf("discovery port = %q, want 8096", port)
			}
			app.Press("id:test-connection")
			app.WaitFor("id:connection-verified", 5*time.Second)
			app.Type("id:server-name", "Waygu Emby")
			app.Type("id:server-username", "waygu-test")
			app.Type("id:server-password", password)
			app.Press("id:sign-in")
			app.WaitFor("id:import-libraries", 5*time.Second)
			app.WaitFor("id:connection-verified", time.Second)
			app.Screenshot("source-login-" + variant.name + "-connected")
			app.Press("id:import-libraries")
			app.WaitFor("text:Waygu Test Movie", 5*time.Second)
			if saved, _ := app.Storage().GetItem(sourcesKey); saved == "" || strings.Contains(saved, password) {
				t.Fatal("source was not saved or contains the user's password")
			}
		})
	}
}
