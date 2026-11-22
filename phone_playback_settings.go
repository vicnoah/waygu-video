package main

import (
	"fmt"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) phonePlaybackSettings(cx *core.Scope) core.Node {
	failure := core.NewSignal[error](cx, nil)
	save := func(change func(*playbackPreferences)) {
		prefs := a.playbackPrefs.GetUntracked()
		change(&prefs)
		failure.Set(storeSettings(a.storage, playbackPreferencesKey, a.playbackPrefs, prefs))
	}
	quality := []ui.Option[int]{{Label: locales.OriginalQuality(cx)}}
	for _, bitrate := range playbackBitrates[1:] {
		quality = append(quality, ui.Option[int]{Value: bitrate, Label: locales.BitrateMegabits(cx, strconv.Itoa(bitrate/1_000_000))})
	}
	steps := make([]ui.Option[int], 0, len(seekIntervals))
	for _, seconds := range seekIntervals {
		steps = append(steps, ui.Option[int]{Value: seconds, Label: locales.SettingsSeconds(cx, strconv.Itoa(seconds))})
	}
	rates := make([]ui.Option[float32], 0, len(playbackRates))
	for _, rate := range playbackRates {
		rates = append(rates, ui.Option[float32]{Value: rate, Label: playbackRateLabel(rate)})
	}
	picture := []ui.Option[string]{
		{Label: locales.SettingsPictureOriginal(cx)},
		{Value: "crop", Label: locales.SettingsPictureCrop(cx)},
		{Value: "stretch", Label: locales.SettingsPictureStretch(cx)},
	}
	unavailable := func(label, desc, tint string, icon func() *core.SVG) core.Node {
		return (phoneSetting{label, desc, tint, icon}).unavailable(cx)
	}

	unavailableSwitch := func(label, desc, tint string, icon func() *core.SVG) core.Node {
		return (phoneSetting{label, desc, tint, icon}).toggle(cx, "", func() bool { return false }, nil)
	}
	pipControl := unavailableSwitch(locales.SettingsAutoPiP(cx), "", "bg-emerald-500", lucide.PictureInPicture2)
	if cx.Env().OS() == core.OSIOS {
		pipControl = (phoneSetting{locales.SettingsAutoPiP(cx), "", "bg-emerald-500", lucide.PictureInPicture2}).
			toggle(cx, "phone-auto-pip", func() bool { return !a.playbackPrefs.Get().DisableAutoPiP },
				func(enabled bool) { save(func(p *playbackPreferences) { p.DisableAutoPiP = !enabled }) })
	}

	versionControls := []core.Node{
		phoneSettingChoice(cx, phoneSetting{locales.PreferredPlaybackVersion(cx), "", "bg-violet-500", lucide.Layers},
			"phone-playback-version-policy", versionPolicyOptions(cx), a.versionPolicy.Get, func(policy media.VersionPolicy) {
				failure.Set(storeSettingValue(a.storage, versionPolicyKey, a.versionPolicy, policy, string(policy)))
			}),
	}
	if cx.Env().Desktop() {
		control := phoneSetting{locales.PreferMPV(cx), "", "bg-violet-500", lucide.Film}
		versionControls = append(versionControls, control.toggle(cx, "phone-prefer-mpv", a.mpvPreferred.Get, func(enabled bool) {
			failure.Set(storeSettingValue(a.storage, mpvKey, a.mpvPreferred, enabled, strconv.FormatBool(enabled)))
		}))
	}

	return phoneSettingsPage(cx, locales.PlaybackSettings(cx), a.back, "phone-playback-settings",
		setupGroup(locales.PlayerQuality(cx), locales.StreamingQualityProviders(cx),
			phoneSettingChoice(cx, phoneSetting{locales.SettingsWifiQuality(cx), "", "bg-violet-500", lucide.Wifi},
				"phone-streaming-quality", quality, a.bitrateLimit.Get, func(value int) {
					failure.Set(storeSettingValue(a.storage, bitrateKey, a.bitrateLimit, value, strconv.Itoa(value)))
				}),
			unavailable(locales.SettingsCellularQuality(cx), "", "bg-violet-500", lucide.Signal),
			(phoneSetting{locales.SettingsPreferDirect(cx), "", "bg-emerald-500", lucide.Zap}).
				toggle(cx, "", func() bool { return true }, nil),
			unavailableSwitch(locales.SettingsCellularWarning(cx), "", "bg-amber-500", lucide.TriangleAlert),
		),
		setupGroup(locales.SettingsDecoderDisplay(cx), "",
			unavailableSwitch(locales.SettingsHardwareDecode(cx), "", "bg-sky-500", lucide.Cpu),
			unavailable(locales.SettingsDecoder(cx), "", "bg-sky-500", lucide.Binary),
			unavailableSwitch(locales.SettingsMatchHDR(cx), locales.SettingsMatchHDRDescription(cx), "bg-orange-500", lucide.Sun),
			unavailable(locales.SettingsToneMap(cx), "", "bg-orange-500", lucide.SunDim),
			unavailableSwitch(locales.SettingsMatchRate(cx), locales.SettingsMatchRateDescription(cx), "bg-teal-500", lucide.Gauge),
			phoneSettingChoice(cx, phoneSetting{locales.SettingsPicture(cx), "", "bg-zinc-500", lucide.Ratio},
				"phone-picture-mode", picture, func() string { return a.playbackPrefs.Get().Picture },
				func(value string) { save(func(p *playbackPreferences) { p.Picture = value }) }),
		),
		setupGroup(locales.SettingsAudio(cx), "",
			unavailableSwitch(locales.SettingsPassthrough(cx), locales.SettingsPassthroughDescription(cx), "bg-pink-500", lucide.Speaker),
			unavailable(locales.SettingsPassthroughFormats(cx), "", "bg-pink-500", lucide.AudioLines),
			unavailableSwitch(locales.SettingsBoostVolume(cx), "", "bg-rose-500", lucide.Volume2),
			unavailableSwitch(locales.SettingsNightAudio(cx), "", "bg-indigo-500", lucide.Moon),
		),
		setupGroup(locales.SettingsEpisodes(cx), "",
			(phoneSetting{locales.AutoNextEpisode(cx), "", "bg-violet-500", lucide.SkipForward}).
				toggle(cx, "phone-auto-next", a.autoNext.Get, func(enabled bool) {
					failure.Set(storeSettingValue(a.storage, autoNextKey, a.autoNext, enabled, strconv.FormatBool(enabled)))
				}),
			(phoneSetting{locales.SettingsSkipIntro(cx), "", "bg-violet-500", lucide.FastForward}).
				toggle(cx, "phone-skip-intro", func() bool { return !a.playbackPrefs.Get().KeepIntroCredits },
					func(enabled bool) { save(func(p *playbackPreferences) { p.KeepIntroCredits = !enabled }) }),
			(phoneSetting{locales.SettingsIdentifyIntro(cx), "", "bg-violet-500", lucide.ScanSearch}).
				row(phoneSettingValueSized(func() string { return locales.SettingsIntroMarkers(cx) }, "w-[196px] justify-end")),
		),
		setupGroup(locales.SettingsControls(cx), "",
			(phoneSetting{locales.SettingsRememberProgress(cx), "", "bg-blue-500", lucide.Clock}).
				toggle(cx, "", func() bool { return true }, nil),
			phoneSettingChoice(cx, phoneSetting{locales.SettingsSeekBack(cx), "", "bg-blue-500", lucide.RotateCcw},
				"phone-seek-back", steps, func() int { return a.playbackPrefs.Get().BackSeconds },
				func(value int) { save(func(p *playbackPreferences) { p.BackSeconds = value }) }),
			phoneSettingChoice(cx, phoneSetting{locales.SettingsSeekForward(cx), "", "bg-blue-500", lucide.RotateCw},
				"phone-seek-forward", steps, func() int { return a.playbackPrefs.Get().NextSeconds },
				func(value int) { save(func(p *playbackPreferences) { p.NextSeconds = value }) }),
			phoneSettingChoice(cx, phoneSetting{locales.SettingsDefaultRate(cx), "", "bg-blue-500", lucide.Gauge},
				"phone-default-rate", rates, func() float32 { return a.playbackPrefs.Get().Rate },
				func(value float32) { save(func(p *playbackPreferences) { p.Rate = value }) }),
			unavailable(locales.SettingsPlaybackGestures(cx), "", "bg-blue-500", lucide.Hand),
		),
		setupGroup(locales.SettingsBackgroundNetwork(cx), "",
			pipControl,
			unavailableSwitch(locales.SettingsBackgroundAudio(cx), "", "bg-emerald-500", lucide.Headphones),
			unavailable(locales.SettingsPlaybackBuffer(cx), "", "bg-zinc-500", lucide.HardDriveDownload),
		),
		setupGroup(locales.PlaybackVersion(cx), "", versionControls...),
		settingsFailure(cx, failure),
	)
}

func playbackRateLabel(rate float32) string {
	if rate == float32(int(rate)) {
		return fmt.Sprintf("%.1fx", rate)
	}
	return strconv.FormatFloat(float64(rate), 'f', -1, 32) + "x"
}
