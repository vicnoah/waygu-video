package main

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

const desktopMoviePanel = "gap-3 rounded-2xl p-5 shadow-sm shadow-black/5 " + desktopLibrarySurface

func desktopMoviePlaybackOptions(cx *core.Scope, state movieDetailState) core.Node {
	versions, _ := state.available.Get()
	version := state.active()
	options := []ui.Option[string]{{Label: locales.AutomaticPlaybackVersion(cx)}}
	for i, candidate := range versions {
		options = append(options, ui.Option[string]{Label: versionLabel(cx, candidate, i+1), Value: candidate.ID})
	}
	display := cmp.Or(movieVersionSummary(version), locales.MetadataUnavailable(cx))
	if len(versions) == 0 && state.available.Loading() {
		display = locales.Loading(cx)
	}
	choice := state.tracks.Get()
	if choice.version != version.ID {
		choice = detailTrackChoice{version: version.ID}
	}
	audio, audioLabel, audioValue := detailTrackOptions(cx, version, choice, "audio")
	subtitles, subtitleLabel, subtitleValue := detailTrackOptions(cx, version, choice, "subtitle")
	return core.View{Class: "flex-row gap-3", TestID: "desktop-movie-choices", Children: []core.Node{
		desktopMovieChoice(cx, "playback-version", locales.VersionShort(cx), display, options, state.selected.Get(), state.chooseVersion),
		desktopMovieChoice(cx, "detail-audio", locales.DetailAudio(cx), audioLabel, audio, audioValue, func(id string) {
			choice.audio = id
			state.tracks.Set(choice)
		}),
		desktopMovieChoice(cx, "detail-subtitle", locales.Subtitles(cx), subtitleLabel, subtitles, subtitleValue, func(id string) {
			choice.subtitle = id
			state.tracks.Set(choice)
		}),
	}}
}

func desktopMovieChoice(cx *core.Scope, id, label, display string, options []ui.Option[string], value string, change func(string)) core.Node {
	const cell = "min-w-0 flex-1 basis-0"
	const skin = "min-w-0 rounded-xl shadow-sm shadow-black/5 " + desktopLibrarySurface
	line := []core.Node{core.Text{Content: display, TestID: id + "-value", Class: "min-w-0 flex-1 text-sm font-medium " + setupTitle, MaxLines: 1}}
	if len(options) > 1 {
		line = append(line, core.Icon{SVG: lucide.ChevronDown(), Class: "size-4 shrink-0 " + setupMuted})
	}
	trigger := core.View{Class: "gap-1.5 px-4 py-3", Children: []core.Node{
		core.Text{Content: label, Class: "text-xs " + setupMuted, MaxLines: 1},
		core.View{Class: "flex-row items-center gap-2", Children: line},
	}}
	if len(options) <= 1 {
		return core.View{Class: cell + " " + skin, Children: []core.Node{trigger}}
	}
	items := make([]base.SelectItem, 0, len(options))
	for _, option := range options {
		items = append(items, base.SelectItem{Value: option.Value, Title: option.Label})
	}
	return base.Select{Label: label, TestID: id, RootClass: cell, Class: skin, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		PopupClass: tabletDetailPopup, ItemClass: tabletDetailOption, Items: func() []base.SelectItem { return items },
		Value: func() *string { return &value }, RenderItem: tabletDetailOptionText, Trigger: trigger,
		OnChange: func(id *string) {
			if id != nil {
				change(*id)
			}
		}}
}

func movieVersionSummary(version media.Version) string {
	resolution := detailResolution(version)
	if resolution == "4K" {
		resolution = strconv.Itoa(version.Height) + "p"
	}
	summary := desktopMovieFacts(resolution, strings.ToUpper(version.Container), positiveMediaBytes(version.Size))
	return cmp.Or(summary, version.Name)
}

func positiveMediaBytes(size int64) string {
	if size <= 0 {
		return ""
	}
	return mediaBytes(size)
}

func desktopMovieFacts(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(value string) bool { return strings.TrimSpace(value) == "" }), " · ")
}

func desktopMovieHeading(label string) core.Node {
	return core.Text{Content: label, Class: "text-lg font-bold " + setupTitle, Role: core.RoleHeader}
}

