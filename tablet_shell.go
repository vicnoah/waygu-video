package main

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) tabletShell(cx *core.Scope) core.Node {
	category := core.NewSignal(cx, libraryCategory(""))
	collapsed := core.NewSignal(cx, false)
	drawer := core.NewSignal(cx, false)
	portrait := core.NewMemo(cx, func() bool {
		window := cx.Env().Window()
		return window.Width < window.Height
	})
	inline := func() bool { return !portrait.Get() && !collapsed.Get() }
	cx.Effect(func() {
		if !portrait.Get() {
			drawer.Set(false)
		}
	})
	cx.OnKeyWith("mod+k", core.KeyOptions{OverModals: true, Title: func() string { return locales.Search(cx) }}, func() {
		cx.Batch(func() {
			drawer.Set(false)
			a.sourceFilter.Set("")
			a.tab.Set(2)
		})
	})
	closeDrawer := func() { drawer.Set(false) }
	openPanel := func() {
		if portrait.GetUntracked() {
			drawer.Set(true)
		} else {
			collapsed.Set(false)
		}
	}
	return core.View{Class: "relative flex-1 bg-[#f4f4f7] dark:bg-[#0f1017]", Children: []core.Node{
		homeAmbient(false, true), homeAmbient(true, true),
		core.View{Class: "flex-1 flex-row", Style: core.Style{
			PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Left: core.EnvSafeArea, Right: core.EnvSafeArea, Bottom: core.EnvSafeArea},
		}, Children: []core.Node{
			core.Show{When: inline, Then: func(cx *core.Scope) core.Node {
				return a.tabletSidebar(cx, category, func() { collapsed.Set(true) }, func() {})
			}},
			core.View{Class: "min-w-0 flex-1", Children: []core.Node{
				core.Show{When: func() bool { return !inline() }, Then: func(cx *core.Scope) core.Node {
					return core.View{Class: "flex-row items-center gap-2.5 px-6 pt-2", Children: []core.Node{
						tabletPanelButton(cx, locales.OpenSidebar(cx), "tablet-sidebar-open", lucide.PanelLeft(), openPanel),
						tabletBrand(cx),
					}}
				}},
				core.Dynamic(func(cx *core.Scope) core.Node {
					switch a.tab.Get() {
					case 1:
						view := a.libraryView.Get()
						if view != screenLibrary {
							return a.browse(cx, view, core.IdiomTablet, nil)
						}
						return a.browse(cx, screenLibrary, core.IdiomTablet, category)
					case 2:
						return a.browse(cx, screenSearch, core.IdiomTablet, nil)
					case 3:
						return a.settings(cx)
					default:
						return a.browse(cx, screenHome, core.IdiomTablet, nil)
					}
				}),
			}},
			core.Show{When: func() bool { return !inline() }, Then: func(cx *core.Scope) core.Node {
				cx.DeferSystemGestures(core.SystemGestureOptions{IOSEdges: core.GestureEdgeLeft})
				return core.View{Class: "absolute bottom-0 left-0 top-12 w-3", TestID: "tablet-drawer-edge",
					PanActivation: homeHorizontalPan, OnPan: func(event core.PanEvent) {
						if event.Phase == core.GestureEnded && event.Translation.X >= 44 {
							openPanel()
						}
					},
				}
			}},
		}},
		core.Show{When: func() bool { return portrait.Get() && drawer.Get() }, Then: func(cx *core.Scope) core.Node {
			return core.Modal{Class: "flex-1 flex-row", TestID: "tablet-drawer", Label: locales.Navigation(cx),
				OnRequestClose: func(string) { closeDrawer() }, Style: core.Style{
					PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Left: core.EnvSafeArea, Bottom: core.EnvSafeArea},
				}, Children: []core.Node{
					core.View{Class: "w-56 shrink-0 flex-row bg-[#f4f4f7] dark:bg-[#0f1017]", PanActivation: homeHorizontalPan,
						OnPan: func(event core.PanEvent) {
							if event.Phase == core.GestureEnded && event.Translation.X <= -44 {
								closeDrawer()
							}
						},
						Children: []core.Node{a.tabletSidebar(cx, category, closeDrawer, closeDrawer)},
					},
					base.Button{Class: "flex-1 bg-black/40", Label: locales.CloseSidebar(cx), TestID: "tablet-drawer-backdrop",
						OnPress: func(core.PressEvent) { closeDrawer() }},
				}}
		}},
	}}
}

