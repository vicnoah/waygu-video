package main

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

const parentalUsageUnavailable parentalLimit = parentalQuietHours + 1

func (a *application) parentalViewing(cx *core.Scope, playing func() bool, pause func()) (func() bool, core.Node) {
	status, _ := a.parental.GetUntracked().viewingLimit(cx.WallTime(), a.parentalUsage.GetUntracked())
	block := core.NewSignal(cx, status)
	failure := core.NewSignal[error](cx, nil)
	pin, askPIN := a.parentalPINPrompt(cx)
	setUsage := func(usage parentalUsage) {
		failure.Set(storeSettings(a.storage, parentalUsageKey, a.parentalUsage, usage))
	}
	refresh := func() {
		status, _ := a.parental.GetUntracked().viewingLimit(cx.WallTime(), a.parentalUsage.GetUntracked())
		if failure.GetUntracked() != nil {
			status = parentalUsageUnavailable
		}
		block.Set(status)
		if status != parentalCanPlay {
			pause()
		}
	}
	cx.Effect(func() {
		prefs := a.parental.Get()
		if !prefs.Enabled || !prefs.TimeLimit {
			block.Set(parentalCanPlay)
			return
		}
		started := cx.Now()
		running := playing() && block.GetUntracked() == parentalCanPlay
		charge := func() {
			if !running {
				return
			}
			instant, now := cx.Now(), cx.WallTime()
			elapsed := instant.Sub(started)
			started = instant
			year, month, day := now.Date()
			midnight := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
			elapsed = min(elapsed, now.Sub(midnight))
			if elapsed <= 0 {
				return
			}
			usage := a.parentalUsage.GetUntracked().onDay(now)
			usage.Used += elapsed
			setUsage(usage)
		}
		refresh()
		var stop func() bool
		var tick func()
		tick = func() {
			charge()
			refresh()
			stop = cx.AfterFunc(time.Second, tick)
		}
		stop = cx.AfterFunc(time.Second, tick)
		cx.Cleanup(func() {
			stop()
			charge()
		})
	})
	blocked := func() bool { return block.Get() != parentalCanPlay }
	message := func() string {
		switch block.Get() {
		case parentalQuietHours:
			return locales.ParentalQuietBlocked(cx)
		case parentalUsageUnavailable:
			return locales.ParentalUsageUnavailable(cx)
		default:
			return locales.ParentalLimitReached(cx)
		}
	}
	overlay := core.Show{When: blocked, Then: func(cx *core.Scope) core.Node {
		return core.View{Class: "absolute inset-0 items-center justify-center gap-4 bg-black/90 p-8", TestID: "parental-playback-lock", Children: []core.Node{
			core.Icon{SVG: lucide.Hourglass(), Class: "size-10 text-violet-400"},
			core.Text{ContentFunc: message, Class: "text-center text-lg font-semibold text-white"},
			core.Show{When: func() bool { return block.Get() == parentalTimeUsed }, Then: func(cx *core.Scope) core.Node {
				return ui.Button{Text: locales.ParentalExtend(cx), TestID: "parental-extend-time", OnPress: func() {
					askPIN(false, func() {
						now := cx.WallTime()
						usage := a.parentalUsage.GetUntracked().onDay(now)
						status, _ := a.parental.GetUntracked().viewingLimit(now, usage)
						if status == parentalTimeUsed {
							usage.Bonus += 30 * time.Minute
							setUsage(usage)
						}
						refresh()
					})
				}}
			}},
			ui.Button{Text: locales.ClosePlayer(cx), TestID: "parental-leave-player", Variant: ui.ButtonOutline, OnPress: a.back},
		}}
	}}
	return blocked, core.Group([]core.Node{overlay, pin})
}
