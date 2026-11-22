package main

import (
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/danmaku"
	"waygu-video/locales"
)

func (p desktopPlayerControls) composer(cx *core.Scope) core.Node {
	text := core.NewSignal(cx, "")
	color := core.NewSignal(cx, uint32(danmaku.White))
	mode := core.NewSignal(cx, danmaku.Scroll)
	failed := core.NewSignal(cx, false)
	send := func(raw string) {
		message := strings.TrimSpace(raw)
		if message == "" {
			return
		}
		ok := p.comments.addLocal(danmaku.Comment{Text: message, Color: color.GetUntracked(),
			Mode: mode.GetUntracked(), At: p.position.GetUntracked()})
		failed.Set(!ok)
		if ok {
			text.Set("")
		}
	}
	options := []core.Node{}
	for _, entry := range []struct {
		color uint32
		label string
	}{
		{danmaku.White, locales.DesktopDanmakuWhite(cx)},
		{0xfcd34d, locales.DesktopDanmakuAmber(cx)},
		{0x7dd3fc, locales.DesktopDanmakuBlue(cx)},
	} {
		options = append(options, ui.MenuItem{Text: entry.label, Checked: func() bool { return color.Get() == entry.color },
			OnSelect: func() { color.Set(entry.color) }, NoCloseOnSelect: true,
			Leading: core.View{Class: "size-3 rounded-full", Style: core.Style{Background: core.RGBA(entry.color, 1)}}})
	}
	options = append(options, ui.MenuSeparator{})
	for _, entry := range []struct {
		mode  danmaku.Mode
		label string
	}{
		{danmaku.Scroll, locales.DanmakuScrolling(cx)}, {danmaku.Top, locales.DanmakuTop(cx)}, {danmaku.Bottom, locales.DanmakuBottom(cx)},
	} {
		options = append(options, ui.MenuItem{Text: entry.label, Checked: func() bool { return mode.Get() == entry.mode },
			OnSelect: func() { mode.Set(entry.mode) }})
	}
	return core.View{Class: "relative h-9 min-w-0 max-w-[360px] shrink flex-row items-center gap-2 rounded-full border border-white/10 bg-white/15 pr-1 pl-3",
		TestID: "desktop-player-composer", Children: []core.Node{
			base.Button{TestID: "toggle-danmaku", Label: locales.ShowDanmaku(cx), Selected: p.comments.enabled, Style: core.Style{FocusVisible: ui.FocusRing(cx, 1)},
				Class:   "size-5 shrink-0 items-center justify-center rounded bg-white/15 selected:bg-linear-to-br selected:from-violet-500 selected:to-fuchsia-500",
				OnPress: func(core.PressEvent) { p.comments.toggle() }, Children: []core.Node{
					core.Text{Content: locales.PhonePlayerDanmakuBadge(cx), Class: "text-[11px] font-bold text-white"},
				}},
			core.Input{Label: locales.DesktopDanmakuLocal(cx), TestID: "desktop-danmaku-draft", Class: "h-7 w-56 min-w-20 flex-1 border-0 bg-transparent p-0 text-sm text-white",
				Placeholder: locales.DesktopDanmakuPlaceholder(cx), MaxLength: 120, Value: text.Get, OnChange: text.Set, OnSubmit: send},
			base.Menu{Label: locales.DesktopDanmakuPalette(cx), TestID: "desktop-danmaku-palette", PopupClass: tabletDetailPopup,
				Class: "size-5 shrink-0 items-center justify-center rounded", Style: core.Style{FocusVisible: ui.FocusRing(cx, 1)},
				Trigger: core.Icon{SVG: lucide.Palette(), Class: "size-4 text-zinc-400"}, Children: options},
			base.Button{Label: locales.DesktopDanmakuSend(cx), Hint: locales.DesktopDanmakuLocal(cx), TestID: "desktop-danmaku-send",
				Class: "h-7 shrink-0 items-center justify-center rounded-full bg-violet-500 px-3.5 shadow-md shadow-violet-500/40",
				Style: core.Style{FocusVisible: ui.FocusRing(cx, 1)}, OnPress: func(core.PressEvent) { send(text.GetUntracked()) },
				Children: []core.Node{core.Text{Content: locales.DesktopDanmakuSend(cx), Class: "text-xs font-semibold text-white"}}},
			core.Show{When: failed.Get, Then: func(cx *core.Scope) core.Node {
				return core.Text{Class: "absolute inset-x-0 bottom-10 rounded-lg bg-black/85 px-3 py-2 text-xs text-red-300",
					Content: locales.DanmakuPreferencesError(cx)}
			}},
		}}
}
