package main

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func tvPlayerPreview(cx *core.Scope, l tvLayout, p tvPlayerControls, scrub *core.Signal[time.Duration], settings func() bool) core.Node {
	requested := core.NewSignal(cx, false)
	cx.Effect(func() {
		if scrub.Get() >= 0 {
			requested.Set(true)
		}
	})
	provider, supported := p.client.(media.SeekPreviewClient)
	sourceID := core.NewMemo(cx, func() string { return p.stream().SourceID })
	source := core.NewResource(cx, func() (string, bool) {
		return sourceID.Get(), supported && p.current.downloadID == "" && requested.Get()
	}, func(ctx context.Context, sourceID string, _ core.ResourceInfo) (media.SeekPreview, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return provider.SeekPreviews(ctx, p.current.item, sourceID)
	})
	type frameRequest struct {
		load media.SeekPreview
		at   time.Duration
	}
	frame := core.NewResource(cx, func() (frameRequest, bool) {
		at := scrub.Get()
		load, ready := source.Get()
		if !ready || source.Loading() || load == nil || at < 0 {
			return frameRequest{}, false
		}
		return frameRequest{load, at}, true
	}, func(ctx context.Context, request frameRequest, _ core.ResourceInfo) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return request.load(ctx, request.at)
	})
	return core.Show{When: func() bool { return scrub.Get() >= 0 }, Then: func(cx *core.Scope) core.Node {
		timer := l.text("", 24, "font-semibold text-white")
		timer.ContentFunc, timer.TestID = func() string { return playbackClock(scrub.Get()) }, "tv-player-seek-time"
		picture := core.Dynamic(func(*core.Scope) core.Node {
			data, ready := frame.Get()
			if !ready || frame.Loading() || frame.Err() != nil || len(data) == 0 {
				return nil
			}
			return core.View{Class: "relative overflow-hidden rounded-xl border-2 border-white shadow-2xl",
				Style: core.Style{Width: l.length(284), Height: l.length(160)}, Children: []core.Node{
					core.Image{Class: "absolute inset-0", TestID: "tv-player-seek-thumbnail", Label: locales.PlaybackPosition(cx),
						Source: core.ImageSource{Data: data}, ResizeMode: core.Cover},
				}}
		})
		return core.View{Class: "relative", Style: core.Style{Height: l.length(198)}, Children: []core.Node{
			core.View{Class: "absolute bottom-0 items-center", StyleFunc: func() core.Style {
				right := core.Points(96)
				if settings() {
					right = 600
				}
				rail := max(0, cx.Env().Window().Width-l.points(96+right))
				left := core.Points(tvPlaybackFraction(scrub.Get(), p.duration.Get()))*rail - l.points(142)
				return core.Style{Left: core.Pt(max(0, min(max(0, rail-l.points(284)), left))), Width: l.length(284), Gap: l.points(8)}
			}, Children: []core.Node{picture, timer}},
		}}
	}}
}
