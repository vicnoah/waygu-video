package main

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func (a *application) desktopSettingsSupplement(cx *core.Scope, link desktopSettingsLink) core.Node {
	failure := core.NewSignal[error](cx, nil)
	var content core.Node
	switch link.page {
	case "subtitles":
		content = a.desktopSettingsSubtitles(cx, failure)
	case "appearance":
		content = a.appearanceSettings(cx)
	case "library":
		label := locales.MergeIdenticalTitles(cx)
		content = desktopFormGroup(locales.MediaSources(cx),
			desktopFormRow(label, desktopFormToggle(cx, "library-merge-titles", label, a.mergeTitles.Get, func(value bool) {
				failure.Set(storeSettingValue(a.storage, mergeTitlesKey, a.mergeTitles, value, strconv.FormatBool(value)))
			})),
			desktopFormRow(locales.DesktopSourceVisibleLibraries(cx), desktopSettingsAction(cx, "desktop-library-settings", locales.MediaSources(cx), setupAccent,
				func() { a.desktopSettingsTab.Set(desktopSettingsSources) })),
		)
	case "sync":
		content = desktopFormGroup(locales.MediaSources(cx), desktopFormRow(locales.DesktopSourceSyncProgress(cx), desktopSettingsAction(cx,
			"desktop-sync-sources", locales.MediaSources(cx), setupAccent, func() { a.desktopSettingsTab.Set(desktopSettingsSources) })))
	case "storage":
		content = desktopFormGroup(locales.StorageDownloads(cx), desktopFormRow(locales.StorageDownloads(cx), desktopSettingsAction(cx,
			"desktop-settings-downloads", locales.SettingsEdit(cx), setupAccent, func() { a.openDesktopSettingsFlow(route{screen: screenDownloads}) })))
	case "parental":
		content = a.parentalSettings(cx)
	case "keys":
		content = desktopFormGroup(locales.PlaybackSettings(cx),
			desktopShortcut(locales.Pause(cx)+" / "+locales.Play(cx), "Space"),
			desktopShortcut(locales.DesktopSeekStep(cx), "← / →"),
			desktopShortcut(locales.DesktopPlayerVolume(cx), "↑ / ↓"),
			desktopShortcut(locales.Danmaku(cx), "D"),
			desktopShortcut(locales.Subtitles(cx), "S"),
			desktopShortcut(locales.SettingsAudio(cx), "A"),
			desktopShortcut(locales.PlaybackSpeed(cx), "[ / ]"),
			desktopShortcut(locales.AutoNextEpisode(cx), "N"),
			desktopShortcut(locales.DesktopPlayerFullscreen(cx), "F / Esc"),
			desktopShortcut(locales.DesktopPlayerPanel(cx), "Tab"),
		)
	case "about":
		content = desktopFormGroup(locales.AppName(cx),
			desktopFormRow(locales.DesktopSettingsVersion(cx, core.App().Version),
				desktopSettingsAction(cx, "desktop-about-releases", locales.DesktopSettingsReleasePage(cx), setupAccent, func() { cx.OpenURL(wayguReleasePage, nil) })),
		)
	}
	return core.View{Class: "gap-5", Children: []core.Node{desktopSettingsHeading(link.label, ""), content, settingsFailure(cx, failure)}}
}

func desktopShortcut(label, keys string) core.Node {
	return desktopFormRow(label, core.Text{Content: keys, Class: "rounded bg-zinc-900/5 px-2 py-1 text-xs dark:bg-white/10 " + setupTitle})
}

func (a *application) desktopSettingsSubtitles(cx *core.Scope, failure *core.Signal[error]) core.Node {
	prefs := a.subtitlePrefs.Get
	save := func(change func(*subtitlePreferences)) {
		value := a.subtitlePrefs.GetUntracked()
		change(&value)
		failure.Set(storeSettings(a.storage, subtitlePreferencesKey, a.subtitlePrefs, value))
	}
	audio := []ui.Option[string]{{Label: locales.SettingsOriginalLanguage(cx)}}
	for _, code := range subtitleLanguages {
		audio = append(audio, ui.Option[string]{Value: code, Label: settingsLanguageName(cx, code)})
	}
	return core.View{Class: "gap-5", Children: []core.Node{
		desktopFormGroup(locales.SettingsLanguages(cx),
			desktopFormRow(locales.SettingsPreferredSubtitles(cx), desktopSettingsAction(cx, "desktop-subtitle-language-order", locales.SettingsEdit(cx), setupAccent,
				func() { a.openDesktopSettingsFlow(route{screen: screenSubtitleLanguages}) })),
			desktopFormRow(locales.SettingsPreferredAudio(cx), desktopFormSelect(cx, "subtitle-audio-language", locales.SettingsPreferredAudio(cx), audio,
				func() string { return prefs().Audio }, func(value string) { save(func(p *subtitlePreferences) { p.Audio = value }) })),
			desktopFormRow(locales.SettingsAutoSubtitle(cx), desktopFormSelect(cx, "subtitle-auto-mode", locales.SettingsAutoSubtitle(cx), []ui.Option[string]{
				{Value: "always", Label: locales.SettingsAlwaysOn(cx)},
				{Value: "foreign", Label: locales.SettingsSubtitleForeign(cx)},
				{Value: "off", Label: locales.SubtitlesOff(cx)},
			}, func() string { return prefs().Mode }, func(value string) { save(func(p *subtitlePreferences) { p.Mode = value }) })),
		),
		desktopFormGroup(locales.SettingsSubtitleStyle(cx), subtitleStyleRows(cx, prefs, save)...),
	}}
}
