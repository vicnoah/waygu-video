package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
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
	resize core.ResizeMode, rate func() float32, useCanvas func() bool, showOnMPV func(*danmaku.Track)) danmakuControls {
	type remoteComments struct {
		comments []danmaku.Comment
		shift    float64
	}
	type input struct {
		local  []danmaku.Comment
		remote remoteComments
		prefs  danmakuPreferences
		offset time.Duration
	}
	editor := a.editDanmaku(cx)
	offset, offsetControls := a.danmakuOffset(cx, current)
	local := core.NewSignalFunc(cx, []danmaku.Comment(nil), nil)
	remote := core.NewSignalFunc(cx, remoteComments{}, nil)
	track := core.NewResource(cx, func() (input, bool) {
		return input{local.Get(), remote.Get(), a.danmakuPrefs.Get(), offset.Get()}, true
	}, func(ctx context.Context, sources input, _ core.ResourceInfo) (*danmaku.Track, error) {
		if len(sources.local)+len(sources.remote.comments) == 0 {
			return nil, nil
		}
		comments := make([]danmaku.Comment, 0, len(sources.local)+len(sources.remote.comments))
		comments = append(comments, sources.local...)
		shift := time.Duration(0)
		if !math.IsNaN(sources.remote.shift) {
			shift = time.Duration(max(-86400, min(86400, sources.remote.shift)) * float64(time.Second))
		}
		for _, comment := range sources.remote.comments {
			if shift > 0 && comment.At > time.Duration(math.MaxInt64)-shift {
				continue
			}
			comment.At += shift
			comments = append(comments, comment)
		}
		scheduled := danmaku.ScheduleContext(ctx, comments, sources.prefs.rendererSettings(sources.offset))
		return &scheduled, ctx.Err()
	})
	open := core.NewSignal(cx, a.danmakuPrefs.GetUntracked().Enabled)
	panel := core.NewSignal(cx, false)
	tab := core.NewSignal(cx, 0)
	invalid := core.NewSignal(cx, false)
	busy := core.NewSignal(cx, false)
	clock := core.NewSignal(cx, time.Duration(0))
	viewContext := cx.Context()
	var source core.Node
	if current.downloadID == "" {
		source = a.onlineDanmaku(cx, current, stream, func(comments []danmaku.Comment, shift float64) {
			remote.Set(remoteComments{comments, shift})
		}, func() { panel.Set(false) })
	} else {
		source = hint(locales.DownloadOfflinePlayback(cx))
	}

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

	pages := []core.Node{
		editor.display(cx, ui.Checkbox{Text: locales.ShowDanmaku(cx), TestID: "danmaku-display-enabled", Checked: open.Get,
			OnChange: func(value bool) {
				if editor.save(func(p *danmakuPreferences) { p.Enabled = value }) {
					open.Set(value)
				}
			}}, offsetControls),
		core.View{Class: "gap-3", Children: []core.Node{
			ui.Button{Text: locales.AddDanmaku(cx), Icon: lucide.FolderOpen, Variant: ui.ButtonOutline,
				Busy: busy.Get, TestID: "danmaku-import-file", OnPress: choose},
			core.Show{When: invalid.Get, Then: func(cx *core.Scope) core.Node { return notice(locales.DanmakuFileError(cx)) }},
			source,
		}},
		editor.filters(cx),
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
		dialog: ui.Dialog{Open: panel.Get, OnOpenChange: func(visible bool, _ base.Reason) { panel.Set(visible) },
			Title: locales.Danmaku(cx), TestID: "danmaku-sources-dialog",
			Trailing: []core.Node{ui.IconButton{Label: locales.TracksDone(cx), Icon: lucide.X, Size: 36,
				TestID: "close-danmaku-sources", OnPress: func() { panel.Set(false) }}},
			Children: []core.Node{
				ui.Tabs[int]{Options: []ui.Option[int]{
					{Value: 0, Label: locales.DanmakuDisplayTab(cx), TestID: "danmaku-tab-display"},
					{Value: 1, Label: locales.DanmakuSourceTab(cx), TestID: "danmaku-tab-source"},
					{Value: 2, Label: locales.DanmakuFilterTab(cx), TestID: "danmaku-tab-filters"},
				}, Value: tab.Get, OnChange: tab.Set, Equal: true, TestID: "danmaku-tabs"},
				editor.warning(),
				core.Dynamic(func(cx *core.Scope) core.Node {
					return core.Scroll{StyleFunc: func() core.Style {
						return core.Style{MaxHeight: core.Pt(min(365, max(105, cx.Env().Window().Height*0.46))), FlexShrink: 1}
					}, Children: []core.Node{pages[tab.Get()]}}
				}),
			}},
		buttons: core.View{Class: "flex-row items-center gap-1", Children: []core.Node{
			ui.Button{Label: locales.Danmaku(cx), Icon: lucide.SlidersHorizontal, Variant: ui.ButtonSecondary,
				TestID: "danmaku-sources", OnPress: func() { panel.Set(true) }},
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
						drawDanmaku(d, bounds, size.Get(), resize, *comments, clock.Get())
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

const maxDanmakuOffset = 60 * time.Second

func (a *application) danmakuOffset(cx *core.Scope, current route) (*core.Signal[time.Duration], core.Node) {
	key := "danmaku-offset-v1:" + danmakuItemKey(current)
	stored := core.Untracked(cx, func() string { value, _ := a.storage.GetItem(key); return value })
	ms, _ := strconv.ParseInt(stored, 10, 64)
	offset := core.NewSignal(cx, time.Duration(min(maxDanmakuOffset.Milliseconds(), max(-maxDanmakuOffset.Milliseconds(), ms)))*time.Millisecond)
	failed := core.NewSignal(cx, false)
	set := func(value time.Duration) {
		value = min(maxDanmakuOffset, max(-maxDanmakuOffset, value))
		if value == 0 {
			a.storage.RemoveItem(key)
		} else if err := a.storage.SetItem(key, strconv.FormatInt(value.Milliseconds(), 10)); err != nil {
			failed.Set(true)
			return
		}
		failed.Set(false)
		offset.Set(value)
	}
	return offset, core.View{Class: "gap-1", Children: []core.Node{
		core.View{Class: "flex-row flex-wrap items-center gap-2", Children: []core.Node{
			core.Text{ContentFunc: func() string {
				return locales.DanmakuOffset(cx, fmt.Sprintf("%+.1f", offset.Get().Seconds()))
			}, Class: "flex-1 text-sm font-medium", TestID: "danmaku-offset"},
			ui.IconButton{Label: locales.DanmakuEarlier(cx), Icon: lucide.Minus, Size: 44, TestID: "danmaku-earlier",
				Disabled: func() bool { return offset.Get() <= -maxDanmakuOffset },
				OnPress:  func() { set(offset.GetUntracked() - 500*time.Millisecond) }},
			ui.IconButton{Label: locales.DanmakuLater(cx), Icon: lucide.Plus, Size: 44, TestID: "danmaku-later",
				Disabled: func() bool { return offset.Get() >= maxDanmakuOffset },
				OnPress:  func() { set(offset.GetUntracked() + 500*time.Millisecond) }},
			ui.Button{Text: locales.DanmakuResetOffset(cx), Variant: ui.ButtonGhost, TestID: "danmaku-reset-offset",
				Disabled: func() bool { return offset.Get() == 0 }, OnPress: func() { set(0) }},
		}},
		core.Show{When: failed.Get, Then: func(cx *core.Scope) core.Node { return notice(locales.DanmakuPreferencesError(cx)) }},
	}}
}

func drawDanmaku(d *core.Drawing, bounds, video core.Size, resize core.ResizeMode, track danmaku.Track, at time.Duration) {
	frame := resize.Fit(video, bounds, core.ObjectPosition{})
	width, height := float32(frame.Size.Width), float32(frame.Size.Height)
	x, y := float32(frame.Origin.X), float32(frame.Origin.Y)
	scaleX, scaleY := width/float32(track.Width), height/float32(track.Height)
	scale := min(scaleX, scaleY)
	font := core.Font{Size: core.Points(max(6, float32(track.FontSize)*scale)), Weight: core.FontWeightNormal}
	if track.Bold {
		font.Weight = core.FontWeightBold
	}
	for _, event := range track.Visible(at) {
		left := x + float32(event.X(at))*scaleX
		top := y + float32(event.Y)*scaleY + float32(font.Size)
		text := event.DisplayText()
		if event.Mode != danmaku.Scroll {
			left -= d.MeasureText(text, font) / 2
		}
		if track.Outline > 0 {
			d.SetStrokeColor(core.RGBA(0x000000, float32(track.Opacity))).SetLineWidth(max(.5, float32(track.Outline)*scale)).StrokeText(text, left, top, font)
		}
		d.SetFillColor(core.RGBA(event.Color, float32(track.Opacity))).FillText(text, left, top, font)
	}
}
