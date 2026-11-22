package main

import (
	"cmp"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type detailTrackChoice struct {
	version, audio, subtitle string
}

type movieDetailState struct {
	loaded        *core.Resource[detailResult]
	personal      detailPersonalActions
	available     *core.Resource[[]media.Version]
	selected      *core.Signal[string]
	tracks        *core.Signal[detailTrackChoice]
	item          func() media.Item
	active        func() media.Version
	play          func(bool)
	chooseVersion func(string)
}

func (a *application) movieDetailState(cx *core.Scope, current route, persistSelection bool) movieDetailState {
	loaded := a.detailResource(cx, current)
	personal := a.detailPersonal(cx, current, loaded)
	available := a.versionResource(cx, current)
	initial, _ := a.automaticPlaybackOptions(current)
	key := versionSelectionKey(current)
	remembered := a.savedVersion(key)
	selected := core.NewSignal(cx, remembered)
	tracks := core.NewSignal(cx, detailTrackChoice{})
	active := func() media.Version {
		versions, _ := available.Get()
		options := initial
		if id := selected.Get(); id != "" {
			options.SourceID = id
		}
		if version, ok := media.PreferredVersion(versions, options); ok {
			return version
		}
		options.SourceID = ""
		v, _ := media.PreferredVersion(versions, options)
		return v
	}
	item := func() media.Item {
		if result, ready := loaded.Get(); ready {
			return result.item
		}
		return current.item
	}
	play := func(resume bool) {
		version := core.Untracked(cx, active)
		choice := tracks.GetUntracked()
		next := route{screen: screenPlayer, source: current.source, item: core.Untracked(cx, item), resume: resume, match: current.match, downloadID: current.downloadID}
		if version.ID != "" {
			if selected.GetUntracked() == version.ID {
				next.versionID = version.ID
			}
			if choice.version == version.ID && (choice.audio != "" || choice.subtitle != "") {
				next.versionID, next.audioTrack, next.subtitleTrack = version.ID, choice.audio, choice.subtitle
			}
		}
		a.push(next)
	}

	return movieDetailState{
		loaded: loaded, personal: personal, available: available, selected: selected, tracks: tracks,
		item: item, active: active, play: play,
		chooseVersion: func(id string) {
			cx.Batch(func() {
				selected.Set(id)
				tracks.Set(detailTrackChoice{})
				if persistSelection {
					_ = a.storage.SetItem(key, id)
				}
			})
		},
	}
}

func (a *application) tabletMovieDetail(cx *core.Scope, current route, copies func() []ui.Option[copyKey], selectCopy func(copyKey)) core.Node {
	state := a.movieDetailState(cx, current, true)
	loaded, personal := state.loaded, state.personal
	available, selected, tracks := state.available, state.selected, state.tracks
	item, active, play := state.item, state.active, state.play
	poster := func(compact bool) core.Node {
		return core.Dynamic(func(cx *core.Scope) core.Node {
			dot := "bg-zinc-400"
			result, ready := loaded.Get()
			if ready {
				dot = "bg-emerald-400"
				if result.failure != nil {
					dot = "bg-amber-400"
				}
			}
			entry := item()
			buttons := personal.buttons(cx, result, detailPosterAction)
			if ready {
				if download := a.downloadAction(cx, current, detailPosterAction, func() route {
					selectedRoute := current
					selectedRoute.item, selectedRoute.versionID = entry, selected.GetUntracked()
					return selectedRoute
				}); download != nil {
					buttons = append(buttons, download)
				}
			}
			return a.tabletDetailPoster(cx, current, entry, compact, dot, copies, selectCopy, play, buttons)
		})
	}
	body := core.Dynamic(func(cx *core.Scope) core.Node {
		entry := item()
		version := active()
		sections := tabletDetailHeader(cx, entry, version)
		versions, _ := available.Get()
		versionOptions := []ui.Option[string]{{Label: locales.AutomaticPlaybackVersion(cx)}}
		for i, candidate := range versions {
			versionOptions = append(versionOptions, ui.Option[string]{Label: versionLabel(cx, candidate, i+1), Value: candidate.ID})
		}
		versionText := locales.Loading(cx)
		if version.ID != "" {
			versionText = versionLabel(cx, version, 1)
			if len(versions) == 1 {
				versionText = cmp.Or(detailResolution(version), versionText)
			}
		} else if !available.Loading() {
			versionText = locales.MetadataUnavailable(cx)
		}
		choice := tracks.Get()
		if choice.version != version.ID {
			choice = detailTrackChoice{version: version.ID}
		}
		sections = append(sections, core.View{Class: "flex-row gap-3", Children: []core.Node{
			tabletDetailChoice(cx, "playback-version", locales.VersionShort(cx), versionText, versionOptions, selected.GetUntracked(), state.chooseVersion),
			tabletDetailTrack(cx, version, choice, "audio", tracks.Set),
			tabletDetailTrack(cx, version, choice, "subtitle", tracks.Set),
		}})
		sections = append(sections, personal.error)
		if err := available.Err(); err != nil {
			sections = append(sections, ui.Button{Text: locales.ReloadPlaybackVersions(cx), Variant: ui.ButtonOutline, OnPress: available.Refetch})
		}
		if result, ready := loaded.Get(); ready && result.failure != nil {
			sections = append(sections, notice(errorText(cx, result.failure)), ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: loaded.Refetch})
		}
		overview := entry.Overview
		if overview == "" {
			overview = locales.NoOverview(cx)
			if loaded.Loading() {
				overview = locales.Loading(cx)
			}
		}
		sections = append(sections, core.View{Class: "gap-2", Children: []core.Node{
			tabletDetailHeading(locales.Synopsis(cx)),
			core.Text{Content: overview, Class: "text-sm leading-relaxed " + setupTitle, MaxLines: 3, TestID: "detail-synopsis"},
		}})
		if len(entry.People) != 0 {
			sections = append(sections, tabletDetailCast(cx, entry.People))
		}
		sections = append(sections, tabletDetailFile(cx, current.source.Name, version))
		return core.View{Class: "min-w-0 gap-5 px-8 pt-6 pb-2", Children: sections}
	})
	compact := core.NewMemo(cx, func() bool {
		size := cx.Env().Window()
		return size.Width < size.Height || size.Width < 900
	})
	return core.View{Class: "relative flex-1 bg-[#f4f4f7] dark:bg-[#0f1017]", Children: []core.Node{
		homeAmbient(false, true), homeAmbient(true, true),
		core.View{Class: "flex-1", Style: core.Style{
			PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Left: core.EnvSafeArea, Right: core.EnvSafeArea, Bottom: core.EnvSafeArea},
		}, Children: []core.Node{
			core.Dynamic(func(*core.Scope) core.Node {
				if compact.Get() {
					return core.Scroll{Class: "flex-1", TestID: "tablet-detail-scroll", Children: []core.Node{
						core.View{Class: "gap-2 p-4 pt-2", Children: []core.Node{poster(true), body}},
					}}
				}
				return core.View{Class: "flex-1 flex-row gap-2 p-4 pt-2", Children: []core.Node{
					poster(false), core.Scroll{Class: "min-w-0 flex-1", TestID: "tablet-detail-scroll", Children: []core.Node{body}},
				}}
			}),
		}},
	}}
}

