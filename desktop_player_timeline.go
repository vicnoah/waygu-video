package main

import (
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/locales"
)

func (p desktopPlayerControls) timeline(cx *core.Scope) core.Node {
	width := core.NewSignal(cx, core.Points(0))
	hover := core.NewSignal(cx, time.Duration(-1))
	drag := core.NewSignal(cx, time.Duration(-1))
	showAt := func() time.Duration {
		if value := drag.Get(); value >= 0 {
			return value
		}
		return hover.Get()
	}
	frames := playerPreviewFrames(cx, p.current, p.client, p.stream, func() time.Duration {
		if at := showAt(); at >= 0 {
			return at.Truncate(time.Second)
		}
		return -1
	})
	rail := playerRail(cx, base.Slider{Label: locales.PlaybackPosition(cx), TestID: "desktop-player-position-rail", ThumbTestID: "desktop-player-position-thumb",
		Value: func() base.SliderValue {
			at := drag.Get()
			if at < 0 {
				at = p.position.Get()
			}
			return base.SingleValue(at.Seconds())
		}, Max: func() float64 { return p.duration.Get().Seconds() }, Step: func() float64 { return 1 },
		Disabled: func() bool { return p.duration.Get() <= 0 }, Class: "h-4 flex-1",
		Marks:    []core.Node{p.bufferedMarks()},
		OnChange: func(v base.SliderValue) { drag.Set(time.Duration(v.Start * float64(time.Second))) },
		OnChangeEnd: func(v base.SliderValue) {
			p.seek(time.Duration(v.Start * float64(time.Second)))
			drag.Set(-1)
		}}, "size-4", "h-1.5")
	preview := core.Show{When: func() bool { return showAt() >= 0 }, Then: func(cx *core.Scope) core.Node {
		return core.View{Class: "absolute bottom-9 w-44 items-center gap-1", PointerEvents: core.PointerEventsNone,
			StyleFunc: func() core.Style {
				span := width.Get()
				left := span*core.Points(tvPlaybackFraction(showAt(), p.duration.Get())) - 88
				return core.Style{Left: core.Pt(min(max(0, span-176), max(0, left)))}
			}, Children: []core.Node{
				core.Show{When: func() bool { data, ok := frames.Get(); return ok && !frames.Loading() && len(data) > 0 }, Then: func(*core.Scope) core.Node {
					return core.Image{Class: "h-24 w-44 overflow-hidden rounded-lg border border-white/20",
						TestID: "desktop-player-seek-thumbnail", ResizeMode: core.Cover, SourceFunc: func() core.ImageSource {
							data, _ := frames.Get()
							return core.ImageSource{Data: data}
						}}
				}},
				core.Text{TestID: "desktop-player-seek-time", ContentFunc: func() string { return playbackClock(showAt()) },
					Class: "rounded bg-black/70 px-1.5 text-xs font-medium text-white"},
			}}
	}}
	move := func(event core.MouseEvent) {
		if span, duration := width.GetUntracked(), p.duration.GetUntracked(); span > 0 && duration > 0 {
			ratio := max(0, min(1, float64(event.Position.X/span)))
			hover.Set(time.Duration(ratio * float64(duration)))
		}
	}
	return core.View{Class: "relative gap-1", OnLayout: func(bounds core.Bounds) { width.Set(bounds.Size.Width) }, Children: []core.Node{
		p.heatmap(cx),
		core.View{Class: "relative h-4 flex-row items-center", OnMouseEnter: move, OnMouseMove: move, OnMouseLeave: func(core.MouseEvent) { hover.Set(-1) },
			Children: []core.Node{rail}},
		preview,
	}}
}

func (p desktopPlayerControls) bufferedMarks() core.Node {
	return core.Show{When: func() bool {
		info, ok := p.stats()
		return ok && len(info.CacheRanges) > 0 && p.duration.Get() > 0 && !p.cast.active()
	}, Then: func(*core.Scope) core.Node {
		return core.View{Class: "absolute inset-0 overflow-hidden rounded-full", PointerEvents: core.PointerEventsNone,
			Children: []core.Node{core.Canvas{Class: "absolute inset-0", TestID: "desktop-player-buffered",
				Draw: func(d *core.Drawing, size core.Size) {
					info, _ := p.stats()
					length := p.duration.Get()
					// Composite to the design's 45% white over the 20% base rail.
					const cachedOpacity = (.45 - .20) / (1 - .20)
					d.SetFillColor(core.RGBA(0xffffff, cachedOpacity))
					for _, cached := range info.CacheRanges {
						start := float32(tvPlaybackFraction(cached.Start, length)) * float32(size.Width)
						end := float32(tvPlaybackFraction(cached.End, length)) * float32(size.Width)
						if end > start {
							d.FillRect(start, 0, end-start, float32(size.Height))
						}
					}
				}}}}
	}}
}

func (p desktopPlayerControls) heatmap(cx *core.Scope) core.Node {
	const bins = 48
	histogram := core.NewMemo(cx, func() [bins]int {
		var counts [bins]int
		if duration := p.duration.Get(); duration > 0 {
			for _, event := range p.comments.events() {
				if event.Start >= 0 && event.Start < duration {
					index := min(bins-1, int(float64(event.Start)*bins/float64(duration)))
					counts[index] += max(1, event.Copies)
				}
			}
		}
		return counts
	})
	return core.Canvas{Class: "h-6 w-full", TestID: "desktop-player-heatmap",
		Draw: func(d *core.Drawing, size core.Size) {
			counts := histogram.Get()
			peak := 0
			for _, value := range counts {
				peak = max(peak, value)
			}
			if peak == 0 {
				return
			}
			w, h := float32(size.Width), float32(size.Height)
			step, x, y := w/(bins-1), float32(0), h
			d.SetFillColor(core.RGBA(0xd946ef, .35)).BeginPath().MoveTo(0, h)
			for index, value := range counts {
				nextX := float32(index) * step
				nextY := h - h*float32(value)/float32(peak)
				middle := (x + nextX) / 2
				d.CubicTo(middle, y, middle, nextY, nextX, nextY)
				x, y = nextX, nextY
			}
			d.LineTo(w, h).ClosePath().Fill()
		}}
}
