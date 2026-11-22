package main

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

var phoneTabInset = core.NewKey[func() core.Points]("waygu-phone-tab-inset", nil)

func tabBarInset(cx *core.Scope) core.Points {
	if phone := phoneTabInset.Use(cx); phone != nil {
		return phone()
	}
	return ui.TabBarInset(cx)
}

func (a *application) phoneShell(cx *core.Scope, definitions []ui.ShellTab) core.Node {
	env := cx.Env()
	insets := core.InsetsFunc(cx)
	phoneTabInset.Provide(cx, func() core.Points {
		if env.KeyboardHeight() > 0 {
			return 0
		}
		return 72 + max(28, insets().Bottom)
	})
	tabs := make([]core.Node, 0, len(definitions))
	pages := make([]core.Node, 0, len(definitions))
	for index, tab := range definitions {
		selected := func() bool { return a.tab.Get() == index }
		seen := false
		pages = append(pages, core.View{Class: "flex-1", StyleFunc: func() core.Style {
			if !selected() {
				return core.Style{Display: core.DisplayNone}
			}
			return core.Style{}
		}, Children: []core.Node{core.Show{
			When: func() bool { seen = seen || selected(); return seen },
			Then: tab.Content,
		}}})
		tabs = append(tabs, base.Tab{Label: tab.Text, TestID: tab.TestID, Selected: selected,
			Class: "group h-12 flex-1 items-center justify-center gap-0.5 rounded-full selected:bg-violet-500/12",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { a.tab.Set(index) },
			Children: []core.Node{
				core.Icon{SVG: tab.Icon(), Class: "size-[22px] " + phoneTabTint},
				core.Text{Content: tab.Text, Class: "text-[10px] font-medium " + phoneTabTint, MaxLines: 1},
			}})
	}
	return core.View{Class: "relative flex-1", Children: []core.Node{
		core.View{Class: "flex-1", Children: pages},
		core.Show{When: func() bool { return env.KeyboardHeight() == 0 }, Then: func(*core.Scope) core.Node {
			return core.View{Class: "absolute inset-x-0 bottom-0 px-5 pt-2", PointerEvents: core.PointerEventsBoxNone,
				StyleFunc: func() core.Style { return core.Style{Padding: core.Edges{Bottom: max(28, insets().Bottom)}} },
				Children: []core.Node{base.Tabs{TestID: "tab-bar", Class: "h-16 flex-row items-center rounded-full border border-zinc-900/8 bg-white/90 px-2 shadow-xl shadow-black/10 backdrop-blur-md dark:border-white/8 dark:bg-[#1a1b25]/90",
					Children: tabs}},
			}
		}},
	}}
}

const phoneTabTint = "text-zinc-500 group-selected:text-violet-500 dark:text-zinc-400 dark:group-selected:text-violet-400"
