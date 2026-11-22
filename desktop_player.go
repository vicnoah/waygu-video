package main

import (
	"slices"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/internal/playback/mpv"
	"waygu-video/locales"
)

type desktopPlayerControls struct {
	current          route
	client           media.Client
	stream           func() media.Stream
	position         *core.Signal[time.Duration]
	duration         *core.Signal[time.Duration]
	paused           *core.Signal[bool]
	picture          *core.Signal[core.Size]
	rate, volume     *core.Signal[float32]
	canRate          func() bool
	seek             func(time.Duration)
	toggle           func()
	comments         danmakuControls
	tracks           trackControls
	next             nextEpisodeControls
	qualityAvailable bool
	stats            func() (mpv.Stats, bool)
	buffering        func() bool
}

type desktopPlayerTab string

const (
	desktopPlayerEpisodes desktopPlayerTab = "episodes"
	desktopPlayerDanmaku  desktopPlayerTab = "danmaku"
	desktopPlayerInfo     desktopPlayerTab = "info"
)

func (a *application) desktopPlayer(cx *core.Scope, p desktopPlayerControls, scene []core.Node) core.Node {
	episode := p.current.item.Kind == media.Episode
	start := desktopPlayerInfo
	if episode {
		start = desktopPlayerEpisodes
	}
	active := core.NewSignal(cx, start)
	shown := core.NewSignal(cx, core.Untracked(cx, func() bool { return cx.Env().Window().Width >= 1024 }))
	wide := core.NewMemo(cx, func() bool { return cx.Env().Window().Width >= 1024 })
	togglePanel := func() { shown.Update(func(value bool) bool { return !value }) }
	pickPanel := func(tab desktopPlayerTab) { active.Set(tab); shown.Set(true) }
	changeVolume := func(delta float32) {
		p.volume.Update(func(value float32) float32 { return min(1, max(0, value+delta)) })
	}
	keys := core.KeyOptions{When: core.KeyNotEditing}
	repeat := core.KeyOptions{When: core.KeyNotEditing, Repeat: true}
	oldFullScreen := core.Untracked(cx, cx.Env().FullScreen)
	back := func() {
		if !oldFullScreen && cx.Env().FullScreen() {
			cx.ToggleFullScreen()
		}
		a.back()
	}
	cx.OnKeyWith("space", keys, p.toggle)
	cx.OnKeyWith("left", repeat, func() { p.seek(p.position.GetUntracked() - desktopPlayerSeekStep) })
	cx.OnKeyWith("right", repeat, func() { p.seek(p.position.GetUntracked() + desktopPlayerSeekStep) })
	cx.OnKeyWith("up", repeat, func() { changeVolume(.05) })
	cx.OnKeyWith("down", repeat, func() { changeVolume(-.05) })
	cx.OnKeyWith("d", keys, p.comments.toggle)
	cx.OnKeyWith("s", keys, p.tracks.cycleSubtitle)
	cx.OnKeyWith("a", keys, p.tracks.cycleAudio)
	cx.OnKeyWith("[", keys, func() { p.stepRate(-1) })
	cx.OnKeyWith("]", keys, func() { p.stepRate(1) })
	cx.OnKeyWith("n", keys, func() {
		if p.next.play != nil {
			p.next.play()
		}
	})
	cx.OnKeyWith("f", keys, cx.ToggleFullScreen)
	cx.OnKeyWith("escape", keys, func() {
		if cx.Env().FullScreen() {
			cx.ToggleFullScreen()
		} else if shown.GetUntracked() {
			shown.Set(false)
		}
	})
	cx.OnKeyWith("tab", keys, togglePanel)

	disabled := func() bool { return true }
	header := core.View{Class: "absolute inset-x-0 top-0 h-32 flex-row items-start gap-4 bg-linear-to-b from-black/75 via-black/35 to-transparent px-6 pt-5",
		PointerEvents: core.PointerEventsBoxNone, Children: []core.Node{
			desktopPlayerButton(cx, "close-player", locales.ClosePlayer(cx), lucide.ChevronLeft(), desktopPlayerTopButton, "size-5", nil, back),
			p.title(cx),
			desktopPlayerButton(cx, "desktop-player-cast", locales.PhonePlayerCast(cx), lucide.Cast(), desktopPlayerTopButton, "size-5", disabled, nil),
			desktopPlayerButton(cx, "desktop-player-panel-toggle", locales.DesktopPlayerPanel(cx), lucide.PanelRight(), desktopPlayerTopButton, "size-5", nil, togglePanel),
		}}
	viewport := core.View{Class: "relative flex-1 min-w-0 overflow-hidden bg-black", TestID: "desktop-player-video-pane",
		Children: append(scene, header, p.transport(cx, func() { pickPanel(desktopPlayerDanmaku) }))}
	sidebar := core.Show{When: shown.Get, Then: func(cx *core.Scope) core.Node {
		return a.desktopPlayerSidebar(cx, p, active, wide.Get)
	}}
	return core.View{Class: "relative flex-1 flex-row bg-zinc-950", TestID: "desktop-player",
		Children: []core.Node{viewport, sidebar, p.tracks.dialog, p.comments.dialog}}
}

