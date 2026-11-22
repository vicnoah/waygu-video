package main

import (
	"context"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type detailResult struct {
	item        media.Item
	children    []media.Item
	hasVersions bool
	failure     error
}

func (a *application) detail(cx *core.Scope, current route) core.Node {
	loaded := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) (detailResult, error) {
		result := detailResult{item: current.item}
		client, err := a.client(current.source)
		if err != nil {
			result.failure = err
			return result, nil
		}
		_, result.hasVersions = client.(media.VersionClient)
		item, err := client.Detail(ctx, current.item)
		if err == nil {
			result.item = item
		} else {
			result.failure = err
		}
		switch current.item.Kind {
		case media.Series, media.Season, media.Folder:
			result.children, err = client.Children(ctx, current.item)
			if err != nil {
				result.failure = err
			}
		}
		return result, nil
	})
	versionID := core.NewSignal(cx, "")
	versions := a.versionPicker(cx, current, versionID)
	return pageFrame(pageHeader(cx, locales.Details(cx), a.back), pageBody(core.Dynamic(func(cx *core.Scope) core.Node {
		result, ready := loaded.Get()
		if !ready {
			result.item = current.item
		}
		item := result.item
		art := item.Backdrop
		if art == "" {
			art = item.Poster
		}
		var title []core.Node
		if art != "" {
			title = append(title, core.Image{Source: core.Src(art), Class: "absolute inset-0", ResizeMode: core.Cover, ElementsHidden: true},
				core.View{Class: "absolute inset-0 bg-linear-to-t from-black to-black/10"})
		}
		title = append(title, core.View{Class: "gap-2 p-6", Children: []core.Node{
			core.Text{Content: current.source.Name, Class: "text-sm font-medium text-violet-200"},
			core.Text{Content: item.Title, Class: "text-3xl font-bold text-white"},
			core.Text{Content: itemMetadata(cx, item), Class: "text-sm text-zinc-200"},
		}})
		rows := []core.Node{core.View{Class: "relative h-60 justify-end overflow-hidden rounded-3xl bg-zinc-900 md:h-80", Children: title}}
		if playable(item) {
			label := locales.Play(cx)
			if item.Position > 0 {
				label = locales.Resume(cx)
			}
			start := func(resume bool) {
				a.push(route{screen: screenPlayer, source: current.source, item: item, resume: resume, versionID: versionID.GetUntracked()})
			}
			actions := []core.Node{ui.Button{Text: label, Icon: lucide.Play, Size: ui.SizeLarge, TestID: "detail-play",
				OnPress: func() { start(true) }}}
			if item.Position > 0 {
				actions = append(actions, ui.Button{Text: locales.FromStart(cx), Variant: ui.ButtonOutline, Size: ui.SizeLarge,
					OnPress: func() { start(false) }})
			}
			rows = append(rows, core.View{Class: "flex-row flex-wrap gap-3", Children: actions})
			rows = append(rows, versions)
		}
		if item.Overview != "" {
			rows = append(rows, core.Text{Content: item.Overview, Class: "text-base leading-relaxed"})
		} else if ready && playable(item) {
			rows = append(rows, hint(locales.NoOverview(cx)))
		}
		if len(item.Genres) > 0 {
			rows = append(rows, hint(strings.Join(item.Genres, " · ")))
		}
		if loaded.Loading() {
			rows = append(rows, hint(locales.Loading(cx)))
		}
		if result.failure != nil {
			rows = append(rows, notice(errorText(cx, result.failure)), ui.Button{Text: locales.Retry(cx),
				Variant: ui.ButtonOutline, OnPress: loaded.Refetch})
		}
		if len(result.children) > 0 {
			section := locales.Episodes(cx)
			if item.Kind == media.Series {
				section = locales.Seasons(cx)
			}
			rows = append(rows, heading(section))
			for _, child := range result.children {
				label := child.Title
				if child.Kind == media.Episode && child.Episode > 0 {
					label = locales.EpisodeLabel(cx, strconv.Itoa(child.Episode)) + " · " + label
				}
				if child.Kind == media.Season && label == "" {
					label = locales.SeasonLabel(cx, strconv.Itoa(child.Season))
				}
				open := ui.Button{Text: label, Icon: lucide.ChevronRight, Size: ui.SizeLarge,
					Variant: ui.ButtonOutline, TestID: "child-" + child.ID, OnPress: func() {
						target := screenDetail
						if playable(child) {
							target = screenPlayer
						}
						a.push(route{screen: target, source: current.source, item: child, resume: true})
					}}
				if result.hasVersions && child.Kind == media.Episode {
					rows = append(rows, core.View{Class: "flex-row flex-wrap items-center gap-3", Children: []core.Node{
						open, ui.Button{Label: locales.EpisodeDetails(cx, label), Icon: lucide.Info,
							Size: ui.SizeLarge, Variant: ui.ButtonOutline, TestID: "child-details-" + child.ID,
							OnPress: func() { a.push(route{screen: screenDetail, source: current.source, item: child}) }},
					}})
				} else {
					rows = append(rows, open)
				}
			}
		}
		return core.View{Class: "gap-5", Children: rows}
	})))
}
