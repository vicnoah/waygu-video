package main

import (
	"context"
	_ "embed"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

const wayguIssuesPage = "https://github.com/vicnoah/waygu-video/issues/new"

var (
	//go:embed build/android/LICENSE.nsgo
	nsgoLicense string

	//go:embed docs/licenses/third-party.txt
	thirdPartyLicenses string

	//go:embed internal/playback/opencc/LICENSE
	openCCLicense string
)

func (a *application) phoneAboutSettings(cx *core.Scope) core.Node {
	return phoneSettingsPage(cx, locales.SettingsAbout(cx), a.back, "phone-about-settings", a.aboutSettings(cx))
}

func (a *application) aboutSettings(cx *core.Scope) core.Node {
	failure := core.NewSignal[error](cx, nil)
	feedback := core.NewSignal(cx, "")
	panel := core.NewSignal(cx, "")
	reset := core.NewSignal(cx, false)
	exporting := core.NewSignal(cx, false)
	openPanel := func(name string) func() { return func() { panel.Set(name) } }
	export := func() {
		if exporting.GetUntracked() {
			return
		}
		exporting.Set(true)
		failure.Set(nil)
		feedback.Set("")
		now, lifetime := cx.WallTime(), cx.Context()
		go func() {
			data, err := a.diagnostics.archive(now)
			cx.Post(func() {
				if lifetime.Err() != nil {
					return
				}
				if err != nil {
					failure.Set(err)
					exporting.Set(false)
					return
				}
				cx.SaveFile(core.SaveOptions{Name: "waygu-network-logs.zip", MIME: "application/zip", Data: data},
					func(saved bool, _ string, err error) {
						exporting.Set(false)
						failure.Set(err)
						if saved {
							feedback.Set(locales.AboutLogsSaved(cx))
						}
					})
			})
		}()
	}

	showLogs := func() bool { return a.diagnosticPrefs.Get().Network }
	record := (phoneSetting{locales.AboutNetworkLogs(cx), "", "bg-orange-500", lucide.Activity}).toggle(cx, "about-network-logs", showLogs, nil)
	if a.diagnostics.dir != "" {
		record = (phoneSetting{locales.AboutNetworkLogs(cx), "", "bg-orange-500", lucide.Activity}).toggle(cx, "about-network-logs", showLogs, func(enabled bool) {
			failure.Set(a.saveDiagnosticPreferences(func(p *diagnosticPreferences) { p.Network = enabled }))
		})
	}
	resetLabel := locales.AboutReset(cx)
	formats := (phoneSetting{locales.AboutFormats(cx), "", "bg-violet-500", lucide.FileVideoCamera}).link(cx,
		"about-formats", "MKV · MP4 · TS …", openPanel("formats"))
	return core.View{Class: "gap-6", Children: []core.Node{
		aboutHeader(cx, latestWayguRelease),
		setupGroup(locales.AboutHelpSection(cx), "",
			(phoneSetting{locales.AboutHelp(cx), "", "bg-blue-500", lucide.BookOpen}).link(cx, "about-help", "", openPanel("help")),
			formats,
			(phoneSetting{locales.AboutFeedback(cx), "", "bg-emerald-500", lucide.MessageCircle}).link(cx, "about-feedback", "", func() {
				cx.OpenURL(wayguIssuesPage, func(opened bool) {
					if !opened {
						feedback.Set(locales.AboutLinkFailed(cx, wayguIssuesPage))
					}
				})
			}),
			(phoneSetting{locales.AboutExportLogs(cx), "", "bg-zinc-500", lucide.FileDown}).link(cx, "about-export-logs", "", export),
		),
		setupGroup(locales.AboutDebug(cx), "", record,
			(phoneSetting{locales.AboutOverlay(cx), "", "bg-orange-500", lucide.Gauge}).toggle(cx, "about-player-overlay",
				func() bool { return a.diagnosticPrefs.Get().Overlay }, func(enabled bool) {
					failure.Set(a.saveDiagnosticPreferences(func(p *diagnosticPreferences) { p.Overlay = enabled }))
				})),
		setupGroup(locales.AboutMore(cx), "",
			(phoneSetting{locales.AboutRate(cx), "", "bg-amber-500", lucide.Star}).unavailable(cx),
			(phoneSetting{locales.AboutPrivacy(cx), "", "bg-zinc-500", lucide.Lock}).link(cx, "about-privacy", "", openPanel("privacy")),
			(phoneSetting{locales.AboutLicenses(cx), "", "bg-zinc-500", lucide.ScrollText}).link(cx, "about-licenses", "", openPanel("licenses"))),
		setupGroup("", "", base.Button{Label: resetLabel, TestID: "about-reset", Class: "h-12 items-center justify-center bg-white dark:bg-[#1a1b25]",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { reset.Set(true) },
			Children: []core.Node{core.Text{Content: resetLabel, Class: "text-[15px] text-rose-500"}}}),
		core.Text{Content: locales.AboutCopyright(cx), Class: "text-center text-xs text-zinc-500/70 dark:text-zinc-400/70"},
		core.Show{When: func() bool { return exporting.Get() || feedback.Get() != "" }, Then: func(*core.Scope) core.Node {
			return core.Text{Class: "px-5 text-sm " + setupMuted, TestID: "about-status", ContentFunc: func() string {
				if exporting.Get() {
					return locales.Loading(cx)
				}
				return feedback.Get()
			}}
		}},
		settingsFailure(cx, failure),
		ui.AlertDialog{Title: resetLabel, Description: locales.AboutResetDetail(cx), Open: reset.Get,
			OnOpenChange: func(open bool, _ base.Reason) { reset.Set(open) }, Actions: []ui.AlertAction{
				{Text: locales.DesktopSettingsCancel(cx), Role: core.AlertCancel, TestID: "about-cancel-reset"},
				{Text: resetLabel, Role: core.AlertDestructive, TestID: "about-confirm-reset", Variant: ui.ButtonDanger, OnPress: func() {
					a.resetPreferences(cx)
					failure.Set(nil)
					feedback.Set(locales.AboutResetDone(cx))
				}},
			}},
		core.Dynamic(func(cx *core.Scope) core.Node { return aboutDetail(cx, panel.Get(), func() { panel.Set("") }) }),
	}}
}

func aboutHeader(cx *core.Scope, lookup func(context.Context) (string, error)) core.Node {
	request := core.NewSignal(cx, uint64(0))
	latest := core.NewResource(cx, func() (uint64, bool) {
		id := request.Get()
		return id, id > 0
	}, func(ctx context.Context, _ uint64, _ core.ResourceInfo) (string, error) { return lookup(ctx) })
	app := core.App()
	version := locales.DesktopSettingsVersion(cx, app.Version)
	if app.Build != "" {
		version = locales.AboutVersionBuild(cx, app.Version, app.Build)
	}
	check := locales.DesktopSettingsCheckUpdate(cx)
	return core.View{Class: "items-center gap-2 py-4", Children: []core.Node{
		core.View{Class: "size-20 items-center justify-center rounded-[22px] bg-linear-to-br from-violet-500 to-fuchsia-500 shadow-xl shadow-violet-500/30",
			Children: []core.Node{core.Icon{SVG: lucide.Play(), Class: "size-9 text-white"}}},
		core.Text{Content: locales.Brand(cx), Class: "mt-1 text-xl font-bold " + setupTitle},
		core.Text{Content: version, Class: "text-sm " + setupMuted},
		base.Button{Label: check, TestID: "about-check-update", Class: "mt-1 h-8 items-center justify-center rounded-full bg-waygu-control/12 px-4",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, Disabled: latest.Loading,
			OnPress:  func(core.PressEvent) { request.Update(func(id uint64) uint64 { return id + 1 }) },
			Children: []core.Node{core.Text{Content: check, Class: "text-sm font-medium " + setupAccent}}},
		core.Show{When: func() bool { return request.Get() != 0 }, Then: func(cx *core.Scope) core.Node {
			return core.View{Class: "items-center gap-2 px-5", Children: []core.Node{
				core.Text{Class: "text-center text-sm " + setupMuted, TestID: "about-update-status", ContentFunc: func() string {
					if latest.Loading() {
						return locales.Loading(cx)
					}
					if latest.Err() != nil {
						return locales.DesktopSettingsUpdateError(cx)
					}
					tag, _ := latest.Get()
					if tag == "" {
						return locales.DesktopSettingsNoRelease(cx)
					}
					if strings.TrimPrefix(tag, "v") == strings.TrimPrefix(app.Version, "v") {
						return locales.DesktopSettingsCurrentRelease(cx)
					}
					return locales.DesktopSettingsPublishedRelease(cx, tag)
				}},
				core.Show{When: func() bool { tag, _ := latest.Get(); return tag != "" && latest.Err() == nil && !latest.Loading() },
					Then: func(cx *core.Scope) core.Node {
						return ui.Button{Text: locales.DesktopSettingsReleasePage(cx), Variant: ui.ButtonGhost,
							OnPress: func() { cx.OpenURL(wayguReleasePage, nil) }}
					}},
			}}
		}},
	}}
}

func aboutDetail(cx *core.Scope, panel string, close func()) core.Node {
	var title string
	var body []core.Node
	switch panel {
	case "help":
		title = locales.AboutHelp(cx)
		body = []core.Node{hint(locales.AboutHelpSources(cx)), hint(locales.AboutHelpPlayback(cx)), hint(locales.AboutHelpSupport(cx)), hint(locales.AboutTMDBCredit(cx)),
			ui.Button{Text: "弹弹play开放弹幕网络", Variant: ui.ButtonGhost, OnPress: func() { cx.OpenURL("https://www.dandanplay.com/", nil) }}}
	case "formats":
		title = locales.AboutFormats(cx)
		body = []core.Node{hint(locales.AboutVideoFormats(cx)), hint(locales.AboutSubtitleFormats(cx)), hint(locales.AboutDanmakuFormats(cx))}
	case "privacy":
		title = locales.AboutPrivacy(cx)
		body = []core.Node{hint(locales.AboutPrivacyServers(cx)), hint(locales.AboutPrivacyPlayback(cx)),
			hint(locales.AboutPrivacyDiagnostics(cx)), hint(locales.AboutPrivacyLocation(cx)), hint(locales.AboutTMDBPrivacy(cx))}
	case "licenses":
		title = locales.AboutLicenses(cx)
		body = []core.Node{core.Text{Content: "NSGo\n\n" + nsgoLicense + "\n\n" + thirdPartyLicenses + "\n\nOpenCC dictionaries\n\n" + openCCLicense,
			Class: "text-sm " + setupTitle, Selectable: core.SelectableOn}}
	default:
		return nil
	}
	return ui.Dialog{Title: title, Open: func() bool { return true }, TestID: "about-detail",
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				close()
			}
		},
		Trailing: []core.Node{ui.IconButton{Label: locales.TracksDone(cx), Icon: lucide.X, Size: 36, TestID: "close-about-detail", OnPress: close}},
		Children: []core.Node{
			core.Scroll{StyleFunc: func() core.Style {
				return core.Style{MaxHeight: core.Pt(min(430, cx.Env().Window().Height/2)), FlexShrink: 1}
			}, Children: []core.Node{core.View{Class: "gap-3 pr-2", Children: body}}},
		}}
}
