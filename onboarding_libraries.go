package main

import (
	"cmp"
	"context"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) importLibraryCounts(cx *core.Scope, candidate candidateSource) *core.Resource[map[string]int] {
	return core.NewResource(cx, func() (struct{}, bool) {
		return struct{}{}, len(candidate.libraries) > 0
	}, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) (map[string]int, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		address := cmp.Or(candidate.endpoint.address, candidate.source.Address)
		client, err := a.sourceClient(candidate.source, address, candidate.session, false)
		if err != nil {
			return nil, nil
		}
		counter, ok := client.(media.LibraryCounter)
		if !ok {
			return nil, nil
		}
		// Each worker owns its result slots. The resource publishes the counts
		// together and the import form stays usable while requests are in flight.
		type result struct {
			count int
			ok    bool
		}
		found := make([]result, len(candidate.libraries))
		workers := min(3, len(found))
		var group sync.WaitGroup
		for first := range workers {
			group.Go(func() {
				for i := first; i < len(found) && ctx.Err() == nil; i += workers {
					count, err := counter.LibraryItemCount(ctx, candidate.libraries[i].ID)
					found[i] = result{count, err == nil && count >= 0}
				}
			})
		}
		group.Wait()
		counts := make(map[string]int)
		for i, value := range found {
			if value.ok {
				counts[candidate.libraries[i].ID] = value.count
			}
		}
		return counts, nil
	})
}

func importLibraryRows(cx *core.Scope, draft *core.Signal[[]sourceLibrary], counts *core.Resource[map[string]int], busy func() bool) core.Node {
	printer := message.NewPrinter(language.Make(cx.Env().Locale()))
	return core.Each(draft.Get, func(library sourceLibrary) string { return library.ID },
		func(cx *core.Scope, item func() sourceLibrary, _ func() int) core.Node {
			initial := core.Untracked(cx, item)
			id := initial.ID
			name := func() string { return cmp.Or(item().Name, id) }
			edit := func(change func(*sourceLibrary)) { editSourceLibrary(draft, id, change) }
			options := core.NewSignal[*sourceLibrary](cx, nil)
			isTV := appLayoutIdiom(cx) == core.IdiomTV
			trigger := &core.Ref{}
			rowScope := cx
			return core.View{Class: setupRow, Children: []core.Node{
				base.Button{TestID: "import-library-options-" + id, Ref: trigger, Disabled: busy,
					Class:     "w-7 self-stretch shrink-0 items-center justify-center rounded-lg",
					Style:     core.Style{FocusVisible: ui.FocusRing(cx, 2)},
					LabelFunc: func() string { return locales.EditImportLibrary(cx, name()) },
					OnPress: func(core.PressEvent) {
						library := core.Untracked(cx, item)
						options.Set(&library)
					},
					Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
						icon, fill := lucide.Film(), "bg-violet-500"
						switch item().category() {
						case librarySeries:
							icon = lucide.Tv()
						case libraryAnime:
							icon = lucide.Sparkles()
						case libraryDocumentaries:
							icon, fill = lucide.Globe(), "bg-slate-500"
						}
						return setupBadge(icon, fill)
					})}},
				base.Checkbox{TestID: "import-library-" + id, LabelFunc: name, Disabled: busy,
					Class:    "min-h-12 min-w-0 flex-1 flex-row-reverse items-center gap-3",
					Style:    core.Style{FocusVisible: ui.FocusRing(cx, -2)},
					Checked:  func() bool { return !item().Hidden },
					OnChange: func(show bool) { edit(func(lib *sourceLibrary) { lib.Hidden = !show }) },
					Indicator: core.Dynamic(func(*core.Scope) core.Node {
						if !item().Hidden {
							return core.Icon{SVG: lucide.CircleCheck(), Class: "size-5 " + setupAccent}
						}
						return core.Icon{SVG: lucide.Circle(), Class: "size-5 text-zinc-500/60 dark:text-zinc-400/60"}
					}),
					Children: []core.Node{
						core.View{Class: "min-w-0 flex-1 flex-row items-center gap-3", Children: []core.Node{
							core.Text{ContentFunc: name, MaxLines: 1, Class: "min-w-0 flex-1 text-[15px] " + setupTitle},
							core.Show{When: func() bool {
								available, _ := counts.Get()
								_, ok := available[id]
								return ok
							}, Then: func(cx *core.Scope) core.Node {
								return core.Text{TestID: "import-library-count-" + id, Class: "shrink-0 text-[15px] " + setupMuted, ContentFunc: func() string {
									available, _ := counts.Get()
									return locales.ImportLibraryCount(cx, printer.Sprint(available[id]))
								}}
							}},
						}},
					}},
				core.Dynamic(func(cx *core.Scope) core.Node {
					if opening := options.Get(); opening != nil {
						close := func() {
							options.Set(nil)
							if isTV {
								rowScope.Post(trigger.Focus)
							}
						}
						return importLibraryOptions(cx, name(), *opening, isTV, close, func(updated sourceLibrary) {
							edit(func(current *sourceLibrary) {
								current.Category, current.HideFromHome = updated.Category, updated.HideFromHome
							})
						})
					}
					return nil
				}),
			}}
		})
}

func importLibraryOptions(cx *core.Scope, name string, library sourceLibrary, tv bool, close func(), apply func(sourceLibrary)) core.Node {
	draft := core.NewSignal(cx, library)
	save := func() {
		apply(draft.GetUntracked())
		close()
	}
	return ui.Dialog{Title: name, TestID: "import-library-options-dialog", Open: func() bool { return true },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				close()
			}
		},
		Children: []core.Node{sourceLibraryPreferenceControls(cx, library.ID, draft.Get,
			func(update func(*sourceLibrary)) {
				value := draft.GetUntracked()
				update(&value)
				draft.Set(value)
			}, nil)},
		Footer: []core.Node{
			ui.Button{Text: locales.DesktopSettingsCancel(cx), TestID: "import-library-options-cancel", AutoFocus: tv,
				Variant: ui.ButtonGhost, OnPress: close},
			ui.Button{Text: locales.CompleteSource(cx), TestID: "import-library-options-apply", OnPress: save},
		},
	}
}
