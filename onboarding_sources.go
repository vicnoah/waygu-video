package main

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

const (
	setupTitle  = "text-zinc-900 dark:text-zinc-50"
	setupMuted  = "text-zinc-500 dark:text-zinc-400"
	setupAccent = "text-waygu-control dark:text-waygu-highlight"
	setupBorder = "border border-zinc-900/8 dark:border-white/8"
	setupRow    = "min-h-12 items-center flex-row gap-3 px-4 bg-white dark:bg-[#1a1b25]"
)

type sourceProvider struct {
	provider media.Provider
	icon     func() *core.SVG
	badge    string
	result   string
	tint     string
}

var sourceProviders = [...]sourceProvider{
	{media.Emby, lucide.Server, "bg-emerald-500", "bg-emerald-500/15", "text-emerald-500"},
	{media.Jellyfin, lucide.Server, "bg-sky-500", "bg-sky-500/15", "text-sky-500"},
	{media.Flymoo, lucide.HardDrive, "bg-blue-500", "bg-blue-500/15", "text-blue-500"},
	{media.Plex, lucide.Server, "bg-amber-500", "bg-amber-500/15", "text-amber-500"},
}

func (a *application) chooseSource(cx *core.Scope) core.Node {
	open := func(server media.DiscoveredServer) {
		a.push(route{screen: screenConnect, provider: server.Provider,
			source: source{Address: server.Address, Name: server.Name}})
	}
	choices := make([]core.Node, 0, len(sourceProviders))
	for _, entry := range sourceProviders {
		label := providerName(cx, entry.provider)
		value := ""
		if entry.provider == media.Flymoo {
			value = "fnOS"
		}
		children := []core.Node{
			setupBadge(entry.icon(), entry.badge),
			core.Text{Content: label, Class: "flex-1 text-[15px] " + setupTitle},
		}
		if value != "" {
			children = append(children, core.Text{Content: value, Class: "text-[15px] " + setupMuted})
		}
		children = append(children, core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 text-zinc-500/60 dark:text-zinc-400/60"})
		choices = append(choices, base.Button{Label: label, TestID: "choose-" + string(entry.provider),
			Class: setupRow + " py-2.5", Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
			OnPress: func(core.PressEvent) { open(media.DiscoveredServer{Provider: entry.provider}) }, Children: children})
	}
	return setupPage(cx, "", a.back, core.View{Class: "w-full max-w-[640px] self-center pb-10", Children: []core.Node{
		core.View{Class: "gap-1 px-5 pt-1 pb-5", Children: []core.Node{
			core.Text{Content: locales.AddSource(cx), Class: "text-[28px] font-bold tracking-tight " + setupTitle},
			core.Text{Content: locales.ChooseSourceDetail(cx), Class: "text-sm " + setupMuted},
		}},
		core.View{Class: "gap-6", Children: []core.Node{
			discoveryPanel(cx, open),
			setupGroup(locales.ChooseSource(cx), "", choices...),
		}},
	}})
}

func discoveryPanel(cx *core.Scope, open func(media.DiscoveredServer)) core.Node {
	servers := core.NewSignalFunc[[]media.DiscoveredServer](cx, nil, nil)
	scanning := core.NewSignal(cx, true)
	viewContext := cx.Context()
	search := func() {
		scanning.Set(true)
		servers.Set(nil)
		go func() {
			ctx, cancel := context.WithTimeout(viewContext, 2*time.Second)
			defer cancel()
			media.DiscoverLocal(ctx, func(server media.DiscoveredServer) {
				cx.Post(func() {
					if viewContext.Err() == nil {
						servers.Set(append(slices.Clone(servers.GetUntracked()), server))
					}
				})
			})
			cx.Post(func() {
				if viewContext.Err() == nil {
					scanning.Set(false)
				}
			})
		}()
	}
	search()
	return core.View{TestID: "lan-discovery", Class: "mx-4 gap-3 rounded-2xl border border-violet-500/30 bg-violet-500/10 p-4", Children: []core.Node{
		core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
			core.Icon{SVG: lucide.Radar(), Class: "size-4 " + setupAccent},
			core.Text{Content: locales.LocalDiscovery(cx), Class: "flex-1 text-sm font-semibold " + setupTitle},
			core.Show{When: scanning.Get, Then: func(cx *core.Scope) core.Node {
				return core.Text{Content: locales.Discovering(cx), Class: "text-xs " + setupAccent}
			}, Else: func(cx *core.Scope) core.Node {
				return base.Button{Label: locales.DiscoverAgain(cx), TestID: "scan-again", Class: "rounded",
					Style: core.Style{FocusVisible: ui.FocusRing(cx, 3)}, OnPress: func(core.PressEvent) { search() },
					Children: []core.Node{core.Text{Content: locales.DiscoverAgain(cx), Class: "text-xs " + setupAccent}}}
			}},
		}},
		core.Each(servers.Get, func(server media.DiscoveredServer) string { return string(server.Provider) + "|" + server.Address },
			func(cx *core.Scope, item func() media.DiscoveredServer, _ func() int) core.Node {
				server := core.Untracked(cx, item)
				index := slices.IndexFunc(sourceProviders[:], func(entry sourceProvider) bool { return entry.provider == server.Provider })
				if index < 0 {
					return nil
				}
				entry := sourceProviders[index]
				provider := providerName(cx, server.Provider)
				name := server.Name
				if name == "" {
					name = provider
				}
				address := strings.TrimPrefix(strings.TrimPrefix(server.Address, "http://"), "https://")
				if server.Provider == media.Flymoo {
					address = strings.TrimSuffix(address, "/v")
				}
				return core.View{Class: "flex-row items-center gap-3 rounded-xl bg-white p-3 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder, Children: []core.Node{
					core.View{Class: "size-9 items-center justify-center rounded-lg " + entry.result, Children: []core.Node{
						core.Icon{SVG: entry.icon(), Class: "size-4 " + entry.tint},
					}},
					core.View{Class: "flex-1", Children: []core.Node{
						core.Text{Content: name, Class: "text-sm font-medium " + setupTitle, MaxLines: 1},
						core.Text{Content: provider + " · " + address, Class: "text-xs " + setupMuted},
					}},
					base.Button{Label: locales.AddDiscoveredSource(cx, name), TestID: "local-" + string(server.Provider),
						Class: "rounded-full bg-violet-500 px-3.5 py-1.5 shadow-md shadow-violet-500/30",
						Style: core.Style{FocusVisible: ui.FocusRing(cx, 3)}, OnPress: func(core.PressEvent) { open(server) },
						Children: []core.Node{core.Text{Content: locales.AddDiscovered(cx), Class: "text-xs font-semibold text-white"}}},
				}}
			}),
		core.Show{When: func() bool { return !scanning.Get() && len(servers.Get()) == 0 }, Then: func(cx *core.Scope) core.Node {
			return core.Text{Content: locales.NoLocalServers(cx), Class: "text-xs " + setupMuted}
		}},
	}}
}

