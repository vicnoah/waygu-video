package main

import (
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func phoneLandscape(cx *core.Scope) bool {
	if appLayoutIdiom(cx) != core.IdiomPhone {
		return false
	}
	window := cx.Env().Window()
	os := cx.Env().OS()
	return window.Height > 0 && window.Width > window.Height && (os == core.OSAndroid || os == core.OSIOS || window.Height <= 520)
}

func playerSidePanel(cx *core.Scope, id, closeID, label string, opened func() bool, close func(), heading, tabs, body core.Node) core.Node {
	return core.Show{When: opened, Then: func(cx *core.Scope) core.Node {
		var ios core.IOSModalProps
		if core.Untracked(cx, func() bool { return phoneLandscape(cx) }) {
			ios.SupportedOrientations = core.OrientationLandscape
		}
		return base.Dialog{
			Label: label, TestID: id, Open: opened, Animation: core.ModalNone,
			IOS: ios,
			OnOpenChange: func(show bool, _ base.Reason) {
				if !show {
					close()
				}
			},
			BackdropClass: "bg-black/50", BackdropTestID: id + "-backdrop",
			LayerClass: "flex-row justify-end items-stretch",
			LayerStyle: core.Style{Direction: core.Row, JustifyContent: core.JustifyEnd, AlignItems: core.AlignStretch},
			PopupClass: "h-full w-[360px] max-w-full min-h-0 border-l border-white/10 bg-zinc-900/85 shadow-2xl shadow-black/60",
			PopupStyle: core.Style{PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Right: core.EnvSafeArea, Bottom: core.EnvSafeArea}},
			Children: []core.Node{
				core.View{Class: "shrink-0 flex-row items-center justify-between gap-3 px-5 pt-4 pb-3", Children: []core.Node{
					heading, playerIconButton(cx, closeID, locales.TracksDone(cx), "size-8 rounded-full bg-white/10", "size-4 text-zinc-300", lucide.X, nil, close),
				}},
				tabs,
				core.Scroll{Class: "min-h-0 flex-1", TestID: id + "-scroll", Children: []core.Node{
					core.View{Class: "gap-2.5 px-5 pt-3 pb-4", Children: []core.Node{body}},
				}},
			},
		}
	}}
}

func panelHeading(label string) core.Node {
	return core.Text{Content: label, Class: "text-base font-semibold text-white"}
}

func playerPanelTabs(cx *core.Scope, id, label string, tabs []ui.Option[int], active *core.Signal[int]) core.Node {
	nodes := make([]core.Node, 0, len(tabs))
	for _, option := range tabs {
		nodes = append(nodes, base.Tab{Label: option.Label, TestID: option.TestID,
			Selected: func() bool { return active.Get() == option.Value },
			OnPress:  func(core.PressEvent) { active.Set(option.Value) },
			Class:    "group h-7 flex-1 items-center justify-center rounded-lg selected:bg-violet-500 selected:shadow-md selected:shadow-violet-500/30",
			Style:    core.Style{FocusVisible: ui.FocusRing(cx, -1)}, Children: []core.Node{
				core.Text{Content: option.Label, MaxLines: 1, Class: "text-xs text-zinc-400 group-selected:font-medium group-selected:text-white"},
			},
		})
	}
	return base.Tabs{Label: label, TestID: id, Class: "mx-5 shrink-0 flex-row rounded-xl border border-white/10 bg-white/5 p-1", Children: nodes}
}

func playerIconButton(cx *core.Scope, id, label, class, iconClass string, icon func() *core.SVG, disabled func() bool, action func()) core.Node {
	return base.Button{TestID: id, Label: label, Class: "shrink-0 items-center justify-center disabled:opacity-40 " + class,
		Disabled: disabled, Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) {
			if action != nil {
				action()
			}
		},
		Children: []core.Node{core.Icon{SVG: icon(), Class: iconClass}},
	}
}

func playerRadio(cx *core.Scope, model ui.Radio, detail string, stacked bool) core.Node {
	if !phoneLandscape(cx) {
		return model
	}
	class := "h-10 flex-row items-center gap-2"
	name := "min-w-0 shrink text-sm text-zinc-200 group-selected:font-medium group-selected:text-white"
	sub := "text-[11px] text-zinc-400"
	title := core.Text{Content: model.Text, MaxLines: 1, Class: name}
	var content []core.Node
	if stacked {
		class = "min-h-12 flex-row items-center gap-3"
		content = []core.Node{core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
			title, core.Text{Content: detail, MaxLines: 1, Class: sub},
		}}}
	} else if detail != "" {
		content = []core.Node{title, core.Text{Content: detail, MaxLines: 1, Class: "min-w-0 flex-1 " + sub}}
	} else {
		title.Class += " flex-1"
		content = []core.Node{title}
	}
	return base.Radio{Label: model.Text, Hint: detail, TestID: model.TestID, Checked: model.Checked, Disabled: model.Disabled, OnChange: model.OnChange,
		Class: "group bg-zinc-800/70 px-3 selected:bg-violet-500/20 disabled:opacity-50 " + class,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, Children: append(content,
			core.Show{When: model.Checked, Then: func(*core.Scope) core.Node {
				return core.Icon{SVG: lucide.Check(), Class: "size-4 text-violet-400"}
			}},
		)}
}

