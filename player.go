package main

import (
	"cmp"
	"context"
	"errors"
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
	"waygu-video/internal/playback/mpv"
	"waygu-video/locales"
)

type playerMedia struct {
	client media.Client
	stream media.Stream
	url    string
	close  func()
}

func (a *application) player(cx *core.Scope, current route) core.Node {
	cx.SetSubtreeAppearance(core.AppearanceDark)
	platform := cx.Env().OS()
	if (platform == core.OSIOS || platform == core.OSAndroid) && appLayoutIdiom(cx) == core.IdiomPhone {
		cx.RequestOrientation(core.OrientationLandscape)
	}
	offline := current.downloadID != ""
	if current.resume && !offline && !current.resumeLocal {
		if entry := a.localCheckpoint(current.source, current.item); entry.Revision != "" {
			current.item = applyProgressCheckpoint(current.item, entry)
			current.resumeLocal = true
		}
	}
	var download downloadEntry
	if offline {
		download, _ = a.downloads.find(current.downloadID, a.downloads.entries.GetUntracked())
	}
	showTVHints := sync.OnceValue(a.showTVPlayerHints)
	noServerSubtitles := -1
	options, remembered := a.initialPlaybackOptions(current)
	options.SubtitleIndex = &noServerSubtitles
	subtitlePrefs := a.subtitlePrefs.GetUntracked()
	restrictions := core.Untracked(cx, a.parentalRules)
	approved := a.parentalApprovals.GetUntracked()[copyKey{current.source.ID, current.item.ID}]
	resource := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) (playerMedia, error) {
		if offline {
			return a.downloadedMedia(download, current.source, platform)
		}
		client, err := a.client(current.source)
		if err != nil {
			return playerMedia{}, err
		}
		if restrictions.Enabled {
			if item, err := client.Detail(ctx, current.item); err == nil {
				item.LibraryID = cmp.Or(item.LibraryID, current.item.LibraryID)
				item.ContentRating = cmp.Or(item.ContentRating, current.item.ContentRating)
				if len(item.Genres) == 0 {
					item.Genres = current.item.Genres
				}
				switch restrictions.assess(current.source.ID, item) {
				case parentalBlocked:
					return playerMedia{}, errParentalMedia
				case parentalUnrated:
					if !approved {
						return playerMedia{}, errParentalMedia
					}
				}
			}
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
				return a.playerView(cx, current, medium, showTVHints)
			}
			var state core.Node = hint(locales.Loading(cx))
			if err := resource.Err(); err != nil {
				message := errorText(cx, err)
				if offline {
					message = locales.DownloadFileUnavailable(cx)
				} else if errors.Is(err, errParentalMedia) {
					message = locales.ParentalBlockedTitle(cx)
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

func (a *application) playerView(cx *core.Scope, current route, initial playerMedia, showTVHints func() bool) core.Node {
	medium := core.NewSignalFunc(cx, initial, nil)
	desktopLayout := core.Untracked(cx, func() bool { return appLayoutIdiom(cx) == core.IdiomDesktop })
	phoneLayout := core.Untracked(cx, func() bool { return appLayoutIdiom(cx) == core.IdiomPhone })
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
	status, _ := a.parental.GetUntracked().viewingLimit(cx.WallTime(), a.parentalUsage.GetUntracked())
	paused := core.NewSignal(cx, status != parentalCanPlay)
	phoneVolume := core.NewSignal(cx, float32(1))
	phoneBrightness := core.NewSignal(cx, float32(1))
	prefs := a.playbackPrefs.GetUntracked()
	fit := core.NewSignal(cx, prefs.resizeMode())
	pip := core.NewSignal(cx, core.PictureInPictureState{})
	rate := core.NewSignal(cx, prefs.Rate)
	canChangeRate := core.NewSignal(cx, false)
	failure := core.NewSignal(cx, false)
	buffering := core.NewSignal(cx, false)
	switchingPlayback := core.NewSignal(cx, false)
	budgetReady := core.NewSignal(cx, false)
	blocked, parentalOverlay := a.parentalViewing(cx, func() bool {
		return budgetReady.Get() && !paused.Get() && !buffering.Get() && !ended.Get() && !failure.Get()
	}, func() { paused.Set(true) })
	videoMount := core.NewSignal(cx, uint64(0))
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
				budgetReady.Set(false)
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
		videoMount.Get()
		restrictions := a.parentalRules()
		pipAllowed := cx.Env().OS() == core.OSIOS && !(restrictions.Enabled && restrictions.TimeLimit)
		video := core.Video{Source: active.url, PausedFunc: paused.Get, RateFunc: rate.Get, Ref: ref,
			TestID: "video-player", Class: "absolute inset-0", ResizeModeFunc: fit.Get, ProgressInterval: 150 * time.Millisecond,
			AudioSession:         core.AudioSessionPlayback,
			PictureInPicture:     pipAllowed,
			AutoPictureInPicture: pipAllowed && !prefs.DisableAutoPiP,
			OnPictureInPicture:   pip.Set,
			OnLoad: func(info core.VideoInfo) {
				picture.Set(info.Size)
				if info.Duration > 0 {
					duration.Set(info.Duration)
				}
				loaded = true
				budgetReady.Set(true)
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
				if core.Untracked(cx, blocked) {
					// Native controls can resume even while the bound pause value is true.
					budgetReady.Set(false)
					videoMount.Update(func(mount uint64) uint64 { return mount + 1 })
					return
				}
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
		if desktopLayout {
			video.VolumeFunc = a.desktopPlayerVolume.Get
			video.MutedFunc = func() bool { return a.desktopPlayerVolume.Get() == 0 }
		} else if phoneLayout {
			video.VolumeFunc = phoneVolume.Get
			video.MutedFunc = func() bool { return phoneVolume.Get() == 0 }
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
	tvSettings := core.NewSignal(cx, false)
	comments := a.playerDanmaku(cx, current, initial.stream, sample, picture, paused, buffering, fit.Get, rate.Get, tvSettings.Get, func() bool {
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
		if core.Untracked(cx, blocked) {
			return
		}
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
		if appLayoutIdiom(cx) == core.IdiomTV {
			return core.Func(func(cx *core.Scope) core.Node {
				return a.tvPlayer(cx, tvPlayerControls{
					current: current, position: position, duration: duration, paused: paused,
					rate: rate, canRate: canChangeRate, bitrate: bitrate.Get, seek: seek, toggle: toggle,
					stream: activeStream, client: initial.client, tracks: tracks, comments: comments,
					qualityAvailable: quality != nil, next: nextEpisode.card, hints: showTVHints(),
				}, tvSettings)
			})
		}
		if !phoneLandscape(cx) {
			return legacyChrome
		}
		return (phonePlayerControls{
			current: current, position: position, duration: duration, paused: paused,
			canRate: canChangeRate, rate: rate, fit: fit, picture: picture, pip: pip,
			seek: seek, toggle: toggle, back: a.back, startPiP: ref.StartPictureInPicture,
			prefs: prefs, stream: activeStream, client: initial.client, next: nextEpisode, comments: comments, tracks: tracks,
			volume: phoneVolume, brightness: phoneBrightness, playbackBlocked: blocked,
		}).render(cx)
	})
	transportNotice := core.Show{When: func() bool { return buffering.Get() || failure.Get() }, Then: func(cx *core.Scope) core.Node {
		return core.View{Class: "absolute inset-0 items-center justify-center", PointerEvents: core.PointerEventsNone, Children: []core.Node{
			core.Text{Class: "rounded-xl bg-black/75 p-4 text-white", ContentFunc: func() string {
				if failure.Get() {
					return locales.VideoError(cx)
				}
				return locales.Loading(cx)
			}},
		}}
	}}
	stats := func() (mpv.Stats, bool) {
		if decoder := desktop.Get(); decoder != nil && decoder.active() {
			return decoder.stats.Get(), true
		}
		return mpv.Stats{}, false
	}
	diagnostics := a.playerDiagnostics(cx, activeStream, picture.Get, stats, buffering.Get)
	if desktopLayout {
		desktopPlayer := a.desktopPlayer(cx, desktopPlayerControls{
			current: current, client: initial.client, stream: activeStream,
			position: position, duration: duration, paused: paused, picture: picture,
			rate: rate, volume: a.desktopPlayerVolume, canRate: canChangeRate.Get, seek: seek, toggle: toggle,
			backStep: func() time.Duration { return time.Duration(a.desktopPrefs.GetUntracked().Back) * time.Second },
			nextStep: func() time.Duration { return time.Duration(a.desktopPrefs.GetUntracked().Forward) * time.Second },
			comments: comments, tracks: tracks, next: nextEpisode, qualityAvailable: quality != nil,
			stats: stats, buffering: buffering.Get,
		}, []core.Node{image, comments.overlay, tracks.captions, transportNotice, diagnostics})
		return core.View{Class: "relative flex-1", Children: []core.Node{desktopPlayer, parentalOverlay}}
	}
	dimming := core.Show{When: func() bool { return phoneLandscape(cx) && phoneBrightness.Get() < 1 }, Then: func(*core.Scope) core.Node {
		return core.View{TestID: "phone-player-dimming", Class: "absolute inset-0", PointerEvents: core.PointerEventsNone,
			StyleFunc: func() core.Style {
				return core.Style{Background: core.RGBA(0, 1-phoneBrightness.Get())}
			}}
	}}
	return core.View{Class: "relative flex-1 overflow-hidden bg-black", Style: core.Style{FlexShrink: 1},
		Children: []core.Node{image, dimming, comments.overlay, tracks.captions, chrome, transportNotice, diagnostics, tracks.dialog, comments.dialog, parentalOverlay}}
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
			if watch.superseded || update.entry.LocalOnly || !watch.a.progress.sendsFor(watch.sourceID, watch.account) {
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
			if !watch.a.progress.sendsFor(watch.sourceID, watch.account) {
				return
			}
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
				entry := watch.capture(*final)
				if watch.a.progress.sendsFor(watch.sourceID, watch.account) {
					updates = append(updates, reportUpdate{state: *final, entry: entry})
				}
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
