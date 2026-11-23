package main

import (
	"cmp"
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

const tvTrailerDwell = 750 * time.Millisecond

func (a *application) tvFeaturedBackdrop(cx *core.Scope, entry catalogItem, focused func() bool) core.Node {
	artwork := homeArtwork(cmp.Or(entry.item.Backdrop, entry.item.Poster), "absolute inset-0")
	if entry.item.Kind != media.Movie && entry.item.Kind != media.Series {
		return artwork
	}
	start, playing := core.NewSignal(cx, false), core.NewSignal(cx, false)
	enabled := core.NewMemo(cx, func() bool {
		if !focused() || a.parentalRules().Enabled {
			return false
		}
		network := a.downloads.network.Get()
		return network != downloadNetworkCellular && network != downloadNetworkOffline
	})
	cx.Effect(func() {
		start.Set(false)
		if !enabled.Get() {
			return
		}
		stop := cx.AfterFunc(tvTrailerDwell, func() { start.Set(true) })
		cx.Cleanup(func() { stop() })
	})
	return core.View{Class: "absolute inset-0 overflow-hidden", PointerEvents: core.PointerEventsNone, Children: []core.Node{
		core.Show{When: start.Get, Then: func(cx *core.Scope) core.Node {
			lifetime := cx.Context()
			android := cx.Env().OS() == core.OSAndroid
			cx.Cleanup(func() { playing.Set(false) })
			trailer := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) (playerMedia, error) {
				ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
				defer cancel()
				return a.prepareTVTrailer(ctx, lifetime, entry, android)
			})
			return core.Dynamic(func(cx *core.Scope) core.Node {
				stream, ready := trailer.Get()
				if !ready || stream.url == "" {
					return nil
				}
				done := core.NewSignal(cx, false)
				end := func() {
					playing.Set(false)
					done.Set(true)
					stream.close()
				}
				return core.Show{When: func() bool { return !done.Get() }, Then: func(*core.Scope) core.Node {
					return core.Video{
						Source: stream.url, TestID: "tv-featured-trailer", Class: "absolute inset-0",
						Muted: true, ResizeMode: core.Cover, AudioSession: core.AudioSessionMixWithOthers,
						OnProgress: func(position, _ time.Duration) {
							if position > 0 && !done.GetUntracked() {
								playing.Set(true)
							}
						},
						OnEnd: end, OnError: func(error) { end() },
					}
				}}
			})
		}},
		core.Show{When: func() bool { return !playing.Get() }, Then: func(*core.Scope) core.Node {
			return core.View{TestID: "tv-featured-backdrop-artwork", Class: "absolute inset-0", Children: []core.Node{artwork}}
		}},
	}}
}

func (a *application) prepareTVTrailer(ctx, lifetime context.Context, entry catalogItem, android bool) (playerMedia, error) {
	client, endpoint, err := a.selectSourceClient(ctx, entry.source, "")
	if err != nil {
		return playerMedia{}, err
	}
	extras, ok := client.(media.ExtrasClient)
	if !ok {
		return playerMedia{}, nil
	}
	related, err := extras.Extras(ctx, entry.item)
	if len(related) == 0 {
		return playerMedia{}, err
	}
	noSubtitles := -1
	options := media.PlaybackOptions{
		MaxBitrate: entry.source.capBitrate(endpoint, 2_500_000), SubtitleIndex: &noSubtitles,
	}
	for _, item := range related {
		if !item.Trailer || item.ID == "" || item.ID == entry.item.ID {
			continue
		}
		stream, err := client.Playback(ctx, item, options)
		if err != nil {
			continue
		}
		videoLifetime, stop := context.WithCancel(lifetime)
		if stream.Method != media.DirectPlay {
			context.AfterFunc(videoLifetime, func() {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(videoLifetime), 3*time.Second)
				defer cancel()
				_ = media.StopPrivateTranscode(cleanup, client, stream)
			})
		}
		video, err := prepareVideo(videoLifetime, client, stream, android, false)
		if err != nil {
			stop()
			continue
		}
		closeProxy := video.close
		video.close = func() {
			closeProxy()
			stop()
		}
		return video, nil
	}
	return playerMedia{}, nil
}
