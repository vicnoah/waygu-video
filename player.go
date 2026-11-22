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
	cx.SetTheme(wayguTheme())
	cx.SetSubtreeAppearance(core.AppearanceDark)
	platform := cx.Env().OS()
	if (platform == core.OSIOS || platform == core.OSAndroid) && appLayoutIdiom(cx) == core.IdiomPhone {
		cx.RequestOrientation(core.OrientationLandscape)
	}
	offline := current.downloadID != ""
	var download downloadEntry
	if offline {
		download, _ = a.downloads.find(current.downloadID, a.downloads.entries.GetUntracked())
	}
	noServerSubtitles := -1
	options, remembered := a.initialPlaybackOptions(current)
	options.SubtitleIndex = &noServerSubtitles
	subtitlePrefs := a.subtitlePrefs.GetUntracked()
	resource := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) (playerMedia, error) {
		if offline {
			return a.downloadedMedia(download, current.source, platform)
		}
		client, err := a.client(current.source)
		if err != nil {
			return playerMedia{}, err
		}
		stream, err := openPlaybackVersion(ctx, client, current.item, options, remembered)
		if err != nil {
			return playerMedia{}, err
		}
		if options.AudioIndex == nil {
			if index := preferredAudio(stream.Tracks, subtitlePrefs.Audio); index != nil {
				preferred := options
				preferred.AudioIndex = index
				preferred.SourceID = stream.VersionID()
				if updated, err := client.Playback(ctx, current.item, preferred); err == nil {
					stream = updated
				}
			}
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
				message := errorText(cx, err)
				if offline {
					message = locales.DownloadFileUnavailable(cx)
				}
				state = core.View{Class: "items-center gap-4", Children: []core.Node{
					notice(message), ui.Button{Text: locales.Retry(cx), OnPress: resource.Refetch},
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
		if current.resumeLocal {
			position.Set(current.item.Position)
		}
	}
	duration := core.NewSignal(cx, initial.stream.Duration)
	ended := core.NewSignal(cx, false)
	offline := current.downloadID != ""
	nextEpisode := nextEpisodeControls{unavailable: func() bool { return true }}
	if !offline {
		nextEpisode = a.nextEpisodeCard(cx, current, initial.client, position, duration, ended)
	}
	sample := core.NewSignal(cx, videoSample{})
	picture := core.NewSignal(cx, core.Size{})
	paused := core.NewSignal(cx, false)
	prefs := a.playbackPrefs.GetUntracked()
	fit := core.NewSignal(cx, prefs.resizeMode())
	pip := core.NewSignal(cx, core.PictureInPictureState{})
	rate := core.NewSignal(cx, prefs.Rate)
	canChangeRate := core.NewSignal(cx, false)
	failure := core.NewSignal(cx, false)
	buffering := core.NewSignal(cx, false)
	switchingPlayback := core.NewSignal(cx, false)
	audioError := core.NewSignal(cx, false)
	qualityError := core.NewSignal(cx, false)
	desktop := core.NewSignal[*desktopVideo](cx, nil)
	var danmakuTrack *danmaku.Track
	loaded := false
	var lastReport time.Time
	watch := a.beginProgressWatch(current, initial)
	var enqueue func(media.PlaybackState)
	var finish func(*media.PlaybackState)
	if offline {
		enqueue, finish = a.localPlaybackReports(current, watch)
	} else {
		enqueue, finish = playbackReports(initial.client, watch)
	}
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
	bitrate := core.NewSignal(cx, a.bitrateLimit.GetUntracked())
	noServerSubtitles := -1
	switchStream := func(options media.PlaybackOptions, failed *core.Signal[bool]) {
		if switchingPlayback.GetUntracked() {
			return
		}
		options.SubtitleIndex = &noServerSubtitles
		previous := medium.GetUntracked()
		switchingPlayback.Set(true)
		failed.Set(false)
		go func() {
			ctx, cancel := context.WithTimeout(playerContext, 35*time.Second)
			defer cancel()
			stream, err := previous.client.Playback(ctx, current.item, options)
			var next playerMedia
			if err == nil {
				next, err = prepareVideo(playerContext, previous.client, stream, android)
			}
			cx.Post(func() {
				switchingPlayback.Set(false)
				if err != nil {
					failed.Set(true)
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
				bitrate.Set(options.MaxBitrate)
				previous.close()
				medium.Set(next)
			})
		}()
	}
	chooseAudio := func(track media.Track) {
		index, err := strconv.Atoi(track.ID)
		if err == nil && !track.Selected {
			switchStream(media.PlaybackOptions{SourceID: medium.GetUntracked().stream.VersionID(),
				MaxBitrate: bitrate.GetUntracked(), AudioIndex: &index}, audioError)
		}
	}
	image := core.Dynamic(func(cx *core.Scope) core.Node {
		active := medium.Get()
		video := core.Video{Source: active.url, PausedFunc: paused.Get, RateFunc: rate.Get, Ref: ref,
			TestID: "video-player", Class: "absolute inset-0", ResizeModeFunc: fit.Get, ProgressInterval: 150 * time.Millisecond,
			AudioSession:         core.AudioSessionPlayback,
			PictureInPicture:     cx.Env().OS() == core.OSIOS,
			AutoPictureInPicture: cx.Env().OS() == core.OSIOS && !prefs.DisableAutoPiP,
			OnPictureInPicture:   pip.Set,
			OnLoad: func(info core.VideoInfo) {
				picture.Set(info.Size)
				if info.Duration > 0 {
					duration.Set(info.Duration)
				}
				loaded = true
				if !offline {
					a.rememberPlayedCopy(current)
					a.rememberPlaybackVersion(current, active.client, active.stream.VersionID())
				}
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
	comments := a.playerDanmaku(cx, current, initial.stream, sample, picture, paused, buffering, fit.Get, rate.Get, func() bool {
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
	activeStream := func() media.Stream { return medium.Get().stream }
	var quality func(*core.Scope) core.Node
	if !offline {
		quality = playerQuality(current, initial.client, activeStream, bitrate.Get, switchingPlayback.Get, qualityError.Get,
			func(options media.PlaybackOptions) { switchStream(options, qualityError) })
	}
	tracks := playerTracks(cx, activeStream, directDecoder, current.subtitleTrack, a.subtitlePrefs.Get, position, rate, canChangeRate, switchingPlayback, audioError, chooseAudio, quality, a.playerSubtitleStyle)
	toggle := func() {
		if ended.GetUntracked() {
			seek(0)
			report(media.Started)
		}
		paused.Update(func(value bool) bool { return !value })
		report(media.Progress)
	}
	legacyChrome := core.Group([]core.Node{
		core.View{Class: "absolute left-0 right-0 top-0 flex-row items-center gap-3 bg-linear-to-b from-black/90 to-transparent p-5",
			Style: core.Style{PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea}}, Children: []core.Node{
				ui.Button{Label: locales.ClosePlayer(cx), Icon: lucide.ChevronLeft, Variant: ui.ButtonSecondary, OnPress: a.back, TestID: "close-player"},
				core.View{Class: "flex-1 gap-1", Style: core.Style{FlexShrink: 1}, Children: []core.Node{
					core.Text{Content: current.item.Title, Class: "text-lg font-semibold text-white", TestID: "player-title"},
					core.Text{TestID: "player-mode", ContentFunc: func() string {
						if offline {
							return locales.DownloadOfflinePlayback(cx)
						}
						return current.source.Name + " · " + playMethod(cx, medium.Get().stream.Method)
					}, Class: "text-xs text-zinc-300"},
				}},
				comments.buttons,
				tracks.button,
			}},
		core.View{Class: "absolute bottom-0 left-0 right-0 gap-3 bg-linear-to-t from-black to-transparent p-5",
			Style: core.Style{PaddingEnv: core.EnvEdges{Bottom: core.EnvSafeArea}}, Children: []core.Node{
				nextEpisode.card,
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
						TestID: "player-seek-back", OnPress: func() { seek(position.GetUntracked() - prefs.backStep()) }},
					core.Dynamic(func(cx *core.Scope) core.Node {
						label, icon := locales.Pause(cx), lucide.Pause
						if paused.Get() {
							label, icon = locales.Play(cx), lucide.Play
						}
						return ui.Button{Label: label, Icon: icon, Size: ui.SizeLarge, TestID: "toggle-play", OnPress: toggle}
					}),
					ui.Button{Label: locales.SeekForward(cx), Icon: lucide.RotateCw, Variant: ui.ButtonSecondary, Size: ui.SizeLarge,
						TestID: "player-seek-forward", OnPress: func() { seek(position.GetUntracked() + prefs.forwardStep()) }},
				}},
			}},
	})
	chrome := core.Dynamic(func(cx *core.Scope) core.Node {
		if !phoneLandscape(cx) {
			return legacyChrome
		}
		return (phonePlayerControls{
			current: current, position: position, duration: duration, paused: paused,
			canRate: canChangeRate, rate: rate, fit: fit, picture: picture, pip: pip,
			seek: seek, toggle: toggle, back: a.back, startPiP: ref.StartPictureInPicture,
			prefs: prefs, stream: activeStream, next: nextEpisode, comments: comments, tracks: tracks,
		}).render(cx)
	})
	return core.View{Class: "relative flex-1 overflow-hidden bg-black", Style: core.Style{FlexShrink: 1},
		Children: []core.Node{
			image, comments.overlay, tracks.captions, chrome,
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
			tracks.dialog, comments.dialog,
		}}
}

func playbackReports(client media.Client, watch *progressWatch) (func(media.PlaybackState), func(*media.PlaybackState)) {
	type reportUpdate struct {
		state media.PlaybackState
		entry pendingProgress
	}
	var mu sync.Mutex
	var updates []reportUpdate
	closed := false
	wake := make(chan struct{}, 1)
	notify := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	go func() {
		defer watch.a.scope.Post(watch.close)
		needsRecovery := false
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
			update := updates[0]
			updates[0] = reportUpdate{}
			updates = updates[1:]
			mu.Unlock()
			if needsRecovery {
				continue
			}
			watch.a.progress.transport.Lock()
			if watch.superseded {
				watch.a.progress.transport.Unlock()
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := client.Report(ctx, update.state)
			cancel()
			watch.a.progress.transport.Unlock()
			if err == nil {
				watch.a.scope.Post(func() { watch.confirmed(update.state, update.entry) })
			} else if watch.enabled {
				// Recover via a fresh read of the server checkpoint. An older
				// queued live event could otherwise write over newer progress.
				needsRecovery = true
			}
		}
	}()
	return func(value media.PlaybackState) {
			update := reportUpdate{state: value, entry: watch.capture(value)}
			mu.Lock()
			last := len(updates) - 1
			if last >= 0 && updates[last].state.Event == media.Progress && value.Event == media.Progress {
				updates[last] = update
			} else {
				updates = append(updates, update)
			}
			mu.Unlock()
			notify()
		}, func(final *media.PlaybackState) {
			mu.Lock()
			if final != nil {
				updates = append(updates, reportUpdate{state: *final, entry: watch.capture(*final)})
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
