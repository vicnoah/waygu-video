package main

import (
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func (a *application) phoneGestureSettings(cx *core.Scope, failure *core.Signal[error],
	save func(func(*playbackPreferences))) (core.Node, core.Node) {
	open := core.NewSignal(cx, false)
	prefs := func() phoneGesturePreferences { return a.playbackPrefs.Get().Gestures }
	brightness, volume, seek := locales.SettingsGestureBrightness(cx), locales.SettingsGestureVolume(cx), locales.SettingsGestureSeek(cx)
	summary := func() string {
		current := prefs()
		var enabled []string
		if !current.DisableBrightness {
			enabled = append(enabled, brightness)
		}
		if !current.DisableVolume {
			enabled = append(enabled, volume)
		}
		if !current.DisableSeek {
			enabled = append(enabled, seek)
		}
		if len(enabled) == 0 {
			return locales.SettingsGesturesOff(cx)
		}
		return strings.Join(enabled, " · ")
	}
	title := locales.SettingsPlaybackGestures(cx)
	entry := (phoneSetting{title, "", "bg-blue-500", lucide.Hand}).
		linkValue(cx, "phone-playback-gestures", summary, func() { open.Set(true) })
	dialog := ui.Dialog{Title: title, Open: open.Get, TestID: "phone-playback-gestures-dialog",
		OnOpenChange: func(value bool, _ base.Reason) { open.Set(value) },
		Children: []core.Node{
			setupGroup("", "",
				(phoneSetting{brightness, locales.SettingsGestureLeftSwipe(cx), "bg-blue-500", lucide.Sun}).
					toggle(cx, "phone-gesture-brightness", func() bool { return !prefs().DisableBrightness },
						func(enabled bool) { save(func(p *playbackPreferences) { p.Gestures.DisableBrightness = !enabled }) }),
				(phoneSetting{volume, locales.SettingsGestureRightSwipe(cx), "bg-blue-500", lucide.Volume2}).
					toggle(cx, "phone-gesture-volume", func() bool { return !prefs().DisableVolume },
						func(enabled bool) { save(func(p *playbackPreferences) { p.Gestures.DisableVolume = !enabled }) }),
				(phoneSetting{seek, locales.SettingsGestureHorizontalSwipe(cx), "bg-blue-500", lucide.MoveHorizontal}).
					toggle(cx, "phone-gesture-seek", func() bool { return !prefs().DisableSeek },
						func(enabled bool) { save(func(p *playbackPreferences) { p.Gestures.DisableSeek = !enabled }) }),
			),
			settingsFailure(cx, failure),
		}, Footer: []core.Node{
			ui.Button{Text: locales.TracksDone(cx), TestID: "phone-gestures-done", OnPress: func() { open.Set(false) }},
		}}
	return entry, dialog
}
