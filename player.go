package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
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
	close  func()
}

func (a *application) player(cx *core.Scope, current route) core.Node {
	theme := wayguTheme()
	theme.Light = theme.Dark
	cx.SetTheme(theme)
	platform := cx.Env().OS()
	noServerSubtitles := -1
	options, remembered := a.initialPlaybackOptions(current)
	options.SubtitleIndex = &noServerSubtitles
	resource := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) (playerMedia, error) {
		client, err := a.client(current.source)
		if err != nil {
			return playerMedia{}, err
		}
		stream, err := openPlaybackVersion(ctx, client, current.item, options, remembered)
		if err != nil {
			return playerMedia{}, err
		}
		return prepareVideo(ctx, client, stream, platform == core.OSAndroid)
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

func prepareVideo(lifetime context.Context, client media.Client, stream media.Stream, android bool) (playerMedia, error) {
	medium := playerMedia{client: client, stream: stream, url: stream.URL, close: func() {}}
	if len(stream.Headers) == 0 && !(android && strings.HasPrefix(stream.URL, "http://")) {
		return medium, nil
	}
	address, closeProxy, err := playback.Open(stream.URL, stream.Headers)
	if err != nil {
		return playerMedia{}, err
	}
	stop := context.AfterFunc(lifetime, closeProxy)
	medium.url = address
	medium.close = func() {
		if stop() {
			closeProxy()
		}
	}
	return medium, nil
}

func (a *application) playerView(cx *core.Scope, current route, initial playerMedia) core.Node {
	medium := core.NewSignalFunc(cx, initial, nil)
	ref := &core.Ref{}
	seekVideo := ref.Seek
	position := core.NewSignal(cx, time.Duration(0))
	if current.resume {
		position.Set(initial.stream.Position)
	}
	duration := core.NewSignal(cx, initial.stream.Duration)
	ended := core.NewSignal(cx, false)
	nextEpisode := a.nextEpisodeCard(cx, current, initial.client, position, duration, ended)
	sample := core.NewSignal(cx, videoSample{})
	picture := core.NewSignal(cx, core.Size{})
	paused := core.NewSignal(cx, false)
	rate := core.NewSignal(cx, float32(1))
	canChangeRate := core.NewSignal(cx, false)
	failure := core.NewSignal(cx, false)
	buffering := core.NewSignal(cx, false)
	switchingAudio := core.NewSignal(cx, false)
	audioError := core.NewSignal(cx, false)
	desktop := core.NewSignal[*desktopVideo](cx, nil)
	var danmakuTrack *danmaku.Track
	loaded := false
	var lastReport time.Time
	enqueue, finish := playbackReports(initial.client)
	snapshot := func(event media.PlaybackEvent) media.PlaybackState {
		stream := medium.GetUntracked().stream
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
		medium.GetUntracked().close()
		if loaded && !ended.GetUntracked() {
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
		ended.Set(false)
		position.Set(at)
		sample.Set(videoSample{at, cx.Now()})
		seekVideo(at)
		report(media.Progress)
	}
	playerContext := cx.Context()
	preferMPV := cx.Env().Desktop() && a.mpvPreferred.GetUntracked()
	android := cx.Env().OS() == core.OSAndroid
	bitrate := a.bitrateLimit.GetUntracked()
	noServerSubtitles := -1
	chooseAudio := func(track media.Track) {
		index, err := strconv.Atoi(track.ID)
		if err != nil || track.Selected || switchingAudio.GetUntracked() {
			return
		}
		previous := medium.GetUntracked()
		switchingAudio.Set(true)
		audioError.Set(false)
		go func() {
			ctx, cancel := context.WithTimeout(playerContext, 35*time.Second)
			defer cancel()
			stream, err := previous.client.Playback(ctx, current.item, media.PlaybackOptions{
				SourceID: previous.stream.SourceID, MaxBitrate: bitrate, AudioIndex: &index, SubtitleIndex: &noServerSubtitles,
			})
			var next playerMedia
			if err == nil {
				next, err = prepareVideo(playerContext, previous.client, stream, android)
			}
			cx.Post(func() {
				switchingAudio.Set(false)
				if err != nil {
					audioError.Set(true)
					return
				}
				if loaded && !ended.GetUntracked() {
					enqueue(snapshot(media.Stopped))
				}
				if ended.GetUntracked() {
					position.Set(0)
				}
				loaded = false
				ended.Set(false)
				failure.Set(false)
				buffering.Set(true)
				canChangeRate.Set(false)
				desktop.Set(nil)
				previous.close()
				medium.Set(next)
			})
		}()
	}
	image := core.Dynamic(func(cx *core.Scope) core.Node {
		active := medium.Get()
		video := core.Video{Source: active.url, PausedFunc: paused.Get, RateFunc: rate.Get, Ref: ref,
			TestID: "video-player", Class: "absolute inset-0", ResizeMode: core.Contain, ProgressInterval: 150 * time.Millisecond,
			AudioSession: core.AudioSessionPlayback,
			OnLoad: func(info core.VideoInfo) {
				picture.Set(info.Size)
				if info.Duration > 0 {
					duration.Set(info.Duration)
				}
				loaded = true
				a.rememberPlaybackVersion(current, active.client, active.stream.SourceID)
				buffering.Set(false)
				canChangeRate.Set(info.RateChangeable)
				if !info.RateChangeable {
					rate.Set(1)
				}
				at := position.GetUntracked()
				if at > 0 && (duration.GetUntracked() <= 0 || at < duration.GetUntracked()) {
					sample.Set(videoSample{at, cx.Now()})
					seekVideo(at)
				} else {
					position.Set(0)
					sample.Set(videoSample{0, cx.Now()})
				}
				report(media.Started)
			},
			OnProgress: func(at, length time.Duration) {
				if !loaded {
					return
				}
				position.Set(at)
				sample.Set(videoSample{at, cx.Now()})
				if length > 0 {
					duration.Set(length)
				}
				if !ended.GetUntracked() && cx.Now().Sub(lastReport) >= 10*time.Second {
					report(media.Progress)
				}
			},
			OnPause: func(core.VideoPauseReason) {
				if !paused.GetUntracked() && !ended.GetUntracked() {
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
				ended.Set(true)
				report(media.Stopped)
			},
		}
		if preferMPV {
			decoder := mpvVideo(cx, video)
			decoder.comments(danmakuTrack)
			desktop.Set(&decoder)
			seekVideo = func(at time.Duration) {
				if decoder.active() {
					decoder.seek(at)
				} else {
					ref.Seek(at)
				}
			}
			return decoder.node
		}
		return video
	})
	comments := playerDanmaku(cx, sample, picture, paused, buffering, rate.Get, func() bool {
		decoder := desktop.Get()
		return decoder == nil || !decoder.active()
	}, func(track *danmaku.Track) {
		danmakuTrack = track
		if decoder := desktop.GetUntracked(); decoder != nil {
			decoder.comments(track)
		}
	})
	directDecoder := func() *desktopVideo {
		decoder := desktop.Get()
		if decoder == nil || !decoder.active() || medium.Get().stream.Method != media.DirectPlay {
			return nil
		}
		return decoder
	}
	tracks := playerTracks(cx, func() []media.Track { return medium.Get().stream.Tracks }, directDecoder,
		position, rate, canChangeRate, switchingAudio, audioError, chooseAudio)
	toggle := func() {
		if ended.GetUntracked() {
			seek(0)
			report(media.Started)
		}
		paused.Update(func(value bool) bool { return !value })
		report(media.Progress)
	}
	return core.View{Class: "relative flex-1 overflow-hidden bg-black", Style: core.Style{FlexShrink: 1}, Children: []core.Node{
		image,
		comments.overlay,
		tracks.captions,
		core.View{Class: "absolute left-0 right-0 top-0 flex-row items-center gap-3 bg-linear-to-b from-black/90 to-transparent p-5",
			Style: core.Style{PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea}}, Children: []core.Node{
				ui.Button{Label: locales.ClosePlayer(cx), Icon: lucide.ChevronLeft, Variant: ui.ButtonSecondary, OnPress: a.back, TestID: "close-player"},
				core.View{Class: "flex-1 gap-1", Style: core.Style{FlexShrink: 1}, Children: []core.Node{
					core.Text{Content: current.item.Title, Class: "text-lg font-semibold text-white", TestID: "player-title"},
					core.Text{ContentFunc: func() string { return current.source.Name + " · " + playMethod(cx, medium.Get().stream.Method) }, Class: "text-xs text-zinc-300"},
				}},
				comments.buttons,
				tracks.button,
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
				nextEpisode,
				core.View{Class: "flex-row items-center gap-3", Children: []core.Node{
					core.Text{ContentFunc: func() string { return playbackClock(position.Get()) }, Class: "text-xs text-white", TestID: "player-position"},
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
		tracks.dialog,
	}}
}

func playbackReports(client media.Client) (func(media.PlaybackState), func(*media.PlaybackState)) {
	var mu sync.Mutex
	var updates []media.PlaybackState
	closed := false
	wake := make(chan struct{}, 1)
	notify := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	go func() {
		for {
			mu.Lock()
			if len(updates) == 0 {
				done := closed
				mu.Unlock()
				if done {
					return
				}
				<-wake
				continue
			}
			state := updates[0]
			updates[0] = media.PlaybackState{}
			updates = updates[1:]
			mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = client.Report(ctx, state)
			cancel()
		}
	}()
	return func(value media.PlaybackState) {
			mu.Lock()
			last := len(updates) - 1
			if last >= 0 && updates[last].Event == media.Progress && value.Event == media.Progress {
				updates[last] = value
			} else {
				updates = append(updates, value)
			}
			mu.Unlock()
			notify()
		}, func(final *media.PlaybackState) {
			mu.Lock()
			if final != nil {
				updates = append(updates, *final)
			}
			closed = true
			mu.Unlock()
			notify()
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
