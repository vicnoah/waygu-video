package main

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"waygu-video/locales"
)

func (a *application) phoneSettingsHome(cx *core.Scope) core.Node {
	a.watchSourceConnections(cx)
	routeTo := func(screen screen) func() { return func() { a.push(route{screen: screen}) } }
	card := core.View{Class: "mx-4 flex-row items-center gap-3.5 rounded-2xl border border-violet-500/20 bg-linear-to-br from-violet-500/15 via-white to-fuchsia-500/10 p-4 shadow-lg shadow-violet-500/10 dark:via-[#1a1b25]", Children: []core.Node{
		core.View{Class: "size-14 items-center justify-center rounded-full bg-linear-to-br from-violet-500 to-fuchsia-500 shadow-md shadow-violet-500/30",
			Children: []core.Node{core.Icon{SVG: lucide.User(), Class: "size-6 text-white"}}},
		core.View{Class: "min-w-0 flex-1 gap-1", Children: []core.Node{
			core.Text{Content: locales.SettingsLocalProfile(cx), Class: "text-lg font-semibold " + setupTitle},
			core.Text{Content: locales.SettingsLocalProfileDetail(cx), Class: "text-xs " + setupMuted},
		}},
		core.Icon{SVG: lucide.ChevronRight(), Class: "size-5 " + setupMuted},
	}}
	manage := phoneSetting{locales.SettingsSourceManager(cx), "", "bg-emerald-500", lucide.Server}
	play := phoneSetting{locales.PlaybackSettings(cx), "", "bg-violet-500", lucide.Play}
	subtitle := phoneSetting{locales.Subtitles(cx), "", "bg-sky-500", lucide.Captions}
	danmaku := phoneSetting{locales.Danmaku(cx), "", "bg-pink-500", lucide.MessageSquareText}
	downloads := phoneSetting{locales.StorageDownloads(cx), "", "bg-zinc-500", lucide.HardDrive}
	version := core.App().Version
	footer := locales.SettingsFooter(cx, version)
	body := core.View{Class: "gap-6 pt-2", StyleFunc: func() core.Style {
		return core.Style{PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea},
			Padding: core.Edges{Bottom: tabBarInset(cx) + 12}}
	}, Children: []core.Node{
		core.Text{Content: locales.Settings(cx), Class: "px-5 text-3xl font-bold tracking-tight " + setupTitle},
		card,
		setupGroup(locales.MediaSources(cx), "",
			manage.linkValue(cx, "open-phone-source-settings", func() string {
				if len(a.sources.Get()) == 0 {
					return locales.SettingsNoSource(cx)
				}
				var online int
				checked, catalog := a.sourceChecks.Get(), a.sourceStatus.Get()
				for _, entry := range a.sources.Get() {
					if status, ok := checked[entry.ID]; ok && status.connection == entry.connection() && !status.checking {
						if status.err == nil {
							online++
						}
					} else if catalog[entry.ID].connected {
						online++
					}
				}
				return locales.SourcesOnline(cx, strconv.Itoa(online))
			}, routeTo(screenSourceSettings)),
			(phoneSetting{locales.AddSource(cx), "", "bg-teal-500", lucide.CirclePlus}).
				link(cx, "settings-add-source", locales.SettingsProviders(cx), routeTo(screenSources)),
		),
		setupGroup(locales.SettingsPlaybackExperience(cx), "",
			play.linkValue(cx, "open-phone-playback-settings", func() string {
				if bitrate := a.bitrateLimit.Get(); bitrate > 0 {
					return locales.BitrateMegabits(cx, strconv.Itoa(bitrate/1_000_000))
				}
				return locales.OriginalQuality(cx)
			}, routeTo(screenPlaybackSettings)),
			subtitle.linkValue(cx, "open-phone-subtitle-settings", func() string {
				languages := a.subtitlePrefs.Get().Languages
				return settingsLanguageName(cx, languages[0])
			}, routeTo(screenSubtitleSettings)),
			danmaku.linkValue(cx, "open-phone-danmaku-settings", func() string {
				if a.danmakuPrefs.Get().Enabled {
					return locales.SettingsEnabled(cx)
				}
				return locales.SettingsDisabled(cx)
			}, routeTo(screenDanmakuSettings)),
		),
		setupGroup(locales.SettingsInterfaceLibrary(cx), "",
			(phoneSetting{locales.SettingsAppearance(cx), "", "bg-orange-500", lucide.Palette}).linkValue(cx, "open-appearance-settings",
				func() string { return appearanceModeLabel(cx, a.appearance.Get()) }, routeTo(screenAppearanceSettings)),
			(phoneSetting{locales.SettingsLibraryMetadata(cx), "", "bg-indigo-500", lucide.Database}).link(cx, "open-library-settings", "", routeTo(screenLibrarySettings)),
		),
		setupGroup(locales.SettingsAccountsData(cx), "",
			(phoneSetting{locales.SettingsSyncAccounts(cx), "", "bg-blue-500", lucide.RefreshCw}).linkValue(cx, "open-sync-accounts",
				func() string { return locales.SettingsLocalOnly(cx) }, routeTo(screenSyncAccounts)),
			core.Dynamic(func(cx *core.Scope) core.Node {
				if !a.downloads.available() {
					return downloads.unavailable(cx)
				}
				return downloads.linkValue(cx, "settings-downloads", func() string {
					var bytes int64
					for _, entry := range a.downloads.entries.Get() {
						bytes += entry.Transferred
					}
					return mediaBytes(bytes)
				}, routeTo(screenDownloads))
			}),
			(phoneSetting{locales.SettingsParental(cx), "", "bg-amber-500", lucide.Shield}).linkValue(cx, "open-parental-settings", func() string {
				if a.parentalRules().Enabled {
					return locales.SettingsEnabled(cx)
				}
				return locales.SettingsDisabled(cx)
			}, routeTo(screenParentalSettings)),
		),
		setupGroup(locales.SettingsOther(cx), footer,
			(phoneSetting{locales.SettingsAbout(cx), "", "bg-slate-500", lucide.Info}).link(cx, "settings-about", version, routeTo(screenAboutSettings))),
	}}
	return core.View{Class: "relative flex-1 bg-waygu-page", Children: []core.Node{
		homeAmbient(false, false), homeAmbient(true, false),
		core.Scroll{Class: "flex-1", TestID: "phone-settings-home", Children: []core.Node{body}},
	}}
}
