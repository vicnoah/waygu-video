package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"waygu-video/internal/danmaku"

	"waygu-video/internal/media"
	"waygu-video/internal/playback"
	"waygu-video/locales"
)

type playerMedia struct {
	client media.Client
	stream media.Stream
	url    string
}

func (a *application) player(cx *core.Scope, current route) core.Node {
	theme := wayguTheme()
	theme.Light = theme.Dark
	cx.SetTheme(theme)
	platform := cx.Env().OS()
	bitrate := a.bitrateLimit.GetUntracked()
	resource := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) (playerMedia, error) {
		client, err := a.client(current.source)
		if err != nil {
			return playerMedia{}, err
		}
		stream, err := client.Playback(ctx, current.item, media.PlaybackOptions{MaxBitrate: bitrate})
		if err != nil {
			return playerMedia{}, err
		}
		address := stream.URL
		if len(stream.Headers) > 0 || (platform == core.OSAndroid && strings.HasPrefix(address, "http://")) {
			var close func()
			address, close, err = playback.Open(stream.URL, stream.Headers)
			if err != nil {
				return playerMedia{}, err
			}
			context.AfterFunc(ctx, close)
		}
		return playerMedia{client, stream, address}, nil
	})
	return core.View{Class: "flex-1 bg-black", Style: core.Style{FlexShrink: 1}, Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node {
			if medium, ready := resource.Get(); ready {
				return a.playerView(cx, current, medium)
			}
			var state core.Node = hint(locales.Loading(cx))
			if err := resource.Err(); err != nil {
				state = core.View{Class: "items-center gap-4", Children: []core.Node{
					notice(errorText(cx, err)), ui.Button{Text: locales.Retry(cx), OnPress: resource.Refetch},
				}}
			}
			return pageFrame(pageHeader(cx, current.item.Title, a.back), core.View{
				Class: "flex-1 items-center justify-center p-4", Children: []core.Node{state},
			})
		}),
	}}
}

