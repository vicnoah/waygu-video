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
	"waygu-video/internal/casting"
	"waygu-video/internal/danmaku"

	"waygu-video/internal/media"
	"waygu-video/internal/playback"
	"waygu-video/internal/playback/mpv"
	"waygu-video/locales"
)

type playerMedia struct {
	endpoint         sourceEndpoint
	client           media.Client
	stream           media.Stream
	url              string
	close            func()
	requestedBitrate int
}

func (a *application) player(cx *core.Scope, current route) core.Node {
	cx.SetSubtreeAppearance(core.AppearanceDark)
	platform := cx.Env().OS()
	if (platform == core.OSIOS || platform == core.OSAndroid) && appLayoutIdiom(cx) == core.IdiomPhone {
		cx.RequestOrientation(core.OrientationLandscape)
	}
	offline := current.downloadID != ""
	prefs := a.playbackPrefs.GetUntracked()
	network := playbackNetwork(cx, offline || current.recovery != nil)
	cellularAllowed := core.NewSignal(cx, false)
	confirmCellular := func() bool {
		return prefs.WarnOnCellular && network.Get() == transportCellular && !cellularAllowed.Get()
	}
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
	options.ForceTranscode = prefs.DisableDirectPlay
	options.SubtitleIndex = &noServerSubtitles
	subtitlePrefs := a.subtitlePrefs.GetUntracked()
	restrictions := core.Untracked(cx, a.parentalRules)
	approved := a.parentalApprovals.GetUntracked()[copyKey{current.source.ID, current.item.ID}]
	load := func(ctx context.Context, skip string, cellular bool) (playerMedia, error) {
		client, endpoint, err := a.selectSourceClient(ctx, current.source, skip)
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
		options := options
		if cellular {
			options.MaxBitrate = prefs.CellularBitrate
		}
		preset := options.MaxBitrate
		options.MaxHeight = playbackPresetHeight(preset)
		options.MaxBitrate = current.source.capBitrate(endpoint, options.MaxBitrate)
		stream, err := openPlaybackVersion(ctx, client, current.item, options, remembered)
		if err != nil {
			return playerMedia{endpoint: endpoint}, err
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
		active, err := prepareVideo(ctx, client, stream, platform == core.OSAndroid)
		active.endpoint = endpoint
		active.requestedBitrate = preset
		return active, err
	}
	resource := core.NewResource(cx, func() (bool, bool) {
		transport := network.Get()
		return transport == transportCellular, transport != transportPending && !confirmCellular()
	}, func(ctx context.Context, cellular bool, _ core.ResourceInfo) (playerMedia, error) {
		if offline {
			return a.downloadedMedia(download, current.source, platform)
		}
		if current.recovery != nil {
			return load(ctx, current.recovery.from, cellular)
		}
		active, err := load(ctx, "", cellular)
		if err != nil && active.endpoint.address != "" && a.routesEnabled.Load() && current.source.automaticRoute() && playbackRouteError(err) && ctx.Err() == nil {
			return load(ctx, active.endpoint.address, cellular)
		}
		return active, err
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
		playbackCellularWarning(cx, confirmCellular, a.back, func() { cellularAllowed.Set(true) }),
	}}
}

func prepareVideo(lifetime context.Context, client media.Client, stream media.Stream, android bool) (playerMedia, error) {
	medium := playerMedia{client: client, stream: stream, url: stream.URL, close: func() {}}
	resource := stream.Resource()
	if len(resource.Headers) == 0 && resource.Authorize == nil && !resource.Trust.Matches(resource.URL) && !(android && strings.HasPrefix(resource.URL, "http://")) {
		return medium, nil
	}
	address, closeProxy, err := playback.Open(resource)
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
	if current.recovery != nil {
		position.Set(current.recovery.position)
	}
	duration := core.NewSignal(cx, initial.stream.Duration)
	ended := core.NewSignal(cx, false)
	sleepExpired := core.NewSignal(cx, false)
	timerPreventsAutoNext := func() bool {
		deadline := a.sleepTimer.Get().deadline
		return sleepExpired.Get() || (!deadline.IsZero() && !cx.Now().Before(deadline))
	}
	offline := current.downloadID != ""
	nextEpisode := nextEpisodeControls{unavailable: func() bool { return true }}
	if !offline {
		nextEpisode = a.nextEpisodeCard(cx, current, initial.client, position, duration, ended, timerPreventsAutoNext)
	}
	sample := core.NewSignal(cx, videoSample{})
	picture := core.NewSignal(cx, core.Size{})
	status, _ := a.parental.GetUntracked().viewingLimit(cx.WallTime(), a.parentalUsage.GetUntracked())
	paused := core.NewSignal(cx, status != parentalCanPlay || current.castOnOpen || (current.recovery != nil && current.recovery.paused))
	phoneVolume := core.NewSignal(cx, float32(1))
	phoneBrightness := core.NewSignal(cx, float32(1))
	prefs := a.playbackPrefs.GetUntracked()
	pictureMode := core.NewSignal(cx, playerPictureMode(prefs.Picture))
	fit := func() core.ResizeMode { return pictureMode.Get().fit() }
	viewport := core.NewSignal(cx, core.Size{})
	loop := segmentLoop{segment: core.NewSignal(cx, playbackSegment{})}
	skip := markerSkip{manual: -1}
	markers := a.playbackMarkers(cx, current, initial.client, func() media.Stream { return medium.Get().stream },
		func() bool { return !offline && !a.playbackPrefs.Get().KeepIntroCredits })
	pip := core.NewSignal(cx, core.PictureInPictureState{})
	rate := core.NewSignal(cx, prefs.Rate)
	canChangeRate := core.NewSignal(cx, false)
	failure := core.NewSignal(cx, false)
	buffering := core.NewSignal(cx, false)
	switchingPlayback := core.NewSignal(cx, false)
	budgetReady := core.NewSignal(cx, false)
	var projection *playerCast
	blocked, parentalOverlay := a.parentalViewing(cx, func() bool {
		return budgetReady.Get() && !paused.Get() && !buffering.Get() && !ended.Get() &&
			(!failure.Get() || (projection != nil && projection.active()))
	}, func() { paused.Set(true) })
	videoMount := core.NewSignal(cx, uint64(0))
	audioError := core.NewSignal(cx, false)
	qualityError := core.NewSignal(cx, false)
	desktop := core.NewSignal[*desktopVideo](cx, nil)
	var danmakuTrack *danmaku.Track
	loaded := false
	var lastReport time.Time
	enqueue := func(media.PlaybackState) {}
	finish := func(*media.PlaybackState) {}
	if !prefs.DisableProgressRecording {
		watch := a.beginProgressWatch(current, initial)
		if offline {
			enqueue, finish = a.localPlaybackReports(current, watch)
		} else {
			enqueue, finish = playbackReports(initial.client, watch)
		}
	}
	snapshot := func(event media.PlaybackEvent) media.PlaybackState {
		stream := medium.GetUntracked().stream
		if projection != nil && projection.active() {
			stream = projection.castingStream
		}
		stream.Duration = duration.GetUntracked()
		return media.PlaybackState{Event: event, Stream: stream, Position: position.GetUntracked(), Paused: paused.GetUntracked()}
	}
	report := func(event media.PlaybackEvent) {
		if loaded || (projection != nil && projection.active()) {
			lastReport = cx.Now()
			enqueue(snapshot(event))
		}
	}
	cx.Cleanup(func() {
		medium.GetUntracked().close()
		if (loaded || (projection != nil && projection.active())) && !ended.GetUntracked() {
			final := snapshot(media.Stopped)
			finish(&final)
		} else {
			finish(nil)
		}
	})
	seekPosition := func(at time.Duration) {
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
	seek := func(at time.Duration) {
		if projection != nil && projection.active() {
			projection.command(castCommand{action: castSeek, at: max(0, min(at, duration.GetUntracked()))})
			return
		}
		at = max(0, min(at, duration.GetUntracked()))
		loop.manualSeek(at)
		skip.manualSeek(at)
		seekPosition(at)
	}
	finishPlayback := func() {
		if ended.GetUntracked() {
			return
		}
		position.Set(duration.GetUntracked())
		paused.Set(true)
		ended.Set(true)
		report(media.Stopped)
	}
	a.playerSleep(cx, func() {
		sleepExpired.Set(true)
		if !paused.GetUntracked() {
			if projection != nil && projection.active() {
				if projection.busy.GetUntracked() {
					projection.cancel()
				} else {
					projection.command(castCommand{action: castToggle})
				}
			}
			paused.Set(true)
			report(media.Progress)
		}
	})
	castStarted := false
	projection = newPlayerCast(cx, current, medium, position, paused, blocked,
		func(at casting.Position, state string) {
			if loop.segment.GetUntracked().hasStart {
				loop.clear()
			}
			if at.Duration > 0 && duration.GetUntracked() <= 0 {
				duration.Set(at.Duration)
			}
			if at.Available {
				stamp := max(0, min(at.At, duration.GetUntracked()))
				position.Set(stamp)
				sample.Set(videoSample{stamp, cx.Now()})
			}
			switch state {
			case "PLAYING":
				paused.Set(false)
				buffering.Set(false)
			case "PAUSED_PLAYBACK":
				paused.Set(true)
				buffering.Set(false)
			case "TRANSITIONING":
				buffering.Set(true)
			}
			if !castStarted {
				castStarted = true
				ended.Set(false)
				budgetReady.Set(true)
				active := medium.GetUntracked()
				if loaded && active.stream.SessionID != projection.castingStream.SessionID {
					previous := snapshot(media.Stopped)
					previous.Stream = active.stream
					enqueue(previous)
				}
				a.rememberPlayedCopy(current)
				a.rememberPlaybackVersion(current, active.client, active.stream.VersionID())
				report(media.Started)
			} else {
				report(media.Progress)
			}
		}, func() {
			castStarted = false
			budgetReady.Set(loaded)
			buffering.Set(false)
			if core.Untracked(cx, blocked) || sleepExpired.GetUntracked() {
				paused.Set(true)
			}
			local := medium.GetUntracked().stream
			if remote := projection.castingStream; remote.SessionID != local.SessionID {
				final := snapshot(media.Stopped)
				final.Stream = remote
				enqueue(final)
				if loaded {
					report(media.Started)
				}
			}
			seekPosition(position.GetUntracked())
		}, func() bool { return !switchingPlayback.Get() })
	playerContext := cx.Context()
	preferMPV := cx.Env().Desktop() && a.mpvPreferred.GetUntracked()
	android := cx.Env().OS() == core.OSAndroid
	bitrate := func() int { return medium.Get().requestedBitrate }
	noServerSubtitles := -1
	switchStream := func(options media.PlaybackOptions, failed *core.Signal[bool]) {
		if switchingPlayback.GetUntracked() || projection.active() {
			return
		}
		options.SubtitleIndex = &noServerSubtitles
		options.ForceTranscode = prefs.DisableDirectPlay
		previous := medium.GetUntracked()
		preset := options.MaxBitrate
		options.MaxHeight = playbackPresetHeight(preset)
		options.MaxBitrate = current.source.capBitrate(previous.endpoint, options.MaxBitrate)
		switchingPlayback.Set(true)
		failed.Set(false)
		go func() {
			ctx, cancel := context.WithTimeout(playerContext, 35*time.Second)
			defer cancel()
			stream, err := previous.client.Playback(ctx, current.item, options)
			var next playerMedia
			if err == nil {
				next, err = prepareVideo(playerContext, previous.client, stream, android)
				next.endpoint = previous.endpoint
				next.requestedBitrate = preset
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
				if next.stream.VersionID() != previous.stream.VersionID() {
					loop.clear()
					skip = markerSkip{manual: -1}
				}
				previous.close()
				medium.Set(next)
			})
		}()
	}
	chooseAudio := func(track media.Track) {
		index, err := strconv.Atoi(track.ID)
		if err == nil && !track.Selected {
			active := medium.GetUntracked()
			switchStream(media.PlaybackOptions{SourceID: active.stream.VersionID(),
				MaxBitrate: active.requestedBitrate, AudioIndex: &index}, audioError)
		}
	}
	image := core.Dynamic(func(cx *core.Scope) core.Node {
		active := medium.Get()
		videoMount.Get()
		restrictions := a.parentalRules()
		pipAllowed := cx.Env().OS() == core.OSIOS && !(restrictions.Enabled && restrictions.TimeLimit)
		video := core.Video{Source: active.url, PausedFunc: func() bool { return paused.Get() || projection.active() }, RateFunc: rate.Get, Ref: ref,
			TestID: "video-player", Class: "absolute inset-0", ResizeModeFunc: fit, ProgressInterval: 150 * time.Millisecond,
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
				canChangeRate.Set(info.RateChangeable)
				if !info.RateChangeable {
					rate.Set(1)
				}
				if projection.active() {
					return
				}
				if !offline {
					a.rememberPlayedCopy(current)
					a.rememberPlaybackVersion(current, active.client, active.stream.VersionID())
				}
				buffering.Set(false)
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
				if !loaded || ended.GetUntracked() || projection.active() {
					return
				}
				if length > 0 {
					duration.Set(length)
				}
				length, now := duration.GetUntracked(), cx.Now()
				if loop.boundary(at, length, now, seekPosition) || skip.pending(at, now) {
					return
				}
				if length > 0 && !paused.GetUntracked() && !loop.segment.GetUntracked().active && !core.Untracked(cx, blocked) {
					target, final := skip.next(core.Untracked(cx, markers), at, length, now)
					if final {
						finishPlayback()
						return
					}
					if target > 0 {
						seekPosition(target)
						return
					}
				}
				position.Set(at)
				sample.Set(videoSample{at, cx.Now()})
				if now.Sub(lastReport) >= 10*time.Second {
					report(media.Progress)
				}
			},
			OnPause: func(core.VideoPauseReason) {
				if projection.active() {
					return
				}
				if !paused.GetUntracked() && !ended.GetUntracked() {
					paused.Set(true)
					report(media.Progress)
				}
			},
			OnPlay: func() {
				if projection.active() {
					return
				}
				if core.Untracked(cx, blocked) {
					// Native controls can resume even while the bound pause value is true.
					budgetReady.Set(false)
					videoMount.Update(func(mount uint64) uint64 { return mount + 1 })
					return
				}
				if paused.GetUntracked() {
					sleepExpired.Set(false)
					paused.Set(false)
					report(media.Progress)
				}
			},
			OnBuffer: func(value bool) {
				if !projection.active() {
					buffering.Set(value)
				}
			},
			OnError: func(error) {
				if !offline && !ended.GetUntracked() && !projection.active() &&
					a.recoverPlayer(current, active, position.GetUntracked(), paused.GetUntracked()) {
					return
				}
				failure.Set(true)
			},
			OnEnd: func() {
				if projection.active() {
					return
				}
				if loop.boundary(duration.GetUntracked(), duration.GetUntracked(), cx.Now(), seekPosition) {
					return
				}
				finishPlayback()
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
	pictureArea := func() core.Bounds { return pictureMode.Get().frame(viewport.Get()) }
	comments := a.playerDanmaku(cx, current, initial.stream, sample, picture, paused, buffering, fit, pictureArea, rate.Get, tvSettings.Get, func() bool {
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
		quality = playerQuality(current, initial.client, activeStream, bitrate, switchingPlayback.Get, qualityError.Get,
			func(options media.PlaybackOptions) { switchStream(options, qualityError) })
	}
	tracks := playerTracks(cx, activeStream, directDecoder, current.subtitleTrack, a.subtitlePrefs.Get, position, rate, canChangeRate, switchingPlayback, audioError, chooseAudio, quality, a.playerSubtitleStyle)
	toggle := func() {
		if core.Untracked(cx, blocked) {
			return
		}
		if projection.active() {
			projection.command(castCommand{action: castToggle, playing: paused.GetUntracked()})
			return
		}
		if ended.GetUntracked() {
			start := time.Duration(0)
			if segment := loop.segment.GetUntracked(); segment.active {
				start = segment.start
			}
			skip = markerSkip{manual: -1}
			seekPosition(start)
			report(media.Started)
		}
		sleepExpired.Set(false)
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
				core.Show{When: func() bool { return projection.available }, Then: func(cx *core.Scope) core.Node {
					return ui.Button{Label: locales.PhonePlayerCast(cx), Icon: lucide.Cast, TestID: "legacy-player-cast",
						Variant: ui.ButtonSecondary, OnPress: func() { projection.open.Set(true) }}
				}},
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
					rate: rate, canRate: canChangeRate, bitrate: func() int {
						active := medium.Get()
						return current.source.capBitrate(active.endpoint, active.requestedBitrate)
					}, seek: seek, toggle: toggle,
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
			canRate: canChangeRate, rate: rate, pictureMode: pictureMode, picture: picture, pip: pip,
			seek: seek, toggle: toggle, back: a.back, startPiP: ref.StartPictureInPicture,
			prefs: prefs, stream: activeStream, client: initial.client, next: nextEpisode, comments: comments, tracks: tracks,
			volume: phoneVolume, brightness: phoneBrightness, playbackBlocked: blocked,
			loop: &loop, sleepTimer: a.sleepTimer, ready: func() bool { return budgetReady.Get() && !projection.active() },
			cast: projection,
		}).render(cx)
	})
	transportNotice := core.Show{When: func() bool { return buffering.Get() || (failure.Get() && !projection.active()) }, Then: func(cx *core.Scope) core.Node {
		return core.View{Class: "absolute inset-0 items-center justify-center", PointerEvents: core.PointerEventsNone, Children: []core.Node{
			core.Text{Class: "rounded-xl bg-black/75 p-4 text-white", ContentFunc: func() string {
				if failure.Get() && !projection.active() {
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
			stats: stats, buffering: buffering.Get, cast: projection,
		}, []core.Node{image, comments.overlay, tracks.captions, transportNotice, projection.indicator(cx), diagnostics})
		return core.View{Class: "relative flex-1", Children: []core.Node{desktopPlayer, projection.panel, parentalOverlay}}
	}
	dimming := core.Show{When: func() bool { return phoneLandscape(cx) && phoneBrightness.Get() < 1 }, Then: func(*core.Scope) core.Node {
		return core.View{TestID: "phone-player-dimming", Class: "absolute inset-0", PointerEvents: core.PointerEventsNone,
			StyleFunc: func() core.Style {
				return core.Style{Background: core.RGBA(0, 1-phoneBrightness.Get())}
			}}
	}}
	pictureLayers := []core.Node{image, dimming, comments.overlay, tracks.captions}
	if phoneLayout {
		pictureLayers = []core.Node{core.View{Class: "absolute inset-0 overflow-hidden", TestID: "phone-player-picture",
			PointerEvents: core.PointerEventsBoxNone,
			StyleFunc:     func() core.Style { return pictureMode.Get().viewport(viewport.Get()) },
			Children:      pictureLayers}}
	}
	return core.View{Class: "relative flex-1 overflow-hidden bg-black", Style: core.Style{FlexShrink: 1},
		OnLayout: func(bounds core.Bounds) { viewport.Set(bounds.Size) },
		Children: append(pictureLayers, chrome, transportNotice, projection.indicator(cx), diagnostics, tracks.dialog, comments.dialog, projection.panel, parentalOverlay)}
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
