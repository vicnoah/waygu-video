package main

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func downloadRoute(current route, item media.Item, version media.Version) route {
	current.item = item
	if version.ID != "" {
		current.versionID = version.ID
		current.item.FileSize = version.Size
	}
	return current
}

func (a *application) downloadAction(cx *core.Scope, current route, render detailActionRenderer, request func() route) core.Node {
	if !a.downloads.available() || !playable(current.item) {
		return nil
	}
	id := downloadID(mediaAccountKey(current.source), current.item.ID)
	open := core.NewSignal(cx, false)
	height := core.NewSignal(cx, 0)
	selected := core.NewSignalFunc(cx, current, nil)
	action := core.Dynamic(func(cx *core.Scope) core.Node {
		entry, exists := a.downloads.find(id, a.downloads.entries.Get())
		label, icon := locales.DownloadAction(cx), lucide.Download
		if exists {
			label = a.downloads.stateLabel(cx, entry)
			if entry.State == downloadComplete {
				icon = lucide.CircleCheck
			} else if entry.State == downloadRunning && entry.Total > 0 {
				label = locales.DownloadPercent(cx, strconv.Itoa(int(100*float64(entry.Transferred)/float64(entry.Total))))
			}
		}
		press := func() {
			if exists {
				if entry.State == downloadComplete {
					a.openDownload(entry)
				} else {
					a.push(route{screen: screenDownloads})
				}
			} else {
				item := request()
				value := 0
				if canDownloadTranscodes(item.source.Provider) {
					value = a.downloads.height.GetUntracked()
				}
				cx.Batch(func() {
					selected.Set(item)
					height.Set(value)
					open.Set(true)
				})
			}
		}
		if render != nil {
			return render(cx, "detail-download", label, icon(), exists && entry.State == downloadComplete, nil, press)
		}
		return ui.Button{Text: label, Icon: icon, Variant: ui.ButtonOutline, TestID: "detail-download", OnPress: press}
	})
	confirm := func(waitForWiFi bool) {
		open.Set(false)
		if !a.downloads.enqueue(selected.GetUntracked(), height.GetUntracked(), waitForWiFi) {
			a.push(route{screen: screenDownloads})
		}
	}
	dialog := a.downloads.selectionDialog(cx, downloadDialog{
		id: "detail-download", title: locales.DownloadQuality(cx), open: open, height: height,
		transcodes: func() bool { return canDownloadTranscodes(selected.Get().source.Provider) },
		selection:  func() []route { return []route{selected.Get()} }, confirm: confirm,
	})
	return core.Group([]core.Node{action, dialog})
}

func canDownloadTranscodes(provider media.Provider) bool {
	return provider == media.Emby || provider == media.Jellyfin || provider == media.Plex
}

func downloadQualityOptions(cx *core.Scope) []ui.Option[int] {
	options := []ui.Option[int]{{Label: locales.OriginalQuality(cx)}}
	for _, height := range []int{1080, 720} {
		options = append(options, ui.Option[int]{Value: height, Label: locales.DownloadQualityPreset(cx,
			strconv.Itoa(height), strconv.Itoa(downloadBitrate(height)/1_000_000))})
	}
	return options
}

func (q *downloadQueue) stateLabel(cx *core.Scope, entry downloadEntry) string {
	switch entry.State {
	case downloadQueued:
		if waiting := q.waitFor(entry); waiting != downloadNetworkReady {
			return waiting.label(cx)
		}
		return locales.DownloadQueued(cx)
	case downloadRunning:
		return locales.DownloadRunning(cx)
	case downloadPaused:
		return locales.DownloadPaused(cx)
	case downloadFailed:
		return locales.DownloadFailed(cx)
	case downloadComplete:
		return locales.Downloaded(cx)
	default:
		return locales.DownloadAction(cx)
	}
}

func downloadErrorText(cx *core.Scope, code string) string {
	switch code {
	case "file":
		return locales.DownloadFileUnavailable(cx)
	case "source":
		return locales.DownloadSourceUnavailable(cx)
	case "space":
		return locales.DownloadNoSpace(cx)
	case "busy":
		return locales.DownloadBusy(cx)
	case "save":
		return locales.DownloadSaveError(cx)
	case "remove":
		return locales.DownloadRemoveError(cx)
	default:
		return locales.DownloadTransferError(cx)
	}
}