func desktopMovieSynopsis(cx *core.Scope, item media.Item, loading bool) core.Node {
	overview := item.Overview
	if overview == "" {
		overview = locales.NoOverview(cx)
		if loading {
			overview = locales.Loading(cx)
		}
	}
	var directors []string
	writers := slices.Clone(item.Writers)
	for _, member := range item.People {
		if member.Director || strings.EqualFold(member.Kind, "director") {
			if member.Name != "" && !slices.Contains(directors, member.Name) {
				directors = append(directors, member.Name)
			}
		} else if strings.EqualFold(member.Kind, "writer") {
			if member.Name != "" && !slices.Contains(writers, member.Name) {
				writers = append(writers, member.Name)
			}
		}
	}
	credits := []core.Node{}
	for _, row := range []struct {
		label string
		value []string
	}{
		{locales.Director(cx), directors}, {locales.DesktopMovieWriter(cx), writers}, {locales.DesktopMovieCountries(cx), item.Countries},
	} {
		if len(row.value) > 0 {
			credits = append(credits, core.View{Class: "gap-1", Children: []core.Node{
				core.Text{Content: row.label, Class: "text-xs " + setupMuted},
				core.Text{Content: strings.Join(row.value, " / "), Class: "text-sm " + setupTitle},
			}})
		}
	}
	nodes := []core.Node{desktopMovieHeading(locales.Synopsis(cx)),
		core.Text{Content: overview, Class: "text-sm leading-relaxed text-zinc-900/80 dark:text-zinc-50/80", TestID: "detail-synopsis"}}
	if len(credits) > 0 {
		nodes = append(nodes, core.View{Class: "flex-row flex-wrap gap-x-8 gap-y-3 pt-1", Children: credits})
	}
	return core.View{Class: "gap-3", Children: nodes}
}

func desktopMovieCast(cx *core.Scope, people []media.Person) core.Node {
	var cards []core.Node
	for _, person := range people {
		if person.Name == "" || person.Director || (person.Kind != "" && !strings.EqualFold(person.Kind, "actor") && !strings.EqualFold(person.Kind, "gueststar")) {
			continue
		}
		cards = append(cards, core.View{Class: "w-24 shrink-0 items-center gap-2", Children: []core.Node{
			core.View{Class: "relative size-20 rounded-full shadow-md ring-2 ring-zinc-900/8 dark:ring-white/8",
				Children: []core.Node{desktopMovieArtwork(person.Portrait, "absolute inset-0 overflow-hidden rounded-full")}},
			core.Text{Content: person.Name, Class: "text-center text-xs " + setupTitle, MaxLines: 1},
			core.Text{Content: person.Role, Class: "text-center text-[11px] " + setupMuted, MaxLines: 1},
		}})
	}
	if len(cards) == 0 {
		return nil
	}
	return core.View{Class: "gap-4", Children: []core.Node{
		desktopMovieHeading(locales.CastAndCrew(cx)),
		homeRail("desktop-movie-cast", "gap-5", "", cards),
	}}
}

