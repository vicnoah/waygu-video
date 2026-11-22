package main

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func desktopBitrates(cx *core.Scope) []ui.Option[int] {
	options := []ui.Option[int]{{Label: locales.OriginalQuality(cx)}}
	for _, value := range playbackBitrates[1:] {
		options = append(options, ui.Option[int]{Value: value, Label: locales.BitrateMegabits(cx, strconv.Itoa(value/1_000_000))})
	}
	return options
}

func desktopUnavailableChoice(cx *core.Scope, label string) core.Node {
	return desktopFormRow(label, desktopFormSelect(cx, "", label, []ui.Option[string]{{Label: locales.MetadataUnavailable(cx)}}, func() string { return "" }, nil))
}

func (a *application) desktopSettingsPlayback(cx *core.Scope) core.Node {
	failure := core.NewSignal[error](cx, nil)
	engine, quality, next, seek := locales.SettingsDecoder(cx), locales.DesktopDefaultQuality(cx), locales.AutoNextEpisode(cx), locales.DesktopSeekStep(cx)
	unavailable := func(label string) core.Node { return desktopUnavailableChoice(cx, label) }
	disabledSwitch := func(label string, value bool) core.Node {
		return desktopFormRow(label, desktopFormToggle(cx, "", label, func() bool { return value }, nil))
	}
	prefs := a.desktopPrefs.Get
	type steps struct{ back, forward int }
	stepLabel := func(value steps) string {
		return locales.DesktopSeekPair(cx, strconv.Itoa(value.back), strconv.Itoa(value.forward))
	}
	var seekOptions []ui.Option[steps]
	for _, value := range []steps{{5, 5}, {10, 10}, {10, 30}, {15, 30}, {30, 30}, {60, 60}} {
		seekOptions = append(seekOptions, ui.Option[steps]{Label: stepLabel(value), Value: value})
	}
	currentStep := func() steps { value := prefs(); return steps{value.Back, value.Forward} }
	nextOptions := []ui.Option[int]{{Label: locales.DesktopSettingsOff(cx)}}
	for _, seconds := range []int{5, 10, 20} {
		nextOptions = append(nextOptions, ui.Option[int]{Label: locales.NextEpisodeCountdown(cx, strconv.Itoa(seconds)), Value: seconds})
	}
	video := desktopFormGroup(locales.MovieFileVideo(cx),
		desktopFormRow(engine, desktopFormSelect(cx, "desktop-playback-engine", engine, []ui.Option[bool]{
			{Label: locales.DesktopPlayerNativeEngine(cx)}, {Label: locales.DesktopBuiltinMPV(cx), Value: true},
		}, a.mpvPreferred.Get, func(value bool) {
			failure.Set(storeSettingValue(a.storage, mpvKey, a.mpvPreferred, value, strconv.FormatBool(value)))
		})),
		unavailable(locales.SettingsHardwareDecode(cx)),
		desktopFormRow(quality, desktopFormSelect(cx, "bitrate-limit", quality, desktopBitrates(cx), a.bitrateLimit.Get, func(value int) {
			failure.Set(storeSettingValue(a.storage, bitrateKey, a.bitrateLimit, value, strconv.Itoa(value)))
		})),
		desktopFormRow(locales.SettingsPreferDirect(cx), desktopFormToggle(cx, "desktop-prefer-direct", locales.SettingsPreferDirect(cx),
			a.preferDirectPlay, func(enabled bool) { failure.Set(a.setPreferDirectPlay(enabled)) })),
		unavailable(locales.SettingsMatchHDR(cx)),
		unavailable(locales.SettingsToneMap(cx)),
		disabledSwitch(locales.SettingsMatchRate(cx), false),
		desktopFormRow(locales.SettingsPicture(cx), desktopFormSelect(cx, "desktop-picture-mode", locales.SettingsPicture(cx), []ui.Option[string]{
			{Label: locales.SettingsPictureOriginal(cx)}, {Label: locales.SettingsPictureCrop(cx), Value: "crop"},
			{Label: locales.SettingsPictureStretch(cx), Value: "stretch"},
		}, func() string { return a.playbackPrefs.Get().Picture }, func(value string) {
			player := a.playbackPrefs.GetUntracked()
			player.Picture = value
			failure.Set(storeSettings(a.storage, playbackPreferencesKey, a.playbackPrefs, player))
		})),
	)
	audio := desktopFormGroup(locales.SettingsAudio(cx),
		unavailable(locales.DesktopSettingsAudioOutput(cx)),
		disabledSwitch(locales.SettingsPassthrough(cx), false),
		disabledSwitch(locales.SettingsNightAudio(cx), false),
	)
	behavior := desktopFormGroup(locales.DesktopSettingsPlaybackBehavior(cx),
		desktopFormRow(next, desktopFormSelect(cx, "desktop-auto-next", next, nextOptions,
			func() int {
				if a.autoNext.Get() {
					return prefs().Countdown
				}
				return 0
			}, func(value int) {
				if value != 0 {
					err := a.saveDesktopPreferences(func(p *desktopPreferences) { p.Countdown = value })
					if err != nil {
						failure.Set(err)
						return
					}
				}
				failure.Set(storeSettingValue(a.storage, autoNextKey, a.autoNext, value != 0, strconv.FormatBool(value != 0)))
			})),
		desktopFormRow(locales.SettingsSkipIntro(cx), desktopFormToggle(cx, "desktop-skip-intro", locales.SettingsSkipIntro(cx),
			func() bool { return !a.playbackPrefs.Get().KeepIntroCredits }, func(enabled bool) {
				player := a.playbackPrefs.GetUntracked()
				player.KeepIntroCredits = !enabled
				failure.Set(storeSettings(a.storage, playbackPreferencesKey, a.playbackPrefs, player))
			})),
		desktopFormRow(seek, desktopFormSelect(cx, "desktop-seek-step", seek, seekOptions, currentStep,
			func(value steps) {
				failure.Set(a.saveDesktopPreferences(func(p *desktopPreferences) { p.Back, p.Forward = value.back, value.forward }))
			})),
		disabledSwitch(locales.DesktopRememberTracks(cx), false),
	)
	return core.View{Class: "gap-5", Children: []core.Node{
		desktopSettingsHeading(locales.PlaybackSettings(cx), locales.DesktopSettingsPlaybackHint(cx)),
		core.View{Class: "flex-row gap-5", Children: []core.Node{
			core.View{Class: "min-w-0 flex-1", Children: []core.Node{video}},
			core.View{Class: "min-w-0 flex-1 gap-5", Children: []core.Node{audio, behavior}},
		}},
		settingsFailure(cx, failure),
	}}
}