func mediaBytes(bytes int64) string {
	switch {
	case bytes >= 1<<40:
		return fmt.Sprintf("%.1f TB", float64(bytes)/(1<<40))
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(bytes)/(1<<10))
	default:
		return strconv.FormatInt(bytes, 10) + " B"
	}
}

func (a *application) downloadStorage(cx *core.Scope) core.Node {
	queue := a.downloads
	cx.Untrack(queue.checkDownloadSources)
	confirm := core.NewSignal(cx, "")
	slots := []ui.Option[int]{{Value: 1, Label: "1"}, {Value: 2, Label: "2"}, {Value: 3, Label: "3"}}
	quotas := []ui.Option[int64]{{Label: locales.DownloadUnlimited(cx)}}
	for _, limit := range downloadQuotaOptions[1:] {
		quotas = append(quotas, ui.Option[int64]{Value: limit, Label: mediaBytes(limit)})
	}
	body := core.View{Class: "gap-6 pt-2 pb-8", Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node { return downloadUsageCard(cx, queue) }),
		core.Dynamic(func(cx *core.Scope) core.Node {
			entries := slices.Clone(queue.entries.Get())
			slices.SortStableFunc(entries, func(a, b downloadEntry) int {
				switch {
				case a.State == downloadComplete && b.State != downloadComplete:
					return 1
				case a.State != downloadComplete && b.State == downloadComplete:
					return -1
				default:
					return 0
				}
			})
			rows := make([]core.Node, 0, len(entries))
			rules := a.parentalRules()
			sources := a.sources.Get()
			for _, entry := range entries {
				if rules.Enabled {
					linked, exists := queue.source(entry, sources)
					if !exists || !rules.visible(linked.ID, entry.Item) {
						continue
					}
				}
				rows = append(rows, a.downloadRow(cx, entry, confirm))
			}
			if len(rows) == 0 {
				rows = append(rows, core.Text{Content: locales.DownloadNone(cx), Class: "px-4 py-3 text-sm bg-white dark:bg-[#1a1b25] " + setupMuted})
			}
			return setupGroup(locales.OfflineDownloads(cx), "", rows...)
		}),
		setupGroup(locales.DownloadOptions(cx), "",
			settingPicker(cx, "download-quality", locales.DownloadQuality(cx), downloadQualityOptions(cx), queue.height.Get, queue.setHeight,
				[]core.Node{setupBadge(lucide.Film(), "bg-violet-500"),
					core.Text{Content: locales.DownloadQuality(cx), Class: "min-w-0 flex-1 text-[15px] " + setupTitle, MaxLines: 1}}),
			core.Dynamic(func(cx *core.Scope) core.Node {
				set := queue.setWiFiOnly
				if queue.network.Get() == downloadNetworkUnobserved {
					set = nil
				}
				return (phoneSetting{locales.DownloadWiFiOnly(cx), "", "bg-sky-500", lucide.Wifi}).
					toggle(cx, "download-wifi-only", queue.wifiOnly.Get, set)
			}),
			downloadPreference(cx, locales.DownloadConcurrency(cx), lucide.Layers(), "bg-indigo-500",
				downloadPicker(cx, "download-concurrency", locales.DownloadConcurrency(cx), slots, queue.slots.Get, queue.setSlots)),
			downloadPreference(cx, locales.DownloadQuota(cx), lucide.HardDrive(), "bg-sky-500",
				downloadPicker(cx, "download-quota", locales.DownloadQuota(cx), quotas, queue.quota.Get, queue.setQuota)),
			downloadPreference(cx, locales.DownloadLocation(cx), lucide.Folder(), "bg-zinc-500", hint(locales.DownloadThisDevice(cx))),
			(phoneSetting{locales.DownloadDeleteWatched(cx), locales.DownloadDeleteWatchedDetail(cx), "bg-rose-500", lucide.Trash}).
				toggle(cx, "download-delete-watched", func() bool { return queue.cleanup.Get() != "" }, queue.setDeleteWatched),
			(phoneSetting{locales.DownloadIncludeTracks(cx), "", "bg-pink-500", lucide.Captions}).
				toggle(cx, "download-include-tracks", queue.tracks.Get, queue.setIncludeTracks),
		),
		core.Dynamic(func(cx *core.Scope) core.Node {
			entries := queue.entries.Get()
			if len(entries) == 0 {
				return nil
			}
			if confirm.Get() == "all" {
				return setupGroup(locales.StorageCache(cx), locales.DownloadDeleteAllPrompt(cx),
					core.View{Class: "flex-row items-center justify-end gap-2 px-4 py-2 bg-white dark:bg-[#1a1b25]", Children: []core.Node{
						ui.Button{Text: locales.Back(cx), Variant: ui.ButtonGhost, OnPress: func() { confirm.Set("") }},
						ui.Button{Text: locales.DownloadDeleteAll(cx), TestID: "confirm-remove-all-downloads",
							Color: func() core.Color { return cx.Palette().Danger }, OnPress: func() {
								var ids []string
								for _, entry := range entries {
									ids = append(ids, entry.ID)
								}
								queue.remove(ids)
								confirm.Set("")
							}},
					}})
			}
			return setupGroup(locales.StorageCache(cx), "",
				base.Button{Label: locales.DownloadDeleteAll(cx), TestID: "remove-all-downloads",
					Disabled: func() bool {
						for _, entry := range queue.entries.Get() {
							if entry.State == downloadRunning || queue.running[entry.ID] != nil {
								return true
							}
						}
						return false
					},
					Class: "h-12 items-center justify-center bg-white disabled:opacity-50 dark:bg-[#1a1b25]",
					Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { confirm.Set("all") },
					Children: []core.Node{core.Text{Content: locales.DownloadDeleteAll(cx), Class: "text-[15px] text-rose-500"}}})
		}),
		core.Dynamic(func(cx *core.Scope) core.Node {
			if code := queue.failure.Get(); code != "" {
				return core.View{Class: "px-4", Children: []core.Node{notice(downloadErrorText(cx, code))}}
			}
			return nil
		}),
	}}
	return phoneSettingsFrame(cx, locales.StorageDownloads(cx), a.back, "storage-downloads-scroll", body)
}

