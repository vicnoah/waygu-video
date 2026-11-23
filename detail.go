package main

import (
	"context"
	"slices"
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
	canFavorite bool
	canWatch    bool
	failure     error
}

func (a *application) detail(cx *core.Scope, current route) core.Node {
	layout := core.NewMemo(cx, func() core.Idiom { return appLayoutIdiom(cx) })
	downloaded := core.NewMemo(cx, func() bool {
		if current.downloadID == "" {
			return false
		}
		entry, ok := a.downloads.find(current.downloadID, a.downloads.entries.Get())
		return ok && entry.State == downloadComplete
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		shown := current
		if !downloaded.Get() {
			shown.downloadID = ""
		}
		idiom := layout.Get()
		if idiom == core.IdiomDesktop && shown.item.Kind == media.Movie {
			return a.desktopShell(cx, func(cx *core.Scope) core.Node {
				return a.detailLayout(cx, shown, idiom)
			})
		}
		return a.detailLayout(cx, shown, idiom)
	})
}

func (a *application) detailLayout(cx *core.Scope, current route, idiom core.Idiom) core.Node {
	var titleDetail func(*core.Scope, route, func() []ui.Option[copyKey], func(copyKey)) core.Node
	switch current.item.Kind {
	case media.Movie:
		switch idiom {
		case core.IdiomDesktop:
			titleDetail = a.desktopMovieDetail
		case core.IdiomTablet:
			titleDetail = a.tabletMovieDetail
		case core.IdiomPhone:
			titleDetail = a.phoneMovieDetail
		}
	case media.Series:
		if idiom == core.IdiomPhone {
			titleDetail = a.phoneSeriesDetail
		}
	}
	if idiom == core.IdiomTV {
		titleDetail = a.tvMediaDetail
	}
	if current.match == nil {
		if titleDetail != nil {
			return titleDetail(cx, current, nil, nil)
		}
		return pageFrame(pageHeader(cx, locales.Details(cx), a.back), pageBody(a.detailContent(cx, current)))
	}
	anchor := catalogCopy{current.source, current.item}
	sources := a.sources.GetUntracked()
	selected := core.NewSignal(cx, current.seriesView.copy)
	available := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) ([]catalogCopy, error) {
		return a.relatedCopies(ctx, current, sources), nil
	})
	copies := core.NewMemoFunc(cx, func() []catalogCopy {
		loaded, ready := available.Get()
		if !ready {
			loaded = append([]catalogCopy{anchor}, current.match.copies...)
		}
		rules := a.parentalRules()
		if rules.Enabled {
			loaded = slices.DeleteFunc(slices.Clone(loaded), func(copy catalogCopy) bool {
				return copy.key() != anchor.key() && !rules.visible(copy.source.ID, copy.item)
			})
		}
		return prioritizeCopies(loaded, sources)
	}, nil)
	active := core.NewMemoFunc(cx, func() catalogCopy {
		choices := copies.Get()
		if key := selected.Get(); key != (copyKey{}) {
			for _, copy := range choices {
				if copy.key() == key {
					return copy
				}
			}
		}
		if remembered := a.rememberedCopy(current); remembered != "" {
			for _, copy := range choices {
				if encodeCopyKey(copy.key()) == remembered {
					return copy
				}
			}
		}
		return choices[0]
	}, func(a, b catalogCopy) bool { return a.key() == b.key() })
	sourceOptions := core.NewMemoFunc(cx, func() []ui.Option[copyKey] {
		choices := copies.Get()
		if len(choices) < 2 {
			return nil
		}
		names := make(map[string]int)
		for _, copy := range choices {
			names[copy.source.Name]++
		}
		options := make([]ui.Option[copyKey], 0, len(choices))
		for _, copy := range choices {
			label := copy.source.Name
			if names[label] > 1 {
				label += " · " + copy.item.Title
				for _, library := range copy.source.Libraries {
					if library.ID == copy.item.LibraryID {
						label += " · " + library.Name
						break
					}
				}
			}
			options = append(options, ui.Option[copyKey]{Label: label, Value: copy.key()})
		}
		return options
	}, func(a, b []ui.Option[copyKey]) bool {
		return slices.EqualFunc(a, b, func(a, b ui.Option[copyKey]) bool { return a.Label == b.Label && a.Value == b.Value })
	})
	selectedRoute := func() route {
		copy := active.Get()
		chosen := current
		chosen.source, chosen.item = copy.source, copy.item
		return chosen
	}
	if titleDetail != nil {
		return core.Dynamic(func(cx *core.Scope) core.Node {
			return titleDetail(cx, selectedRoute(), sourceOptions.Get, selected.Set)
		})
	}
	picker := core.Dynamic(func(cx *core.Scope) core.Node {
		options := sourceOptions.Get()
		if len(options) == 0 {
			return nil
		}
		label := locales.PlaybackSource(cx)
		return core.View{Class: "flex-row flex-wrap items-center gap-3", Children: []core.Node{
			core.Text{Content: label, Class: "text-sm font-medium"},
			playbackChoice(cx, label, "playback-source", options, func() copyKey { return active.Get().key() }, selected.Set),
		}}
	})
	content := core.Dynamic(func(cx *core.Scope) core.Node {
		return a.detailContent(cx, selectedRoute())
	})
	return pageFrame(pageHeader(cx, locales.Details(cx), a.back), pageBody(picker, content))
}

