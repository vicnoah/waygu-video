package main

import (
	"math"
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

type phonePlayerControls struct {
	current                route
	position, duration     *core.Signal[time.Duration]
	paused, canRate        *core.Signal[bool]
	rate                   *core.Signal[float32]
	pictureMode            *core.Signal[playerPictureMode]
	picture                *core.Signal[core.Size]
	pip                    *core.Signal[core.PictureInPictureState]
	seek                   func(time.Duration)
	toggle, back, startPiP func()
	volume, brightness     *core.Signal[float32]
	playbackBlocked        func() bool
	prefs                  playbackPreferences
	stream                 func() media.Stream
	client                 media.Client
	loop                   *segmentLoop
	sleepTimer             *core.Signal[playerSleepTimer]
	ready                  func() bool
	cast                   *playerCast
	next                   nextEpisodeControls
	comments               danmakuControls
	tracks                 trackControls
}

func (p phonePlayerControls) render(cx *core.Scope) core.Node {
	show := core.NewSignal(cx, true)
	locked := core.NewSignal(cx, false)
	info := core.NewSignal(cx, false)
	aspect := core.NewSignal(cx, false)
	preview := core.NewSignal(cx, time.Duration(-1))
	railDrag := core.NewSignal(cx, false)
	gesturesDisabled := func() bool {
		return locked.Get() || info.Get() || aspect.Get() || p.comments.panelOpen() || p.tracks.panelOpen() || p.playbackBlocked() || p.cast.open.Get()
	}
	frames := playerPreviewFrames(cx, p.current, p.client, p.stream, func() time.Duration {
		if at := preview.Get(); at >= 0 && !gesturesDisabled() {
			return at.Truncate(time.Second)
		}
		return -1
	})
	chapters := p.chapters(cx)
	quickRates := []float32{1, 1.25, 1.5, 2}
	rateTap := func() {
		now := p.rate.GetUntracked()
		next := slices.IndexFunc(quickRates, func(candidate float32) bool { return candidate > now })
		if next < 0 {
			next = 0
		}
		p.rate.Set(quickRates[next])
	}
	scrubChanged := func(value base.SliderValue) {
		railDrag.Set(true)
		preview.Set(time.Duration(value.End * float64(time.Second)))
	}
	seekCommit := func(value base.SliderValue) {
		p.seek(time.Duration(value.End * float64(time.Second)))
		railDrag.Set(false)
		preview.Set(-1)
	}
	gesturePlane, gestureHUD := p.gestureSurface(cx, show, preview, gesturesDisabled)
	shades := core.View{Class: "absolute inset-0", PointerEvents: core.PointerEventsNone, Children: []core.Node{
		core.View{Class: "absolute inset-x-0 top-0 h-28 bg-linear-to-b from-black/75 via-black/35 to-transparent"},
		core.View{Class: "absolute inset-x-0 bottom-0 h-40 bg-linear-to-t from-black/85 via-black/45 to-transparent"},
	}}
	name := core.View{Class: "min-w-0 flex-1 gap-1", Children: []core.Node{
		core.Text{Content: p.current.item.Title, TestID: "player-title", MaxLines: 1, Class: "text-base font-semibold text-white"},
		core.View{Class: "min-w-0 flex-row items-center gap-1.5", Children: []core.Node{
			core.Show{When: func() bool { return p.height(cx) > 0 }, Then: func(*core.Scope) core.Node {
				return core.Text{ContentFunc: func() string { return strconv.Itoa(p.height(cx)) + "p" }, TestID: "phone-player-resolution",
					Class: "rounded bg-white/15 px-1 py-px text-[9px] font-bold text-white"}
			}},
			core.Show{When: func() bool {
				active := p.stream()
				return active.Method == media.DirectPlay && meaningfulVideoRange(active.Rendition.VideoRange)
			}, Then: func(*core.Scope) core.Node {
				return core.Text{ContentFunc: func() string { return p.stream().Rendition.VideoRange },
					Class: "rounded bg-amber-400/20 px-1 py-px text-[9px] font-bold text-amber-300"}
			}},
			core.Text{ContentFunc: func() string {
				if p.current.downloadID != "" {
					return locales.DownloadOfflinePlayback(cx)
				}
				return locales.PhonePlayerSource(cx, p.current.source.Name)
			}, TestID: "player-mode", MaxLines: 1, Class: "min-w-0 shrink text-[11px] text-zinc-300"},
		}},
	}}
	tools := core.View{Class: "h-9 shrink-0 flex-row items-center gap-1 rounded-full border border-white/10 bg-white/15 px-1", Children: []core.Node{
		playerIconButton(cx, "phone-player-cast", locales.PhonePlayerCast(cx), "size-8 rounded-full", "size-[18px] text-white", lucide.Cast,
			func() bool { return !p.cast.available }, func() { p.cast.open.Set(true) }),
		playerIconButton(cx, "phone-player-pip", locales.SettingsAutoPiP(cx), "size-8 rounded-full", "size-[18px] text-white", lucide.PictureInPicture2,
			func() bool {
				state := p.pip.Get()
				return p.cast.active() || !state.Supported || !state.Possible || state.Active
			}, p.startPiP),
		playerIconButton(cx, "phone-player-info", locales.PhonePlayerMore(cx), "size-8 rounded-full", "size-[18px] text-white", lucide.Ellipsis, nil, func() { info.Set(true) }),
	}}
	header := core.View{Class: "absolute inset-x-0 top-0 flex-row items-center gap-3 px-6 pt-4",
		Style: core.Style{PaddingEnv: core.EnvEdges{Left: core.EnvSafeArea, Right: core.EnvSafeArea, Top: core.EnvSafeArea}},
		Children: []core.Node{
			playerIconButton(cx, "close-player", locales.ClosePlayer(cx), "size-9 rounded-full border border-white/10 bg-white/15", "size-5 text-white", lucide.ChevronLeft, nil, p.back),
			name, tools,
		},
	}
	center := core.View{Class: "absolute inset-0 items-center justify-center", PointerEvents: core.PointerEventsBoxNone, Children: []core.Node{
		core.View{Class: "flex-row items-center gap-10 min-[800px]:translate-x-6 min-[800px]:-translate-y-[5px]", PointerEvents: core.PointerEventsBoxNone, Children: []core.Node{
			p.skip(cx, "player-seek-back", locales.SeekBack(cx), p.prefs.BackSeconds, lucide.RotateCcw, -p.prefs.backStep()),
			p.play(cx, "toggle-play", "size-[68px] rounded-full border border-white/15 bg-white/20 shadow-2xl", "size-8 text-white"),
			p.skip(cx, "player-seek-forward", locales.SeekForward(cx), p.prefs.NextSeconds, lucide.RotateCw, p.prefs.forwardStep()),
		}},
	}}
	visualPosition := func() time.Duration {
		if at := preview.Get(); at >= 0 {
			return at
		}
		return p.position.Get()
	}
	scrubber := core.View{Class: "flex-row items-center gap-3", Children: []core.Node{
		core.Text{TestID: "player-position", Class: "text-xs font-medium text-white", ContentFunc: func() string { return landscapeClock(visualPosition()) }},
		p.timeline(cx, base.Slider{Label: locales.PlaybackPosition(cx), TestID: "phone-player-position-rail", ThumbTestID: "phone-player-position-thumb",
			Value: func() base.SliderValue { return base.SingleValue(visualPosition().Seconds()) },
			Max:   func() float64 { return p.duration.Get().Seconds() }, Step: func() float64 { return 1 },
			Disabled: func() bool { return p.duration.Get() <= 0 }, OnChange: scrubChanged, OnChangeEnd: seekCommit,
		}, chapters, railDrag, preview, frames),
		core.Text{TestID: "phone-player-duration", Class: "text-xs font-medium text-zinc-300",
			ContentFunc: func() string { return landscapeClock(p.duration.Get()) }},
	}}
	danmakuPill := core.View{Class: "h-8 shrink flex-row items-center gap-2 rounded-full border border-white/10 bg-white/15 pr-3 pl-1", Children: []core.Node{
		base.Button{TestID: "toggle-danmaku", Label: locales.ShowDanmaku(cx), Selected: p.comments.enabled, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
			Class:   "size-6 shrink-0 items-center justify-center rounded-full bg-white/15 selected:bg-linear-to-br selected:from-violet-500 selected:to-fuchsia-500",
			OnPress: func(core.PressEvent) { p.comments.toggle() }, Children: []core.Node{core.Text{Content: locales.PhonePlayerDanmakuBadge(cx), Class: "text-[11px] font-bold text-white"}}},
		base.Button{TestID: "load-danmaku", Label: locales.AddDanmaku(cx), Class: "h-7 min-w-0 shrink flex-row items-center rounded",
			OnPress: func(core.PressEvent) { p.comments.importFile() }, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
			Children: []core.Node{core.Text{Content: locales.AddDanmaku(cx), MaxLines: 1, Class: "w-36 text-xs text-zinc-300"}}},
	}}
	options := core.View{Class: "h-8 shrink-0 flex-row items-center gap-0.5 rounded-full border border-white/10 bg-white/10 px-1", Children: []core.Node{
		playerLabeledAction(cx, "danmaku-sources", locales.PhonePlayerDanmakuSettings(cx), lucide.SlidersHorizontal, p.cast.active, p.comments.openPanel),
		playerLabeledAction(cx, "player-tracks", locales.PhonePlayerTracks(cx), lucide.Captions, p.cast.active, func() { p.tracks.show(trackSubtitleTab) }),
		base.Button{TestID: "phone-player-rate", Label: locales.PlaybackSpeed(cx), Disabled: func() bool { return !p.canRate.Get() || p.cast.active() },
			Class: "h-7 items-center justify-center rounded-full px-2.5 disabled:opacity-45", Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
			OnPress: func(core.PressEvent) { rateTap() }, OnLongPress: func(core.PressEvent) { p.tracks.show(trackSpeedTab) },
			Children: []core.Node{core.Text{ContentFunc: func() string { return playbackRateLabel(p.rate.Get()) }, Class: "text-xs font-semibold text-white"}}},
		playerIconButton(cx, "phone-player-fit", locales.PhonePlayerPictureRatio(cx), "h-7 w-9 rounded-full", "size-4 text-white", lucide.Scan, p.cast.active, func() { aspect.Set(true) }),
	}}
	footer := core.View{Class: "absolute inset-x-0 bottom-0 gap-2.5 px-6 pb-4",
		Style: core.Style{PaddingEnv: core.EnvEdges{Left: core.EnvSafeArea, Right: core.EnvSafeArea, Bottom: core.EnvSafeArea}},
		Children: []core.Node{
			p.next.card, scrubber,
			core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
				p.play(cx, "phone-player-play-small", "size-9", "size-5 text-white"),
				playerIconButton(cx, "phone-player-next", locales.NextEpisode(cx), "size-9", "size-5 text-white", lucide.SkipForward, p.next.unavailable, p.next.play),
				danmakuPill, core.View{Class: "flex-1"}, options,
			}},
		},
	}
	lockButton := func(isLocked bool) core.Node {
		label, icon, id := locales.PhonePlayerLock(cx), lucide.LockOpen, "phone-player-lock"
		if isLocked {
			label, icon, id = locales.PhonePlayerUnlock(cx), lucide.Lock, "phone-player-unlock"
		}
		return core.View{Class: "absolute inset-y-0 left-0 justify-center pl-6 min-[800px]:-translate-y-[9px]", PointerEvents: core.PointerEventsBoxNone,
			Style: core.Style{PaddingEnv: core.EnvEdges{Left: core.EnvSafeArea}},
			Children: []core.Node{playerIconButton(cx, id, label, "size-10 rounded-full border border-white/10 bg-black/35",
				"size-[18px] text-white", icon, nil, func() { locked.Set(!isLocked); show.Set(true) })},
		}
	}
	playbackInfo := core.Dynamic(func(cx *core.Scope) core.Node {
		active := p.stream()
		details := []string{playMethod(cx, active.Method)}
		if height := p.height(cx); height > 0 {
			details = append(details, strconv.Itoa(height)+"p")
		}
		if active.Method != media.Transcode && active.Rendition.VideoCodec != "" {
			details = append(details, strings.ToUpper(active.Rendition.VideoCodec))
		}
		return core.View{Class: "gap-2 rounded-xl border border-white/10 bg-zinc-800/70 p-3", Children: []core.Node{
			core.Text{Content: p.current.item.Title, Class: "text-sm font-semibold text-white"},
			core.Text{Content: strings.Join(details, " · "), Class: "text-xs text-zinc-300"},
		}}
	})
	return core.View{Class: "absolute inset-0", TestID: "phone-player", PointerEvents: core.PointerEventsBoxNone, Children: []core.Node{
		gesturePlane,
		core.Show{When: func() bool {
			return show.Get() && !gesturesDisabled()
		}, Then: func(*core.Scope) core.Node {
			return core.Group([]core.Node{shades, header, center, lockButton(false), footer})
		}},
		core.Show{When: locked.Get, Then: func(*core.Scope) core.Node { return lockButton(true) }},
		gestureHUD,
		core.Show{When: func() bool { return preview.Get() >= 0 && !railDrag.Get() && !gesturesDisabled() }, Then: func(*core.Scope) core.Node {
			return core.View{Class: "absolute inset-0 items-center justify-center", PointerEvents: core.PointerEventsNone, Children: []core.Node{
				phoneSeekPreview(preview.Get, frames),
			}}
		}},
		p.morePanel(cx, info, chapters, playbackInfo),
		p.picturePanel(cx, aspect),
	}}
}

