package main

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func desktopSettingsAction(cx *core.Scope, id, label, class string, press func()) core.Node {
	return base.Button{Label: label, TestID: id, Class: "shrink-0 rounded " + class,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { press() },
		Children: []core.Node{core.Text{Content: label, Class: "text-xs"}}}
}

func (a *application) desktopSettingsSources(cx *core.Scope) core.Node {
	a.watchSourceConnections(cx)
	selected := core.NewSignal(cx, "")
	confirmed := core.NewSignal(cx, "")
	failure := core.NewSignal[error](cx, nil)
	active := func() string {
		entries, id := a.sources.Get(), selected.Get()
		if slices.ContainsFunc(entries, func(entry source) bool { return entry.ID == id }) {
			return id
		}
		if len(entries) > 0 {
			return entries[0].ID
		}
		return ""
	}
	headerCell := func(label, width string) core.Node {
		return core.Text{Content: label, Class: width + " text-xs " + setupMuted, MaxLines: 1}
	}
	header := core.View{Class: "h-9 flex-row items-center gap-3 border-b border-zinc-900/8 bg-[#e9e9f0]/40 px-4 dark:border-white/8 dark:bg-white/5", Children: []core.Node{
		headerCell(locales.SourceNameShort(cx), "w-40 shrink-0"), headerCell(locales.DesktopSourceType(cx), "w-20 shrink-0"),
		headerCell(locales.ServerAddressShort(cx), "min-w-0 flex-1"), headerCell(locales.DesktopSettingsLibrary(cx), "w-14 shrink-0"),
		headerCell(locales.DesktopSourceStatus(cx), "w-16 shrink-0"), core.View{Class: "w-14 shrink-0"},
	}}
	rows := core.Each(a.sources.Get, func(entry source) string { return entry.ID }, func(cx *core.Scope, entry func() source, _ func() int) core.Node {
		initial := core.Untracked(cx, entry)
		id := initial.ID
		variant := sourceCardStyle(initial.Provider)
		edit := func() {
			item := entry()
			a.openDesktopSettingsFlow(route{screen: screenConnect, provider: item.Provider, source: item})
		}
		statusColor := func() (string, string, string) {
			item := entry()
			check := a.sourceChecks.Get()[id]
			switch {
			case check.connection != item.connection() || check.checking || check.at.IsZero():
				return locales.SettingsCheckingSource(cx), "bg-zinc-400", setupMuted
			case errors.Is(check.err, media.ErrAuthentication):
				return locales.SettingsSourceNeedsLogin(cx), "bg-amber-500", "text-amber-600 dark:text-amber-400"
			case check.err != nil:
				return locales.SettingsSourceOffline(cx), "bg-zinc-400", setupMuted
			case check.endpoint.remote:
				return locales.SourceRemoteRoute(cx), "bg-amber-500", "text-amber-600 dark:text-amber-400"
			default:
				return locales.SettingsSourceOnline(cx), "bg-emerald-500", "text-emerald-600 dark:text-emerald-400"
			}
		}
		return core.View{Class: "h-10 flex-row items-center gap-1 border-b border-transparent pl-0.5 pr-3 last:border-b-0",
			ClassFunc: func() string {
				if active() == id {
					return "border-l-2 border-l-violet-500 bg-violet-500/10 pl-0"
				}
				return "border-b-zinc-900/8 dark:border-b-white/8"
			}, Children: []core.Node{
				base.Button{LabelFunc: func() string { return entry().Name }, TestID: "desktop-source-row-" + id,
					Class: "h-full min-w-0 flex-1 flex-row items-center gap-3 px-3.5",
					Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { selected.Set(id) },
					Children: []core.Node{
						core.Text{ContentFunc: func() string { return entry().Name }, Class: "w-40 shrink-0 text-sm font-medium " + setupTitle, MaxLines: 1},
						core.View{Class: "w-20 shrink-0 flex-row", Children: []core.Node{
							core.Text{Content: variant.title(cx), Class: "rounded px-1.5 py-0.5 text-[11px] " + variant.tag, MaxLines: 1},
						}},
						core.Text{ContentFunc: func() string { return strings.TrimSuffix(entry().Address, "/") }, Class: "min-w-0 flex-1 text-sm " + setupTitle, MaxLines: 1},
						core.Text{ContentFunc: func() string { return strconv.Itoa(len(entry().Libraries)) }, Class: "w-14 shrink-0 text-sm " + setupTitle},
						core.View{Class: "w-16 shrink-0 flex-row items-center gap-1.5", Children: []core.Node{
							core.View{Class: "size-1.5 shrink-0 rounded-full", ClassFunc: func() string { _, dot, _ := statusColor(); return dot }},
							core.Text{ContentFunc: func() string { label, _, _ := statusColor(); return label }, TestID: "source-settings-status-" + id,
								Class: "min-w-0 text-xs", ClassFunc: func() string { _, _, tint := statusColor(); return tint }, MaxLines: 1},
						}},
					}},
				core.View{Class: "w-14 shrink-0 flex-row items-center justify-end gap-1", Children: []core.Node{
					ui.IconButton{Label: locales.SettingsEdit(cx), Icon: lucide.Pencil, Size: 24, TestID: "source-edit-" + id, OnPress: edit},
					ui.IconButton{Label: locales.RemoveSource(cx), Icon: lucide.Trash, Size: 24, TestID: "source-remove-" + id, OnPress: func() { confirmed.Set(id) }},
				}},
			}}
	})
	details := core.Dynamic(func(cx *core.Scope) core.Node {
		id := active()
		for _, entry := range a.sources.Get() {
			if entry.ID == id {
				return a.desktopSourceDetails(cx, entry, failure)
			}
		}
		return nil
	})
	remove := core.Dynamic(func(cx *core.Scope) core.Node {
		id := confirmed.Get()
		for _, entry := range a.sources.Get() {
			if entry.ID != id {
				continue
			}
			return ui.AlertDialog{Open: func() bool { return confirmed.Get() == id }, Title: locales.RemoveSource(cx), Description: locales.DesktopSourceRemoveHint(cx, entry.Name),
				OnOpenChange: func(open bool, _ base.Reason) {
					if !open {
						confirmed.Set("")
					}
				},
				Actions: []ui.AlertAction{
					{Text: locales.DesktopSettingsCancel(cx), Role: core.AlertCancel},
					{Text: locales.RemoveSource(cx), Role: core.AlertDestructive, Variant: ui.ButtonDanger, TestID: "desktop-confirm-source-remove", OnPress: func() { failure.Set(a.removeSource(entry)) }},
				}}
		}
		return nil
	})
	add := locales.AddSource(cx)
	return core.View{Class: "gap-5", Children: []core.Node{
		core.View{Class: "flex-row items-center gap-4", Children: []core.Node{
			desktopSettingsHeading(locales.MediaSources(cx), locales.DesktopSettingsSourcesHint(cx)),
			base.Button{Label: add, TestID: "source-settings-add", Class: "h-9 shrink-0 flex-row items-center gap-1.5 rounded-lg bg-violet-500 px-3.5 shadow-lg shadow-violet-500/30",
				Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { a.openDesktopSettingsFlow(route{screen: screenSources}) },
				Children: []core.Node{core.Icon{SVG: lucide.Plus(), Class: "size-4 text-white"}, core.Text{Content: add, Class: "text-sm font-medium text-white"}}},
		}},
		core.View{Class: "overflow-hidden rounded-xl border border-zinc-900/8 shadow-sm shadow-black/5 dark:border-white/8 " + desktopLibrarySurface,
			Children: []core.Node{header, rows}},
		details, settingsFailure(cx, failure), remove,
	}}
}