func setupPage(cx *core.Scope, title string, back func(), body core.Node) core.Node {
	label := locales.Back(cx)
	header := []core.Node{
		base.Button{Label: label, TestID: "back", Class: "h-11 flex-row items-center gap-0.5 rounded",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { back() },
			Children: []core.Node{
				core.Icon{SVG: lucide.ChevronLeft(), Class: "size-6 " + setupAccent},
				core.Text{Content: label, Class: "text-base " + setupAccent},
			}},
	}
	if title != "" {
		header = append(header, core.Text{Content: title, Class: "absolute inset-x-0 text-center text-[17px] font-semibold " + setupTitle,
			PointerEvents: core.PointerEventsNone})
	}
	return core.View{Class: "relative flex-1 bg-waygu-page", Style: core.Style{FlexShrink: 1,
		PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Bottom: core.EnvKeyboard}}, Children: []core.Node{
		core.SVGView{Class: "absolute inset-x-0 top-0 h-[1100px] w-full dark:hidden", SVG: welcomeAmbientLight},
		core.SVGView{Class: "absolute inset-x-0 top-0 hidden h-[1100px] w-full dark:flex", SVG: welcomeAmbientDark},
		core.View{Class: "relative h-11 flex-row items-center px-3", Children: header},
		core.Scroll{Class: "flex-1", Children: []core.Node{body}},
	}}
}

func setupGroup(title, detail string, rows ...core.Node) core.Node {
	var children []core.Node
	if title != "" {
		children = append(children, core.Text{Content: title, Class: "px-4 text-xs font-medium uppercase tracking-wide " + setupMuted})
	}
	children = append(children, core.View{Class: "gap-px overflow-hidden rounded-2xl bg-zinc-200 shadow-sm shadow-black/5 dark:bg-white/7 " + setupBorder,
		Children: rows})
	if detail != "" {
		children = append(children, core.Text{Content: detail, Class: "px-4 text-xs leading-relaxed " + setupMuted})
	}
	return core.View{Class: "gap-2 px-4", Children: children}
}

func setupBadge(icon *core.SVG, fill string) core.Node {
	return core.View{Class: "size-7 items-center justify-center rounded-lg shadow-sm " + fill, Children: []core.Node{
		core.Icon{SVG: icon, Class: "size-4 text-white"},
	}}
}