func desktopMovieFile(cx *core.Scope, current route, item media.Item, version media.Version,
	copies func() []ui.Option[copyKey], selectCopy func(copyKey)) core.Node {
	header := []core.Node{core.Text{Content: locales.FileInfo(cx), Class: "font-semibold " + setupTitle}}
	if version.Method != "" {
		label, tint := "", "bg-emerald-500/15 text-emerald-500"
		switch version.Method {
		case media.DirectPlay:
			label = locales.DesktopMovieDirectPlay(cx)
		case media.DirectStream:
			label = locales.DirectStream(cx)
		case media.Transcode:
			label, tint = locales.Transcode(cx), "bg-amber-500/15 text-amber-500"
		}
		if label != "" {
			header = append(header, core.Text{Content: label, Class: "rounded-full px-2 py-0.5 text-[11px] font-medium " + tint})
		}
	}
	nodes := []core.Node{core.View{Class: "flex-row items-center justify-between gap-2", Children: header}}
	if copies != nil && len(copies()) > 1 {
		label := locales.MovieSource(cx)
		nodes = append(nodes, desktopMovieFileRow(label, ui.Select[copyKey]{Label: label, TestID: "playback-source", Options: copies(),
			Value: func() copyKey { return (catalogCopy{source: current.source, item: item}).key() }, OnChange: selectCopy}))
	} else {
		nodes = append(nodes, desktopMovieFileFact(locales.MovieSource(cx), current.source.Name))
	}
	pathLabel := locales.DesktopMoviePath(cx)
	file := version.FilePath
	if file == "" {
		pathLabel, file = locales.MovieFileName(cx), version.FileName
	}
	dimensions, bitrate, added := "", "", ""
	if version.Width > 0 && version.Height > 0 {
		dimensions = fmt.Sprintf("%d×%d", version.Width, version.Height)
	}
	if version.Bitrate > 0 {
		bitrate = locales.BitrateMegabits(cx, strconv.FormatFloat(float64(version.Bitrate)/1_000_000, 'f', 1, 64))
	}
	if !item.AddedAt.IsZero() {
		added = item.AddedAt.Local().Format("2006-01-02 15:04")
	}
	audio, subtitles := desktopMovieFileTracks(cx, version.Tracks)
	for _, fact := range []struct{ label, value string }{
		{pathLabel, file},
		{locales.DesktopMovieContainer(cx), desktopMovieFacts(strings.ToUpper(version.Container), positiveMediaBytes(version.Size))},
		{locales.MovieFileVideo(cx), desktopMovieFacts(strings.ToUpper(version.VideoCodec), dimensions, videoFrameRate(version))},
		{locales.DesktopMovieDynamicRange(cx), describeVideoRange(localizedDetailVideoRange(cx, version), version)},
		{locales.MovieFileBitrate(cx), bitrate},
		{locales.DetailAudio(cx), audio},
		{locales.Subtitles(cx), subtitles},
		{locales.DesktopMovieAdded(cx), added},
	} {
		if fact.value != "" {
			nodes = append(nodes, desktopMovieFileFact(fact.label, fact.value))
		}
	}
	return core.View{Class: desktopMoviePanel, TestID: "detail-file", Children: nodes}
}

func desktopMovieFileFact(label, value string) core.Node {
	return desktopMovieFileRow(label, core.Text{Content: value, Class: "min-w-0 flex-1 text-xs leading-relaxed " + setupTitle})
}

func desktopMovieFileRow(label string, value core.Node) core.Node {
	return core.View{Class: "flex-row items-start gap-3", Children: []core.Node{
		core.Text{Content: label, Class: "w-16 shrink-0 text-xs " + setupMuted}, value,
	}}
}

func catalogVideoFormat(cx *core.Scope, profile media.VideoProfile) string {
	var parts []string
	if resolution := libraryResolution(profile); resolution > 0 {
		parts = append(parts, libraryResolutionName(resolution))
	}
	if dynamicRange := localizedDetailVideoRange(cx, media.Version{VideoRange: profile.Range}); dynamicRange != "" {
		parts = append(parts, dynamicRange)
	}
	return strings.Join(parts, " ")
}

func desktopMovieFileTracks(cx *core.Scope, tracks []media.Track) (string, string) {
	var audio media.Track
	var audioCount, embedded, external int
	for _, track := range tracks {
		switch track.Type {
		case "audio":
			audioCount++
			if audio.ID == "" || track.Selected || (track.Default && !audio.Selected) {
				audio = track
			}
		case "subtitle":
			if track.External {
				external++
			} else {
				embedded++
			}
		}
	}
	audioText := ""
	if audioCount > 0 {
		audioText = trackLabel(cx, audio)
		if audioCount > 1 {
			audioText = desktopMovieFacts(audioText, locales.DesktopMovieMoreTracks(cx, strconv.Itoa(audioCount-1)))
		}
	}
	var subtitles []string
	if embedded > 0 {
		subtitles = append(subtitles, locales.DesktopMovieEmbeddedSubtitles(cx, strconv.Itoa(embedded)))
	}
	if external > 0 {
		subtitles = append(subtitles, locales.DesktopMovieExternalSubtitles(cx, strconv.Itoa(external)))
	}
	return audioText, desktopMovieFacts(subtitles...)
}
