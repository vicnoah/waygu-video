package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"waygu-video/locales"
)

func (p tvPlayerControls) tvPlayerFooter(cx *core.Scope, l tvLayout, scrub *core.Signal[time.Duration], hints bool, settings func() bool) core.Node {
	rail := core.View{Class: "relative rounded-full bg-white/20", TestID: "tv-player-progress",
		Style: core.Style{Height: l.length(10)}, Children: []core.Node{
			core.View{Class: "absolute inset-y-0 left-0 rounded-full bg-linear-to-r from-violet-500 to-fuchsia-500 shadow-lg shadow-violet-500/50",
				StyleFunc: func() core.Style {
					return core.Style{Width: core.Pct(float32(100 * tvPlaybackFraction(p.position.Get(), p.duration.Get())))}
				}},
		}}
	position := l.text("", 24, "font-medium text-white")
	position.TestID, position.ContentFunc = "player-position", func() string { return playbackClock(p.position.Get()) }
	remaining := l.text("", 24, "font-medium text-zinc-300")
	remaining.ContentFunc = func() string { return "-" + playbackClock(max(0, p.duration.Get()-p.position.Get())) }
	timeline := core.View{Style: core.Style{Gap: l.points(12)}, Children: []core.Node{
		rail, core.View{Class: "flex-row items-center justify-between", Children: []core.Node{position, remaining}},
	}}
	nodes := []core.Node{p.next, tvPlayerPreview(cx, l, p, scrub, settings), timeline}
	if hints {
		chip := func(width core.Length, padding core.Points, parts ...core.Node) core.Node {
			return core.View{Class: "flex-row items-center justify-center rounded-lg border border-white/15 bg-white/15",
				Style: core.Style{Width: width, Height: l.length(40), Gap: l.points(4), Padding: l.padding(padding, 0)}, Children: parts}
		}
		hint := func(key core.Node, text string) core.Node {
			return core.View{Class: "flex-row items-center", Style: core.Style{Gap: l.points(12)}, Children: []core.Node{
				key, l.text(text, 20, "text-zinc-300"),
			}}
		}
		arrow := func(icon *core.SVG) core.Node { return l.icon(icon, 20, "text-white") }
		nodes = append(nodes, core.View{Class: "flex-row items-center", TestID: "tv-player-remote-hints",
			Style: core.Style{Gap: l.points(40)}, Children: []core.Node{
				hint(chip(l.length(40), 0, arrow(lucide.ArrowUp())), locales.TvPlayerTrackHint(cx)),
				hint(chip(l.length(40), 0, arrow(lucide.ArrowDown())), locales.TvPlayerDanmakuHint(cx)),
				hint(chip(core.Length{}, 8, arrow(lucide.ArrowLeft()), arrow(lucide.ArrowRight())), locales.TvPlayerSeekHint(cx)),
				hint(chip(core.Length{}, 12, l.text(locales.TvPlayerOK(cx), 18, "font-bold text-white")), locales.Pause(cx)),
			}})
	}
	return core.View{Class: "absolute", PointerEvents: core.PointerEventsBoxNone,
		StyleFunc: func() core.Style {
			right := core.Points(96)
			if settings() {
				right = 600
			}
			return core.Style{Left: l.length(96), Right: l.length(right), Bottom: l.length(56), Gap: l.points(24)}
		}, Children: nodes}
}

func tvPlaybackFraction(at, length time.Duration) float64 {
	if length <= 0 {
		return 0
	}
	return max(0, min(1, float64(at)/float64(length)))
}

type tvSettingRow struct {
	id, label string
	icon      *core.SVG
	value     func() string
	switchOn  func() bool
	change    func(int)
	press     func()
	disabled  func() bool
}

