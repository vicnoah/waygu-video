package main

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

type tvOption struct {
	id, label string
	value     func() string
	checked   func() bool
	action    func()
	adjust    func(int)
	content   func(func() bool) core.Node
	disabled  func() bool
	large     bool
}

func tvSettingsRows(cx *core.Scope, l tvLayout, rows []tvOption, first *core.Ref, enter, leave func()) core.Node {
	options := make([]core.Node, 0, len(rows))
	refs := make([]core.Ref, len(rows))
	enabled := func(index int) bool { return rows[index].disabled == nil || !rows[index].disabled() }
	for index, option := range rows {
		ref := &refs[index]
		if index == 0 {
			ref = first
		}
		options = append(options, core.Func(func(cx *core.Scope) core.Node {
			focused := core.NewSignal(cx, false)
			move := func(delta int) {
				for next := index + delta; next >= 0 && next < len(rows); next += delta {
					if enabled(next) {
						if next == 0 {
							first.Focus()
						} else {
							refs[next].Focus()
						}
						return
					}
				}
			}
			cx.Effect(func() {
				if !focused.Get() {
					return
				}
				keys := core.KeyOptions{When: core.KeyNotEditing, Repeat: true}
				cx.OnKeyWith("up", keys, func() { move(-1) })
				cx.OnKeyWith("down", keys, func() { move(1) })
				cx.OnKeyWith("left", keys, func() {
					if option.adjust != nil {
						option.adjust(-1)
					} else {
						leave()
					}
				})
				cx.OnKeyWith("right", keys, func() {
					if option.adjust != nil {
						option.adjust(1)
					}
				})
			})
			check := option.checked
			style := core.Style{Height: l.length(68), Gap: l.points(20), Padding: l.padding(28, 0), Selected: l.focusRing()}
			label := l.text(option.label, 24, "flex-1 min-w-0 "+tvSettingsTitle+" group-selected:font-semibold "+tvSettingsFocusText)
			data := []core.Node{label}
			role := core.RoleButton
			switch {
			case check != nil:
				role = core.RoleSwitch
				data = append(data, tvSettingsSwitch(l, check))
			case option.content != nil:
				if option.large {
					style.Height, style.Gap, style.Padding = core.Length{}, l.points(16), l.padding(28, 20)
					data = []core.Node{option.content(focused.Get)}
				} else {
					data = append(data, option.content(focused.Get))
				}
			default:
				arrow := tvSettingsMuted + " " + tvSettingsFocusText
				if option.adjust != nil {
					data = append(data, l.icon(lucide.ChevronLeft(), 24, arrow))
				}
				if option.value != nil {
					value := l.text("", 24, "min-w-0 max-w-[60%] text-right "+tvSettingsMuted+" "+tvSettingsFocusText)
					value.ContentFunc, value.TestID = option.value, option.id+"-value"
					if option.adjust != nil {
						value.Class = tvSettingsTitle + " text-center " + tvSettingsFocusText
						value.Style.MinWidth = l.length(96)
					}
					data = append(data, value)
				}
				if option.adjust != nil || option.action != nil {
					data = append(data, l.icon(lucide.ChevronRight(), 24, arrow))
				}
			}
			class := tvSettingsCard + " " + tvSettingsFocusRow + " flex-row items-center disabled:opacity-60"
			if option.large {
				class = tvSettingsCard + " " + tvSettingsFocusRow
			}
			button := core.View{Class: class, Style: style, Ref: ref, TestID: option.id,
				Role: role, Label: option.label, Checked: check, Selected: focused.Get, Disabled: option.disabled,
				OnFocus: func() { focused.Set(true); enter() }, OnBlur: func() { focused.Set(false) },
				OnPress: func(core.PressEvent) {
					if option.action != nil {
						option.action()
					}
				}, Children: data}
			if cx.Env().OS() == core.OSIOS && option.adjust != nil {
				return tvAppleAdjustTargets(cx, l, button, option.adjust, ref)
			}
			return button
		}))
	}
	return core.View{Style: core.Style{Gap: l.points(10)}, Children: options}
}

func tvSettingsSwitch(l tvLayout, enabled func() bool) core.Node {
	return core.View{Class: "flex-row items-center rounded-full", StyleFunc: func() core.Style {
		color, position := core.RGBA(0x88888e, .35), core.JustifyStart
		if enabled() {
			color, position = core.RGB(0x8b5cf6), core.JustifyEnd
		}
		return core.Style{Background: color, JustifyContent: position, Width: l.length(68), Height: l.length(40), Padding: l.padding(3, 0)}
	}, Children: []core.Node{core.View{Class: "rounded-full bg-white shadow-md",
		Style: core.Style{Width: l.length(34), Height: l.length(34)}}}}
}
