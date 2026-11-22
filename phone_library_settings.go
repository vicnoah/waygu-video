package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func (a *application) phoneLibrarySettings(cx *core.Scope) core.Node {
	failure := core.NewSignal[error](cx, nil)
	panel := core.NewSignal(cx, "")
	confirmScan := core.NewSignal(cx, false)
	confirmClear := core.NewSignal(cx, false)
	scanStarted := core.NewSignal(cx, false)
	cacheCleared := core.NewSignal(cx, false)
	prefs := a.libraryPrefs.Get
	save := func(edit func(*libraryPreferences)) { failure.Set(a.saveLibraryPreferences(edit)) }
	languages := []ui.Option[string]{
		{Label: locales.SettingsFollowSystem(cx)}, {Value: "zh-CN", Label: "简体中文"},
		{Value: "en-US", Label: "English"}, {Value: "ja-JP", Label: "日本語"}, {Value: "ko-KR", Label: "한국어"},
	}
	intervals := []ui.Option[int]{{Label: locales.SettingsDisabled(cx)}}
	for _, minutes := range []int{15, 30, 60} {
		intervals = append(intervals, ui.Option[int]{Value: minutes, Label: locales.MetadataRefreshEvery(cx, strconv.Itoa(minutes))})
	}
	setting := func(label, detail, color string, icon func() *core.SVG) phoneSetting {
		return phoneSetting{label, detail, color, icon}
	}
	primary := setting(locales.MetadataServerFirst(cx), locales.MetadataServerHint(cx), "bg-emerald-500", lucide.Server)
	tmdb := setting(locales.MetadataMoviesSeries(cx), "", "bg-sky-500", lucide.Clapperboard)
	hidden := setting(locales.MetadataHiddenLibraries(cx), "", "bg-zinc-500", lucide.EyeOff)
	progress := setting(locales.MetadataProgressSync(cx), "", "bg-emerald-500", lucide.RefreshCw)
	rescan, clear := locales.MetadataRescan(cx), locales.MetadataClear(cx)
	syncing := func() bool {
		checks := a.sourceChecks.Get()
		for _, entry := range a.sources.Get() {
			if check := checks[entry.ID]; check.syncing && check.connection == entry.connection() {
				return true
			}
		}
		return false
	}
	return phoneSettingsPage(cx, locales.SettingsLibraryMetadata(cx), a.back, "phone-library-settings",
		setupGroup(locales.MetadataSection(cx), locales.MetadataSectionHint(cx),
			primary.toggle(cx, "metadata-server-first", func() bool { return !prefs().PreferExternal },
				func(value bool) { save(func(p *libraryPreferences) { p.PreferExternal = !value }) }),
			tmdb.linkValue(cx, "metadata-tmdb", func() string {
				if prefs().TMDB {
					return "TMDB"
				}
				return locales.MetadataServer(cx)
			}, func() { panel.Set("tmdb") }),
			setting(locales.MetadataRatings(cx), "", "bg-green-600", lucide.Star).unavailable(cx),
			setting(locales.MetadataSeriesExtras(cx), "", "bg-indigo-500", lucide.Tv).unavailable(cx),
			setting(locales.MetadataAnime(cx), "", "bg-pink-500", lucide.Sparkles).unavailable(cx),
			phoneSettingChoice(cx, setting(locales.MetadataLanguage(cx), "", "bg-blue-500", lucide.Languages), "metadata-language",
				languages, func() string { return prefs().Language }, func(v string) { save(func(p *libraryPreferences) { p.Language = v }) })),
		setupGroup(locales.MetadataIdentification(cx), "",
			setting(locales.MetadataFileScrape(cx), "", "bg-violet-500", lucide.ScanSearch).toggle(cx, "",
				func() bool { return false }, nil),
			setting(locales.MetadataFilenameRules(cx), "", "bg-violet-500", lucide.Regex).unavailable(cx),
			setting(locales.MetadataCollections(cx), "", "bg-orange-500", lucide.Library).toggle(cx, "",
				func() bool { return false }, nil),
			setting(locales.MetadataBangumiGrouping(cx), locales.MetadataBangumiHint(cx), "bg-pink-500", lucide.Sparkles).toggle(cx, "",
				func() bool { return false }, nil)),
		setupGroup(locales.Library(cx), "",
			progress.toggle(cx, "metadata-progress-sync", func() bool { return !prefs().DisableProgress },
				func(value bool) { save(func(p *libraryPreferences) { p.DisableProgress = !value }) }),
			hidden.linkValue(cx, "metadata-hidden-libraries", func() string {
				total := 0
				for _, entry := range a.sources.Get() {
					for _, library := range entry.Libraries {
						if library.Hidden {
							total++
						}
					}
				}
				return locales.MetadataLibraryCount(cx, strconv.Itoa(total))
			}, func() { panel.Set("hidden") }),
			phoneSettingChoice(cx, setting(locales.MetadataAutoRefresh(cx), "", "bg-zinc-500", lucide.Clock), "metadata-auto-refresh",
				intervals, func() int { return prefs().RefreshMinutes }, func(v int) { save(func(p *libraryPreferences) { p.RefreshMinutes = v }) })),
		setupGroup("", "",
			metadataAction(cx, "metadata-rescan", rescan, setupAccent, lucide.RefreshCw, func() bool {
				return len(a.sources.Get()) == 0 || syncing()
			}, func() { confirmScan.Set(true) }),
			metadataAction(cx, "metadata-cache-clear", clear, "text-rose-500", nil, nil, func() { confirmClear.Set(true) })),
		core.Show{When: scanStarted.Get, Then: func(cx *core.Scope) core.Node { return a.libraryScanStatus(cx, syncing) }},
		core.Show{When: cacheCleared.Get, Then: func(cx *core.Scope) core.Node {
			return core.Text{Class: "px-5 text-sm " + setupMuted, TestID: "metadata-cache-cleared", Content: locales.MetadataCleared(cx)}
		}},
		settingsFailure(cx, failure),
		ui.AlertDialog{Title: rescan, Description: locales.MetadataRescanConfirm(cx), Open: confirmScan.Get,
			OnOpenChange: func(open bool, _ base.Reason) { confirmScan.Set(open) },
			Actions: []ui.AlertAction{
				{Text: locales.DesktopSettingsCancel(cx), Role: core.AlertCancel},
				{Text: rescan, TestID: "metadata-confirm-scan", OnPress: func() {
					scanStarted.Set(true)
					cx.Batch(func() {
						for _, entry := range a.sources.GetUntracked() {
							a.checkSourceWithScan(entry, true, true)
						}
					})
				}},
			}},
		ui.AlertDialog{Title: clear, Description: locales.MetadataClearConfirm(cx), Open: confirmClear.Get,
			OnOpenChange: func(open bool, _ base.Reason) { confirmClear.Set(open) },
			Actions: []ui.AlertAction{
				{Text: locales.DesktopSettingsCancel(cx), Role: core.AlertCancel},
				{Text: clear, TestID: "metadata-confirm-clear", Role: core.AlertDestructive, Variant: ui.ButtonDanger, OnPress: func() {
					a.metadata.Clear()
					cacheCleared.Set(true)
				}},
			}},
		core.Dynamic(func(cx *core.Scope) core.Node {
			var title string
			var content core.Node
			switch panel.Get() {
			case "hidden":
				title, content = hidden.label, a.hiddenLibraries(cx, failure.Set)
			case "tmdb":
				title, content = "TMDB", a.metadataCredentials(cx)
			default:
				return nil
			}
			close := func() { panel.Set("") }
			return ui.Dialog{Title: title, Open: func() bool { return true }, TestID: "metadata-dialog",
				OnOpenChange: func(open bool, _ base.Reason) {
					if !open {
						close()
					}
				},
				Trailing: []core.Node{ui.IconButton{Label: locales.TracksDone(cx), Icon: lucide.X, Size: 36, TestID: "metadata-close-dialog", OnPress: close}},
				Children: []core.Node{content},
			}
		}),
	)
}

