package main

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) phoneSyncAccounts(cx *core.Scope) core.Node {
	return phoneSettingsPage(cx, locales.SettingsSyncAccounts(cx), a.back, "phone-sync-accounts", a.syncAccounts(cx))
}

func (a *application) syncAccounts(cx *core.Scope) core.Node {
	failure := core.NewSignal[error](cx, nil)
	dandanOpen := core.NewSignal(cx, false)
	configured := core.NewResource(cx, nil, func(context.Context, struct{}, core.ResourceInfo) (bool, error) {
		credentials, err := a.dandanCredentials()
		return credentials.AppID != "" && credentials.Secret != "", err
	})
	dandan := phoneSetting{locales.TvDanmakuDandan(cx), "", "bg-violet-500", lucide.Play}
	cloudSetting := func(title string, color string, icon func() *core.SVG) core.Node {
		return (phoneSetting{title, "", color, icon}).toggle(cx, "", func() bool { return false }, nil)
	}
	return core.View{Class: "gap-6", Children: []core.Node{
		a.syncProfileCard(cx),
		setupGroup(locales.SyncCloud(cx), locales.SyncCloudHint(cx),
			cloudSetting(locales.SyncICloud(cx), "bg-sky-500", lucide.Cloud),
			cloudSetting(locales.SyncSettings(cx), "bg-zinc-500", lucide.Settings),
			cloudSetting(locales.SyncSources(cx), "bg-emerald-500", lucide.Server),
			cloudSetting(locales.SyncLocalHistory(cx), "bg-zinc-500", lucide.HardDrive)),
		core.Dynamic(func(cx *core.Scope) core.Node {
			var rows []core.Node
			paused := a.libraryPrefs.Get().DisableProgress
			for _, entry := range a.sources.Get() {
				icon, tint := syncSourceIcon(entry.Provider)
				var change func(bool)
				if !paused {
					change = func(enabled bool) { failure.Set(a.setSourceProgress(entry.ID, enabled)) }
				}
				rows = append(rows, (phoneSetting{entry.Name, "", tint, icon}).toggle(cx,
					"sync-progress-"+entry.ID, func() bool { return !paused && !entry.ProgressDisabled }, change))
			}
			if len(rows) == 0 {
				rows = append(rows, (phoneSetting{label: locales.NoSources(cx)}).link(cx, "sync-add-source", "",
					func() { a.push(route{screen: screenSources}) }))
			}
			note := locales.SyncProgressHint(cx)
			if paused {
				note = locales.SyncProgressPaused(cx)
			}
			return setupGroup(locales.SyncProgress(cx), note, rows...)
		}),
		setupGroup(locales.SyncAccountsSection(cx), "",
			(phoneSetting{"Trakt", "", "bg-rose-600", lucide.CheckCheck}).unavailable(cx),
			dandan.linkValue(cx, "sync-dandan", func() string {
				if configured, _ := configured.Get(); configured {
					return locales.DesktopDanmakuConfigured(cx)
				}
				return locales.PhoneDanmakuUnconfigured(cx)
			}, func() { dandanOpen.Set(true) }),
			(phoneSetting{locales.SyncBangumi(cx), "", "bg-pink-500", lucide.Sparkles}).unavailable(cx),
			(phoneSetting{locales.SyncDouban(cx), "", "bg-green-600", lucide.Bookmark}).unavailable(cx)),
		a.syncDevices(cx),
		settingsFailure(cx, failure),
		core.Show{When: dandanOpen.Get, Then: func(cx *core.Scope) core.Node {
			close := func() { dandanOpen.Set(false); configured.Refetch() }
			return ui.Dialog{Open: dandanOpen.Get, Title: dandan.label, TestID: "sync-dandan-dialog",
				OnOpenChange: func(open bool, _ base.Reason) {
					if !open {
						close()
					}
				}, Trailing: []core.Node{ui.IconButton{Label: locales.TracksDone(cx), Icon: lucide.X, Size: 36,
					TestID: "close-sync-dandan", OnPress: close}}, Children: []core.Node{a.danmakuSettings(cx)}}
		}},
	}}
}

func (a *application) setSourceProgress(id string, enabled bool) error {
	sources := a.sources.GetUntracked()
	disabled := !enabled
	for i, entry := range sources {
		if entry.ID == id && entry.ProgressDisabled != disabled {
			next := slices.Clone(sources)
			next[i].ProgressDisabled = disabled
			return a.saveSources(next)
		}
	}
	return nil
}

