package main

import (
	"crypto/rand"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"waygu-video/locales"
)

const (
	androidBackgroundSupport = "waygu.playback.background.supported"
	androidBackgroundStart   = "waygu.playback.background.start"
	androidBackgroundStop    = "waygu.playback.background.stop"
	androidBackgroundPause   = "waygu.playback.background.pause"
	androidBackgroundTimeout = 3 * time.Second
)

func androidHandset(cx *core.Scope) bool {
	env := cx.Env()
	return env.OS() == core.OSAndroid && (env.Idiom() == core.IdiomPhone || env.Idiom() == core.IdiomTablet)
}

func (a *application) phoneBackgroundAudioSetting(cx *core.Scope, save func(func(*playbackPreferences))) core.Node {
	available := core.NewSignal(cx, false)
	if androidHandset(cx) {
		cx.CallHostTimeout(androidBackgroundSupport, nil, androidBackgroundTimeout, func(result any, err error) {
			supported, _ := result.(bool)
			available.Set(err == nil && supported)
		})
	}
	setting := phoneSetting{locales.SettingsBackgroundAudio(cx), "", "bg-emerald-500", lucide.Headphones}
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if !available.Get() {
			return setting.toggle(cx, "", func() bool { return false }, nil)
		}
		return setting.toggle(cx, "phone-background-audio", func() bool { return a.playbackPrefs.Get().BackgroundAudio },
			func(enabled bool) { save(func(p *playbackPreferences) { p.BackgroundAudio = enabled }) })
	})
}

func androidBackgroundPlayback(cx *core.Scope, title string, enabled bool, ready, playing func() bool, pause func()) {
	if !androidHandset(cx) {
		return
	}
	granted := core.NewSignal(cx, false)
	var lease string
	release := func() {
		if lease != "" {
			cx.PostHost(androidBackgroundStop, lease)
			lease = ""
			granted.Set(false)
		}
	}
	cx.Cleanup(release)
	cx.OnHostEvent(androidBackgroundPause, func(data any) {
		if token, ok := data.(string); ok && token != "" && token == lease {
			pause()
			release()
		}
	})
	cx.Effect(func() {
		life := cx.Env().Lifecycle()
		if !playing() {
			release()
			return
		}
		if life == core.LifecycleBackground {
			if !granted.Get() {
				pause()
			}
			return
		}
		// Android only lets a visible app start this kind of foreground service.
		if enabled && life == core.LifecycleActive && ready() && lease == "" {
			token := rand.Text()
			lease = token
			cx.Run(func() {
				cx.CallHostTimeout(androidBackgroundStart, map[string]string{
					"token": token, "title": title, "channel": locales.PlaybackSettings(cx), "pause": locales.Pause(cx),
				}, androidBackgroundTimeout, func(result any, err error) {
					if lease == token {
						started, _ := result.(bool)
						granted.Set(err == nil && started)
					}
				})
			})
		}
	})
}
