package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
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

func (a *application) desktopPlayerSidebar(cx *core.Scope, p desktopPlayerControls, active *core.Signal[desktopPlayerTab], wide func() bool) core.Node {
	label := func(tab desktopPlayerTab) string {
		switch tab {
		case desktopPlayerEpisodes:
			return locales.DesktopPlayerEpisodes(cx)
		case desktopPlayerDanmaku:
			return locales.Danmaku(cx)
		default:
			return locales.DesktopPlayerInfo(cx)
		}
	}
	tabs := []core.Node{}
	for _, tab := range []desktopPlayerTab{desktopPlayerEpisodes, desktopPlayerDanmaku, desktopPlayerInfo} {
		tabs = append(tabs, base.Button{Label: label(tab), TestID: "desktop-player-tab-" + string(tab), Selected: func() bool { return active.Get() == tab },
			Class: "group h-8 flex-1 items-center justify-center rounded-lg selected:bg-violet-500 selected:shadow-md selected:shadow-violet-500/30",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { active.Set(tab) },
			Children: []core.Node{core.Text{Content: label(tab), Class: "text-sm text-zinc-400 group-selected:font-semibold group-selected:text-white"}}})
	}
	body := core.Dynamic(func(cx *core.Scope) core.Node {
		switch active.Get() {
		case desktopPlayerEpisodes:
			return a.desktopPlayerEpisodePanel(cx, p)
		case desktopPlayerDanmaku:
			return a.desktopPlayerDanmakuPanel(cx, p)
		default:
			return p.information(cx)
		}
	})
	return core.View{Class: "w-80 shrink-0 border-l border-white/10 bg-zinc-900", TestID: "desktop-player-sidebar",
		ClassFunc: func() string {
			if !wide() {
				return "absolute inset-y-0 right-0 shadow-2xl"
			}
			return ""
		}, Children: []core.Node{
			core.View{Class: "m-3 mb-0 h-10 flex-row items-center gap-1 rounded-xl border border-white/10 bg-white/5 p-1", Children: tabs},
			body,
		}}
}

func (a *application) desktopPlayerEpisodePanel(cx *core.Scope, p desktopPlayerControls) core.Node {
	episode := p.current.item
	if episode.SeriesID == "" || p.current.downloadID != "" {
		return core.View{Class: "p-4", Children: []core.Node{hint(locales.DesktopPlayerNoEpisodes(cx))}}
	}
	seasons := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) ([]media.Item, error) {
		rows, err := p.client.Children(ctx, media.Item{ID: episode.SeriesID, Kind: media.Series})
		rows = slices.DeleteFunc(slices.Clone(rows), func(row media.Item) bool { return row.Kind != media.Season || row.ID == "" })
		slices.SortStableFunc(rows, func(left, right media.Item) int { return cmp.Compare(left.Season, right.Season) })
		return rows, err
	})
	seasonID := core.NewSignal(cx, "")
	selected := core.NewMemo(cx, func() string {
		items, _ := seasons.Get()
		for _, item := range items {
			if item.ID == seasonID.Get() || (seasonID.Get() == "" && item.Season == episode.Season) {
				return item.ID
			}
		}
		if len(items) > 0 {
			return items[0].ID
		}
		return ""
	})
	episodes := core.NewResource(cx, func() (string, bool) { id := selected.Get(); return id, id != "" },
		func(ctx context.Context, id string, _ core.ResourceInfo) ([]media.Item, error) {
			items, err := p.client.Children(ctx, media.Item{ID: id, SeriesID: episode.SeriesID, Kind: media.Season})
			items = slices.DeleteFunc(slices.Clone(items), func(item media.Item) bool { return item.ID == "" || item.Kind != media.Episode })
			slices.SortStableFunc(items, func(left, right media.Item) int { return cmp.Compare(left.Episode, right.Episode) })
			return items, err
		})
	body := core.Dynamic(func(cx *core.Scope) core.Node {
		items, _ := episodes.Get()
		if rules := a.parentalRules(); rules.Enabled {
			items = slices.DeleteFunc(slices.Clone(items), func(item media.Item) bool {
				return !rules.visible(p.current.source.ID, inheritSeriesEpisode(item, episode))
			})
		}
		choose := func(next media.Item) { a.playEpisodeFrom(p.current, next) }
		var children []core.Node
		if itemsSeason, _ := seasons.Get(); len(itemsSeason) > 0 {
			label := func(season media.Item) string {
				if season.Season == 0 {
					return locales.DesktopPlayerSpecials(cx)
				}
				return locales.SeasonLabel(cx, strconv.Itoa(season.Season))
			}
			var picks []core.Node
			activeLabel := ""
			for _, season := range itemsSeason {
				name := label(season)
				picks = append(picks, ui.MenuItem{Text: name, Checked: func() bool { return selected.Get() == season.ID },
					OnSelect: func() { seasonID.Set(season.ID) }})
				if season.ID == selected.Get() {
					activeLabel = name
				}
			}
			children = append(children, core.View{Class: "flex-row items-center justify-between px-1 pb-2", Children: []core.Node{
				base.Menu{Label: activeLabel, TestID: "desktop-player-season", Class: "rounded", PopupClass: tabletDetailPopup,
					Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, Children: picks, Trigger: core.View{Class: "flex-row items-center gap-1", Children: []core.Node{
						core.Text{Content: activeLabel, Class: "text-sm font-semibold text-white"},
						core.Icon{SVG: lucide.ChevronDown(), Class: "size-4 text-zinc-500"},
					}}},
				core.Text{Content: locales.DesktopPlayerEpisodeCount(cx, strconv.Itoa(len(items))), Class: "text-xs text-zinc-500"},
			}})
		}
		if err := cmp.Or(seasons.Err(), episodes.Err()); err != nil {
			children = append(children, notice(errorText(cx, err)), ui.Button{Text: locales.Retry(cx), OnPress: func() { seasons.Refetch(); episodes.Refetch() }})
		} else if seasons.Loading() || episodes.Loading() {
			children = append(children, hint(locales.Loading(cx)))
		} else if len(items) == 0 {
			children = append(children, hint(locales.DesktopPlayerNoEpisodes(cx)))
		}
		for _, item := range items {
			children = append(children, p.episodeRow(cx, item, choose))
		}
		return core.View{Class: "gap-1 p-3", Children: children}
	})
	return core.Scroll{Class: "flex-1 min-h-0", TestID: "desktop-player-episodes", Children: []core.Node{body}}
}