func (a *application) tabletDetailPoster(cx *core.Scope, current route, item media.Item, compact bool, sourceStatus string,
	copies func() []ui.Option[copyKey], selectCopy func(copyKey), play func(bool), personal []core.Node) core.Node {
	shape := "w-[420px] shrink-0"
	if compact {
		shape = "h-[520px] shrink-0"
	}
	back := base.Button{Class: "absolute top-4 left-4 size-10 items-center justify-center rounded-full bg-black/40",
		Label: locales.Back(cx), TestID: "back", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress:  func(core.PressEvent) { a.back() },
		Children: []core.Node{core.Icon{SVG: lucide.ChevronLeft(), Class: "size-5 text-white"}}}
	label := locales.Play(cx)
	if item.Position > 0 {
		label = locales.DetailResumeAt(cx, playbackClock(item.Position))
	}
	actions := []core.Node{base.Button{Label: label, TestID: "detail-play", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		Class:   "h-12 flex-row items-center justify-center gap-2 rounded-2xl bg-linear-to-r from-violet-500 to-fuchsia-500 shadow-lg shadow-violet-500/40",
		OnPress: func(core.PressEvent) { play(true) }, Children: []core.Node{
			core.Icon{SVG: lucide.Play(), Class: "size-5 text-white"},
			core.Text{Content: label, Class: "font-semibold text-white"},
		}}}
	if item.Position > 0 {
		if item.Duration > 0 {
			value := float32(min(100, 100*float64(item.Position)/float64(item.Duration)))
			actions = append(actions, core.View{Class: "overflow-hidden rounded-full", Children: []core.Node{
				base.Progress{TestID: "detail-progress", Value: func() float32 { return value }, Label: locales.ResumePercent(cx, strconv.Itoa(int(value))),
					TrackClass: "h-1 bg-white/25", IndicatorClass: "h-1 bg-violet-400"},
			}})
		}
		personal = append([]core.Node{detailPosterAction(cx, "detail-from-start", locales.FromStart(cx), lucide.RotateCcw(),
			false, nil, func() { play(false) })}, personal...)
	}
	if len(personal) != 0 {
		actions = append(actions, core.View{Class: "flex-row gap-3", Children: personal})
	}
	return core.View{Class: "relative overflow-hidden rounded-3xl shadow-2xl shadow-black/30 " + shape, TestID: "tablet-detail-poster",
		Children: []core.Node{
			homeArtwork(cmp.Or(item.Poster, item.Backdrop), "absolute inset-0"),
			core.View{Class: "absolute inset-0 bg-linear-to-t from-black/90 via-black/30 to-black/10"},
			back, tabletDetailSource(cx, current, sourceStatus, copies, selectCopy),
			core.View{Class: "absolute right-7 bottom-7 left-7 gap-4", Children: actions},
		}}
}