func (a *application) tvPlayerSettings(cx *core.Scope, l tvLayout, p tvPlayerControls, close func()) core.Node {
	edit := a.editDanmaku(cx)
	opacity := func() string { return locales.DanmakuPercent(cx, strconv.Itoa(edit.prefs.Get().Opacity)) }
	rate := func() string {
		label := strconv.FormatFloat(float64(p.rate.Get()), 'f', -1, 32)
		if !strings.Contains(label, ".") {
			label += ".0"
		}
		return label + "x"
	}
	open := func(tab int) func() { return func() { p.tracks.show(tab) } }
	enabled := p.comments.enabled
	toggle := func(delta int) {
		if enabled() != (delta > 0) {
			p.comments.toggle()
		}
	}
	changeOpacity := func(delta int) {
		edit.save(func(prefs *danmakuPreferences) {
			prefs.Opacity = max(10, min(100, prefs.Opacity+delta*10))
		})
	}
	quality := func() string {
		if limit := p.bitrate(); limit > 0 {
			return locales.BitrateMegabits(cx, strconv.Itoa(limit/1_000_000))
		}
		return locales.OriginalQuality(cx)
	}
	rows := []tvSettingRow{
		{id: "toggle-danmaku", label: locales.Danmaku(cx), icon: lucide.MessageSquare(), switchOn: enabled, change: toggle, press: p.comments.toggle},
		{id: "tv-player-opacity", label: locales.TvPlayerOpacity(cx), icon: lucide.Droplet(), value: opacity, change: changeOpacity},
		{id: "tv-player-subtitles", label: locales.Subtitles(cx), icon: lucide.Captions(), value: p.tracks.subtitle, press: open(trackSubtitleTab)},
		{id: "tv-player-audio", label: locales.AudioTracks(cx), icon: lucide.AudioLines(), value: p.tracks.audio, press: open(trackAudioTab)},
		{id: "tv-player-quality", label: locales.PlayerQuality(cx), icon: lucide.Gauge(), value: quality, press: open(trackQualityTab),
			disabled: func() bool { return !p.qualityAvailable }},
		{id: "tv-player-speed", label: locales.PlaybackSpeed(cx), icon: lucide.FastForward(), value: rate, press: open(trackSpeedTab),
			disabled: func() bool { return !p.canRate.Get() }},
		{id: "tv-player-more-settings", label: locales.TvMorePlaybackSettings(cx), icon: lucide.Settings(),
			press: func() { a.openTVPlayerSettings(p.position.GetUntracked()) }},
	}
	selected := core.NewSignal(cx, 0)
	refs := make([]core.Ref, len(rows))
	cx.Post(refs[0].Focus)
	isEnabled := func(row tvSettingRow) bool { return row.disabled == nil || !row.disabled() }
	move := func(delta int) {
		for next := selected.GetUntracked() + delta; next >= 0 && next < len(rows); next += delta {
			if isEnabled(rows[next]) {
				selected.Set(next)
				refs[next].Focus()
				return
			}
		}
	}
	adjust := func(delta int) {
		row := rows[selected.GetUntracked()]
		if isEnabled(row) {
			if row.change != nil {
				row.change(delta)
			} else if delta > 0 && row.press != nil {
				row.press()
			}
		}
	}
	keys := core.KeyOptions{When: core.KeyNotEditing, Repeat: true}
	cx.OnKeyWith("up", keys, func() { move(-1) })
	cx.OnKeyWith("down", keys, func() { move(1) })
	cx.OnKeyWith("left", keys, func() { adjust(-1) })
	cx.OnKeyWith("right", keys, func() { adjust(1) })
	cx.OnKey("escape", close)

	cells := make([]core.Node, 0, len(rows))
	for i, row := range rows {
		button := tvPlayerSettingRow(cx, l, row, func() bool { return selected.Get() == i }, func() { selected.Set(i) }, &refs[i])
		if cx.Env().OS() == core.OSIOS && row.change != nil {
			button = tvAppleAdjustTargets(cx, l, button, row, &refs[i])
		}
		cells = append(cells, button)
	}
	return core.View{Class: "absolute inset-y-0 right-0 border-l border-white/10 bg-zinc-900/85 shadow-2xl shadow-black/60",
		TestID: "tv-player-quick-settings", Label: locales.TvQuickSettings(cx), Role: core.RoleGroup,
		Style: core.Style{Width: l.length(560), Gap: l.points(32), Padding: core.Edges{
			Top: l.points(64), Left: l.points(40), Right: l.points(40), Bottom: l.points(48),
		}}, Children: []core.Node{
			core.View{Style: core.Style{Gap: l.points(8)}, Children: []core.Node{
				l.text(locales.TvQuickSettings(cx), 36, "text-4xl font-bold text-white"),
				l.text(locales.TvQuickSettingsHint(cx), 20, "text-zinc-500"),
			}},
			core.View{Style: core.Style{Gap: l.points(12)}, Children: cells[:len(cells)-1]},
			edit.warning(), core.View{Class: "flex-1"}, cells[len(cells)-1],
		}}
}

