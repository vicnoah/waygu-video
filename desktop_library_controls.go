package main

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

const desktopLibrarySurface = "border border-zinc-900/8 bg-white dark:border-white/8 dark:bg-[#1a1b25]"
const desktopFilterShape = "h-8 rounded-full px-3"

func desktopLibraryAction(cx *core.Scope, id, label string, icon *core.SVG, active bool, class, tint string,
	iconAfterLabel bool, disabled func() bool, action func()) core.Node {
	textClass, iconClass, gap := "text-[13px]", "size-3.5", "gap-1.5"
	if id == "desktop-library-shuffle" {
		textClass, iconClass, gap = "text-sm", "size-4", "gap-2"
	} else if iconAfterLabel {
		gap = "gap-1"
	}
	children := []core.Node{core.Text{Content: label, Class: textClass + " " + tint, MaxLines: 1}}
	if icon != nil {
		glyph := core.Icon{SVG: icon, Class: iconClass + " shrink-0 " + tint}
		if iconAfterLabel {
			children = append(children, glyph)
		} else {
			children = append([]core.Node{glyph}, children...)
		}
	}
	return base.Button{Label: label, TestID: id, Class: "shrink-0 flex-row items-center justify-center disabled:opacity-55 " + gap + " " + class,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, Selected: func() bool { return active }, Disabled: disabled,
		OnPress: func(core.PressEvent) { action() }, Children: children}
}

func desktopLibraryMenu[T comparable](cx *core.Scope, id, label, display, class string, icon *core.SVG,
	options []ui.Option[T], value T, change func(T)) core.Node {
	textClass, chevron, gap := "text-[13px] text-zinc-900/80 dark:text-zinc-50/80", "size-3.5", "gap-1"
	if icon != nil {
		textClass, chevron, gap = "text-sm "+setupTitle, "size-4", "gap-2"
	}
	children := []core.Node{core.Text{Content: display, Class: textClass, MaxLines: 1}}
	if icon != nil {
		children = append([]core.Node{core.Icon{SVG: icon, Class: "size-4 " + setupMuted}}, children...)
	}
	children = append(children, core.Icon{SVG: lucide.ChevronDown(), Class: chevron + " " + setupMuted})
	items := make([]core.Node, 0, len(options))
	for i, option := range options {
		items = append(items, ui.MenuItem{Text: option.Label, TestID: id + "-" + strconv.Itoa(i),
			Checked: func() bool { return value == option.Value }, OnSelect: func() { change(option.Value) }})
	}
	return base.Menu{Label: label, TestID: id, Class: "shrink-0 flex-row items-center " + class + " " + desktopLibrarySurface,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, PopupClass: tabletDetailPopup, Children: items,
		Trigger: core.View{Class: "flex-row items-center " + gap, Children: children}}
}

func desktopLibrarySortMenu(cx *core.Scope, current librarySort, change func(librarySort)) core.Node {
	options := []ui.Option[librarySort]{
		{Value: libraryRecent, Label: locales.Recent(cx)},
		{Value: libraryTitle, Label: locales.LibrarySortTitle(cx)},
		{Value: libraryYear, Label: locales.DesktopLibraryReleaseDate(cx)},
		{Value: libraryRating, Label: locales.LibrarySortRating(cx)},
		{Value: libraryDuration, Label: locales.DesktopLibraryDuration(cx)},
		{Value: libraryFileSize, Label: locales.DesktopLibraryFileSize(cx)},
		{Value: libraryLastPlayed, Label: locales.DesktopLibraryLastPlayed(cx)},
	}
	return desktopLibraryMenu(cx, "desktop-library-sort", locales.LibrarySort(cx), libraryFilterValue(options[0].Label, options, current),
		"h-9 rounded-lg px-3 shadow-sm shadow-black/5", lucide.ArrowDownWideNarrow(), options, current, change)
}

func desktopLibraryModePicker(cx *core.Scope, current func() desktopLibraryMode, change func(desktopLibraryMode)) core.Node {
	rows := []core.Node{}
	for _, mode := range []struct {
		id    desktopLibraryMode
		label string
		icon  *core.SVG
	}{
		{desktopGrid, locales.DesktopGridView(cx), lucide.LayoutGrid()},
		{desktopList, locales.LibraryListView(cx), lucide.List()},
		{desktopWall, locales.DesktopPosterWall(cx), lucide.GalleryHorizontal()},
	} {
		rows = append(rows, base.Button{Label: mode.label, TestID: "desktop-library-mode-" + string(mode.id),
			Selected: func() bool { return current() == mode.id }, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
			Class:   "group w-9 items-center justify-center rounded-md selected:bg-white selected:shadow-sm dark:selected:bg-[#1a1b25]",
			OnPress: func(core.PressEvent) { change(mode.id) }, Children: []core.Node{
				core.Icon{SVG: mode.icon, Class: "size-4 " + setupMuted + " group-selected:text-zinc-900 dark:group-selected:text-white"},
			}})
	}
	return core.View{Class: "h-9 shrink-0 flex-row rounded-lg border border-zinc-900/8 bg-zinc-900/5 p-1 dark:border-white/8 dark:bg-white/5",
		TestID: "desktop-library-modes", Children: rows}
}

