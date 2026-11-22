package main

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/locales"
)

const appearanceKey = "appearance-v1"

var appearanceChoices = []string{"system", "light", "dark"}

func (a *application) applyAppearance(cx *core.Scope) {
	stored, _ := a.storage.GetItem(appearanceKey)
	if stored != "light" && stored != "dark" {
		stored = "system"
	}
	a.appearance = core.NewSignal(cx, stored)
	forced := false
	cx.Effect(func() {
		switch a.appearance.Get() {
		case "light":
			cx.SetSubtreeAppearance(core.AppearanceLight)
			forced = true
		case "dark":
			cx.SetSubtreeAppearance(core.AppearanceDark)
			forced = true
		default:
			if forced {
				cx.SetSubtreeAppearance(core.AppearanceSystem)
				forced = false
			}
		}
	})
}

func (a *application) tvSettingsOptions(cx *core.Scope, l tvLayout, section tvSettingsSection, current route, sources []source, dialogs *[]core.Node) []tvOption {
	link := func(id, label string, destination route) tvOption {
		return tvOption{id: id, label: label, action: func() { a.push(destination) }}
	}
	unavailable := func(id, label string) []tvOption {
		return []tvOption{{id: id, label: label, value: func() string { return locales.SettingsUnavailable(cx) },
			disabled: func() bool { return true }}}
	}
	switch section {
	case tvSources:
		rows := []tvOption{link("settings-add-source", locales.AddSource(cx), route{screen: screenSources})}
		for _, entry := range sources {
			row := link("tv-source-"+entry.ID, entry.Name, route{screen: screenManageLibraries, source: entry})
			row.value = func() string { return providerName(cx, entry.Provider) }
			rows = append(rows, row)
		}
		return append(rows, link("tv-source-manager", locales.SettingsSourceManager(cx), route{screen: screenSourceSettings}))
	case tvPlayback:
		set := func(value bool) {
			_ = storeSettingValue(a.storage, autoNextKey, a.autoNext, value, strconv.FormatBool(value))
		}
		return []tvOption{
			{id: "tv-settings-auto-next", label: locales.AutoNextEpisode(cx), checked: a.autoNext.Get,
				action: func() { set(!a.autoNext.GetUntracked()) }, adjust: func(direction int) { set(direction > 0) }},
			link("tv-open-playback-settings", locales.SettingsPlaybackExperience(cx), route{screen: screenPlaybackSettings}),
		}
	case tvSubtitles:
		return []tvOption{
			link("tv-open-subtitle-settings", locales.SettingsSubtitleStyle(cx), route{screen: screenSubtitleSettings}),
			link("tv-open-subtitle-languages", locales.SettingsPreferredSubtitles(cx), route{screen: screenSubtitleLanguages}),
		}
	case tvDanmaku:
		return a.tvDanmakuOptions(cx, l, current, dialogs)
	case tvAppearance:
		labels := []string{locales.SettingsFollowSystem(cx), locales.TvSettingsLight(cx), locales.TvSettingsDark(cx)}
		index := func() int {
			switch a.appearance.Get() {
			case "light":
				return 1
			case "dark":
				return 2
			default:
				return 0
			}
		}
		adjust := func(delta int) {
			next := appearanceChoices[max(0, min(2, index()+delta))]
			_ = storeSettingValue(a.storage, appearanceKey, a.appearance, next, next)
		}
		return []tvOption{{id: "tv-settings-appearance", label: locales.SettingsAppearance(cx),
			value: func() string { return labels[index()] }, adjust: adjust}}
	case tvLibraries:
		toggle := func(enabled bool) {
			_ = storeSettingValue(a.storage, mergeTitlesKey, a.mergeTitles, enabled, strconv.FormatBool(enabled))
		}
		rows := []tvOption{{id: "tv-settings-merge-titles", label: locales.MergeIdenticalTitles(cx), checked: a.mergeTitles.Get,
			action: func() { toggle(!a.mergeTitles.GetUntracked()) }, adjust: func(direction int) { toggle(direction > 0) }}}
		for _, entry := range sources {
			row := link("tv-manage-libraries-"+entry.ID, entry.Name, route{screen: screenManageLibraries, source: entry})
			row.value = func() string { return strconv.Itoa(len(entry.Libraries)) }
			rows = append(rows, row)
		}
		return rows
	case tvAccounts:
		return []tvOption{{id: "tv-settings-profile", label: locales.SettingsLocalProfile(cx),
			value: func() string { return locales.SettingsLocalOnly(cx) }}}
	case tvStorage:
		if !a.downloads.available() {
			return unavailable("tv-settings-storage-unavailable", locales.StorageDownloads(cx))
		}
		return []tvOption{link("settings-downloads", locales.StorageDownloads(cx), route{screen: screenDownloads})}
	case tvParental:
		return unavailable("tv-settings-parental-unavailable", locales.TvSettingsParental(cx))
	default:
		return []tvOption{{id: "tv-settings-version", label: locales.AppName(cx),
			value: func() string { return core.App().Version }}}
	}
}
