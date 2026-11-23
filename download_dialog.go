package main

import (
	"math"
	"strconv"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

type downloadDialog struct {
	id, title  string
	open       *core.Signal[bool]
	height     *core.Signal[int]
	transcodes func() bool
	selection  func() []route
	confirm    func(bool)
	children   []core.Node
}

func (q *downloadQueue) selectionDialog(cx *core.Scope, form downloadDialog) core.Node {
	type diskCheck struct {
		available int64
		known     bool
		ready     bool
	}
	disk := core.NewSignal(cx, diskCheck{})
	cx.Effect(func() {
		if !form.open.Get() {
			return
		}
		disk.Set(diskCheck{})
		active := true
		cx.Cleanup(func() { active = false })
		cx.DiskSpace(func(space core.DiskSpace, err error) {
			if active {
				disk.Set(diskCheck{available: max(0, space.Available), known: err == nil, ready: true})
			}
		})
	})
	network := q.network.Get
	wait := func() downloadNetworkWait { return downloadWaitFor(network(), q.wifiOnly.Get()) }
	allowChoice := func() bool {
		transport := network()
		return !q.wifiOnly.Get() && (transport == downloadNetworkCellular || transport == downloadNetworkOffline)
	}
	space := func() (needed, available int64, insufficient bool) {
		free := disk.Get()
		available = free.available
		if quota := q.quota.Get(); quota > 0 {
			for _, entry := range q.entries.Get() {
				quota -= min(quota, max(0, entry.Transferred, entry.Total))
			}
			if free.known {
				quota = min(quota, available)
			}
			available = quota
		} else if !free.known {
			return 0, 0, false
		}
		needed, _ = downloadEstimate(form.selection(), form.height.Get())
		return needed, available, needed > available
	}
	disabled := func() bool {
		_, _, insufficient := space()
		return !disk.Get().ready || insufficient || len(form.selection()) == 0
	}
	confirm := func(wifi bool) {
		if !core.Untracked(cx, disabled) {
			form.confirm(wifi)
		}
	}
	children := []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
		if network() == downloadNetworkCellular && allowChoice() {
			return hint(locales.DownloadCellularNotice(cx))
		}
		if waiting := wait(); waiting != downloadNetworkReady {
			return hint(waiting.label(cx))
		}
		return nil
	})}
	children = append(children, form.children...)
	children = append(children, core.Dynamic(func(cx *core.Scope) core.Node {
		choices := form.selection()
		options := downloadQualityOptions(cx)
		if !form.transcodes() {
			options = options[:1]
		}
		rows := make([]core.Node, 0, len(options))
		for _, option := range options {
			bytes, exactEstimate := downloadEstimate(choices, option.Value)
			var estimate core.Node
			if bytes > 0 {
				label := locales.DownloadEstimatedSize(cx, mediaBytes(bytes))
				if !exactEstimate {
					label = locales.DownloadMinimumEstimate(cx, mediaBytes(bytes))
				}
				estimate = core.Text{Content: label, Class: "text-xs text-muted-foreground"}
			}
			rows = append(rows, core.View{Class: "min-h-11 flex-row items-center justify-between gap-3", Children: []core.Node{
				ui.Radio{Text: option.Label, TestID: form.id + "-quality-" + strconv.Itoa(option.Value),
					Checked: func() bool { return form.height.Get() == option.Value },
					OnChange: func(checked bool) {
						if checked {
							form.height.Set(option.Value)
						}
					}}, estimate,
			}})
		}
		return ui.RadioGroup{Label: locales.DownloadQuality(cx), Children: rows}
	}), core.Dynamic(func(cx *core.Scope) core.Node {
		needed, available, insufficient := space()
		if !insufficient {
			return nil
		}
		return core.View{Class: "gap-2", TestID: form.id + "-insufficient-space", Children: []core.Node{
			notice(locales.DownloadSpaceComparison(cx, mediaBytes(needed), mediaBytes(available))),
			ui.Button{Text: locales.StorageDownloads(cx), TestID: form.id + "-storage", Variant: ui.ButtonOutline, OnPress: func() {
				form.open.Set(false)
				q.app.push(route{screen: screenDownloads})
			}},
		}}
	}))
	return ui.Dialog{Title: form.title, Open: form.open.Get, TestID: form.id + "-quality",
		OnOpenChange: func(show bool, _ base.Reason) { form.open.Set(show) },
		OnConfirm:    func() { confirm(false) }, Children: children,
		Footer: []core.Node{
			ui.Button{Text: locales.DesktopSettingsCancel(cx), Variant: ui.ButtonOutline, OnPress: func() { form.open.Set(false) }},
			core.Show{When: allowChoice, Then: func(cx *core.Scope) core.Node {
				return ui.Button{Text: locales.DownloadWaitWiFi(cx), TestID: form.id + "-wait-wifi",
					Variant: ui.ButtonOutline, Disabled: disabled, OnPress: func() { confirm(true) }}
			}},
			ui.Button{TextFunc: func() string {
				if waiting := wait(); waiting != downloadNetworkReady {
					return waiting.label(cx)
				}
				if network() == downloadNetworkCellular {
					return locales.DownloadUseCellular(cx)
				}
				return locales.DownloadAction(cx)
			}, TestID: form.id + "-confirm", Disabled: disabled, OnPress: func() { confirm(false) }},
		},
	}
}

func downloadEstimate(choices []route, height int) (int64, bool) {
	var total int64
	known := true
	for _, choice := range choices {
		bytes := choice.item.FileSize
		if height > 0 {
			bytes = int64(choice.item.Duration.Seconds() * float64(downloadBitrate(height)/8))
		}
		if bytes <= 0 {
			known = false
			continue
		}
		total += min(bytes, math.MaxInt64-total)
	}
	return total, known
}