func tabletBrand(cx *core.Scope) core.Node {
	return core.View{Class: "flex-row items-center gap-2 px-2", Children: []core.Node{
		core.Image{Class: "size-7", Source: core.ImageSource{Data: welcomeMark}, ResizeMode: core.Contain, ElementsHidden: true},
		core.Text{Content: locales.Brand(cx), Class: "text-lg font-bold tracking-tight " + setupTitle},
	}}
}

func tabletPanelButton(cx *core.Scope, label, id string, icon *core.SVG, press func()) core.Node {
	return base.Button{Label: label, TestID: id, Class: "size-7 items-center justify-center rounded-lg",
		HitSlop: core.All(6), Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { press() },
		Children: []core.Node{core.Icon{SVG: icon, Class: "size-4 " + setupMuted}}}
}

func (a *application) tabletSidebar(cx *core.Scope, category *core.Signal[libraryCategory], closePanel, afterSelect func()) core.Node {
	open := func(tab int, kind libraryCategory) {
		cx.Batch(func() {
			a.sourceFilter.Set("")
			category.Set(kind)
			if tab == 1 {
				a.libraryView.Set(screenLibrary)
			}
			a.tab.Set(tab)
			afterSelect()
		})
	}
	header := core.View{Class: "flex-row items-center justify-between pr-1", Children: []core.Node{
		tabletBrand(cx), tabletPanelButton(cx, locales.CloseSidebar(cx), "tablet-sidebar-close", lucide.PanelLeftClose(), closePanel),
	}}
	search := base.Button{Label: locales.Search(cx), TestID: "tab-search",
		Class: "h-9 flex-row items-center gap-2 rounded-lg bg-white px-3 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { open(2, "") },
		Children: []core.Node{
			core.Icon{SVG: lucide.Search(), Class: "size-4 " + setupMuted},
			core.Text{Content: locales.SearchHint(cx), Class: "min-w-0 flex-1 text-sm " + setupMuted, MaxLines: 1},
			core.Text{Content: cx.KeyLabel("mod+k"), Class: "text-xs opacity-70 " + setupMuted, MaxLines: 1},
		},
	}
	browsing := []core.Node{tabletSidebarHeading(locales.Browse(cx)),
		tabletSidebarLink(cx, "tab-home", locales.Home(cx), lucide.House, func() bool {
			return a.tab.Get() == 0 && a.sourceFilter.Get() == ""
		}, func() { open(0, "") }),
	}
	icons := []func() *core.SVG{lucide.Film, lucide.Tv, lucide.Sparkles, lucide.Globe}
	for i, option := range libraryCategories(cx, "")[1:] {
		browsing = append(browsing, tabletSidebarLink(cx, "tablet-library-"+string(option.Value), option.Label, icons[i],
			func() bool {
				return a.tab.Get() == 1 && a.libraryView.Get() == screenLibrary && category.Get() == option.Value
			}, func() { open(1, option.Value) }))
	}
	return core.View{Class: "w-56 shrink-0 border-r border-zinc-900/8 bg-[#e9e9f0]/40 dark:border-white/8 dark:bg-[#1a1b25]/40", TestID: "tablet-sidebar",
		Children: []core.Node{
			core.Scroll{Class: "flex-1", Children: []core.Node{
				core.View{Class: "gap-5 px-3 pt-3", Children: []core.Node{
					header, search, core.View{Class: "gap-0.5", Children: browsing},
					core.View{Class: "gap-0.5", Children: []core.Node{
						tabletSidebarHeading(locales.MyLibrary(cx)),
						tabletSidebarLink(cx, "tab-favorites", locales.Favorites(cx), lucide.Heart,
							func() bool { return a.tab.Get() == 1 && a.libraryView.Get() == screenFavorites },
							func() { a.openPersonalLibrary(screenFavorites); afterSelect() }),
						tabletSidebarLink(cx, "tab-history", locales.WatchHistory(cx), lucide.Clock,
							func() bool { return a.tab.Get() == 1 && a.libraryView.Get() == screenHistory },
							func() { a.openPersonalLibrary(screenHistory); afterSelect() }),
					}},
					a.tabletSidebarSources(cx, afterSelect),
				}},
			}},
			core.View{Class: "mx-3 gap-0.5 border-t border-zinc-900/8 pb-4 pt-3 dark:border-white/8", Children: []core.Node{
				tabletSidebarLink(cx, "tab-settings", locales.Settings(cx), lucide.Settings, func() bool { return a.tab.Get() == 3 },
					func() { a.tab.Set(3); afterSelect() }),
				a.tabletSidebarProfile(cx, func() { a.tab.Set(3); afterSelect() }),
			}},
		},
	}
}