func playerRadioList(cx *core.Scope, label string, rows []core.Node) core.Node {
	if phoneLandscape(cx) {
		return base.RadioGroup{Label: label,
			Class: "gap-px overflow-hidden rounded-xl border border-white/10 bg-white/10", Children: rows}
	}
	return ui.RadioGroup{Label: label, Children: rows}
}

func playerRail(ctx *core.Scope, slider base.Slider, thumb, rail string) core.Node {
	slider.Class = "justify-center " + slider.Class
	slider.TrackClass = "h-full justify-center"
	slider.IndicatorClass = "rounded-full bg-white/20 " + rail
	slider.RangeClass = "rounded-full bg-linear-to-r from-violet-500 to-fuchsia-500"
	slider.ThumbClass = "rounded-full bg-white shadow-md shadow-violet-500/50 " + thumb
	slider.ThumbStyle = core.Style{FocusVisible: ui.FocusRing(ctx, 2)}
	return slider
}

func playerChoice(cx *core.Scope, id, label, size string, checked, disabled func() bool, choose func(bool)) core.Node {
	return base.Radio{TestID: id, Label: label, Checked: checked, Disabled: disabled, OnChange: choose,
		Class: "group items-center justify-center rounded-lg border border-white/10 bg-white/5 selected:bg-linear-to-br selected:from-violet-500 selected:to-fuchsia-500 disabled:opacity-50 " + size,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, Children: []core.Node{
			core.Text{Content: label, MaxLines: 1, Class: "text-sm text-zinc-300 group-selected:font-semibold group-selected:text-white"},
		},
	}
}

func playerPanelAction(cx *core.Scope, id, label string, icon func() *core.SVG, inList bool, disabled func() bool, action func()) core.Node {
	class := "h-9 rounded-lg border border-white/10 bg-white/5"
	if inList {
		class = "h-10 bg-zinc-800/70"
	}
	return base.Button{TestID: id, Label: label, Disabled: disabled, Class: "flex-row items-center gap-2.5 px-3 disabled:opacity-50 " + class,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { action() },
		Children: []core.Node{core.Icon{SVG: icon(), Class: "size-4 text-violet-400"},
			core.Text{Content: label, Class: "flex-1 text-sm text-zinc-100"},
			core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 text-zinc-500"},
		},
	}
}

func playerStepRow(cx *core.Scope, label string, value func() string, minusID, minusLabel, plusID, plusLabel string,
	disabled func() bool, adjust func(int)) core.Node {
	return core.View{Class: "flex-row items-center justify-between gap-2", Children: []core.Node{
		core.Text{Content: label, Class: "min-w-0 flex-1 text-sm text-white"},
		core.View{Class: "h-8 flex-row items-center rounded-lg border border-white/10 bg-white/5", Children: []core.Node{
			playerIconButton(cx, minusID, minusLabel, "h-8 w-9 rounded-l-lg", "size-3.5 text-zinc-300", lucide.Minus, disabled, func() { adjust(-1) }),
			core.Text{ContentFunc: value, Class: "w-12 text-center text-xs font-medium text-white"},
			playerIconButton(cx, plusID, plusLabel, "h-8 w-9 rounded-r-lg", "size-3.5 text-zinc-300", lucide.Plus, disabled, func() { adjust(1) }),
		}},
	}}
}

func playerTrackTitle(cx *core.Scope, track media.Track) string {
	if code := subtitleLanguage(track.Language); code != "" && code != "und" {
		return settingsLanguageName(cx, code)
	}
	if track.Title != "" {
		return track.Title
	}
	return locales.TrackNumber(cx, track.ID)
}

func playerSubtitleDetails(cx *core.Scope, track media.Track) string {
	origin := locales.EmbeddedSubtitle(cx)
	if track.External {
		origin = locales.ExternalSubtitle(cx)
	}
	if track.Forced {
		origin += " · " + locales.ForcedSubtitle(cx)
	}
	if track.Codec != "" {
		return strings.ToUpper(track.Codec) + " · " + origin
	}
	return origin
}