func desktopLibraryFilterBar(cx *core.Scope, readFilters func() desktopLibraryFilters, readFacets func() libraryFacets, readSources func() []source,
	change func(func(*desktopLibraryFilters)), sourceChanged func(string), clear, open func()) core.Node {
	toggle := func(id, label string, active bool, icon *core.SVG, action func()) core.Node {
		class, tint := desktopLibrarySurface, "text-zinc-900/80 dark:text-zinc-50/80"
		if active {
			class, tint = "bg-violet-500/15", libraryFilterTint(true)
		}
		return desktopLibraryAction(cx, id, label, icon, active, desktopFilterShape+" "+class, tint, true, nil, action)
	}
	metadata := core.Dynamic(func(cx *core.Scope) core.Node {
		filters, facets := readFilters(), readFacets()
		genres := []ui.Option[string]{{Label: locales.LibraryAnyGenre(cx)}}
		for _, genre := range facets.genres {
			genres = append(genres, ui.Option[string]{Label: genre, Value: genre})
		}
		years := []ui.Option[int]{{Label: locales.LibraryAnyYear(cx)}}
		for _, year := range facets.years {
			years = append(years, ui.Option[int]{Label: strconv.Itoa(year), Value: year})
		}
		resolutions := []ui.Option[int]{{Label: locales.LibraryAnyResolution(cx)}}
		for _, resolution := range facets.resolutions {
			resolutions = append(resolutions, ui.Option[int]{Label: libraryResolutionName(resolution), Value: resolution})
		}
		filterLabel, filterStyle := locales.DesktopFilterLabel(cx), desktopLibrarySurface
		count := filters.count()
		if count > 0 {
			filterLabel, filterStyle = locales.DesktopFilterCount(cx, strconv.Itoa(count)), "border border-violet-500/50 bg-violet-500/15"
		}
		var resolution core.Node
		if filters.Resolution == 0 {
			resolution = desktopLibraryMenu(cx, "desktop-filter-resolution", locales.LibraryResolution(cx), locales.LibraryResolution(cx),
				desktopFilterShape, nil, resolutions, filters.Resolution, func(value int) { change(func(f *desktopLibraryFilters) { f.Resolution = value }) })
		} else {
			resolution = toggle("desktop-filter-remove-resolution", locales.DesktopResolutionValue(cx, libraryResolutionName(filters.Resolution)),
				true, lucide.X(), func() { change(func(f *desktopLibraryFilters) { f.Resolution = 0 }) })
		}
		hdr := (*core.SVG)(nil)
		if filters.HDR {
			hdr = lucide.X()
		}
		return core.Group([]core.Node{
			desktopLibraryAction(cx, "desktop-library-filters-open", filterLabel, lucide.Funnel(), count > 0,
				desktopFilterShape+" "+filterStyle, "font-medium "+libraryFilterTint(count > 0), false, nil, open),
			desktopLibraryMenu(cx, "desktop-filter-genre", locales.LibraryGenre(cx), libraryFilterValue(locales.LibraryGenre(cx), genres, filters.Genre),
				desktopFilterShape, nil, genres, filters.Genre, func(value string) { change(func(f *desktopLibraryFilters) { f.Genre = value }) }),
			desktopLibraryMenu(cx, "desktop-filter-year", locales.LibraryYear(cx), libraryFilterValue(locales.LibraryYear(cx), years, filters.Year),
				desktopFilterShape, nil, years, filters.Year, func(value int) { change(func(f *desktopLibraryFilters) { f.Year = value }) }),
			resolution,
			toggle("desktop-filter-hdr", locales.DesktopHDR(cx), filters.HDR, hdr, func() { change(func(f *desktopLibraryFilters) { f.HDR = !f.HDR }) }),
		})
	})
	// Loading more catalog metadata must leave an open source menu in place.
	sourceMenu := core.Dynamic(func(cx *core.Scope) core.Node {
		selected := readFilters().Source
		sourceOptions := []ui.Option[string]{{Label: locales.AllSources(cx)}}
		for _, source := range readSources() {
			sourceOptions = append(sourceOptions, ui.Option[string]{Label: source.Name, Value: source.ID})
		}
		return desktopLibraryMenu(cx, "desktop-filter-source", locales.MediaSources(cx), libraryFilterValue(locales.DesktopLibrarySource(cx), sourceOptions, selected),
			desktopFilterShape, nil, sourceOptions, selected, sourceChanged)
	})
	personal := core.Dynamic(func(cx *core.Scope) core.Node {
		filters := readFilters()
		count := filters.count()
		return core.Group([]core.Node{
			toggle("desktop-filter-unwatched", locales.LibraryUnwatched(cx), filters.Unwatched, nil, func() { change(func(f *desktopLibraryFilters) { f.Unwatched = !f.Unwatched }) }),
			toggle("desktop-filter-favorite", locales.Favorites(cx), filters.Favorite, nil, func() { change(func(f *desktopLibraryFilters) { f.Favorite = !f.Favorite }) }),
			core.View{Class: "flex-1"},
			desktopLibraryAction(cx, "desktop-library-clear", locales.DesktopClearAll(cx), nil, false, "h-8", setupMuted, false, func() bool { return count == 0 }, clear),
		})
	})
	return core.View{Class: "flex-row flex-wrap items-center gap-2", TestID: "desktop-library-filters",
		Children: []core.Node{metadata, sourceMenu, personal}}
}