func syncSourceIcon(provider media.Provider) (func() *core.SVG, string) {
	switch provider {
	case media.Jellyfin:
		return lucide.Database, "bg-sky-500"
	case media.Flymoo:
		return lucide.HardDrive, "bg-blue-600"
	case media.Plex:
		return lucide.Server, "bg-amber-500"
	default:
		return lucide.Server, "bg-emerald-500"
	}
}

func (a *application) syncProfileCard(cx *core.Scope) core.Node {
	return core.View{Class: "mx-4 gap-4 rounded-2xl border border-violet-500/20 bg-linear-to-br from-violet-500/15 via-white to-fuchsia-500/10 p-4 shadow-lg shadow-violet-500/10 dark:via-[#1a1b25]",
		TestID: "sync-local-profile", Children: []core.Node{
			core.View{Class: "flex-row items-center gap-3.5", Children: []core.Node{
				core.View{Class: "size-14 shrink-0 items-center justify-center rounded-full bg-linear-to-br from-violet-500 to-fuchsia-500 shadow-md shadow-violet-500/30",
					Children: []core.Node{core.Icon{SVG: lucide.User(), Class: "size-6 text-white"}}},
				core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
					core.Text{Content: locales.SettingsLocalProfile(cx), Class: "text-lg font-semibold " + setupTitle},
					core.Text{Content: locales.SettingsLocalProfileDetail(cx), Class: "text-xs " + setupMuted},
				}},
			}},
			core.View{Class: "flex-row items-center justify-between border-t border-zinc-900/8 pt-3 dark:border-white/8", Children: []core.Node{
				core.Text{Content: locales.SettingsLocalOnly(cx), Class: "text-sm " + setupTitle},
				base.Button{Label: locales.SyncRestorePurchases(cx), Hint: locales.SettingsUnavailable(cx), TestID: "sync-restore-purchases",
					Disabled: func() bool { return true }, Children: []core.Node{
						core.Text{Content: locales.SyncRestorePurchases(cx), Class: "text-sm " + setupAccent},
					}},
			}},
		}}
}

func (a *application) syncDevices(cx *core.Scope) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		local := a.pairingDevice(cx)
		name := local.Name
		if name == "" {
			name = locales.SyncCurrentDevice(cx)
		}
		var pair core.Node
		if appLayoutIdiom(cx) != core.IdiomTV {
			pair = ui.IconButton{Label: locales.TvPairPhoneTitle(cx), Icon: lucide.QrCode, Size: 36,
				TestID: "open-phone-tv-pair", OnPress: func() { a.push(route{screen: screenTVPairPhone}) }}
		}
		rows := []core.Node{syncDeviceRow(cx, "sync-device-local", name, locales.SyncThisDevice(cx), local.Kind, true, pair)}
		for _, paired := range a.pairedDevices.Get() {
			name := strings.TrimSpace(paired.Device.Name)
			if name == "" {
				name = locales.SyncPairedDevice(cx)
			}
			rows = append(rows, syncDeviceRow(cx, "sync-device-"+paired.Device.ID, name,
				locales.SyncLastPaired(cx, paired.SeenAt.In(time.Local).Format(time.DateTime)), paired.Device.Kind, false, nil))
		}
		return setupGroup(locales.SyncDevices(cx), "", rows...)
	})
}

func syncDeviceRow(cx *core.Scope, id, name, state, kind string, local bool, action core.Node) core.Node {
	icon := lucide.Smartphone
	switch kind {
	case "tablet":
		icon = lucide.Tablet
	case "desktop":
		icon = lucide.Monitor
	case "tv":
		icon = lucide.Tv
	}
	color := setupMuted
	if local {
		color = "text-emerald-500"
	}
	return core.View{Class: "flex-row items-center gap-3 bg-white px-4 py-3 dark:bg-[#1a1b25]", TestID: id, Children: []core.Node{
		core.View{Class: "size-9 shrink-0 items-center justify-center rounded-xl bg-zinc-900/5 dark:bg-zinc-50/5",
			Children: []core.Node{core.Icon{SVG: icon(), Class: "size-[18px] text-zinc-900/80 dark:text-zinc-50/80"}}},
		core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
			core.Text{Content: name, Class: "text-[15px] " + setupTitle, MaxLines: 1},
			core.Text{Content: state, Class: "text-xs " + color, MaxLines: 1},
		}}, action,
	}}
}