func tvPlayerSettingRow(cx *core.Scope, l tvLayout, row tvSettingRow, selected func() bool, focus func(), ref *core.Ref) core.Node {
	style := core.Style{Height: l.length(80), Padding: l.padding(24, 0), Gap: l.points(20)}
	style.Selected = l.focusRing()
	label := l.text(row.label, 24, "min-w-0 flex-1 text-white group-selected:font-semibold group-selected:text-black")
	if row.id == "tv-player-more-settings" {
		label.Style.FontSize = l.points(22)
	}
	icon := l.icon(row.icon, 32, "shrink-0 text-zinc-300 group-selected:text-black")
	class := "group flex-row items-center rounded-2xl border border-white/10 bg-white/5 selected:bg-white selected:border-transparent disabled:opacity-50"
	button := core.View{Class: class, Style: style, Role: core.RoleButton, TestID: row.id, Label: row.label,
		Selected: selected, OnFocus: focus, Ref: ref, Disabled: row.disabled,
		Children: []core.Node{icon, label}}
	button.OnPress = func(core.PressEvent) {
		focus()
		if row.press != nil {
			row.press()
		}
	}
	if row.switchOn != nil {
		button.Role, button.Checked = core.RoleSwitch, row.switchOn
		button.Children = append(button.Children, core.View{Class: "flex-row items-center rounded-full bg-white/15",
			StyleFunc: func() core.Style {
				justify, background := core.JustifyStart, core.RGBA(0xffffff, .15)
				if row.switchOn() {
					justify, background = core.JustifyEnd, core.RGB(0x8b5cf6)
				} else if selected() {
					background = core.RGB(0xe4e4e7)
				}
				return core.Style{JustifyContent: justify, Background: background, Width: l.length(68), Height: l.length(40), Padding: l.padding(3, 0)}
			}, Children: []core.Node{core.View{Class: "rounded-full bg-white shadow-md",
				Style: core.Style{Width: l.length(34), Height: l.length(34)}}}})
		return button
	}
	arrowClass := "shrink-0 text-zinc-500"
	if row.change != nil {
		button.Children = append(button.Children, l.icon(lucide.ChevronLeft(), 24, arrowClass))
	}
	if row.value != nil {
		value := l.text("", 24, "max-w-[52%] min-w-0 shrink text-zinc-400 group-selected:text-violet-600")
		if row.change != nil {
			value = l.text("", 24, "shrink-0 text-center font-semibold text-zinc-400 group-selected:text-violet-600")
			value.Style.Width = l.length(64)
		}
		value.ContentFunc, value.TestID = row.value, row.id+"-value"
		button.Children = append(button.Children, value)
	}
	button.Children = append(button.Children, l.icon(lucide.ChevronRight(), 24, arrowClass))
	return button
}

func (a *application) openTVPlayerSettings(at time.Duration) {
	stack := a.routes.GetUntracked()
	if len(stack) == 0 || stack[len(stack)-1].screen != screenPlayer {
		return
	}
	next := append([]route(nil), stack...)
	play := &next[len(next)-1]
	play.item.Position, play.resume, play.resumeLocal = at, true, true
	a.routes.Set(append(next, route{screen: screenTVPlayerSettings}))
}

func tvAppleRemoteTargets(cx *core.Scope, focus *core.Ref, up, down, left, right func()) core.Node {
	target := func(class string, action func(), refocus bool) core.Node {
		return core.View{Class: "absolute size-16 " + class, Focusable: func() bool { return true },
			AccessibilityElementsHidden: func() bool { return true }, OnFocus: func() {
				action()
				if refocus {
					cx.Post(focus.Focus)
				}
			}}
	}
	return core.Group{
		target("left-1/2 top-1/4", up, false),
		target("left-1/2 top-3/4", down, false),
		target("left-1/4 top-1/2", left, true),
		target("left-3/4 top-1/2", right, true),
	}
}

func tvAppleAdjustTargets(cx *core.Scope, l tvLayout, button core.Node, row tvSettingRow, ref *core.Ref) core.Node {
	target := func(edge string, delta int) core.Node {
		return core.View{Class: "absolute inset-y-0 " + edge, Style: core.Style{Width: l.length(28)},
			Focusable: func() bool { return true }, AccessibilityElementsHidden: func() bool { return true }, OnFocus: func() {
				row.change(delta)
				cx.Post(ref.Focus)
			}}
	}
	return core.View{Class: "relative", Children: []core.Node{
		button, target("right-full", -1), target("left-full", 1),
	}}
}