func downloadPicker[T comparable](cx *core.Scope, id, label string, options []ui.Option[T], value func() T, change func(T)) core.Node {
	return settingPicker(cx, id, label, options, value, change, nil)
}

func settingPicker[T comparable](cx *core.Scope, id, label string, options []ui.Option[T], value func() T, change func(T), parts []core.Node) core.Node {
	items := make([]base.SelectItem, 0, len(options))
	for i, option := range options {
		items = append(items, base.SelectItem{Value: strconv.Itoa(i), Title: option.Label})
	}
	selected := func() *string {
		current := value()
		for i, option := range options {
			if option.Value == current {
				return &items[i].Value
			}
		}
		return nil
	}
	trigger := core.View{Class: "shrink-0 flex-row items-center gap-3", Children: []core.Node{
		core.Text{ContentFunc: func() string {
			current := value()
			for _, option := range options {
				if option.Value == current {
					return option.Label
				}
			}
			return ""
		}, Class: "shrink text-right text-[15px] " + setupMuted},
		core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 opacity-60 " + setupMuted},
	}}
	rootClass, class := "shrink-0", "min-h-9 justify-center rounded"
	if len(parts) > 0 {
		rootClass, class = "w-full", "w-full "+setupRow+" py-2.5"
		trigger = core.View{Class: "w-full flex-row items-center gap-3", Children: append(parts, trigger)}
	}
	return base.Select{Label: label, TestID: id, RootClass: rootClass, Class: class,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, PopupClass: tabletDetailPopup, ItemClass: tabletDetailOption,
		Items: func() []base.SelectItem { return items }, Value: selected, RenderItem: tabletDetailOptionText,
		OnChange: func(value *string) {
			if value != nil {
				if i, err := strconv.Atoi(*value); err == nil && i >= 0 && i < len(options) {
					change(options[i].Value)
				}
			}
		},
		Trigger: trigger,
	}
}

func downloadPreference(cx *core.Scope, label string, icon *core.SVG, tint string, value core.Node) core.Node {
	return core.View{Class: setupRow + " py-2", Children: []core.Node{
		setupBadge(icon, tint),
		core.Text{Content: label, Class: "min-w-0 flex-1 text-[15px] " + setupTitle, MaxLines: 1},
		value,
	}}
}

