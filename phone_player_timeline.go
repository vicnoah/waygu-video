package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (p phonePlayerControls) chapters(cx *core.Scope) func() []media.Chapter {
	provider, supported := p.client.(media.ChapterClient)
	source := core.NewMemo(cx, func() string { return p.stream().SourceID })
	request := core.NewResource(cx, func() (string, bool) {
		return source.Get(), supported && p.current.downloadID == ""
	}, func(ctx context.Context, sourceID string, _ core.ResourceInfo) ([]media.Chapter, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return provider.Chapters(ctx, p.current.item, sourceID)
	})
	return func() []media.Chapter {
		if chapters, current := request.Get(); current && !request.Loading() && request.Err() == nil {
			return chapters
		}
		return nil
	}
}

func (p phonePlayerControls) timeline(cx *core.Scope, slider base.Slider, chapters func() []media.Chapter,
	drag *core.Signal[bool], preview *core.Signal[time.Duration], frames *core.Resource[[]byte]) core.Node {
	const previewWidth core.Points = 144
	width := core.NewSignal(cx, core.Points(0))
	cancelled := false
	change, commit := slider.OnChange, slider.OnChangeEnd
	slider.OnChange = func(value base.SliderValue) {
		cancelled = false
		change(value)
	}
	slider.OnChangeEnd = func(value base.SliderValue) {
		if !cancelled {
			commit(value)
		}
		cancelled = false
	}
	slider.Class = "h-4 w-full"
	slider.Children = []core.Node{
		core.Show{When: func() bool { return len(chapters()) > 0 }, Then: func(*core.Scope) core.Node {
			ticks := core.Canvas{Class: "absolute inset-0", TestID: "phone-player-chapter-ticks",
				Draw: func(d *core.Drawing, size core.Size) {
					length := p.duration.Get()
					position := tvPlaybackFraction(p.position.Get(), length)
					if at := preview.Get(); at >= 0 {
						position = tvPlaybackFraction(at, length)
					}
					d.SetFillColor(core.RGBA(0, .60))
					for _, chapter := range chapters() {
						if chapter.Start <= 0 || chapter.Start >= length {
							continue
						}
						fraction := tvPlaybackFraction(chapter.Start, length)
						x := float32(size.Width) * float32(fraction)
						// The thumb sits above chapter marks.
						if thumb := float32(size.Width) * float32(position); x < thumb-7 || x > thumb+7 {
							d.FillRect(x-1, 0, 2, float32(size.Height))
						}
					}
				},
			}
			return core.View{Class: "absolute inset-x-0 top-1.5 h-1", PointerEvents: core.PointerEventsNone, Children: []core.Node{ticks}}
		}},
	}
	return core.View{Class: "relative h-4 min-w-0 flex-1", OnLayout: func(bounds core.Bounds) { width.Set(bounds.Size.Width) },
		OnTouchCapture: func(event core.TouchEvent) {
			if event.Phase == core.TouchCancel {
				cancelled = true
				drag.Set(false)
				preview.Set(-1)
			}
		}, Children: []core.Node{
			playerRail(cx, slider, "size-3.5", "h-1"),
			core.Show{When: drag.Get, Then: func(*core.Scope) core.Node {
				return core.View{Class: "absolute bottom-6 w-36 items-center", PointerEvents: core.PointerEventsNone,
					StyleFunc: func() core.Style {
						span := width.Get()
						x := span * core.Points(tvPlaybackFraction(preview.Get(), p.duration.Get()))
						return core.Style{Left: core.Pt(max(0, min(span-previewWidth, x-previewWidth/2)))}
					}, Children: []core.Node{phoneSeekPreview(preview.Get, frames)},
				}
			}},
		},
	}
}

func phoneSeekPreview(at func() time.Duration, frames *core.Resource[[]byte]) core.Node {
	return core.View{Class: "items-center gap-1", PointerEvents: core.PointerEventsNone, Children: []core.Node{
		core.Show{When: func() bool {
			data, ready := frames.Get()
			return ready && !frames.Loading() && frames.Err() == nil && len(data) > 0
		}, Then: func(*core.Scope) core.Node {
			return core.Image{Class: "h-20 w-36 overflow-hidden rounded-lg border border-white/20 shadow-xl",
				TestID: "phone-player-seek-thumbnail", ResizeMode: core.Cover, SourceFunc: func() core.ImageSource {
					data, _ := frames.Get()
					return core.ImageSource{Data: data}
				}}
		}},
		core.Text{TestID: "phone-player-seek-time", ContentFunc: func() string { return landscapeClock(at()) },
			Class: "rounded-lg bg-black/85 px-2.5 py-1 text-xs font-semibold text-white"},
	}}
}

func (p phonePlayerControls) chapterActions(cx *core.Scope, chapters func() []media.Chapter, opened *core.Signal[bool]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		available := chapters()
		length := p.duration.Get()
		if len(available) == 0 || length <= 0 {
			return nil
		}
		rows := []core.Node{core.Text{Content: locales.PhonePlayerChapters(cx), Class: "pb-1 pt-3 text-sm font-semibold text-white"}}
		for index, chapter := range available {
			if chapter.Start >= length {
				break
			}
			name := strings.TrimSpace(chapter.Name)
			if name == "" {
				name = locales.PhonePlayerChapterNumber(cx, strconv.Itoa(index+1))
			}
			rows = append(rows, base.Button{TestID: "phone-player-chapter-" + strconv.Itoa(index), Label: name, Hint: landscapeClock(chapter.Start),
				Class: "h-10 flex-row items-center gap-2.5 rounded-lg bg-zinc-800/70 px-3",
				OnPress: func(core.PressEvent) {
					opened.Set(false)
					p.seek(chapter.Start)
				}, Children: []core.Node{
					core.Icon{SVG: lucide.Bookmark(), Class: "size-4 text-violet-400"},
					core.Text{Content: name, MaxLines: 1, Class: "min-w-0 flex-1 text-sm text-zinc-100"},
					core.Text{Content: landscapeClock(chapter.Start), Class: "shrink-0 text-xs text-zinc-400"},
				},
			})
		}
		if len(rows) == 1 {
			return nil
		}
		return core.View{Class: "gap-1.5", Children: rows}
	})
}