func (p desktopPlayerControls) episodeRow(cx *core.Scope, item media.Item, choose func(media.Item)) core.Node {
	active := item.ID == p.current.item.ID
	title := item.Title
	if item.Episode > 0 {
		title = locales.EpisodeLabel(cx, strconv.Itoa(item.Episode))
	}
	runtime, status := "", ""
	if item.Duration > 0 {
		runtime = playbackClock(item.Duration)
		if item.Duration >= time.Minute {
			runtime = locales.HomeDuration(cx, strconv.Itoa(int(item.Duration/time.Minute)))
		}
	}
	if active {
		status = locales.DesktopPlayerPlaying(cx)
	} else if item.Watched {
		status = locales.DesktopPlayerWatched(cx)
	}
	art := []core.Node{desktopPlayerEpisodeArt(cmp.Or(item.Backdrop, item.Poster))}
	if active {
		art = append(art, core.View{Class: "absolute inset-0 items-center justify-center bg-black/40", Children: []core.Node{
			core.Icon{SVG: lucide.AudioLines(), Class: "size-5 text-violet-300"},
		}})
	}
	text := []core.Node{
		core.Text{Content: title, Class: "text-sm text-white group-selected:font-semibold", MaxLines: 1},
		core.Text{Content: desktopMovieFacts(status, runtime), Class: "text-xs text-zinc-500 group-selected:text-violet-300", MaxLines: 1},
	}
	if active {
		text = append(text, base.Progress{Label: locales.PlaybackPosition(cx), TrackClass: "h-1 rounded-full bg-white/15",
			IndicatorClass: "h-1 rounded-full bg-linear-to-r from-violet-500 to-fuchsia-500", Value: func() float32 {
				return 100 * float32(tvPlaybackFraction(p.position.Get(), p.duration.Get()))
			}})
	}
	children := []core.Node{
		core.View{Class: "relative h-14 w-24 shrink-0 overflow-hidden rounded-lg", Children: art},
		core.View{Class: "min-w-0 flex-1 gap-1", Children: text},
	}
	if !active && item.Watched {
		children = append(children, core.Icon{SVG: lucide.CircleCheck(), Class: "size-4 text-violet-400"})
	}
	class := "group flex-row items-center gap-3 rounded-xl p-2"
	if active {
		class += " border border-violet-500/30 bg-violet-500/15"
	}
	return base.Button{Label: title, Hint: item.Title, TestID: "desktop-episode-" + item.ID, Selected: func() bool { return active },
		Class: class,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { choose(item) }, Children: children}
}

func desktopPlayerEpisodeArt(url string) core.Node {
	if url != "" {
		return homeArtwork(url, "absolute inset-0")
	}
	return core.View{Class: "absolute inset-0 items-center justify-center bg-[#f5f5f5]", Children: []core.Node{
		core.Icon{SVG: lucide.Image(), Class: "size-7 text-[#737373]"},
	}}
}

const desktopInfoCard = "gap-2.5 rounded-xl border border-white/10 bg-white/5 p-4"