func downloadUsageCard(cx *core.Scope, queue *downloadQueue) core.Node {
	var used int64
	for _, entry := range queue.entries.Get() {
		used += entry.Transferred
	}
	label := []core.Node{core.Text{Content: locales.DownloadStorageUsed(cx, mediaBytes(used)),
		Class: "text-base font-semibold " + setupTitle}}
	var percent float32
	if free := queue.store.Available(); free != nil {
		label = append(label, core.Text{Content: locales.DownloadStorageAvailable(cx, mediaBytes(*free)), Class: "text-xs " + setupMuted})
		if used > 0 {
			percent = float32(100 * float64(used) / float64(used+max(0, *free)))
		}
	}
	return core.View{Class: "mx-4 gap-3 rounded-2xl bg-white p-4 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder,
		Children: []core.Node{
			core.View{Class: "flex-row flex-wrap items-end justify-between gap-2", Children: label},
			core.View{Class: "overflow-hidden rounded-full", Children: []core.Node{
				base.Progress{Value: func() float32 { return percent }, Label: locales.DownloadStorageUsed(cx, mediaBytes(used)),
					TrackClass: "h-3 bg-zinc-900/10 dark:bg-white/10", IndicatorClass: "h-3 bg-violet-500"},
			}},
			core.View{Class: "flex-row items-center gap-1.5", Children: []core.Node{
				core.View{Class: "size-2 rounded-full bg-violet-500"},
				core.Text{Content: locales.DownloadUsage(cx, mediaBytes(used)), Class: "text-xs " + setupTitle},
			}},
		},
	}
}