func (a *application) playerView(cx *core.Scope, current route, medium playerMedia) core.Node {
	ref := &core.Ref{}
	seekVideo := ref.Seek
	position := core.NewSignal(cx, time.Duration(0))
	duration := core.NewSignal(cx, medium.stream.Duration)
	sample := core.NewSignal(cx, videoSample{})
	picture := core.NewSignal(cx, core.Size{})
	paused := core.NewSignal(cx, false)
	failure := core.NewSignal(cx, false)
	buffering := core.NewSignal(cx, false)
	loaded, ended := false, false
	var lastReport time.Time
	enqueue, finish := playbackReports(medium.client)
	snapshot := func(event media.PlaybackEvent) media.PlaybackState {
		stream := medium.stream
		stream.Duration = duration.GetUntracked()
		return media.PlaybackState{Event: event, Stream: stream, Position: position.GetUntracked(), Paused: paused.GetUntracked()}
	}
	report := func(event media.PlaybackEvent) {
		if loaded {
			lastReport = cx.Now()
			enqueue(snapshot(event))
		}
	}
	cx.Cleanup(func() {
		if loaded && !ended {
			final := snapshot(media.Stopped)
			finish(&final)
		} else {
			finish(nil)
		}
	})
	seek := func(at time.Duration) {
		if !loaded {
			return
		}
		at = max(0, min(at, duration.GetUntracked()))
		ended = false
		position.Set(at)
		sample.Set(videoSample{at, cx.Now()})
		seekVideo(at)
		report(media.Progress)
	}
	video := core.Video{Source: medium.url, PausedFunc: paused.Get, Ref: ref,
		TestID: "video-player", Class: "absolute inset-0", ResizeMode: core.Contain, ProgressInterval: 250 * time.Millisecond,
		AudioSession: core.AudioSessionPlayback,
		OnLoad: func(info core.VideoInfo) {
			picture.Set(info.Size)
			if info.Duration > 0 {
				duration.Set(info.Duration)
			}
			loaded = true
			if current.resume && medium.stream.Position > 0 && medium.stream.Position < duration.GetUntracked() {
				position.Set(medium.stream.Position)
				sample.Set(videoSample{medium.stream.Position, cx.Now()})
				seekVideo(medium.stream.Position)
			}
			report(media.Started)
		},
		OnProgress: func(at, length time.Duration) {
			position.Set(at)
			sample.Set(videoSample{at, cx.Now()})
			if length > 0 {
				duration.Set(length)
			}
			if !ended && cx.Now().Sub(lastReport) >= 10*time.Second {
				report(media.Progress)
			}
		},
		OnPause: func(core.VideoPauseReason) {
			if !paused.GetUntracked() && !ended {
				paused.Set(true)
				report(media.Progress)
			}
		},
		OnPlay: func() {
			if paused.GetUntracked() {
				paused.Set(false)
				report(media.Progress)
			}
		},
		OnBuffer: buffering.Set, OnError: func(error) { failure.Set(true) },
		OnEnd: func() {
			position.Set(duration.GetUntracked())
			paused.Set(true)
			ended = true
			report(media.Stopped)
		},
	}
	var image core.Node = video
	var showOnMPV func(*danmaku.Track)
	useCanvas := func() bool { return true }
	if cx.Env().Desktop() && a.mpvPreferred.GetUntracked() {
		desktop := mpvVideo(cx, video)
		image, showOnMPV = desktop.node, desktop.comments
		useCanvas = func() bool { return !desktop.active() }
		seekVideo = func(at time.Duration) {
			if desktop.active() {
				desktop.seek(at)
			} else {
				ref.Seek(at)
			}
		}
	}
	comments := playerDanmaku(cx, sample, picture, paused, buffering, useCanvas, showOnMPV)
	toggle := func() {
		if ended {
			seek(0)
			report(media.Started)
		}
		paused.Update(func(value bool) bool { return !value })
		report(media.Progress)
	}
	return core.View{Class: "relative flex-1 overflow-hidden bg-black", Style: core.Style{FlexShrink: 1}, Children: []core.Node{
		image,
		comments.overlay,
		core.View{Class: "absolute left-0 right-0 top-0 flex-row items-center gap-3 bg-linear-to-b from-black/90 to-transparent p-5",
			Style: core.Style{PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea}}, Children: []core.Node{
				ui.Button{Label: locales.ClosePlayer(cx), Icon: lucide.ChevronLeft, Variant: ui.ButtonSecondary, OnPress: a.back, TestID: "close-player"},
				core.View{Class: "flex-1 gap-1", Style: core.Style{FlexShrink: 1}, Children: []core.Node{
					core.Text{Content: current.item.Title, Class: "text-lg font-semibold text-white"},
					core.Text{Content: current.source.Name + " · " + playMethod(cx, medium.stream.Method), Class: "text-xs text-zinc-300"},
				}},
				comments.buttons,
			}},
		core.Show{When: func() bool { return buffering.Get() || failure.Get() }, Then: func(cx *core.Scope) core.Node {
			return core.View{Class: "absolute inset-0 items-center justify-center", Children: []core.Node{
				core.Text{Class: "rounded-xl bg-black/75 p-4 text-white", ContentFunc: func() string {
					if failure.Get() {
						return locales.VideoError(cx)
					}
					return locales.Loading(cx)
				}},
			}}
		}},
		core.View{Class: "absolute bottom-0 left-0 right-0 gap-3 bg-linear-to-t from-black to-transparent p-5",
			Style: core.Style{PaddingEnv: core.EnvEdges{Bottom: core.EnvSafeArea}}, Children: []core.Node{
				core.View{Class: "flex-row items-center gap-3", Children: []core.Node{
					core.Text{ContentFunc: func() string { return playbackClock(position.Get()) }, Class: "text-xs text-white"},
					ui.Slider{Label: locales.PlaybackPosition(cx), Value: func() float64 { return position.Get().Seconds() },
						MaxFunc: func() float64 { return duration.Get().Seconds() }, Disabled: func() bool { return duration.Get() <= 0 },
						OnChange: func(seconds float64) { seek(time.Duration(seconds * float64(time.Second))) },
						Style:    core.Style{FlexGrow: 1, FlexShrink: 1}},
					core.Text{ContentFunc: func() string { return playbackClock(duration.Get()) }, Class: "text-xs text-white"},
				}},
				core.View{Class: "flex-row items-center justify-center gap-4", Children: []core.Node{
					ui.Button{Label: locales.SeekBack(cx), Icon: lucide.RotateCcw, Variant: ui.ButtonSecondary, Size: ui.SizeLarge,
						OnPress: func() { seek(position.GetUntracked() - 10*time.Second) }},
					core.Dynamic(func(cx *core.Scope) core.Node {
						label, icon := locales.Pause(cx), lucide.Pause
						if paused.Get() {
							label, icon = locales.Play(cx), lucide.Play
						}
						return ui.Button{Label: label, Icon: icon, Size: ui.SizeLarge, TestID: "toggle-play", OnPress: toggle}
					}),
					ui.Button{Label: locales.SeekForward(cx), Icon: lucide.RotateCw, Variant: ui.ButtonSecondary, Size: ui.SizeLarge,
						OnPress: func() { seek(position.GetUntracked() + 10*time.Second) }},
				}},
			}},
	}}
}

func playbackReports(client media.Client) (func(media.PlaybackState), func(*media.PlaybackState)) {
	updates := make(chan media.PlaybackState, 12)
	done := make(chan *media.PlaybackState, 1)
	go func() {
		report := func(state media.PlaybackState) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = client.Report(ctx, state)
		}
		for state := range updates {
			report(state)
		}
		if final := <-done; final != nil {
			report(*final)
		}
	}()
	return func(value media.PlaybackState) {
			select {
			case updates <- value:
			default:
			}
		}, func(final *media.PlaybackState) {
			done <- final
			close(updates)
		}
}

func playbackClock(value time.Duration) string {
	seconds := max(0, int(value/time.Second))
	if hours := seconds / 3600; hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func playMethod(cx *core.Scope, method media.PlayMethod) string {
	switch method {
	case media.Transcode:
		return locales.Transcode(cx)
	case media.DirectStream:
		return locales.DirectStream(cx)
	default:
		return locales.DirectPlay(cx)
	}
}
