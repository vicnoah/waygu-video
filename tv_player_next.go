package main

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"waygu-video/locales"
)

type tvNextFocus uint8

const (
	tvNextUnfocused tvNextFocus = iota
	tvNextPlay
	tvNextDismiss
)

func tvNextEpisodeCard(cx *core.Scope, l tvLayout, next nextEpisodeControls, focused *core.Signal[tvNextFocus],
	buttons *[2]core.Ref, dismiss func(), finished func() bool) core.Node {
	status := l.text("", 20, "font-medium text-violet-300")
	status.ContentFunc, status.TestID = next.status, "tv-next-episode-status"
	title := l.text("", 30, "font-semibold text-white")
	title.ContentFunc, title.MaxLines, title.TestID = next.title, 2, "tv-next-episode-title"
	action := func(target tvNextFocus, id, label string, icon *core.SVG, press func()) core.View {
		button := l.action(id, label, icon, tvDetailAction, press)
		button.Class = "group flex-row items-center justify-center rounded-full border border-white/20 bg-white/10 selected:border-transparent selected:bg-white"
		button.Style.Height, button.Style.Padding = l.length(56), l.padding(18, 0)
		button.Style.Selected = l.focusRing()
		button.Children = []core.Node{l.icon(icon, 22, "text-zinc-300 group-selected:text-black"),
			l.text(label, 22, "font-medium text-white group-selected:text-black")}
		button.Ref = &buttons[target-1]
		button.Selected = func() bool { return focused.Get() == target }
		button.OnFocus = func() { focused.Set(target) }
		return button
	}
	return core.View{Class: "absolute rounded-2xl border border-white/15 bg-zinc-900/95 shadow-2xl shadow-black/60",
		TestID: "tv-next-episode", Role: core.RoleGroup, Label: locales.NextEpisode(cx),
		Style: core.Style{Width: l.length(500), Right: l.length(96), Bottom: l.length(210), Padding: l.padding(24, 20), Gap: l.points(12)},
		Children: []core.Node{status, title, core.View{Class: "flex-row flex-wrap", Style: core.Style{Gap: l.points(12)}, Children: []core.Node{
			action(tvNextPlay, "play-next-episode", locales.Play(cx), lucide.SkipForward(), next.play),
			action(tvNextDismiss, "dismiss-next-episode", locales.Dismiss(cx), lucide.X(), dismiss),
		}}, core.Show{When: func() bool { return !finished() && focused.Get() == tvNextUnfocused }, Then: func(cx *core.Scope) core.Node {
			return l.text(locales.TvNextEpisodeHint(cx), 18, "text-zinc-400")
		}}},
	}
}
