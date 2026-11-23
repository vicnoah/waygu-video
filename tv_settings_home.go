package main

import (
	"slices"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/locales"
)

func (a *application) tvHomeLayoutOptions(cx *core.Scope, report func(error)) func() []tvOption {
	setShelf := func(id string, enabled bool) error {
		shelves := a.appearancePrefs.GetUntracked().tvSourceShelves(a.sources.GetUntracked())
		index := slices.IndexFunc(shelves, func(shelf homeModule) bool { return shelf.ID == id })
		if index < 0 {
			return nil
		}
		shelves[index].Enabled = enabled
		return a.saveAppearance(func(p *appearancePreferences) { p.TVSourceShelves = shelves })
	}
	moveShelf := func(id string, delta int) error {
		shelves := a.appearancePrefs.GetUntracked().tvSourceShelves(a.sources.GetUntracked())
		next, moved := reorderHomeModules(shelves, id, delta)
		if !moved {
			return nil
		}
		return a.saveAppearance(func(p *appearancePreferences) { p.TVSourceShelves = next })
	}
	return func() []tvOption {
		prefs := a.appearancePrefs.Get().forHome(true)
		sources := a.sources.Get()
		rows := make([]tvOption, 0, len(prefs.Home)+len(sources))
		for _, module := range prefs.Home {
			rows = append(rows, tvOption{
				id: "tv-layout-module-" + module.ID, label: homeModuleLabel(cx, module.ID),
				checked: func() bool { return module.Enabled }, reorder: true,
				action: func() { report(a.saveHomeModule(module.ID, !module.Enabled)) },
				adjust: func(delta int) { report(a.moveHomeModule(module.ID, delta)) },
			})
		}
		names := make(map[string]string, len(sources))
		for _, entry := range sources {
			names[entry.ID] = entry.Name
		}
		for _, shelf := range prefs.tvSourceShelves(sources) {
			rows = append(rows, tvOption{
				id: "tv-layout-source-" + shelf.ID, label: locales.TvFromSource(cx, names[shelf.ID]),
				checked: func() bool { return shelf.Enabled }, reorder: len(sources) > 1,
				action: func() { report(setShelf(shelf.ID, !shelf.Enabled)) },
				adjust: func(delta int) { report(moveShelf(shelf.ID, delta)) },
			})
		}
		return rows
	}
}