const desktopPlayerSeekStep = 10 * time.Second

const desktopPlayerTopButton = "size-10 rounded-full border border-white/10 bg-white/15"

func (p desktopPlayerControls) title(cx *core.Scope) core.Node {
	item := p.current.item
	title := item.Title
	if item.Kind == media.Episode && item.SeriesTitle != "" {
		title = item.SeriesTitle
	}
	facts := []core.Node{}
	if item.Kind == media.Episode {
		facts = append(facts, core.Text{Content: locales.DesktopPlayerEpisode(cx, strconv.Itoa(item.Season), strconv.Itoa(item.Episode)),
			Class: "text-sm text-zinc-300", MaxLines: 1})
	}
	facts = append(facts,
		core.Text{ContentFunc: func() string {
			if p.current.downloadID != "" {
				return locales.DownloadOfflinePlayback(cx)
			}
			return p.current.source.Name
		}, TestID: "player-mode", Class: "rounded bg-emerald-500/20 px-1.5 py-0.5 text-[10px] text-emerald-300", MaxLines: 1},
		core.Text{ContentFunc: func() string { return playMethod(cx, p.stream().Method) },
			Class: "rounded bg-white/15 px-1.5 py-0.5 text-[10px] text-white", MaxLines: 1},
		core.Show{When: func() bool { return p.format(cx) != "" }, Then: func(cx *core.Scope) core.Node {
			return core.Text{ContentFunc: func() string { return p.format(cx) },
				Class: "rounded bg-white/15 px-1.5 py-0.5 text-[10px] text-white", MaxLines: 1}
		}},
	)
	return core.View{Class: "min-w-0 flex-1 gap-0.5", PointerEvents: core.PointerEventsNone, Children: []core.Node{
		core.Text{Content: title, Class: "text-lg font-semibold text-white", TestID: "player-title", MaxLines: 1},
		core.View{Class: "flex-row flex-wrap items-center gap-2", Children: facts},
	}}
}

func (p desktopPlayerControls) format(cx *core.Scope) string {
	version := p.stream().Rendition
	if picture := p.picture.Get(); picture.Width > 0 && picture.Height > 0 {
		version.Width, version.Height = int(picture.Width), int(picture.Height)
	}
	if p.stream().Method == media.Transcode {
		version.VideoRange = ""
	}
	return desktopMovieFacts(detailResolution(version), desktopVideoRange(cx, version))
}