func metadataAction(cx *core.Scope, id, label, color string, icon func() *core.SVG, disabled func() bool, run func()) core.Node {
	var children []core.Node
	if icon != nil {
		children = append(children, core.Icon{SVG: icon(), Class: "size-4 " + color})
	}
	children = append(children, core.Text{Content: label, Class: "text-[15px] " + color})
	return base.Button{Label: label, TestID: id, Disabled: disabled,
		Class: "h-12 flex-row items-center justify-center gap-2 bg-white dark:bg-[#1a1b25]",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { run() }, Children: children}
}

func (a *application) libraryScanStatus(cx *core.Scope, syncing func() bool) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if syncing() {
			return core.Text{Class: "px-5 text-sm " + setupMuted, Content: locales.Loading(cx)}
		}
		checks := a.sourceChecks.Get()
		rows := []core.Node{core.Text{Content: locales.MetadataRescanFinished(cx), Class: "text-sm font-medium " + setupTitle, TestID: "metadata-scan-finished"}}
		for _, source := range a.sources.Get() {
			check := checks[source.ID]
			status := locales.MetadataListRefreshed(cx)
			switch {
			case check.err != nil:
				status = errorText(cx, check.err)
			case check.saveError != nil:
				status = errorText(cx, check.saveError)
			case check.scanError != nil:
				status = locales.MetadataScanRejected(cx)
			case check.scanSent:
				status = locales.MetadataScanRequested(cx)
			}
			rows = append(rows, core.Text{Content: source.Name + " · " + status, Class: "text-xs " + setupMuted})
		}
		return core.View{Class: "gap-1.5 px-5", Children: rows}
	})
}

