package main

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func phoneSettingsPage(cx *core.Scope, title string, back func(), scrollID string, groups ...core.Node) core.Node {
	return phoneSettingsFrame(cx, title, back, scrollID,
		core.View{Class: "w-full max-w-[720px] self-center gap-6 pt-2 pb-8", Children: groups})
}

func phoneSettingsFrame(cx *core.Scope, title string, back func(), scrollID string, body core.Node) core.Node {
	label := locales.Settings(cx)
	return core.View{Class: "relative flex-1 bg-[#f4f4f7] dark:bg-[#0f1017]", Style: core.Style{
		FlexShrink: 1, PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Bottom: core.EnvKeyboard},
	}, Children: []core.Node{
		homeAmbient(false, false), homeAmbient(true, false),
		core.View{Class: "gap-1 pb-2", Children: []core.Node{
			core.View{Class: "h-11 flex-row items-center px-2", Children: []core.Node{
				base.Button{Label: label, TestID: "back", Class: "h-11 flex-row items-center gap-0.5 rounded px-2",
					Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { back() },
					Children: []core.Node{
						core.Icon{SVG: lucide.ChevronLeft(), Class: "size-6 " + setupAccent},
						core.Text{Content: label, Class: "text-[17px] " + setupAccent},
					}},
			}},
			core.Text{Content: title, Class: "px-5 text-3xl font-bold " + setupTitle},
		}},
		core.Scroll{Class: "flex-1", TestID: scrollID, Children: []core.Node{body}},
	}}
}

type phoneSetting struct {
	label, description, tint string
	icon                     func() *core.SVG
}

func (s phoneSetting) parts() []core.Node {
	var children []core.Node
	if s.icon != nil {
		children = append(children, setupBadge(s.icon(), s.tint))
	}
	text := []core.Node{core.Text{Content: s.label, Class: "text-[15px] " + setupTitle}}
	if s.description != "" {
		text = append(text, core.Text{Content: s.description, Class: "text-xs leading-snug " + setupMuted})
	}
	return append(children, core.View{Class: "min-w-0 flex-1 gap-0.5", Children: text})
}

func (s phoneSetting) row(value core.Node) core.Node {
	return core.View{Class: setupRow + " py-2.5", Children: append(s.parts(), value)}
}

func (s phoneSetting) toggle(cx *core.Scope, id string, checked func() bool, change func(bool)) core.Node {
	track := core.View{Class: "h-[31px] w-[51px] shrink-0 flex-row items-center rounded-full px-0.5",
		ClassFunc: func() string {
			if checked() {
				return "justify-end bg-violet-500"
			}
			return "justify-start bg-zinc-900/15 dark:bg-zinc-50/15"
		}, Children: []core.Node{
			core.View{Class: "size-[27px] rounded-full bg-white shadow-md"},
		}}
	control := core.View{Class: setupRow + " py-2.5", Label: s.label, Hint: s.description,
		TestID: id, Role: core.RoleSwitch, Checked: checked, Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
		Children: append(s.parts(), track)}
	if change == nil {
		control.Disabled = func() bool { return true }
		control.Hint = locales.SettingsUnavailable(cx)
		if core.Untracked(cx, checked) {
			control.Hint = locales.SettingsAlwaysOn(cx)
		}
	} else {
		control.OnPress = func(core.PressEvent) { change(!core.Untracked(cx, checked)) }
	}
	return control
}

func (s phoneSetting) unavailable(cx *core.Scope) core.Node {
	return s.row(phoneSettingValue(locales.SettingsUnavailable(cx)))
}

func phoneSettingValue(value string) core.Node {
	return core.View{Class: "max-w-[55%] shrink-0 flex-row items-center gap-3", Children: []core.Node{
		core.Text{Content: value, Class: "shrink text-right text-[15px] " + setupMuted},
		core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 opacity-60 " + setupMuted},
	}}
}

func (s phoneSetting) link(cx *core.Scope, id, value string, action func()) core.Node {
	return base.Button{Label: s.label, TestID: id, Class: setupRow + " py-2.5",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { action() },
		Children: append(s.parts(), phoneSettingValue(value))}
}

func phoneSettingChoice[T comparable](cx *core.Scope, setting phoneSetting, id string,
	options []ui.Option[T], value func() T, change func(T)) core.Node {
	return settingPicker(cx, id, setting.label, options, value, change, setting.parts())
}

func settingsFailure(cx *core.Scope, failure *core.Signal[error]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if err := failure.Get(); err != nil {
			return core.View{Class: "px-4", Children: []core.Node{notice(errorText(cx, err))}}
		}
		return nil
	})
}
