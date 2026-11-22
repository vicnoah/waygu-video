package main

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"waygu-video/locales"
)

func (a *application) desktopShell(cx *core.Scope, detail func(*core.Scope) core.Node) core.Node {
	category := a.desktopCategory
	collapsed := a.desktopCollapsed
	afterSelect := func() {
		if detail != nil {
			a.routes.Set(a.routes.GetUntracked()[:1])
		}
	}
	openSearch := func() {
		cx.Batch(func() {
			a.sourceFilter.Set("")
			a.tab.Set(2)
			afterSelect()
		})
	}
	cx.OnKey("mod+,", func() { a.openDesktopSettings(cx, desktopSettingsOverview) })
	cx.OnKeyWith("mod+k", core.KeyOptions{Title: func() string { return locales.Search(cx) }}, openSearch)
	return core.View{Class: "relative flex-1 bg-waygu-page", TestID: "desktop-shell", Children: []core.Node{
		homeAmbient(false, true), homeAmbient(true, true),
		core.View{Class: "flex-1 flex-row", Children: []core.Node{
			core.Show{When: func() bool { return !collapsed.Get() }, Then: func(cx *core.Scope) core.Node {
				return a.tabletSidebar(cx, category, func() { collapsed.Set(true) }, afterSelect)
			}},
			core.View{Class: "min-w-0 flex-1", Children: []core.Node{
				core.Show{When: collapsed.Get, Then: func(cx *core.Scope) core.Node {
					return core.View{Class: "flex-row items-center px-8 pt-2", Children: []core.Node{
						tabletPanelButton(cx, locales.OpenSidebar(cx), "desktop-sidebar-open", lucide.PanelLeft(), func() { collapsed.Set(false) }),
					}}
				}},
				core.Dynamic(func(cx *core.Scope) core.Node {
					if detail != nil {
						return detail(cx)
					}
					switch a.tab.Get() {
					case 1:
						page := a.libraryView.Get()
						if page == screenLibrary {
							return a.desktopLibrary(cx, category.Get, collapsed.Get)
						}
						return a.browse(cx, page, core.IdiomDesktop, nil)
					case 2:
						return a.browse(cx, screenSearch, core.IdiomDesktop, nil)
					case 3:
						return a.desktopSettings(cx, func() { a.tab.Set(0) })
					default:
						return a.browse(cx, screenHome, core.IdiomDesktop, category)
					}
				}),
			}},
		}},
	}}
}