func tabletDetailSource(cx *core.Scope, current route, status string, copies func() []ui.Option[copyKey], selectCopy func(copyKey)) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		chip := []core.Node{
			core.View{Class: "size-2 shrink-0 rounded-full " + status},
			core.Text{Content: current.source.Name, Class: "min-w-0 shrink text-xs font-medium text-white", MaxLines: 1},
		}
		const placement = "absolute top-4 right-4 max-w-[240px]"
		const shape = "rounded-full bg-black/40"
		if copies == nil || len(copies()) < 2 {
			return core.View{Class: placement + " " + shape + " flex-row items-center gap-1.5 px-3 py-1.5", TestID: "detail-source", Children: chip}
		}
		chip = append(chip, core.Icon{SVG: lucide.ChevronDown(), Class: "size-3 text-white"})
		options := copies()
		items := make([]base.SelectItem, 0, len(options))
		active := ""
		for i, option := range options {
			id := strconv.Itoa(i)
			items = append(items, base.SelectItem{Value: id, Title: option.Label})
			if option.Value == (catalogCopy{current.source, current.item}).key() {
				active = id
			}
		}
		return base.Select{Label: locales.PlaybackSource(cx), TestID: "playback-source", RootClass: placement, Class: shape,
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, PopupClass: tabletDetailPopup, ItemClass: tabletDetailOption,
			Items: func() []base.SelectItem { return items }, Value: func() *string { return &active },
			OnChange: func(value *string) {
				if value != nil {
					if index, err := strconv.Atoi(*value); err == nil && index >= 0 && index < len(options) {
						selectCopy(options[index].Value)
					}
				}
			}, RenderItem: tabletDetailOptionText,
			Trigger: core.View{Class: "flex-row items-center gap-1.5 px-3 py-1.5", Children: chip},
		}
	})
}

func tabletDetailChoice(cx *core.Scope, id, label, display string, options []ui.Option[string], value string, change func(string)) core.Node {
	const cell = "min-w-0 flex-1 basis-0"
	shape := "min-w-0 rounded-xl bg-white shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder
	trigger := core.View{Class: "gap-1 px-3.5 py-2.5", Children: []core.Node{
		core.Text{Content: label, Class: "text-[11px] " + setupMuted, MaxLines: 1},
		core.View{Class: "flex-row items-center gap-1", Children: []core.Node{
			core.Text{Content: display, TestID: id + "-value", Class: "min-w-0 flex-1 text-sm font-medium " + setupTitle, MaxLines: 1},
			core.Show{When: func() bool { return len(options) > 1 }, Then: func(*core.Scope) core.Node {
				return core.Icon{SVG: lucide.ChevronDown(), Class: "size-4 shrink-0 " + setupMuted}
			}},
		}},
	}}
	if len(options) <= 1 {
		return core.View{Class: cell + " " + shape, Children: []core.Node{trigger}}
	}
	items := make([]base.SelectItem, 0, len(options))
	for _, option := range options {
		items = append(items, base.SelectItem{Value: option.Value, Title: option.Label})
	}
	return base.Select{Label: label, TestID: id, RootClass: cell, Class: shape,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, PopupClass: tabletDetailPopup, ItemClass: tabletDetailOption,
		Items: func() []base.SelectItem { return items }, Value: func() *string { return &value },
		OnChange: func(id *string) {
			if id != nil {
				change(*id)
			}
		}, RenderItem: tabletDetailOptionText, Trigger: trigger,
	}
}

