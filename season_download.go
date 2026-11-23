package main

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

const seasonDownloadID = "season-download"

func (a *application) seasonDownload(cx *core.Scope, current route, seasons func() []media.Item, view func() seriesViewState) (func(), core.Node) {
	queue := a.downloads
	if !queue.available() || (current.item.Kind != media.Series && current.item.Kind != media.Season) {
		return nil, nil
	}
	if current.item.Kind == media.Season {
		season := current.item
		seasons = func() []media.Item { return []media.Item{season} }
		current.item = media.Item{ID: season.SeriesID, Title: season.SeriesTitle, LibraryID: season.LibraryID, ExternalIDs: season.SeriesExternalIDs}
	}
	options := func(cx *core.Scope) []ui.Option[string] {
		var choices []ui.Option[string]
		for _, season := range seasons() {
			if season.Kind != media.Season || season.ID == "" {
				continue
			}
			label := locales.SeriesSpecials(cx)
			if season.Season > 0 {
				label = locales.SeasonLabel(cx, strconv.Itoa(season.Season))
			}
			choices = append(choices, ui.Option[string]{Value: season.ID, Label: label})
		}
		return choices
	}
	open := core.NewSignal(cx, false)
	tab := core.NewSignal(cx, "")
	height := core.NewSignal(cx, 0)
	omitted := core.NewSignalFunc[map[string]bool](cx, nil, nil)
	failed := core.NewSignal(cx, false)
	type request struct {
		tab     string
		seasons []media.Item
	}
	loaded := core.NewResource(cx, func() (request, bool) {
		return request{tab.Get(), seasons()}, open.Get() && tab.Get() != ""
	}, func(ctx context.Context, request request, _ core.ResourceInfo) (seriesEpisodePage, error) {
		return a.loadSeriesEpisodes(ctx, current, request.tab, request.seasons), nil
	})
	items := core.NewMemoFunc(cx, func() []media.Item {
		page, ready := loaded.Get()
		if !open.Get() || !ready || loaded.Loading() || page.tab != tab.Get() {
			return nil
		}
		policy := a.parentalRules()
		return slices.DeleteFunc(slices.Clone(page.items), func(episode media.Item) bool {
			return episode.Kind != media.Episode || !policy.visible(current.source.ID, episode)
		})
	}, nil)
	queued := core.NewMemoFunc(cx, func() map[string]downloadState {
		states := make(map[string]downloadState)
		account := mediaAccountKey(current.source)
		for _, entry := range queue.entries.Get() {
			if entry.Account == account {
				states[entry.Item.ID] = entry.State
			}
		}
		return states
	}, maps.Equal)
	eligible := func(episode media.Item) bool { return queued.Get()[episode.ID] == "" }
	selected := core.NewMemoFunc(cx, func() []route {
		var rows []route
		skipped := omitted.Get()
		for _, item := range items.Get() {
			if eligible(item) && !skipped[item.ID] {
				rows = append(rows, route{source: current.source, item: item})
			}
		}
		return rows
	}, nil).Get
	selectSeason := func(value string) {
		cx.Batch(func() { tab.Set(value); omitted.Set(nil); failed.Set(false) })
	}
	selectCount := func() int { return len(selected()) }
	eligibleCount := core.NewMemo(cx, func() int {
		count := 0
		for _, item := range items.Get() {
			if eligible(item) {
				count++
			}
		}
		return count
	}).Get
	seasonSelect := core.Dynamic(func(cx *core.Scope) core.Node {
		choices := options(cx)
		if len(choices) == 1 {
			return hint(choices[0].Label)
		}
		return downloadPicker(cx, seasonDownloadID+"-season", locales.Seasons(cx), choices, tab.Get, selectSeason)
	})
	episodes := core.Dynamic(func(cx *core.Scope) core.Node {
		page, ready := loaded.Get()
		if !ready || loaded.Loading() || page.tab != tab.Get() {
			return hint(locales.Loading(cx))
		}
		shown := items.Get()
		if len(shown) == 0 && page.failure == nil {
			return hint(locales.SeriesNoEpisodes(cx))
		}
		rows := make([]core.Node, 0, len(shown))
		for _, episode := range shown {
			label := episode.Title
			if episode.Episode > 0 {
				label = locales.EpisodeLabel(cx, strconv.Itoa(episode.Episode)) + " · " + label
			}
			rows = append(rows, ui.Checkbox{Text: label, TestID: seasonDownloadID + "-episode-" + episode.ID,
				Checked:  func() bool { return eligible(episode) && !omitted.Get()[episode.ID] },
				Disabled: func() bool { return !eligible(episode) },
				OnChange: func(checked bool) {
					next := make(map[string]bool)
					maps.Copy(next, omitted.GetUntracked())
					next[episode.ID] = !checked
					omitted.Set(next)
				},
				Children: []core.Node{core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
					core.Text{Content: label, Class: "text-sm text-foreground", MaxLines: 2},
					core.Dynamic(func(cx *core.Scope) core.Node {
						if status := queued.Get()[episode.ID]; status != "" {
							entry, _ := a.seriesDownload(current.source, episode, queue.entries.Get())
							return hint(queue.stateLabel(cx, entry))
						}
						return nil
					}),
				}}},
			})
		}
		if page.failure != nil {
			rows = append(rows, notice(errorText(cx, page.failure)), ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: loaded.Refetch})
		}
		return core.View{Class: "gap-2", Children: []core.Node{
			core.View{Class: "flex-row items-center justify-between gap-2", Children: []core.Node{
				ui.Checkbox{Text: locales.DownloadSelectAll(cx), TestID: seasonDownloadID + "-all",
					Disabled:      func() bool { return eligibleCount() == 0 },
					Checked:       func() bool { return eligibleCount() > 0 && selectCount() == eligibleCount() },
					Indeterminate: func() bool { return selectCount() > 0 && selectCount() < eligibleCount() },
					OnChange: func(checked bool) {
						next := make(map[string]bool)
						if !checked {
							for _, item := range items.GetUntracked() {
								next[item.ID] = true
							}
						}
						omitted.Set(next)
					}},
				core.Text{TestID: seasonDownloadID + "-count", ContentFunc: func() string {
					return locales.DownloadEpisodeSelection(cx, strconv.Itoa(selectCount()))
				}, Class: "text-xs text-muted-foreground"},
			}},
			core.Scroll{Class: "max-h-[220px]", TestID: seasonDownloadID + "-episodes",
				Children: []core.Node{core.View{Class: "gap-2 pr-2", Children: rows}}},
		}}
	})
	dialog := queue.selectionDialog(cx, downloadDialog{id: seasonDownloadID, title: locales.DownloadSeason(cx),
		open: open, height: height, transcodes: func() bool { return canDownloadTranscodes(current.source.Provider) }, selection: selected,
		children: []core.Node{seasonSelect, episodes, core.Show{When: failed.Get, Then: func(cx *core.Scope) core.Node {
			return notice(locales.DownloadSaveError(cx))
		}}},
		confirm: func(waitForWiFi bool) {
			if queue.enqueueBatch(core.Untracked(cx, selected), height.GetUntracked(), waitForWiFi) {
				open.Set(false)
				next := route{screen: screenDownloads}
				if view != nil {
					a.pushFromSeries(view(), next)
				} else {
					a.push(next)
				}
				return
			}
			failed.Set(true)
		},
	})
	launch := func() {
		choices := core.Untracked(cx, func() []ui.Option[string] { return options(cx) })
		if len(choices) == 0 {
			return
		}
		choice := choices[0].Value
		if view != nil {
			want := view().tab
			if want == seriesSpecials {
				for _, season := range core.Untracked(cx, seasons) {
					if season.Kind == media.Season && season.Season == 0 {
						want = season.ID
						break
					}
				}
			}
			if slices.ContainsFunc(choices, func(option ui.Option[string]) bool { return option.Value == want }) {
				choice = want
			}
		} else {
			for _, season := range core.Untracked(cx, seasons) {
				if season.Kind == media.Season && season.Season > 0 && season.ID != "" {
					choice = season.ID
					break
				}
			}
		}
		value := 0
		if canDownloadTranscodes(current.source.Provider) {
			value = queue.height.GetUntracked()
		}
		cx.Batch(func() { selectSeason(choice); height.Set(value); open.Set(true) })
	}
	return launch, dialog
}

func attachSeriesParent(parent, child media.Item) media.Item {
	if parent.Kind == media.Series && (child.Kind == media.Season || child.Kind == media.Episode) {
		return inheritSeriesEpisode(child, parent)
	}
	if parent.Kind == media.Season && child.Kind == media.Episode {
		child.Season = cmp.Or(child.Season, parent.Season)
		child.SeriesID = cmp.Or(child.SeriesID, parent.SeriesID)
		child.SeriesTitle = cmp.Or(child.SeriesTitle, parent.SeriesTitle)
	}
	child.LibraryID = cmp.Or(child.LibraryID, parent.LibraryID)
	return child
}