func (p desktopPlayerControls) transport(cx *core.Scope, showDanmaku func()) core.Node {
	var lastVolume float32 = .75
	volume := core.Dynamic(func(cx *core.Scope) core.Node {
		icon, label := lucide.Volume2(), locales.DesktopPlayerMute(cx)
		if p.volume.Get() == 0 {
			icon, label = lucide.VolumeX(), locales.DesktopPlayerUnmute(cx)
		}
		return desktopPlayerButton(cx, "desktop-player-mute", label, icon, "size-6", "size-5", nil, func() {
			if value := p.volume.GetUntracked(); value > 0 {
				lastVolume = value
				p.volume.Set(0)
			} else {
				p.volume.Set(lastVolume)
			}
		})
	})
	volumeRail := base.Slider{Class: "w-24 h-5 justify-center", TrackClass: "h-full justify-center", IndicatorClass: "h-1 rounded-full bg-white/20",
		RangeClass: "rounded-full bg-white", ThumbClass: "size-2 rounded-full bg-transparent", ThumbStyle: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		Label: locales.DesktopPlayerVolume(cx), TestID: "desktop-player-volume", ThumbTestID: "desktop-player-volume-thumb",
		Value: func() base.SliderValue { return base.SingleValue(float64(p.volume.Get()) * 100) }, Max: func() float64 { return 100 },
		Step: func() float64 { return 5 }, OnChange: func(value base.SliderValue) { p.volume.Set(float32(value.Start / 100)) }}
	next := p.next.play
	if next == nil {
		next = func() {}
	}
	play := core.Dynamic(func(cx *core.Scope) core.Node {
		label, icon := locales.Pause(cx), lucide.Pause()
		if p.paused.Get() {
			label, icon = locales.Play(cx), lucide.Play()
		}
		return desktopPlayerButton(cx, "toggle-play", label, icon, "size-8", "size-7", nil, p.toggle)
	})
	fullscreen := core.Dynamic(func(cx *core.Scope) core.Node {
		label, icon := locales.DesktopPlayerFullscreen(cx), lucide.Maximize()
		if cx.Env().FullScreen() {
			label, icon = locales.DesktopPlayerExitFullscreen(cx), lucide.Minimize()
		}
		return desktopPlayerButton(cx, "desktop-player-fullscreen", label, icon, "size-6", "size-5", nil, cx.ToggleFullScreen)
	})
	bar := core.View{Class: "flex-row flex-wrap items-center gap-3 pt-1", Children: []core.Node{
		core.View{Class: "shrink-0 flex-row items-center gap-4", Children: []core.Node{
			play,
			desktopPlayerButton(cx, "player-seek-back", locales.SeekBack(cx), lucide.RotateCcw(), "size-6", "size-5", nil,
				func() { p.seek(p.position.GetUntracked() - desktopPlayerSeekStep) }),
			desktopPlayerButton(cx, "player-seek-forward", locales.SeekForward(cx), lucide.RotateCw(), "size-6", "size-5", nil,
				func() { p.seek(p.position.GetUntracked() + desktopPlayerSeekStep) }),
			desktopPlayerButton(cx, "desktop-player-next", locales.NextEpisode(cx), lucide.SkipForward(), "size-6", "size-5", p.next.unavailable, next),
			core.View{Class: "flex-row items-center gap-2", Children: []core.Node{volume, volumeRail}},
			core.View{Class: "flex-row items-center gap-1", Children: []core.Node{
				core.Text{TestID: "player-position", ContentFunc: func() string { return playbackClock(p.position.Get()) }, Class: "text-sm text-zinc-300"},
				core.Text{ContentFunc: func() string { return "/ " + playbackClock(p.duration.Get()) }, Class: "text-sm text-zinc-300"},
			}},
		}},
		core.View{Class: "flex-1"},
		p.composer(cx),
		desktopPlayerButton(cx, "danmaku-sources", locales.Danmaku(cx), lucide.SlidersHorizontal(), "size-9 rounded-full border border-white/10 bg-white/15",
			"size-4", nil, showDanmaku),
		core.View{Class: "flex-1"},
		core.View{Class: "shrink-0 flex-row items-center gap-4", Children: []core.Node{
			desktopPlayerButton(cx, "player-tracks", locales.Subtitles(cx), lucide.Captions(), "size-6", "size-5", nil, func() { p.tracks.show(trackSubtitleTab) }),
			desktopPlayerButton(cx, "desktop-player-audio", locales.DetailAudio(cx), lucide.AudioLines(), "size-6", "size-5", nil, func() { p.tracks.show(trackAudioTab) }),
			base.Button{Label: locales.PlaybackSpeed(cx), TestID: "desktop-player-speed", Class: "shrink-0 rounded",
				Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { p.tracks.show(trackSpeedTab) },
				Children: []core.Node{core.Text{Class: "text-sm font-semibold text-white", ContentFunc: func() string { return playbackRateLabel(p.rate.Get()) }}}},
			desktopPlayerButton(cx, "desktop-player-pip", locales.SettingsAutoPiP(cx), lucide.PictureInPicture2(), "size-6", "size-5", func() bool { return true }, nil),
			fullscreen,
		}},
	}}
	return core.View{Class: "absolute inset-x-0 bottom-0 gap-2 bg-linear-to-t from-black/85 via-black/50 to-transparent px-6 pt-24 pb-5",
		PointerEvents: core.PointerEventsBoxNone, Children: []core.Node{p.next.card, p.timeline(cx), bar}}
}

func desktopPlayerButton(cx *core.Scope, id, label string, icon *core.SVG, class, size string, disabled func() bool, action func()) core.Node {
	return base.Button{Class: "shrink-0 items-center justify-center disabled:opacity-45 " + class, Label: label, Tooltip: core.Text{Content: label}, TestID: id,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, Disabled: disabled,
		OnPress: func(core.PressEvent) {
			if action != nil {
				action()
			}
		}, Children: []core.Node{core.Icon{SVG: icon, Class: size + " text-white"}}}
}

func (p desktopPlayerControls) stepRate(direction int) {
	if !p.canRate() {
		return
	}
	value := p.rate.GetUntracked()
	index, found := slices.BinarySearch(playbackRates, value)
	if found {
		index += direction
	} else if direction < 0 {
		index--
	}
	p.rate.Set(playbackRates[min(len(playbackRates)-1, max(0, index))])
}