func (a *application) hiddenLibraries(cx *core.Scope, report func(error)) core.Node {
	return core.Scroll{Style: core.Style{MaxHeight: core.Pt(360), FlexShrink: 1}, Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node {
			var groups []core.Node
			for _, entry := range a.sources.Get() {
				var rows []core.Node
				for _, lib := range entry.Libraries {
					if !lib.Hidden {
						continue
					}
					rows = append(rows, (phoneSetting{label: lib.Name}).link(cx, "show-library-"+entry.ID+"-"+lib.ID,
						locales.MetadataShowLibrary(cx), func() {
							report(a.showMetadataLibrary(entry.ID, lib.ID))
						}))
				}
				if len(rows) > 0 {
					groups = append(groups, setupGroup(entry.Name, "", rows...))
				}
			}
			if len(groups) == 0 {
				return hint(locales.MetadataNoHidden(cx))
			}
			return core.View{Class: "gap-4", Children: groups}
		}),
	}}
}

func (a *application) showMetadataLibrary(sourceID, libraryID string) error {
	sources := slices.Clone(a.sources.GetUntracked())
	for i, source := range sources {
		if source.ID != sourceID {
			continue
		}
		index := slices.IndexFunc(source.Libraries, func(lib sourceLibrary) bool { return lib.ID == libraryID })
		if index >= 0 && source.Libraries[index].Hidden {
			source.Libraries = slices.Clone(source.Libraries)
			source.Libraries[index].Hidden = false
			sources[i] = source
			return a.saveSources(sources)
		}
		break
	}
	return nil
}

func (a *application) metadataCredentials(cx *core.Scope) core.Node {
	token := core.NewSignal(cx, "")
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal[error](cx, nil)
	present := func() bool { return a.libraryPrefs.Get().TMDB }
	update := func(remove bool) {
		value := strings.TrimSpace(token.GetUntracked())
		if busy.GetUntracked() || (!remove && value == "") {
			return
		}
		busy.Set(true)
		view := cx.Context()
		go func() {
			var err error
			if remove {
				err = a.secrets.Delete(tmdbSecretKey)
			} else {
				err = a.secrets.Set(tmdbSecretKey, value)
			}
			a.scope.Post(func() {
				if err == nil {
					a.metadata.Clear()
					err = a.saveLibraryPreferences(func(p *libraryPreferences) { p.TMDB = !remove })
				}
				if view.Err() == nil {
					failure.Set(err)
					busy.Set(false)
					if err == nil {
						token.Set("")
					}
				}
			})
		}()
	}
	return core.View{Class: "gap-3", Children: []core.Node{
		hint(locales.MetadataTMDBHelp(cx)),
		ui.Input{Label: locales.MetadataTMDBToken(cx), Placeholder: locales.MetadataTMDBToken(cx), TestID: "metadata-tmdb-token",
			Masked: true, NoAutoCorrect: true, Value: token.Get, OnChange: token.Set, Disabled: busy.Get},
		ui.Button{Text: locales.MetadataTMDBSave(cx), TestID: "metadata-tmdb-save", Busy: busy.Get,
			Disabled: func() bool { return strings.TrimSpace(token.Get()) == "" }, OnPress: func() { update(false) }},
		core.Show{When: present, Then: func(cx *core.Scope) core.Node {
			return ui.Button{Text: locales.MetadataTMDBRemove(cx), TestID: "metadata-tmdb-remove", Variant: ui.ButtonOutline,
				Disabled: busy.Get, OnPress: func() { update(true) }}
		}},
		settingsFailure(cx, failure),
	}}
}
