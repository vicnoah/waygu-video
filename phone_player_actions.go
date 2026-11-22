package main

import (
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type phonePlayerAction uint8

const (
	playerActionMenu phonePlayerAction = iota
	playerActionInfo
	playerActionChapter
	playerActionLoop
	playerActionSleep
)

func (p phonePlayerControls) morePanel(cx *core.Scope, open *core.Signal[bool], chapters func() []media.Chapter, playbackInfo core.Node) core.Node {
	page := core.NewSignal(cx, playerActionMenu)
	cx.Effect(func() {
		if !open.Get() {
			page.Set(playerActionMenu)
		}
	})
	title := locales.PhonePlayerMore(cx)
	goTo := func(destination phonePlayerAction) func() {
		return func() { page.Set(destination) }
	}
	heading := core.Dynamic(func(cx *core.Scope) core.Node {
		var name string
		switch page.Get() {
		case playerActionInfo:
			name = locales.PhonePlayerInfo(cx)
		case playerActionChapter:
			name = locales.PhonePlayerChapters(cx)
		case playerActionLoop:
			name = locales.PhonePlayerLoop(cx)
		case playerActionSleep:
			name = locales.PhonePlayerSleep(cx)
		default:
			return panelHeading(title)
		}
		return core.View{Class: "min-w-0 flex-1 flex-row items-center gap-2", Children: []core.Node{
			playerIconButton(cx, "phone-player-more-back", title, "size-8 rounded-full bg-white/10", "size-4 text-zinc-300",
				lucide.ChevronLeft, nil, goTo(playerActionMenu)),
			panelHeading(name),
		}}
	})
	unavailable := func() bool { return true }
	row := func(id, label string, icon func() *core.SVG, disabled func() bool, action func()) core.Node {
		return playerPanelAction(cx, "phone-player-action-"+id, label, icon, true, disabled, action)
	}
	menu := core.View{Class: "gap-px overflow-hidden rounded-xl border border-white/10 bg-white/10", Children: []core.Node{
		row("screenshot", locales.PhonePlayerScreenshot(cx), lucide.Camera, unavailable, nil),
		row("loop", locales.PhonePlayerLoop(cx), lucide.Repeat2, func() bool { return !p.ready() || p.duration.Get() <= 0 }, goTo(playerActionLoop)),
		row("info", locales.PhonePlayerInfo(cx), lucide.Info, nil, goTo(playerActionInfo)),
		row("chapters", locales.PhonePlayerChapters(cx), lucide.BookOpen, func() bool { return len(chapters()) == 0 }, goTo(playerActionChapter)),
		row("av-sync", locales.PhonePlayerAVSync(cx), lucide.AudioLines, unavailable, nil),
		row("sleep", locales.PhonePlayerSleep(cx), lucide.Timer, nil, goTo(playerActionSleep)),
	}}
	content := core.Dynamic(func(cx *core.Scope) core.Node {
		switch page.Get() {
		case playerActionInfo:
			return playbackInfo
		case playerActionChapter:
			return p.chapterActions(cx, chapters, open)
		case playerActionLoop:
			return p.loopActions(cx)
		case playerActionSleep:
			return p.sleepActions(cx, open)
		default:
			return menu
		}
	})
	return playerSidePanel(cx, "phone-player-info-panel", "phone-player-info-close", title, open.Get,
		func() { open.Set(false) }, heading, nil, content)
}

func (p phonePlayerControls) loopActions(cx *core.Scope) core.Node {
	segment := p.loop.segment.Get
	mark := func(id, label string, disabled func() bool, action func()) core.Node {
		return playerPanelAction(cx, id, label, lucide.Pin, false, disabled, action)
	}
	return core.View{Class: "gap-2", Children: []core.Node{
		core.Show{When: func() bool { return segment().hasStart }, Then: func(*core.Scope) core.Node {
			return core.Text{TestID: "phone-player-loop-range", Class: "rounded-lg bg-violet-500/15 px-3 py-2 text-sm font-semibold text-violet-200",
				ContentFunc: func() string {
					value := segment()
					start := landscapeClock(value.start)
					if value.active {
						return locales.PhonePlayerLoopRange(cx, start, landscapeClock(value.end))
					}
					return locales.PhonePlayerLoopStartAt(cx, start)
				}}
		}},
		mark("phone-player-loop-start", locales.PhonePlayerLoopStart(cx), func() bool { return !p.ready() }, func() {
			p.loop.markStart(p.position.GetUntracked())
		}),
		mark("phone-player-loop-end", locales.PhonePlayerLoopEnd(cx), func() bool {
			return !p.ready() || !segment().hasStart || p.position.Get()-segment().start < minSegmentDuration
		}, func() { p.loop.markEnd(p.position.GetUntracked(), p.duration.GetUntracked()) }),
		playerPanelAction(cx, "phone-player-loop-clear", locales.PhonePlayerLoopClear(cx), lucide.X, false,
			func() bool { return !segment().hasStart }, func() { p.loop.clear() }),
	}}
}

func (p phonePlayerControls) sleepActions(cx *core.Scope, open *core.Signal[bool]) core.Node {
	label := locales.PhonePlayerSleep(cx)
	var rows []core.Node
	for _, minutes := range []int{0, 15, 30, 45, 60} {
		name := locales.PhonePlayerSleepMinutes(cx, strconv.Itoa(minutes))
		if minutes == 0 {
			name = locales.PhonePlayerSleepOff(cx)
		}
		delay := time.Duration(minutes) * time.Minute
		item := ui.Radio{Text: name, TestID: "phone-player-sleep-" + strconv.Itoa(minutes),
			Checked: func() bool { return p.sleepTimer.Get().delay == delay },
			OnChange: func(bool) {
				timer := playerSleepTimer{}
				if delay > 0 {
					timer = playerSleepTimer{delay: delay, deadline: cx.Now().Add(delay)}
				}
				p.sleepTimer.Set(timer)
				open.Set(false)
			}}
		rows = append(rows, playerRadio(cx, item, "", false))
	}
	return playerRadioList(cx, label, rows)
}
