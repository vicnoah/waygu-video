package main

import (
	"os"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"waygu-video/internal/danmaku"
	"waygu-video/locales"
)

type danmakuControls struct {
	buttons core.Node
	overlay core.Node
}

type videoSample struct {
	position time.Duration
	seen     time.Time
}

func playerDanmaku(cx *core.Scope, sample *core.Signal[videoSample], size *core.Signal[core.Size], paused, buffering *core.Signal[bool],
	rate func() float32, useCanvas func() bool, showOnMPV func(*danmaku.Track)) danmakuControls {
	track := core.NewSignal[*danmaku.Track](cx, nil)
	open := core.NewSignal(cx, false)
	invalid := core.NewSignal(cx, false)
	busy := core.NewSignal(cx, false)
	clock := core.NewSignal(cx, time.Duration(0))
	viewContext := cx.Context()

	choose := func() {
		cx.PickFiles(core.PickFilesOptions{Types: []string{".xml"}, Title: locales.AddDanmaku(cx)}, func(files []core.File, err error) {
			if err != nil {
				invalid.Set(true)
				return
			}
			if len(files) == 0 {
				return
			}
			busy.Set(true)
			invalid.Set(false)
			go func() {
				file := files[0]
				if file.Temporary {
					defer os.Remove(file.Path)
				}
				var result *danmaku.Track
				if reader, err := file.Open(); err == nil {
					comments, err := danmaku.ReadXML(reader)
					reader.Close()
					if err == nil && len(comments) > 0 {
						value := danmaku.Schedule(comments, danmaku.DefaultSettings())
						result = &value
					}
				}
				cx.Post(func() {
					if viewContext.Err() != nil {
						return
					}
					busy.Set(false)
					invalid.Set(result == nil)
					if result != nil {
						track.Set(result)
						open.Set(true)
					}
				})
			}()
		})
	}

	cx.Effect(func() {
		if showOnMPV != nil {
			var visible *danmaku.Track
			if open.Get() {
				visible = track.Get()
			}
			showOnMPV(visible)
		}
	})
	cx.Effect(func() {
		value := sample.Get()
		clock.Set(value.position)
		if !open.Get() || !useCanvas() || paused.Get() || buffering.Get() || value.seen.IsZero() {
			return
		}
		speed := float64(rate())
		var next func()
		var animate func(time.Time)
		animate = func(now time.Time) {
			elapsed := min(max(0, now.Sub(value.seen)), 250*time.Millisecond)
			clock.Set(value.position + time.Duration(float64(elapsed)*speed))
			if elapsed < 250*time.Millisecond {
				next = cx.RequestAnimationFrame(animate)
			}
		}
		next = cx.RequestAnimationFrame(animate)
		cx.Cleanup(func() { next() })
	})

	return danmakuControls{
		buttons: core.View{Class: "flex-row items-center gap-1", Children: []core.Node{
			ui.Button{Label: locales.AddDanmaku(cx), Icon: lucide.FolderOpen, Variant: ui.ButtonSecondary,
				Busy: busy.Get, TestID: "load-danmaku", OnPress: choose},
			core.Show{When: func() bool { return track.Get() != nil }, Then: func(cx *core.Scope) core.Node {
				return core.Dynamic(func(cx *core.Scope) core.Node {
					label, variant := locales.ShowDanmaku(cx), ui.ButtonSecondary
					if open.Get() {
						label, variant = locales.HideDanmaku(cx), ui.ButtonPrimary
					}
					return ui.Button{Label: label, Icon: lucide.MessageSquareText, Variant: variant,
						TestID: "toggle-danmaku", OnPress: func() { open.Update(func(value bool) bool { return !value }) }}
				})
			}},
		}},
		overlay: core.View{Class: "absolute inset-0", Children: []core.Node{
			core.Show{When: func() bool { return open.Get() && useCanvas() }, Then: func(cx *core.Scope) core.Node {
				return core.Canvas{Class: "absolute inset-0", TestID: "danmaku-canvas", Draw: func(d *core.Drawing, bounds core.Size) {
					comments := track.Get()
					if comments != nil {
						drawDanmaku(d, bounds, size.Get(), *comments, clock.Get())
					}
				}}
			}},
			core.Show{When: invalid.Get, Then: func(cx *core.Scope) core.Node {
				return core.View{Class: "absolute inset-0 items-center justify-center", Children: []core.Node{
					notice(locales.DanmakuFileError(cx)),
				}}
			}},
		}},
	}
}

func drawDanmaku(d *core.Drawing, bounds, video core.Size, track danmaku.Track, at time.Duration) {
	width, height := float32(bounds.Width), float32(bounds.Height)
	if video.Width > 0 && video.Height > 0 {
		ratio := float32(video.Width / video.Height)
		width, height = min(width, height*ratio), min(height, width/ratio)
	}
	x := (float32(bounds.Width) - width) / 2
	y := (float32(bounds.Height) - height) / 2
	scaleX, scaleY := width/float32(track.Width), height/float32(track.Height)
	font := core.Font{Size: core.Points(max(11, float32(track.FontSize)*min(scaleX, scaleY))), Weight: core.FontWeightSemibold}
	for _, event := range track.Visible(at) {
		left := x + float32(event.X(at))*scaleX
		top := y + float32(event.Y)*scaleY + float32(font.Size)
		if event.Mode != danmaku.Scroll {
			left -= d.MeasureText(event.Text, font) / 2
		}
		d.SetStrokeColor(core.RGBA(0x000000, float32(track.Opacity))).SetLineWidth(2).StrokeText(event.Text, left, top, font)
		d.SetFillColor(core.RGBA(event.Color, float32(track.Opacity))).FillText(event.Text, left, top, font)
	}
}
