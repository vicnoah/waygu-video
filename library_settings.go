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

type libraryCategory string

const (
	libraryMovies        libraryCategory = "movies"
	librarySeries        libraryCategory = "series"
	libraryAnime         libraryCategory = "anime"
	libraryDocumentaries libraryCategory = "documentaries"
)

type sourceLibrary struct {
	media.Library
	Category     libraryCategory `json:",omitempty"`
	Hidden       bool            `json:",omitempty"`
	HideFromHome bool            `json:",omitempty"`
}

func (library sourceLibrary) category() libraryCategory {
	switch library.Category {
	case libraryMovies, librarySeries, libraryAnime, libraryDocumentaries:
		return library.Category
	}
	switch strings.ToLower(strings.TrimSpace(library.Kind)) {
	case "movie", "movies", "film":
		return libraryMovies
	case "show", "series", "tv", "tvshow", "tvshows":
		return librarySeries
	case "anime", "animation":
		return libraryAnime
	case "documentary", "documentaries":
		return libraryDocumentaries
	default:
		return ""
	}
}

func (library sourceLibrary) matchesCategory(category libraryCategory, item media.Item) bool {
	if category == "" {
		return true
	}
	if configured := library.category(); configured != "" {
		return configured == category
	}
	switch item.Kind {
	case media.Movie, media.Video:
		return category == libraryMovies
	case media.Series, media.Season, media.Episode:
		return category == librarySeries
	default:
		return false
	}
}

// mergeLibraries keeps the saved order and preferences for libraries that
// the server still returns, followed by any newly available libraries.
func mergeLibraries(available []media.Library, saved []sourceLibrary, hideNew bool) []sourceLibrary {
	remaining := make(map[string]media.Library, len(available))
	for _, library := range available {
		if library.ID != "" {
			remaining[library.ID] = library
		}
	}
	merged := make([]sourceLibrary, 0, len(remaining))
	for _, library := range saved {
		if current, ok := remaining[library.ID]; ok {
			library.Library = current
			merged = append(merged, library)
			delete(remaining, library.ID)
		}
	}
	for _, library := range available {
		if current, ok := remaining[library.ID]; ok {
			merged = append(merged, sourceLibrary{Library: current, Hidden: hideNew})
			delete(remaining, library.ID)
		}
	}
	return merged
}

func libraryCategories(cx *core.Scope, defaultLabel string) []ui.Option[libraryCategory] {
	return []ui.Option[libraryCategory]{
		{Label: defaultLabel},
		{Label: locales.LibraryMovies(cx), Value: libraryMovies},
		{Label: locales.LibrarySeries(cx), Value: librarySeries},
		{Label: locales.LibraryAnime(cx), Value: libraryAnime},
		{Label: locales.LibraryDocumentaries(cx), Value: libraryDocumentaries},
	}
}