func (p desktopPlayerControls) information(cx *core.Scope) core.Node {
	data := core.Dynamic(func(cx *core.Scope) core.Node {
		active := p.stream()
		metric, usingMPV := p.stats()
		engine := locales.DesktopPlayerNativeEngine(cx)
		if usingMPV {
			engine = "libmpv"
			if metric.Hardware != "" && metric.Hardware != "no" {
				engine = desktopMovieFacts(engine, metric.Hardware)
			}
		}
		method := playMethod(cx, active.Method)
		signal := "bg-emerald-400"
		if active.Method == media.Transcode {
			signal = "bg-amber-400"
		}
		mode := []core.Node{
			core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
				core.View{Class: "size-2 rounded-full " + signal},
				core.Text{Content: method, Class: "min-w-0 flex-1 text-sm font-semibold text-white", MaxLines: 1},
				core.Show{When: func() bool { return p.qualityAvailable }, Then: func(cx *core.Scope) core.Node {
					return desktopPlayerButton(cx, "desktop-player-quality", locales.StreamingQuality(cx), lucide.ChevronRight(),
						"size-6", "size-4", nil, func() { p.tracks.show(trackQualityTab) })
				}},
			}},
			core.Text{Content: locales.DesktopPlayerEngine(cx, engine), Class: "text-xs text-zinc-400"},
		}
		picture := p.picture.Get()
		width, height := active.Rendition.Width, active.Rendition.Height
		if picture.Width > 0 && picture.Height > 0 {
			width, height = int(picture.Width), int(picture.Height)
		}
		resolution := ""
		if width > 0 && height > 0 {
			resolution = fmt.Sprintf("%d×%d", width, height)
		}
		fps, bitrate, videoRange, codec := active.Rendition.FrameRate, int64(active.Rendition.Bitrate), desktopVideoRange(cx, active.Rendition),
			strings.ToUpper(active.Rendition.VideoCodec)
		if active.Method == media.Transcode {
			fps, bitrate, videoRange, codec = 0, 0, "", ""
		}
		if metric.FPS > 0 {
			fps = metric.FPS
		}
		if metric.Bitrate > 0 {
			bitrate = metric.Bitrate
		}
		if metric.VideoCodec != "" {
			codec = strings.ToUpper(metric.VideoCodec)
		}
		if fps > 0 {
			resolution = desktopMovieFacts(resolution, fmt.Sprintf("%s fps", strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", fps), "0"), ".")))
		}
		bitrateText := ""
		if bitrate > 0 {
			precision := 1
			if bitrate < 100_000 {
				precision = 3
			}
			bitrateText = locales.BitrateMegabits(cx, strconv.FormatFloat(float64(bitrate)/1e6, 'f', precision, 64))
		}
		video := []core.Node{
			desktopPlayerFact(locales.DesktopPlayerCodec(cx), codec),
			desktopPlayerFact(locales.LibraryResolution(cx), resolution),
			desktopPlayerFact(locales.DesktopMovieDynamicRange(cx), videoRange),
			desktopPlayerFact(locales.MovieFileBitrate(cx), bitrateText),
		}
		if usingMPV {
			video = append(video, desktopPlayerFact(locales.DesktopPlayerDropped(cx), strconv.FormatInt(metric.DroppedFrames, 10)))
		}
		audio := []core.Node{
			desktopPlayerFact(locales.DesktopPlayerCodec(cx), p.tracks.audio()),
			desktopPlayerFact(locales.DesktopPlayerOutput(cx), metric.AudioOutput),
		}
		buffer := locales.DesktopPlayerBufferReady(cx)
		switch {
		case p.buffering():
			buffer = locales.Loading(cx)
		case metric.Buffered > 0:
			buffer = playbackClock(metric.Buffered)
		}
		network := []core.Node{desktopPlayerFact(locales.DesktopPlayerBuffer(cx), buffer)}
		if metric.DownloadRate > 0 && p.current.downloadID == "" {
			network = append(network, desktopPlayerFact(locales.DesktopPlayerDownloadRate(cx), mediaBytes(metric.DownloadRate)+"/s"))
		}
		return core.View{Class: "gap-4 p-4", Children: []core.Node{
			desktopPlayerInfoCard(locales.DesktopPlayerMethod(cx), mode...),
			desktopPlayerInfoCard(locales.MovieFileVideo(cx), video...),
			desktopPlayerInfoCard(locales.DetailAudio(cx), audio...),
			desktopPlayerInfoCard(locales.DesktopPlayerNetwork(cx), network...),
		}}
	})
	return core.Scroll{Class: "flex-1 min-h-0", TestID: "desktop-player-info", Children: []core.Node{data}}
}

func desktopPlayerInfoCard(title string, rows ...core.Node) core.Node {
	header := core.Text{Content: title, Class: "text-xs font-medium text-zinc-500"}
	if !slices.ContainsFunc(rows, func(node core.Node) bool { return node != nil }) {
		return nil
	}
	return core.View{Class: desktopInfoCard, Children: append([]core.Node{header}, rows...)}
}

func desktopPlayerFact(label, value string) core.Node {
	if value == "" {
		return nil
	}
	return core.View{Class: "flex-row items-start justify-between gap-3", Children: []core.Node{
		core.Text{Content: label, Class: "shrink-0 text-xs text-zinc-400"},
		core.Text{Content: value, Class: "min-w-0 shrink text-right text-xs text-white", MaxLines: 2},
	}}
}