func (a *application) desktopSourceDetails(cx *core.Scope, entry source, failure *core.Signal[error]) core.Node {
	refresh := func() { a.checkSource(entry, true) }
	changeUser := func() {
		a.openDesktopSettingsFlow(route{screen: screenConnect, provider: entry.Provider, source: entry})
	}
	var libraries []core.Node
	for _, library := range entry.Libraries {
		libraries = append(libraries, base.Checkbox{Label: library.Name, TestID: "desktop-source-library-" + library.ID,
			Checked:  func() bool { return !library.Hidden },
			OnChange: func(value bool) { failure.Set(a.setDesktopLibraryVisible(entry, library.ID, value)) },
			Class:    "group h-8 flex-row items-center gap-2.5 rounded",
			Style:    core.Style{FocusVisible: ui.FocusRing(cx, 2)}, Children: []core.Node{
				core.View{Class: "size-4 items-center justify-center rounded border border-zinc-900/25 group-checked:border-transparent group-checked:bg-violet-500 dark:border-white/25",
					Children: []core.Node{core.Icon{SVG: lucide.Check(), Class: "size-3 text-transparent group-checked:text-white"}}},
				core.Text{Content: library.Name, MaxLines: 1, Class: "min-w-0 flex-1 text-sm " + setupTitle},
			}})
	}
	libraries = append(libraries, desktopSettingsAction(cx, "desktop-source-sync", locales.SettingsSourceSync(cx), "mt-1 self-start "+setupAccent, refresh))
	inline := func(label, value string, action core.Node) core.Node {
		return core.View{Class: "h-10 min-w-0 flex-row items-center gap-3 bg-white px-4 dark:bg-[#1a1b25]", Children: []core.Node{
			core.Text{Content: label, Class: "w-20 shrink-0 text-sm " + setupMuted, MaxLines: 1},
			core.Text{Content: value, Class: "min-w-0 flex-1 text-sm " + setupTitle, MaxLines: 1}, action,
		}}
	}
	automatic, progress := locales.DesktopSourceAutoRoute(cx), locales.DesktopSourceSyncProgress(cx)
	var setAutomatic func(bool)
	if entry.ServerID != "" && (entry.RemoteAddress != "" || entry.Provider == media.Flymoo && entry.FNID != "") {
		setAutomatic = func(value bool) {
			failure.Set(a.updateSourceRouting(entry, func(saved *source) { saved.RemoteDisabled = !value }))
		}
	}
	connections := core.View{Class: "min-w-0 flex-1 gap-px overflow-hidden rounded-xl border border-zinc-900/8 bg-zinc-200 shadow-sm shadow-black/5 dark:border-white/8 dark:bg-white/7", Children: []core.Node{
		inline(locales.DesktopSourceLocal(cx), strings.TrimSuffix(entry.Address, "/"), nil),
		inline(locales.DesktopSourceRemote(cx), cmp.Or(entry.RemoteAddress, locales.MetadataUnavailable(cx)), nil),
		desktopFormRowHeight(automatic, "h-10", desktopFormToggle(cx, "desktop-source-auto-route", automatic, entry.automaticRoute, setAutomatic)),
		inline(locales.DesktopSourceUser(cx), cmp.Or(entry.Username, locales.MetadataUnavailable(cx)),
			desktopSettingsAction(cx, "desktop-source-user", locales.DesktopSourceChangeUser(cx), setupAccent, changeUser)),
		desktopFormRowHeight(progress, "h-10", desktopFormToggle(cx, "", progress, func() bool { return true }, nil)),
		desktopFormRow(locales.DesktopSourceRemoteBitrate(cx), desktopFormSelect(cx, "desktop-source-remote-bitrate",
			locales.DesktopSourceRemoteBitrate(cx), desktopBitrates(cx), func() int { return entry.RemoteBitrate }, func(value int) {
				failure.Set(a.updateSourceRouting(entry, func(saved *source) { saved.RemoteBitrate = value }))
			})),
		core.Show{When: func() bool { return a.sourceChecks.Get()[entry.ID].saveError != nil }, Then: func(cx *core.Scope) core.Node {
			return core.Text{Content: locales.LibrarySaveError(cx), Class: "px-4 py-2 text-xs text-rose-500"}
		}},
	}}
	return core.View{Class: "gap-2", Children: []core.Node{
		core.Text{Content: locales.DesktopSourceConnection(cx, entry.Name), Class: "px-1 text-xs font-medium " + setupMuted},
		core.View{Class: "flex-row gap-4", Children: []core.Node{connections,
			core.View{Class: "w-60 shrink-0 gap-1 rounded-xl border border-zinc-900/8 p-4 shadow-sm shadow-black/5 dark:border-white/8 " + desktopLibrarySurface,
				Children: append([]core.Node{core.Text{Content: locales.DesktopSourceVisibleLibraries(cx), Class: "pb-2 text-sm font-medium " + setupTitle}}, libraries...)},
		}},
	}}
}

func (a *application) setDesktopLibraryVisible(entry source, id string, visible bool) error {
	sources := slices.Clone(a.sources.GetUntracked())
	for i := range sources {
		current := &sources[i]
		if current.ID != entry.ID || current.connection() != entry.connection() {
			continue
		}
		for j, library := range current.Libraries {
			if library.ID == id && library.Hidden == visible {
				current.Libraries = slices.Clone(current.Libraries)
				current.Libraries[j].Hidden = !visible
				if err := a.saveSources(sources); err != nil {
					return err
				}
				a.libraryChanges.Update(func(revision uint64) uint64 { return revision + 1 })
				return nil
			}
		}
	}
	return nil
}