const tabletDetailPopup = "min-w-[180px] overflow-hidden rounded-xl bg-white shadow-lg dark:bg-[#1a1b25] " + setupBorder
const tabletDetailOption = "min-h-11 items-center px-3 selected:bg-violet-500/12"

func tabletDetailOptionText(row base.SelectEntry) core.Node {
	return core.Text{Content: row.Title, Class: "text-sm " + setupTitle, MaxLines: 1}
}

func tabletDetailTrack(cx *core.Scope, version media.Version, choice detailTrackChoice, kind string, set func(detailTrackChoice)) core.Node {
	label := locales.DetailAudio(cx)
	if kind == "subtitle" {
		label = locales.Subtitles(cx)
	}
	options, display, value := detailTrackOptions(cx, version, choice, kind)
	return tabletDetailChoice(cx, "detail-"+kind, label, display, options, value, func(id string) {
		if kind == "audio" {
			choice.audio = id
		} else {
			choice.subtitle = id
		}
		set(choice)
	})
}

func detailTrackOptions(cx *core.Scope, version media.Version, choice detailTrackChoice, kind string) (options []ui.Option[string], display, value string) {
	display, value = locales.AutomaticPlaybackVersion(cx), choice.audio
	if kind == "subtitle" {
		display, value = locales.SubtitlesOff(cx), choice.subtitle
	}
	options = []ui.Option[string]{{Label: display}}
	for _, track := range version.Tracks {
		if track.Type != kind || (kind == "subtitle" && track.URL == "") {
			continue
		}
		name := trackLabel(cx, track)
		options = append(options, ui.Option[string]{Label: name, Value: track.ID})
		if track.ID == value || (kind == "audio" && value == "" && (track.Selected || track.Default)) {
			display = name
		}
	}
	if len(options) == 1 && kind == "audio" {
		display = locales.MetadataUnavailable(cx)
	}
	return options, display, value
}

func tabletDetailHeader(cx *core.Scope, item media.Item, version media.Version) []core.Node {
	title := []core.Node{core.Text{Content: item.Title, TestID: "tablet-detail-title", Class: "text-4xl font-bold tracking-tight " + setupTitle, MaxLines: 2}}
	if item.OriginalTitle != "" && item.OriginalTitle != item.Title {
		title = append(title, core.Text{Content: item.OriginalTitle, Class: "text-base " + setupMuted, MaxLines: 1})
	}
	nodes := []core.Node{core.View{Class: "gap-1.5", Children: title}}
	var meta []string
	if item.Year > 0 {
		meta = append(meta, strconv.Itoa(item.Year))
	}
	if len(item.Genres) != 0 {
		meta = append(meta, strings.Join(item.Genres, " / "))
	}
	if item.Duration >= time.Minute {
		meta = append(meta, locales.HomeDuration(cx, strconv.Itoa(int(item.Duration/time.Minute))))
	} else if item.Duration > 0 {
		meta = append(meta, playbackClock(item.Duration))
	}
	if item.ContentRating != "" {
		meta = append(meta, item.ContentRating)
	}
	if len(meta) == 0 {
		meta = append(meta, itemMetadata(cx, item))
	}
	nodes = append(nodes, core.Text{Content: strings.Join(meta, " · "), MaxLines: 2, Class: "text-sm " + setupTitle})
	var badges []core.Node
	if item.Rating > 0 {
		badges = append(badges, core.View{Class: "flex-row items-center gap-1.5 rounded-md bg-amber-500/15 px-2 py-1", Children: []core.Node{
			core.Icon{SVG: lucide.Star(), Class: "size-3 text-amber-500"},
			core.Text{Content: strconv.FormatFloat(item.Rating, 'f', 1, 64), Class: "text-xs font-bold " + setupTitle},
		}})
	}
	for _, label := range []string{detailResolution(version), detailVideoRange(version)} {
		if label != "" {
			badges = append(badges, core.Text{Content: label,
				Class: "rounded-md bg-zinc-900/5 px-1.5 py-0.5 text-[11px] font-semibold dark:bg-zinc-50/5 " + setupBorder + " " + setupTitle})
		}
	}
	if len(badges) != 0 {
		nodes = append(nodes, core.View{Class: "flex-row flex-wrap items-center gap-2", Children: badges})
	}
	return nodes
}