func (a *application) downloadRow(cx *core.Scope, entry downloadEntry, confirm *core.Signal[string]) core.Node {
	queue, id := a.downloads, entry.ID
	state := queue.stateLabel(cx, entry)
	art := core.View{Class: "relative h-12 w-20 shrink-0 overflow-hidden rounded-lg", Children: []core.Node{
		homeArtwork("", "absolute inset-0"),
	}}
	title := entry.Item.Title
	if series := entry.Item.SeriesTitle; series != "" {
		title = series + " · " + title
	}
	data := []core.Node{core.Text{Content: title, Class: "text-sm " + setupTitle, MaxLines: 1}}
	if entry.State == downloadComplete {
		quality := locales.OriginalQuality(cx)
		if entry.EncodedHeight > 0 {
			quality = strconv.Itoa(entry.EncodedHeight) + "p"
		}
		state += " · " + quality + " · " + mediaBytes(entry.Total)
	} else if entry.Total > 0 {
		progress := float32(min(100, 100*float64(entry.Transferred)/float64(entry.Total)))
		data = append(data, core.View{Class: "overflow-hidden rounded-full", Children: []core.Node{
			base.Progress{Label: locales.DownloadPercent(cx, strconv.Itoa(int(progress))), Value: func() float32 { return progress },
				TrackClass: "h-1 bg-zinc-900/10 dark:bg-white/10", IndicatorClass: "h-1 bg-violet-500"},
		}})
		state += " · " + mediaBytes(entry.Transferred) + " / " + mediaBytes(entry.Total)
	} else if entry.Transferred > 0 {
		state += " · " + mediaBytes(entry.Transferred)
	}
	if entry.State == downloadRunning && entry.Rate > 0 {
		state += " · " + mediaBytes(entry.Rate) + "/s"
	}
	if entry.Failure != "" {
		state += " · " + downloadErrorText(cx, entry.Failure)
	}
	tint := setupMuted
	if entry.State == downloadComplete {
		tint = "text-emerald-500"
	}
	data = append(data, core.Text{Content: state, TestID: "download-state-" + id, Class: "text-[11px] " + tint, MaxLines: 2})
	content := []core.Node{art, core.View{Class: "min-w-0 flex-1 gap-1", Children: data}}
	var actions []core.Node
	var expiration core.Node
	if entry.State == downloadComplete {
		statusIcon, statusTint := lucide.CircleCheck(), "text-emerald-500"
		if entry.Availability.Expired != "" {
			statusIcon, statusTint = lucide.CircleAlert(), "text-amber-600 dark:text-amber-400"
			reason := locales.DownloadExpiredOffline(cx)
			if entry.Availability.Expired == downloadMissingFromSource {
				reason = locales.DownloadExpiredMissing(cx)
			}
			label := locales.DownloadExpired(cx)
			var keep core.Node = ui.Button{Text: locales.DownloadKeepCopy(cx), Variant: ui.ButtonOutline,
				TestID: "download-keep-" + id, OnPress: func() { queue.keepExpired(id) }}
			if entry.Availability.Keep {
				label, keep = locales.DownloadCopyKept(cx), nil
			}
			expiration = core.View{Class: "flex-row flex-wrap items-center justify-between gap-2",
				TestID: "download-expired-" + id, Children: []core.Node{
					core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
						core.Text{Content: label, Class: "text-xs font-medium " + statusTint},
						core.Text{Content: reason, Class: "text-[11px] " + setupMuted},
					}}, keep,
				}}
		}
		content = append(content, core.Icon{SVG: statusIcon, Class: "size-5 " + statusTint})
		actions = append(actions, base.Button{Class: "min-w-0 flex-1 flex-row items-center gap-3", Label: locales.DownloadPlay(cx, title),
			TestID: "download-play-" + id, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
			OnPress: func(core.PressEvent) { a.openDownload(entry) }, Children: content})
	} else {
		actions = append(actions, core.View{Class: "min-w-0 flex-1 flex-row items-center gap-3", Children: content})
		if entry.State == downloadRunning || entry.State == downloadQueued {
			actions = append(actions, setupIconButton(cx, locales.DownloadPause(cx), "download-pause-"+id, lucide.Pause(), nil, func() { queue.pause(id) }))
		} else {
			actions = append(actions, setupIconButton(cx, locales.DownloadResume(cx), "download-resume-"+id, lucide.Play(), nil, func() { queue.resume(id) }))
		}
	}
	if queue.running[id] == nil && entry.State != downloadRunning {
		actions = append(actions, setupIconButton(cx, locales.DownloadRemove(cx), "download-remove-"+id, lucide.Trash(), nil, func() { confirm.Set(id) }))
	}
	useCellular := core.Show{When: func() bool {
		return entry.State == downloadQueued && entry.WaitForWiFi && !queue.wifiOnly.Get() &&
			queue.network.Get() == downloadNetworkCellular
	}, Then: func(cx *core.Scope) core.Node {
		return ui.Button{Text: locales.DownloadUseCellular(cx), TestID: "download-cellular-" + id,
			Variant: ui.ButtonOutline, OnPress: func() { queue.useCellular(id) }}
	}}
	return core.View{Class: "gap-2 bg-white px-4 py-3 dark:bg-[#1a1b25]", TestID: "download-row-" + id, Children: []core.Node{
		core.View{Class: "flex-row items-center gap-2", Children: actions},
		expiration,
		useCellular,
		core.Show{When: func() bool { return confirm.Get() == id }, Then: func(cx *core.Scope) core.Node {
			return core.View{Class: "flex-row items-center justify-end gap-2", Children: []core.Node{
				ui.Button{Text: locales.Back(cx), Variant: ui.ButtonGhost, OnPress: func() { confirm.Set("") }},
				ui.Button{Text: locales.DownloadRemove(cx), TestID: "confirm-remove-download-" + id, Color: func() core.Color { return cx.Palette().Danger },
					OnPress: func() { queue.remove([]string{id}); confirm.Set("") }},
			}}
		}},
	}}
}

func (a *application) downloadBadge(entry catalogItem) core.Node {
	id := downloadID(mediaAccountKey(entry.source), entry.item.ID)
	return core.Show{When: func() bool {
		download, found := a.downloads.find(id, a.downloads.entries.Get())
		return found && download.State == downloadComplete
	}, Then: func(cx *core.Scope) core.Node {
		return core.View{Class: "absolute bottom-2 right-2 size-6 items-center justify-center rounded-full bg-black/60",
			TestID: "download-badge-" + entry.item.ID, Role: core.RoleImage, Label: locales.Downloaded(cx), Children: []core.Node{
				core.Icon{SVG: lucide.Download(), Class: "size-3.5 text-white"},
			}}
	}}
}