func (p phonePlayerControls) height(cx *core.Scope) int {
	stream := p.stream()
	if stream.Method == media.DirectPlay && stream.Rendition.Height > 0 {
		return stream.Rendition.Height
	}
	if height := p.picture.Get().Height; height > 0 {
		// Android reports the video's natural size in density-scaled points.
		if cx.Env().OS() == core.OSAndroid {
			return int(math.Round(float64(height) * float64(cx.Env().PixelRatio())))
		}
		return int(height)
	}
	return stream.Rendition.Height
}

func meaningfulVideoRange(r string) bool {
	return r != "" && !strings.EqualFold(r, "SDR") && !strings.EqualFold(r, "Unknown")
}

func (p phonePlayerControls) skip(cx *core.Scope, id, label string, seconds int, icon func() *core.SVG, interval time.Duration) core.Node {
	return base.Button{TestID: id, Label: label, Class: "relative size-12 items-center justify-center rounded-full border border-white/10 bg-black/30",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { p.seek(p.position.GetUntracked() + interval) },
		Children: []core.Node{core.Icon{SVG: icon(), Class: "size-6 text-white"},
			core.Text{Content: strconv.Itoa(seconds), Class: "absolute text-center text-[8px] font-bold text-white"}},
	}
}

func (p phonePlayerControls) play(cx *core.Scope, id, class, iconClass string) core.Node {
	return base.Button{TestID: id, LabelFunc: func() string {
		if p.paused.Get() {
			return locales.Play(cx)
		}
		return locales.Pause(cx)
	}, Class: "shrink-0 items-center justify-center " + class, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress: func(core.PressEvent) { p.toggle() }, Children: []core.Node{core.Dynamic(func(*core.Scope) core.Node {
			icon := lucide.Pause
			if p.paused.Get() {
				icon = lucide.Play
			}
			return core.Icon{SVG: icon(), Class: iconClass}
		})}}
}

func playerLabeledAction(cx *core.Scope, id, label string, icon func() *core.SVG, disabled func() bool, action func()) core.Node {
	return base.Button{TestID: id, Label: label, Disabled: disabled, Class: "h-7 shrink-0 flex-row items-center gap-1.5 rounded-full px-2.5 disabled:opacity-45",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { action() },
		Children: []core.Node{core.Icon{SVG: icon(), Class: "size-4 text-white"},
			core.Text{Content: label, Class: "hidden text-xs text-white min-[680px]:flex"}},
	}
}

func landscapeClock(at time.Duration) string {
	if at < time.Hour {
		return "00:" + playbackClock(at)
	}
	value := playbackClock(at)
	if len(value) == 7 {
		return "0" + value
	}
	return value
}