func detailResolution(version media.Version) string {
	if version.Height >= 2000 && version.Height <= 2160 && version.Width >= 3800 {
		return "4K"
	}
	if version.Height > 0 {
		return strconv.Itoa(version.Height) + "p"
	}
	return ""
}

func detailVideoRange(version media.Version) string {
	rangeType := version.VideoRange
	if strings.HasPrefix(strings.ToLower(rangeType), "dovi") {
		return "Dolby Vision"
	}
	if strings.HasPrefix(strings.ToUpper(rangeType), "HDR") || strings.EqualFold(rangeType, "HLG") {
		return rangeType
	}
	return ""
}

func tabletDetailHeading(label string) core.Node {
	return core.Text{Content: label, Class: "text-base font-bold " + setupTitle}
}

func tabletDetailCast(cx *core.Scope, people []media.Person) core.Node {
	rows := make([]core.Node, 0, len(people))
	for _, person := range people {
		role := person.Role
		if person.Director {
			role = locales.Director(cx)
		}
		rows = append(rows, core.View{Class: "w-20 shrink-0 items-center gap-1.5", Children: []core.Node{
			core.View{Class: "relative size-16 overflow-hidden rounded-full shadow-md", Children: []core.Node{
				homeArtwork(person.Portrait, "absolute inset-0"),
			}},
			core.Text{Content: person.Name, Class: "text-xs " + setupTitle, MaxLines: 1},
			core.Text{Content: role, Class: "text-[11px] " + setupMuted, MaxLines: 1},
		}})
	}
	return core.View{Class: "gap-3", Children: []core.Node{
		tabletDetailHeading(locales.CastAndCrew(cx)),
		core.Scroll{Horizontal: true, Children: []core.Node{core.View{Class: "flex-row gap-4", Children: rows}}},
	}}
}

func tabletDetailFile(cx *core.Scope, source string, version media.Version) core.Node {
	label := core.Text{Content: locales.FileInfo(cx), Class: "text-sm font-semibold " + setupTitle}
	header := []core.Node{label}
	if version.Method != "" {
		header = append(header, core.Text{Content: playMethod(cx, version.Method),
			Class: "rounded-full bg-emerald-500/15 px-2 py-0.5 text-[11px] font-medium text-emerald-500"})
	}
	var file, video, audio []string
	if version.Container != "" {
		file = append(file, strings.ToUpper(version.Container))
	}
	if version.Size > 0 {
		file = append(file, versionSize(version.Size))
	}
	if version.Bitrate > 0 {
		precision := 1
		if version.Bitrate < 100_000 {
			precision = 2
		}
		file = append(file, locales.BitrateMegabits(cx, strconv.FormatFloat(float64(version.Bitrate)/1_000_000, 'f', precision, 64)))
	}
	if version.VideoCodec != "" {
		video = append(video, strings.ToUpper(version.VideoCodec))
	}
	if version.Width > 0 && version.Height > 0 {
		video = append(video, strconv.Itoa(version.Width)+"×"+strconv.Itoa(version.Height))
	}
	if name := detailVideoRange(version); name != "" {
		video = append(video, name)
	}
	subtitles := 0
	for _, track := range version.Tracks {
		if track.Type == "subtitle" {
			subtitles++
		} else if track.Type == "audio" && len(audio) == 0 && track.Codec != "" {
			audio = append(audio, strings.ToUpper(track.Codec))
		}
	}
	if subtitles > 0 {
		audio = append(audio, locales.FileSubtitleCount(cx, strconv.Itoa(subtitles)))
	}
	row := func(first, second string) core.Node {
		return core.View{Class: "min-w-0 flex-1 gap-1", Children: []core.Node{
			core.Text{Content: first, Class: "text-xs " + setupMuted, MaxLines: 1},
			core.Text{Content: second, Class: "text-xs " + setupMuted, MaxLines: 1},
		}}
	}
	return core.View{Class: "gap-2 rounded-2xl bg-white p-4 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder, TestID: "detail-file",
		Children: []core.Node{
			core.View{Class: "flex-row items-center justify-between", Children: header},
			core.View{Class: "flex-row gap-6", Children: []core.Node{
				row(locales.FileSource(cx, source), strings.Join(file, " · ")),
				row(strings.Join(video, " "), strings.Join(audio, " · ")),
			}},
		}}
}
