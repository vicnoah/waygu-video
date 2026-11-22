package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestWelcomeReview(t *testing.T) {
	if nsgotest.Target() == "" {
		t.Skip("requires a screenshot target")
	}
	for _, variant := range []struct {
		name       string
		appearance core.Appearance
	}{{"light", core.AppearanceLight}, {"dark", core.AppearanceDark}} {
		t.Run(variant.name, func(t *testing.T) {
			windowHeight := core.Points(800)
			if nsgotest.Target() == "linux" {
				windowHeight += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				return Root(cx)
			}, nsgotest.Size(390, windowHeight), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}))
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
