package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
	"waygu-video/locales"
)

type danmakuControls struct {
	buttons core.Node
	overlay core.Node
	dialog  core.Node
}

type videoSample struct {
	position time.Duration
	seen     time.Time
}

func (a *application) playerDanmaku(cx *core.Scope, current route, stream media.Stream,
	sample *core.Signal[videoSample], size *core.Signal[core.Size], paused, buffering *core.Signal[bool],
	rate func() float32, useCanvas func() bool, showOnMPV func(*danmaku.Track)) danmakuControls {
	type input struct {
		local, remote []danmaku.Comment
		shift         float64
	}
	local := core.NewSignalFunc(cx, []danmaku.Comment(nil), nil)
	remote := core.NewSignalFunc(cx, input{}, nil)
	track := core.NewResource(cx, func() (input, bool) {
		sources := remote.Get()
		sources.local = local.Get()
		return sources, true
	}, func(_ context.Context, sources input, _ core.ResourceInfo) (*danmaku.Track, error) {
		if len(sources.local)+len(sources.remote) == 0 {
			return nil, nil
		}
		comments := make([]danmaku.Comment, 0, len(sources.local)+len(sources.remote))
		comments = append(comments, sources.local...)
		shift := time.Duration(0)
		if !math.IsNaN(sources.shift) {
			shift = time.Duration(max(-86400, min(86400, sources.shift)) * float64(time.Second))
		}
		for _, comment := range sources.remote {
			if shift > 0 && comment.At > time.Duration(math.MaxInt64)-shift {
				continue
			}
			comment.At += shift
			if comment.At >= 0 {
				comments = append(comments, comment)
			}
		}
		scheduled := danmaku.Schedule(comments, danmaku.DefaultSettings())
		return &scheduled, nil
	})
	open := core.NewSignal(cx, false)
	invalid := core.NewSignal(cx, false)
	busy := core.NewSignal(cx, false)
	clock := core.NewSignal(cx, time.Duration(0))
	viewContext := cx.Context()
	sourceButton, sourceDialog := a.onlineDanmaku(cx, current, stream, func(comments []danmaku.Comment, shift float64) {
		remote.Set(input{remote: comments, shift: shift})
		if len(comments) > 0 {
			open.Set(true)
		}
	})

	choose := func() {
		cx.PickFiles(core.PickFilesOptions{Types: []string{".xml", ".json"}, Title: locales.AddDanmaku(cx)}, func(files []core.File, err error) {
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
				var result []danmaku.Comment
				if reader, err := file.Open(); err == nil {
					if strings.EqualFold(filepath.Ext(file.Name), ".json") {
						result, err = danmaku.ReadJSON(reader)
					} else {
						result, err = danmaku.ReadXML(reader)
					}
					reader.Close()
					if err != nil {
						result = nil
					}
				}
				cx.Post(func() {
					if viewContext.Err() != nil {
						return
					}
					busy.Set(false)
					invalid.Set(len(result) == 0)
					if len(result) > 0 {
						local.Set(result)
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
				visible, _ = track.Get()
			}
			showOnMPV(visible)
		}
	})
	cx.Effect(func() {
		value := sample.Get()
		clock.Set(value.position)
		active, _ := track.Get()
		if active == nil || !open.Get() || !useCanvas() || paused.Get() || buffering.Get() || value.seen.IsZero() {
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
		dialog: sourceDialog,
		buttons: core.View{Class: "flex-row items-center gap-1", Children: []core.Node{
			sourceButton,
			ui.Button{Label: locales.AddDanmaku(cx), Icon: lucide.FolderOpen, Variant: ui.ButtonSecondary,
				Busy: busy.Get, TestID: "load-danmaku", OnPress: choose},
			core.Show{When: func() bool { value, _ := track.Get(); return value != nil }, Then: func(cx *core.Scope) core.Node {
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
		overlay: core.View{Class: "absolute inset-0", PointerEvents: core.PointerEventsNone, Children: []core.Node{
			core.Show{When: func() bool { value, _ := track.Get(); return open.Get() && value != nil && useCanvas() }, Then: func(cx *core.Scope) core.Node {
				return core.Canvas{Class: "absolute inset-0", TestID: "danmaku-canvas", Draw: func(d *core.Drawing, bounds core.Size) {
					comments, _ := track.Get()
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
