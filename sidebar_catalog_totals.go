package main

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"waygu-video/locales"
)

func (a *application) sidebarCatalogTotals(cx *core.Scope) *core.Resource[map[libraryCategory]int] {
	type query struct {
		catalogRequest
		combine bool
	}
	source := core.NewMemo(cx, a.desktopDefaultSource)
	return core.NewResource(cx, func() (query, bool) {
		var filter string
		switch appLayoutIdiom(cx) {
		case core.IdiomDesktop:
			filter = source.Get()
		case core.IdiomTablet:
		default:
			return query{}, false
		}
		sources := a.sources.Get()
		a.libraryChanges.Get()
		return query{
			catalogRequest: catalogRequest{sources: sources, parental: a.parentalRules(),
				filter: filter, scan: true, categorize: true},
			combine: a.mergeTitles.Get(),
		}, len(sources) != 0
	}, func(ctx context.Context, q query, _ core.ResourceInfo) (map[libraryCategory]int, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		result := a.loadCatalog(ctx, q.catalogRequest)
		if result.failure != nil {
			return nil, result.failure
		}
		byCategory := make(map[libraryCategory][]catalogItem)
		for _, entry := range result.items {
			byCategory[entry.category] = append(byCategory[entry.category], entry)
		}
		totals := make(map[libraryCategory]int)
		for category, entries := range byCategory {
			if q.combine && q.filter == "" && len(q.sources) > 1 {
				entries = mergeCatalogItems(entries, false, category)
			}
			totals[category] = len(entries)
		}
		return totals, nil
	})
}

func appendSidebarCategoryCount(cx *core.Scope, link *base.Button, totals *core.Resource[map[libraryCategory]int], category libraryCategory, testID string) {
	count := func() string {
		if totals.State() != core.ResourceReady {
			return ""
		}
		values, _ := totals.Get()
		return message.NewPrinter(language.Make(cx.Env().Locale())).Sprint(values[category])
	}
	link.HintFunc = func() string {
		if value := count(); value != "" {
			return locales.HomeItemCount(cx, value)
		}
		return ""
	}
	link.Children = append(link.Children, core.Show{When: func() bool { return totals.State() == core.ResourceReady },
		Then: func(*core.Scope) core.Node {
			return core.View{Class: "shrink-0", AccessibilityElementsHidden: func() bool { return true },
				Children: []core.Node{core.Text{ContentFunc: count, Class: "text-xs opacity-70 " + setupMuted,
					TestID: testID}}}
		},
	})
}
