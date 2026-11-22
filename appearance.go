package main

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func wayguTheme() *core.Theme {
	theme := core.DefaultTheme()
	theme.Radius = 12
	for _, config := range []struct {
		palette                                  *core.Palette
		background, surface, text, muted, accent uint32
	}{
		{&theme.Light, 0xf4f4f7, 0xffffff, 0x18181b, 0x646470, 0x7547d8},
		{&theme.Dark, 0x111117, 0x1d1d27, 0xf5f2fa, 0xaba7b5, 0x9a74f5},
	} {
		p := config.palette
		p.Background, p.Surface, p.Text, p.TextMuted = core.RGB(config.background), core.RGB(config.surface), core.RGB(config.text), core.RGB(config.muted)
		p.Accent, p.AccentHover, p.AccentPressed = core.RGB(config.accent), core.RGB(config.accent), core.RGB(config.accent)
		p.Focus, p.Selection, p.Border = p.Accent.Faded(.8), p.Accent.Faded(.22), p.Text.Faded(.13)
		p.SetColor("primary", p.Accent)
		p.SetColor("foreground", p.Text)
		p.SetColor("muted-foreground", p.TextMuted)
		p.SetColor("card", p.Surface)
		p.SetColor("field", p.Surface)
		p.SetColor("accentFill", p.Accent)
	}
	return theme
}

func brand(title string) core.Node {
	return core.View{Class: "flex-row items-center gap-3", Children: []core.Node{
		core.View{Class: "size-9 items-center justify-center rounded-xl bg-linear-to-br from-violet-500 to-fuchsia-500", Children: []core.Node{
			core.Icon{SVG: lucide.Play(), Class: "size-5 text-white"},
		}},
		core.Text{Content: title, Class: "text-xl font-bold tracking-tight"},
	}}
}

func pageHeader(cx *core.Scope, title string, back func()) core.Node {
	return core.View{Class: "flex-row items-center gap-3 px-5 py-3", Children: []core.Node{
		ui.Button{Label: locales.Back(cx), Icon: lucide.ChevronLeft, Variant: ui.ButtonGhost,
			Size: ui.SizeLarge, TestID: "back", OnPress: back},
		core.Text{Content: title, Class: "text-xl font-semibold"},
	}}
}

func pageBody(children ...core.Node) core.Node {
	return core.Scroll{Class: "flex-1", Children: []core.Node{
		core.View{Class: "w-full max-w-[720px] self-center gap-5 px-5 pb-8 pt-3", Children: children},
	}}
}

func pageFrame(children ...core.Node) core.Node {
	return core.View{Class: "flex-1 bg-background", Style: core.Style{FlexShrink: 1,
		PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Bottom: core.EnvKeyboard}}, Children: children}
}

func heading(text string) core.Node {
	return core.Text{Content: text, Class: "text-xl font-bold tracking-tight"}
}

func hint(text string) core.Node {
	return core.Text{Content: text, Class: "text-sm text-muted-foreground"}
}

func notice(text string) core.Node {
	return core.View{Class: "rounded-xl border border-border bg-card px-4 py-3", Children: []core.Node{hint(text)}}
}
