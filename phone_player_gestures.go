package main

import (
	"math"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"waygu-video/locales"
)

type phonePlayerGesture uint8

const (
	phoneGestureNone phonePlayerGesture = iota
	phoneGestureSeek
	phoneGestureBrightness
	phoneGestureVolume
	phoneGestureSpeed
)

func (p phonePlayerControls) gestureSurface(cx *core.Scope, controls *core.Signal[bool], preview *core.Signal[time.Duration], disabled func() bool) (core.Node, core.Node) {
	const doubleTapTime = 280 * time.Millisecond
	hud := core.NewSignal(cx, phoneGestureNone)
	remoteVolume := core.NewSignal(cx, float32(0))
	width, height := core.Points(0), core.Points(0)
	var stopHoldTimer, stopHUDTimer func() bool
	var holdRate float32
	suppressPress := false
	var lastTap time.Time
	var lastPoint core.Point
	var mode phonePlayerGesture
	var panStart time.Duration
	var initialLevel float32

	cancelHold := func() {
		if stopHoldTimer != nil {
			stopHoldTimer()
			stopHoldTimer = nil
		}
		if holdRate > 0 {
			if p.rate.GetUntracked() == 2 {
				p.rate.Set(holdRate)
			}
			holdRate = 0
			hud.Set(phoneGestureNone)
		}
	}
	reset := func() {
		cancelHold()
		if stopHUDTimer != nil {
			stopHUDTimer()
			stopHUDTimer = nil
		}
		hud.Set(phoneGestureNone)
		preview.Set(-1)
		mode = phoneGestureNone
		lastTap = time.Time{}
	}
	cx.Cleanup(cancelHold)
	cx.Effect(func() {
		if disabled() {
			reset()
		} else if p.paused.Get() || !p.canRate.Get() || p.cast.active() {
			cancelHold()
		}
	})
	startHold := func(core.PressEvent) {
		suppressPress = false
		if core.Untracked(cx, disabled) || p.paused.GetUntracked() || !p.canRate.GetUntracked() || p.cast.active() {
			return
		}
		stopHoldTimer = cx.AfterFunc(500*time.Millisecond, func() {
			stopHoldTimer = nil
			if core.Untracked(cx, disabled) || p.paused.GetUntracked() || !p.canRate.GetUntracked() || p.cast.active() {
				return
			}
			holdRate = p.rate.GetUntracked()
			p.rate.Set(2)
			suppressPress = true
			lastTap = time.Time{}
			if stopHUDTimer != nil {
				stopHUDTimer()
				stopHUDTimer = nil
			}
			hud.Set(phoneGestureSpeed)
		})
	}
	press := func(event core.PressEvent) {
		if core.Untracked(cx, disabled) || suppressPress {
			return
		}
		now := cx.Now()
		distance := math.Hypot(float64(event.Position.X-lastPoint.X), float64(event.Position.Y-lastPoint.Y))
		twice := !event.Keyboard && (event.Clicks == 2 || (!lastTap.IsZero() && now.Sub(lastTap) <= doubleTapTime && distance <= 32))
		if twice {
			lastTap = time.Time{}
			p.toggle()
			controls.Set(true)
			return
		}
		lastTap, lastPoint = now, event.Position
		if event.Keyboard {
			lastTap = time.Time{}
		}
		controls.Update(func(visible bool) bool { return !visible })
	}
	pan := func(event core.PanEvent) {
		if core.Untracked(cx, disabled) {
			return
		}
		switch event.Phase {
		case core.GestureBegan:
			reset()
			suppressPress = true
			panStart = p.position.GetUntracked()
		case core.GestureChanged:
			dx, dy := event.Translation.X, event.Translation.Y
			if mode == phoneGestureNone {
				if math.Abs(float64(dx)) >= math.Abs(float64(dy)) {
					mode = phoneGestureSeek
				} else if event.Position.X-dx < width/2 {
					mode, initialLevel = phoneGestureBrightness, p.brightness.GetUntracked()
				} else {
					mode, initialLevel = phoneGestureVolume, p.volume.GetUntracked()
					if p.cast.active() {
						initialLevel = float32(p.cast.volume.GetUntracked()) / 100
					}
				}
			}
			switch mode {
			case phoneGestureSeek:
				if length := p.duration.GetUntracked(); length > 0 {
					offset := time.Duration(float64(dx) * float64(time.Second/4))
					preview.Set(min(length, max(0, panStart+offset)))
				}
			case phoneGestureBrightness:
				if !p.cast.active() {
					p.brightness.Set(max(.15, min(1, initialLevel-float32(dy/max(1, height)))))
					hud.Set(mode)
				}
			case phoneGestureVolume:
				level := max(0, min(1, initialLevel-float32(dy/max(1, height))))
				if !p.cast.active() {
					p.volume.Set(level)
					hud.Set(mode)
				} else if p.cast.receiver.GetUntracked().HasVolume() {
					remoteVolume.Set(level)
					hud.Set(mode)
				}
			}
		case core.GestureEnded:
			if at := preview.GetUntracked(); mode == phoneGestureSeek && at >= 0 {
				p.seek(at)
			}
			if hud.GetUntracked() == phoneGestureVolume && p.cast.active() {
				p.cast.command(castCommand{action: castVolume, value: roundedPercent(remoteVolume.GetUntracked())})
			}
			preview.Set(-1)
			mode = phoneGestureNone
			if hud.GetUntracked() != phoneGestureNone {
				stopHUDTimer = cx.AfterFunc(650*time.Millisecond, func() {
					stopHUDTimer = nil
					hud.Set(phoneGestureNone)
				})
			}
		case core.GestureCancelled:
			reset()
		}
	}
	surface := core.View{Class: "absolute inset-0", TestID: "phone-player-surface", Label: locales.PhonePlayerControls(cx), Role: core.RoleButton,
		Disabled: disabled, OnPress: press, OnPressDown: startHold, OnPressUp: func(core.PressEvent) { cancelHold() },
		OnTouch: func(event core.TouchEvent) {
			if event.Phase == core.TouchEnd || event.Phase == core.TouchCancel || len(event.Touches) > 1 {
				cancelHold()
			}
		},
		OnLayout:      func(bounds core.Bounds) { width, height = bounds.Size.Width, bounds.Size.Height },
		PanActivation: core.PanActivation{ActiveOffsetX: 14, ActiveOffsetY: 14}, OnPan: pan,
	}
	feedback := core.Show{When: func() bool { return hud.Get() != phoneGestureNone && !disabled() }, Then: func(cx *core.Scope) core.Node {
		return core.View{Class: "absolute inset-x-0 top-[72px] items-center", PointerEvents: core.PointerEventsNone, Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				var id, text string
				var icon func() *core.SVG
				switch hud.Get() {
				case phoneGestureBrightness:
					id, icon = "phone-player-brightness", lucide.Sun
					text = locales.PhonePlayerBrightness(cx, strconv.Itoa(roundedPercent(p.brightness.Get())))
				case phoneGestureVolume:
					id, icon = "phone-player-volume", lucide.Volume2
					level := p.volume.Get()
					if p.cast.active() {
						level = remoteVolume.Get()
					}
					text = locales.PhonePlayerVolume(cx, strconv.Itoa(roundedPercent(level)))
				case phoneGestureSpeed:
					id, icon = "phone-player-boost", lucide.FastForward
					text = playbackRateLabel(2)
				default:
					return nil
				}
				return core.View{TestID: id, Class: "flex-row items-center gap-1.5 rounded-full border border-white/10 bg-black/75 px-3 py-1.5",
					Children: []core.Node{
						core.Icon{SVG: icon(), Class: "size-4 text-white"},
						core.Text{Content: text, Class: "text-xs font-semibold text-white"},
					}}
			}),
		}}
	}}
	return surface, feedback
}

func roundedPercent(level float32) int { return int(math.Round(float64(level * 100))) }
