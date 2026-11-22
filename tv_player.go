package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

const (
	tvPlayerHintKey = "tv-player-hint-count-v1"
	tvSeekStep      = 10 * time.Second
	tvSeekDelay     = 650 * time.Millisecond
)

type tvPlayerControls struct {
	current            route
	position, duration *core.Signal[time.Duration]
	paused, canRate    *core.Signal[bool]
	rate               *core.Signal[float32]
	bitrate            func() int
	seek               func(time.Duration)
	toggle             func()
	stream             func() media.Stream
	client             media.Client
	tracks             trackControls
	comments           danmakuControls
	qualityAvailable   bool
	hints              bool
	next               core.Node
}

func (a *application) showTVPlayerHints() bool {
	stored, _ := a.storage.GetItem(tvPlayerHintKey)
	count, _ := strconv.Atoi(stored)
	if count >= 3 {
		return false
	}
	_ = a.storage.SetItem(tvPlayerHintKey, strconv.Itoa(max(0, count)+1))
	return true
}

func (a *application) tvPlayer(cx *core.Scope, p tvPlayerControls, settings *core.Signal[bool]) core.Node {
	l := tvLayoutFor(cx)
	osd := core.NewSignal(cx, true)
	scrub := core.NewSignal(cx, time.Duration(-1))
	focus := &core.Ref{}
	focusPlayer := func() {
		if !p.tracks.panelOpen() && !p.comments.panelOpen() {
			cx.Post(focus.Focus)
		}
	}
	cx.Post(focus.Focus)
	var hideOSD, commitSeek func() bool
	wake := func() {
		osd.Set(true)
		cx.Post(func() {
			if hideOSD != nil {
				hideOSD()
			}
			hideOSD = cx.AfterFunc(3*time.Second, func() {
				if !settings.GetUntracked() && !p.paused.GetUntracked() && !p.tracks.panelOpen() && !p.comments.panelOpen() && scrub.GetUntracked() < 0 {
					osd.Set(false)
				}
			})
		})
	}
	cx.Effect(func() {
		p.paused.Get()
		p.tracks.panelOpen()
		p.comments.panelOpen()
		wake()
	})
	seekBy := func(delta time.Duration) {
		length := p.duration.GetUntracked()
		if length <= 0 {
			return
		}
		at := scrub.GetUntracked()
		if at < 0 {
			at = p.position.GetUntracked()
		}
		scrub.Set(max(0, min(length, at+delta)))
		// The seek survives opening settings, which removes the timeline's key bindings.
		cx.Post(func() {
			if commitSeek != nil {
				commitSeek()
			}
			commitSeek = cx.AfterFunc(tvSeekDelay, func() {
				p.seek(scrub.GetUntracked())
				scrub.Set(-1)
				wake()
			})
		})
		wake()
	}
	toggle := func() { p.toggle(); wake() }
	activateTracks := func() { p.tracks.show(trackSubtitleTab); wake() }
	openSettings := func() { settings.Set(true); wake() }
	closeSettings := func() { settings.Set(false); focusPlayer(); wake() }
	cx.OnBackPress(func() bool {
		if settings.GetUntracked() {
			closeSettings()
			return true
		}
		return false
	})
	cx.OnKey("escape", func() {
		if settings.GetUntracked() {
			closeSettings()
		} else {
			a.back()
		}
	})
	cx.OnKey("menu", func() {
		if settings.GetUntracked() {
			closeSettings()
		} else {
			openSettings()
		}
	})
	cx.OnKey("mediaplaypause", toggle)
	cx.OnKeyWith("mediarewind", core.KeyOptions{Repeat: true}, func() { seekBy(-tvSeekStep) })
	cx.OnKeyWith("mediafastforward", core.KeyOptions{Repeat: true}, func() { seekBy(tvSeekStep) })
	cx.Effect(func() {
		if settings.Get() {
			return
		}
		options := core.KeyOptions{When: core.KeyNotEditing, Repeat: true}
		cx.OnKeyWith("left", options, func() { seekBy(-tvSeekStep) })
		cx.OnKeyWith("right", options, func() { seekBy(tvSeekStep) })
		cx.OnKey("up", activateTracks)
		cx.OnKey("down", openSettings)
		cx.OnKeyWith("enter", core.KeyOptions{When: core.KeyNotEditing}, toggle)
	})
	cx.Cleanup(func() {
		if hideOSD != nil {
			hideOSD()
		}
		if commitSeek != nil {
			commitSeek()
		}
	})
	footer := p.tvPlayerFooter(cx, l, scrub, p.hints, settings.Get)
	top := p.tvPlayerHeader(cx, l)
	showChrome := func() bool { return osd.Get() || settings.Get() }
	apple := cx.Env().OS() == core.OSIOS
	surfaceClass := "absolute inset-0"
	if apple {
		surfaceClass = "absolute top-1/2 left-1/2 size-16"
		cx.Effect(func() {
			if !settings.Get() && !p.tracks.panelOpen() && !p.comments.panelOpen() {
				cx.Post(focus.Focus)
			}
		})
	}
	layer := core.View{Class: "absolute inset-0", TestID: "tv-player", PointerEvents: core.PointerEventsBoxNone,
		Children: []core.Node{core.View{Class: surfaceClass, TestID: "toggle-play", Role: core.RoleButton, Ref: focus,
			LabelFunc: func() string {
				if p.paused.Get() {
					return locales.Play(cx)
				}
				return locales.Pause(cx)
			}, OnPress: func(core.PressEvent) { toggle() }},
		}}
	layer.Children = append(layer.Children, core.Show{When: showChrome, Then: func(*core.Scope) core.Node {
		return core.View{Class: "absolute inset-0", TestID: "tv-player-osd", PointerEvents: core.PointerEventsBoxNone, Children: []core.Node{
			core.View{Class: "absolute inset-x-0 top-0 bg-linear-to-b from-black/75 via-black/35 to-transparent",
				Style: core.Style{Height: l.length(288)}, PointerEvents: core.PointerEventsNone},
			core.View{Class: "absolute inset-x-0 bottom-0 bg-linear-to-t from-black/85 via-black/45 to-transparent",
				Style: core.Style{Height: l.length(420)}, PointerEvents: core.PointerEventsNone},
			top, footer,
		}}
	}}, core.Show{When: settings.Get, Then: func(cx *core.Scope) core.Node {
		return a.tvPlayerSettings(cx, l, p, closeSettings)
	}})
	if apple {
		layer.Children = append(layer.Children, core.Show{When: func() bool {
			return !settings.Get() && !p.tracks.panelOpen() && !p.comments.panelOpen()
		}, Then: func(cx *core.Scope) core.Node {
			return tvAppleRemoteTargets(cx, focus, activateTracks, openSettings, func() { seekBy(-tvSeekStep) }, func() { seekBy(tvSeekStep) })
		}})
	}
	return layer
}