func libraryEditor(cx *core.Scope, draft *core.Signal[[]sourceLibrary], importing bool, busy func() bool) core.Node {
	options := libraryCategories(cx, locales.LibraryAutomaticCategory(cx))
	return core.Each(draft.Get, func(library sourceLibrary) string { return library.ID },
		func(cx *core.Scope, item func() sourceLibrary, index func() int) core.Node {
			id := core.Untracked(cx, item).ID
			name := func() string {
				library := item()
				if library.Name != "" {
					return library.Name
				}
				return library.ID
			}
			edit := func(change func(*sourceLibrary)) {
				libraries := slices.Clone(draft.GetUntracked())
				if at := slices.IndexFunc(libraries, func(library sourceLibrary) bool { return library.ID == id }); at >= 0 {
					change(&libraries[at])
					draft.Set(libraries)
				}
			}
			visible := ui.Checkbox{
				TextFunc: name, TestID: "import-library-" + id, Disabled: busy,
				Checked:  func() bool { return !item().Hidden },
				OnChange: func(show bool) { edit(func(library *sourceLibrary) { library.Hidden = !show }) },
			}
			title := []core.Node{visible}
			if importing {
				title = []core.Node{base.Checkbox{
					LabelFunc: name, TestID: visible.TestID, Disabled: busy, Checked: visible.Checked, OnChange: visible.OnChange,
					Class: "min-h-12 flex-row-reverse items-center gap-3 px-4", Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
					Indicator: core.Dynamic(func(*core.Scope) core.Node {
						if visible.Checked() {
							return core.Icon{SVG: lucide.CircleCheck(), Class: "size-5 " + setupAccent}
						}
						return core.Icon{SVG: lucide.Circle(), Class: "size-5 text-zinc-500/60 dark:text-zinc-400/60"}
					}),
					Children: []core.Node{core.View{Class: "flex-1 flex-row items-center gap-3", Children: []core.Node{
						core.Dynamic(func(*core.Scope) core.Node {
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
						}),
						core.Text{ContentFunc: name, Class: "flex-1 text-[15px] " + setupTitle},
					}}},
				}}
			} else {
				visible.TextFunc = nil
				visible.Text = locales.ShowLibrary(cx)
				visible.LabelFunc = func() string { return locales.ShowNamedLibrary(cx, name()) }
				visible.TestID = "library-visible-" + id
				move := func(offset int) {
					libraries := slices.Clone(draft.GetUntracked())
					at := slices.IndexFunc(libraries, func(library sourceLibrary) bool { return library.ID == id })
					to := at + offset
					if at >= 0 && to >= 0 && to < len(libraries) {
						libraries[at], libraries[to] = libraries[to], libraries[at]
						draft.Set(libraries)
					}
				}
				title = []core.Node{
					core.View{Class: "flex-row flex-wrap items-center justify-between gap-2", Children: []core.Node{
						core.Text{ContentFunc: name, Class: "text-base font-semibold flex-1"},
						ui.IconButton{LabelFunc: func() string { return locales.MoveLibraryUp(cx, name()) }, Icon: lucide.ArrowUp, Size: 44,
							TestID: "library-up-" + id, Disabled: func() bool { return busy() || index() == 0 }, OnPress: func() { move(-1) }},
						ui.IconButton{LabelFunc: func() string { return locales.MoveLibraryDown(cx, name()) }, Icon: lucide.ArrowDown, Size: 44,
							TestID: "library-down-" + id, Disabled: func() bool { return busy() || index() == len(draft.Get())-1 }, OnPress: func() { move(1) }},
					}},
					visible,
				}
			}
			title = append(title, core.Show{When: func() bool { return !item().Hidden }, Then: func(cx *core.Scope) core.Node {
				class := "flex-row flex-wrap items-center gap-3"
				if importing {
					class += " px-4 pb-2.5"
				}
				return core.View{Class: class, Children: []core.Node{
					ui.Select[libraryCategory]{Options: options, Label: locales.LibraryCategory(cx), TestID: "library-category-" + id,
						Disabled: busy, Value: func() libraryCategory { return item().Category }, OnChange: func(value libraryCategory) {
							edit(func(library *sourceLibrary) { library.Category = value })
						}},
					ui.Checkbox{Text: locales.ShowLibraryOnHome(cx), TestID: "library-home-" + id, Disabled: busy,
						LabelFunc: func() string { return locales.ShowNamedLibraryOnHome(cx, name()) },
						Checked:   func() bool { return !item().HideFromHome },
						OnChange:  func(show bool) { edit(func(library *sourceLibrary) { library.HideFromHome = !show }) }},
				}}
			}})
			if importing {
				return core.View{Class: "bg-white dark:bg-[#1a1b25]", Children: title}
			}
			return core.View{Class: "gap-2 rounded-xl border border-border p-3", Children: title}
		})
}

func (a *application) manageLibraries(cx *core.Scope, current source) core.Node {
	draft := core.NewSignalFunc(cx, current.Libraries, nil)
	refreshed := false
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal(cx, "")
	viewContext := cx.Context()
	refresh := func() {
		if busy.GetUntracked() {
			return
		}
		busy.Set(true)
		failure.Set("")
		timeout := time.Duration(a.sourceTimeout.GetUntracked()) * time.Second
		go func() {
			ctx, cancel := context.WithTimeout(viewContext, timeout)
			defer cancel()
			client, err := a.client(ctx, current)
			var libraries []media.Library
			if err == nil {
				libraries, err = client.Libraries(ctx)
			}
			cx.Post(func() {
				busy.Set(false)
				if err != nil {
					failure.Set(errorText(cx, err))
					return
				}
				refreshed = true
				draft.Set(mergeLibraries(libraries, draft.GetUntracked(), true))
			})
		}()
	}
	save := func() {
		if busy.GetUntracked() {
			return
		}
		sources := slices.Clone(a.sources.GetUntracked())
		index := slices.IndexFunc(sources, func(entry source) bool { return entry.ID == current.ID })
		if index < 0 {
			a.back()
			return
		}
		sources[index].Libraries = draft.GetUntracked()
		if refreshed {
			sources[index].LibrariesSyncedAt = time.Now().Unix()
		}
		if err := a.saveSources(sources); err != nil {
			failure.Set(locales.LibrarySaveError(cx))
			return
		}
		a.back()
	}
	return pageFrame(pageHeader(cx, locales.ManageLibraries(cx), a.back), pageBody(
		core.View{Class: "gap-4 rounded-3xl border border-border bg-card p-5", Children: []core.Node{
			heading(current.Name),
			core.Show{When: func() bool { return len(draft.Get()) == 0 }, Then: func(cx *core.Scope) core.Node {
				return hint(locales.NoLibraries(cx))
			}},
			libraryEditor(cx, draft, false, busy.Get),
			core.Dynamic(func(cx *core.Scope) core.Node {
				if message := failure.Get(); message != "" {
					return notice(message)
				}
				return nil
			}),
			core.View{Class: "flex-row flex-wrap gap-3", Children: []core.Node{
				ui.Button{Text: locales.RefreshLibraries(cx), Icon: lucide.RefreshCw, Variant: ui.ButtonOutline,
					Size: ui.SizeLarge, TestID: "refresh-libraries", Busy: busy.Get, OnPress: refresh},
				ui.Button{Text: locales.SaveLibraries(cx), Size: ui.SizeLarge, TestID: "save-libraries", Disabled: busy.Get, OnPress: save},
			}},
		}},
	))
}