func (a *application) detailResource(cx *core.Scope, current route) *core.Resource[detailResult] {
	type detailQuery struct {
		local                      pendingProgress
		localOnly                  bool
		rules                      parentalPolicy
		supplement, preferExternal bool
		language                   string
	}
	return core.NewResource(cx, func() (detailQuery, bool) {
		prefs := a.libraryPrefs.Get()
		a.sources.Get()
		language := prefs.Language
		if language == "" {
			language = "en-US"
			if strings.HasPrefix(cx.Env().Locale(), "zh") {
				language = "zh-CN"
			}
		}
		return detailQuery{local: a.localCheckpoint(current.source, current.item), localOnly: !a.progress.sendsFor(current.source.ID, mediaAccountKey(current.source)), rules: a.parentalRules(),
			supplement: prefs.TMDB, preferExternal: prefs.PreferExternal, language: language}, true
	}, func(ctx context.Context, query detailQuery, _ core.ResourceInfo) (detailResult, error) {
		rules := query.rules
		result := detailResult{item: current.item, canWatch: query.localOnly}
		client, err := a.client(ctx, current.source)
		if err != nil {
			result.failure = err
			return result, nil
		}
		_, result.hasVersions = client.(media.VersionClient)
		_, result.canFavorite = client.(media.FavoriteClient)
		_, result.canWatch = client.(media.WatchedClient)
		result.canWatch = result.canWatch || query.localOnly
		item, err := client.Detail(ctx, current.item)
		if err == nil {
			if item.LibraryID == "" {
				item.LibraryID = current.item.LibraryID
			}
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
			for i := range result.children {
				child := &result.children[i]
				if child.LibraryID == "" {
					child.LibraryID = result.item.LibraryID
				}
				if rules.Enabled && child.Kind == media.Season && child.ContentRating == "" {
					child.ContentRating = result.item.ContentRating
					child.Genres = result.item.Genres
				}
			}
			if rules.Enabled {
				result.children = slices.DeleteFunc(result.children, func(item media.Item) bool {
					return !rules.visible(current.source.ID, item)
				})
			}
		}
		if query.local.Revision != "" && !result.item.LastPlayed.After(query.local.Update.At) {
			result.item = applyProgressCheckpoint(result.item, query.local)
		}
		if query.supplement && (!rules.Enabled || rules.visible(current.source.ID, result.item)) &&
			(result.item.Kind == media.Movie || result.item.Kind == media.Series) {
			if token, found, err := a.secrets.Get(tmdbSecretKey); err == nil && found {
				result.item, _ = a.metadata.Enrich(ctx, result.item, token, query.language, query.preferExternal)
			}
		}
		return result, nil
	})
}

func (a *application) detailContent(cx *core.Scope, current route) core.Node {
	loaded := a.detailResource(cx, current)
	personal := a.detailPersonal(cx, current, loaded)
	versionID := core.NewSignal(cx, "")
	versions := a.versionPicker(cx, current, versionID)
	return core.Dynamic(func(cx *core.Scope) core.Node {
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
				a.push(route{screen: screenPlayer, source: current.source, item: item, resume: resume, versionID: versionID.GetUntracked(), match: current.match, downloadID: current.downloadID})
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
		buttons := personal.buttons(cx, result, nil)
		if ready {
			if download := a.downloadAction(cx, current, nil, func() route {
				selected := current
				selected.item, selected.versionID = item, versionID.GetUntracked()
				return selected
			}); download != nil {
				buttons = append(buttons, download)
			}
		}
		if len(buttons) != 0 {
			rows = append(rows, core.View{Class: "flex-row flex-wrap gap-3", Children: buttons})
		}
		rows = append(rows, personal.error)
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
	})
}