func (p tvPlayerControls) tvPlayerHeader(cx *core.Scope, l tvLayout) core.Node {
	item := p.current.item
	title := item.Title
	var episode string
	if item.SeriesTitle != "" && item.Kind == media.Episode {
		title, episode = item.SeriesTitle, item.Title
		if item.Season > 0 && item.Episode > 0 {
			episode = locales.HomeEpisode(cx, strconv.Itoa(item.Season), strconv.Itoa(item.Episode)) + " · " + episode
		}
	}
	heading := l.text(title, 36, "text-4xl font-bold text-white")
	heading.TestID = "player-title"
	names := []core.Node{heading}
	if episode != "" {
		names = append(names, l.text(episode, 24, "text-zinc-300"))
	}
	dot, _ := tvProviderStyle(p.current.source.Provider)
	mode := l.text("", 18, "text-white")
	mode.TestID = "player-mode"
	mode.ContentFunc = func() string {
		if p.current.downloadID != "" {
			return locales.DownloadOfflinePlayback(cx)
		}
		return p.current.source.Name + " · " + playMethod(cx, p.stream().Method)
	}
	modeBadge := tvPlayerPill(l, "border-white/10 bg-black/45", core.View{Class: "rounded-full " + dot,
		Style: core.Style{Width: l.length(10), Height: l.length(10)}}, mode)
	formats := func() string {
		stream := p.stream()
		var labels []string
		if res := detailResolution(stream.Rendition); res != "" {
			labels = append(labels, res)
		}
		if hdr := detailVideoRange(stream.Rendition); hdr != "" && stream.Method != media.Transcode {
			labels = append(labels, hdr)
		}
		return strings.Join(labels, " ")
	}
	badges := []core.Node{modeBadge, core.Show{When: func() bool { return formats() != "" }, Then: func(*core.Scope) core.Node {
		format := l.text("", 18, "font-medium text-white")
		format.ContentFunc, format.TestID = formats, "tv-player-format"
		return tvPlayerPill(l, "border-white/10 bg-black/45", format)
	}}, core.Show{When: func() bool { return p.comments.enabled() && p.comments.count() > 0 }, Then: func(cx *core.Scope) core.Node {
		badge := l.text("", 18, "text-pink-100")
		badge.ContentFunc = func() string { return locales.TvPlayerDanmakuCount(cx, strconv.Itoa(p.comments.count())) }
		return tvPlayerPill(l, "border-pink-400/30 bg-pink-500/25", l.icon(lucide.MessageSquare(), 20, "text-pink-200"), badge)
	}}, tvClock(cx, l)}
	return core.View{Class: "absolute flex-row items-start justify-between", PointerEvents: core.PointerEventsNone,
		Style: core.Style{Top: l.length(48), Left: l.length(96), Right: l.length(96), Gap: l.points(24)},
		Children: []core.Node{
			core.View{Class: "min-w-0 shrink", Style: core.Style{Gap: l.points(8)}, Children: names},
			core.View{Class: "shrink-0 flex-row items-center", Style: core.Style{Gap: l.points(12)}, Children: badges},
		}}
}

func tvPlayerPill(l tvLayout, color string, parts ...core.Node) core.Node {
	return core.View{Class: "flex-row items-center rounded-full border " + color,
		Style: core.Style{Padding: l.padding(16, 8), Gap: l.points(8)}, Children: parts}
}