func tabletSidebarHeading(label string) core.Node {
	return core.Text{Content: label, Class: "px-3 pb-1 text-[11px] font-medium " + setupMuted}
}

func tabletSidebarLink(cx *core.Scope, id, label string, icon func() *core.SVG, selected func() bool, press func()) core.Node {
	return base.Button{Label: label, TestID: id, Selected: selected, Class: tabletSidebarRow,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { press() },
		Children: []core.Node{
			core.Dynamic(func(*core.Scope) core.Node {
				tint := setupMuted
				if selected() {
					tint = setupAccent
				}
				return core.Icon{SVG: icon(), Class: "size-4 " + tint}
			}),
			core.Text{Content: label, Class: "text-sm " + setupTitle, ClassFunc: func() string {
				if selected() {
					return "font-medium"
				}
				return "opacity-80"
			}},
		},
	}
}

const tabletSidebarRow = "h-9 flex-row items-center gap-3 rounded-lg px-3 selected:bg-violet-500/12"

func (a *application) tabletSidebarSources(cx *core.Scope, afterSelect func()) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		sources := a.sources.Get()
		if len(sources) == 0 {
			return nil
		}
		rows := []core.Node{tabletSidebarHeading(locales.MediaSources(cx))}
		for _, source := range sources {
			name := providerName(cx, source.Provider)
			if source.Provider == media.Flymoo {
				name = "fnOS"
			}
			var tint string
			for _, provider := range sourceProviders {
				if provider.provider == source.Provider {
					tint = provider.result + " " + provider.tint
					break
				}
			}
			rows = append(rows, base.Button{Label: source.Name, Hint: name, TestID: "tablet-source-" + source.ID, Class: tabletSidebarRow,
				Selected: func() bool { return a.tab.Get() == 0 && a.sourceFilter.Get() == source.ID },
				Style:    core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) {
					cx.Batch(func() {
						a.sourceFilter.Set(source.ID)
						a.tab.Set(0)
						afterSelect()
					})
				}, Children: []core.Node{
					core.View{Class: "size-2 shrink-0 rounded-full", ClassFunc: func() string {
						status := a.sourceStatus.Get()[source.ID]
						if status.failed {
							return "bg-amber-400"
						}
						if status.connected {
							return "bg-emerald-400"
						}
						return "bg-zinc-400"
					}},
					core.Text{Content: source.Name, Class: "min-w-0 flex-1 text-sm " + setupTitle, MaxLines: 1},
					core.Text{Content: name, Class: "shrink-0 rounded bg-zinc-500/15 px-1.5 py-0.5 text-[10px] " + tint, MaxLines: 1},
				}})
		}
		return core.View{Class: "gap-0.5", Children: rows}
	})
}

func (a *application) tabletSidebarProfile(cx *core.Scope, press func()) core.Node {
	user := func() string {
		for _, source := range a.sources.Get() {
			if name := strings.TrimSpace(source.Username); name != "" {
				return name
			}
		}
		return ""
	}
	return base.Button{Label: locales.Settings(cx), TestID: "tablet-profile", Class: "h-11 flex-row items-center gap-3 rounded-lg px-3",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { press() }, Children: []core.Node{
			core.View{Class: "size-7 items-center justify-center rounded-full bg-linear-to-br from-violet-500 to-fuchsia-500", Children: []core.Node{
				core.Dynamic(func(*core.Scope) core.Node {
					if initial, _ := utf8.DecodeRuneInString(user()); initial != utf8.RuneError {
						return core.Text{Content: string(unicode.ToUpper(initial)), Class: "text-xs font-semibold text-white"}
					}
					return core.Icon{SVG: lucide.UserRound(), Class: "size-4 text-white"}
				}),
			}},
			core.View{Class: "min-w-0 flex-1", Children: []core.Node{
				core.Text{Class: "text-sm font-medium " + setupTitle, MaxLines: 1, ContentFunc: func() string {
					if name := user(); name != "" {
						return name
					}
					return locales.AppName(cx)
				}},
				core.Text{Class: "text-[11px] " + setupMuted, MaxLines: 1, ContentFunc: func() string {
					statuses := a.sourceStatus.Get()
					online := 0
					for _, source := range a.sources.Get() {
						if statuses[source.ID].connected {
							online++
						}
					}
					return locales.SourcesOnline(cx, strconv.Itoa(online))
				}},
			}},
		},
	}
}
