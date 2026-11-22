package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/internal/playback"
	"waygu-video/internal/playback/mpv"
	"waygu-video/locales"
)

const (
	trackSubtitleTab = iota
	trackAudioTab
	trackSpeedTab
	trackQualityTab
)

type trackControls struct {
	button, dialog, captions  core.Node
	show                      func(int)
	panelOpen                 func() bool
	subtitle, audio           func() string
	cycleSubtitle, cycleAudio func()
}

type subtitleSelection struct {
	id   string
	name string
	cues *playback.Captions
}

func playerTracks(cx *core.Scope, stream func() media.Stream, decoder func() *desktopVideo, initialSubtitle string, prefs func() subtitlePreferences, position *core.Signal[time.Duration],
	rate *core.Signal[float32], canChangeRate, switchingPlayback, audioError *core.Signal[bool],
	chooseAudio func(media.Track), quality, styleSettings func(*core.Scope) core.Node) trackControls {
	open := core.NewSignal(cx, false)
	tab := core.NewSignal(cx, trackSubtitleTab)
	showingStyle := core.NewSignal(cx, false)
	close := func() {
		open.Set(false)
		showingStyle.Set(false)
	}
	show := func(tabID int) {
		tab.Set(tabID)
		showingStyle.Set(false)
		open.Set(true)
	}
	subtitles := core.NewSignal(cx, subtitleSelection{})
	delay := core.NewSignal(cx, time.Duration(0))
	loading := core.NewSignal(cx, false)
	failed := core.NewSignal(cx, false)
	trackSource := core.NewMemo(cx, func() string { return stream().SourceID })
	viewContext := cx.Context()
	finishSubtitles := func(selection subtitleSelection, err error) {
		loading.Set(false)
		failed.Set(err != nil)
		if err == nil {
			subtitles.Set(selection)
		}
	}
	applySubtitles := func(selection subtitleSelection) {
		d := core.Untracked(cx, decoder)
		if d != nil && slices.ContainsFunc(d.tracks.GetUntracked(), selectedSubtitle) {
			loading.Set(true)
			d.selectTrack(mpv.SubtitleTrack, 0, func(err error) { finishSubtitles(selection, err) })
			return
		}
		finishSubtitles(selection, nil)
	}
	load := func(id, name string, read func(context.Context) (*playback.Captions, error)) {
		if loading.GetUntracked() {
			return
		}
		loading.Set(true)
		failed.Set(false)
		source := trackSource.GetUntracked()
		go func() {
			cues, err := read(viewContext)
			cx.Post(func() {
				if id != "local" && trackSource.GetUntracked() != source {
					loading.Set(false)
				} else if err == nil {
					applySubtitles(subtitleSelection{id: id, name: name, cues: cues})
				} else {
					finishSubtitles(subtitleSelection{}, err)
				}
			})
		}()
	}
	selectServerSubtitle := func(track media.Track) {
		load(track.ID, trackLabel(cx, track), func(ctx context.Context) (*playback.Captions, error) {
			return playback.FetchCaptions(ctx, track.URL)
		})
	}
	selectDecoderSubtitle := func(d *desktopVideo, track mpv.Track) {
		loading.Set(true)
		failed.Set(false)
		d.selectTrack(mpv.SubtitleTrack, track.ID, func(err error) {
			finishSubtitles(subtitleSelection{}, err)
		})
	}
	type trackOption struct {
		selected bool
		pick     func()
	}
	advance := func(choices []trackOption) {
		if len(choices) == 0 {
			return
		}
		index := slices.IndexFunc(choices, func(choice trackOption) bool { return choice.selected })
		choices[(index+1)%len(choices)].pick()
	}
	cycleSubtitle := func() {
		if loading.GetUntracked() {
			return
		}
		choices := []trackOption{{subtitles.GetUntracked().id == "", func() { applySubtitles(subtitleSelection{}) }}}
		native := false
		if d := core.Untracked(cx, decoder); d != nil {
			for _, track := range d.tracks.GetUntracked() {
				if track.Kind == mpv.SubtitleTrack {
					native = true
					choices[0].selected = choices[0].selected && !track.Selected
					choices = append(choices, trackOption{track.Selected, func() { selectDecoderSubtitle(d, track) }})
				}
			}
		}
		for _, track := range core.Untracked(cx, stream).Tracks {
			if track.Type == "subtitle" && track.URL != "" && (!native || track.External) {
				choices = append(choices, trackOption{track.ID == subtitles.GetUntracked().id, func() { selectServerSubtitle(track) }})
			}
		}
		advance(choices)
	}
	cycleAudio := func() {
		if switchingPlayback.GetUntracked() {
			return
		}
		var choices []trackOption
		if d := core.Untracked(cx, decoder); d != nil {
			for _, track := range d.tracks.GetUntracked() {
				if track.Kind == mpv.AudioTrack {
					choices = append(choices, trackOption{track.Selected, func() {
						d.selectTrack(mpv.AudioTrack, track.ID, func(err error) { audioError.Set(err != nil) })
					}})
				}
			}
		}
		if len(choices) == 0 {
			for _, track := range core.Untracked(cx, stream).Tracks {
				if track.Type == "audio" {
					choices = append(choices, trackOption{track.Selected, func() { chooseAudio(track) }})
				}
			}
		}
		advance(choices)
	}
	firstStream := true
	cx.Effect(func() {
		trackSource.Get()
		if choice := subtitles.GetUntracked(); choice.id != "local" {
			subtitles.Set(subtitleSelection{})
		}
		if !firstStream {
			return
		}
		firstStream = false
		available := core.Untracked(cx, stream).Tracks
		if initialSubtitle != "" {
			id := initialSubtitle
			initialSubtitle = ""
			for _, track := range available {
				if track.ID == id && track.Type == "subtitle" && track.URL != "" {
					selectServerSubtitle(track)
					return
				}
			}
		}
		if track := automaticSubtitle(available, core.Untracked(cx, prefs)); track.ID != "" {
			selectServerSubtitle(track)
		}
	})
	pick := func() {
		cx.PickFiles(core.PickFilesOptions{Types: []string{".srt", ".vtt"}, Title: locales.OpenSubtitleFile(cx)}, func(files []core.File, err error) {
			if err != nil {
				failed.Set(true)
				return
			}
			if len(files) == 0 {
				return
			}
			file := files[0]
			load("local", file.Name, func(context.Context) (*playback.Captions, error) {
				if file.Temporary {
					defer os.Remove(file.Path)
				}
				reader, err := file.Open()
				if err != nil {
					return nil, err
				}
				defer reader.Close()
				return playback.ReadCaptions(reader)
			})
		})
	}
	text := core.NewMemo(cx, func() string {
		selection := subtitles.Get()
		if selection.cues == nil {
			return ""
		}
		return selection.cues.At(position.Get() - delay.Get())
	})
	cx.Effect(func() {
		if d := decoder(); d != nil {
			d.subtitleDelay(delay.Get())
		}
	})
	tabs := []ui.Option[int]{
		{Value: trackSubtitleTab, Label: locales.Subtitles(cx), TestID: "track-tab-subtitles"},
		{Value: trackAudioTab, Label: locales.AudioTracks(cx), TestID: "track-tab-audio"},
	}
	if quality != nil {
		tabs = append(tabs, ui.Option[int]{Value: trackQualityTab, Label: locales.PlayerQuality(cx), TestID: "track-tab-quality"})
	}
	tabs = append(tabs, ui.Option[int]{Value: trackSpeedTab, Label: locales.PlaybackSpeed(cx), TestID: "track-tab-speed"})
	rows := core.Dynamic(func(cx *core.Scope) core.Node {
		activeTab := tab.Get()
		if activeTab == trackQualityTab {
			return quality(cx)
		}
		d := decoder()
		var local []mpv.Track
		if d != nil {
			local = d.tracks.Get()
		}
		switch activeTab {
		case trackAudioTab:
			if options := decoderAudio(local); len(options) != 0 {
				return audioChoices(cx, options, false, switchingPlayback.Get, audioError.Get, func(track media.Track) {
					id, err := strconv.ParseInt(track.ID, 10, 64)
					if err != nil || switchingPlayback.GetUntracked() {
						return
					}
					switchingPlayback.Set(true)
					audioError.Set(false)
					d.selectTrack(mpv.AudioTrack, id, func(err error) {
						switchingPlayback.Set(false)
						audioError.Set(err != nil)
					})
				})
			}
			return audioChoices(cx, stream().Tracks, true, switchingPlayback.Get, audioError.Get, chooseAudio)
		case trackSpeedTab:
			return speedChoices(cx, rate, canChangeRate.Get)
		}
		selection := subtitles.Get()
		nativeSubtitles := false
		decoderSelected := slices.ContainsFunc(local, selectedSubtitle)
		choices := []core.Node{
			playerRadio(cx, ui.Radio{Text: locales.SubtitlesOff(cx), TestID: "subtitles-off", Disabled: loading.Get,
				Checked:  func() bool { return !decoderSelected && subtitles.Get().id == "" },
				OnChange: func(bool) { applySubtitles(subtitleSelection{}) }}, "", false),
		}
		for _, track := range local {
			if track.Kind != mpv.SubtitleTrack {
				continue
			}
			nativeSubtitles = true
			descriptor := trackFromDecoder(track)
			label := trackLabel(cx, descriptor)
			if !track.External {
				label += " · " + locales.EmbeddedSubtitle(cx)
			}
			if phoneLandscape(cx) {
				label = playerTrackTitle(cx, descriptor)
			}
			choices = append(choices, playerRadio(cx, ui.Radio{Text: label, TestID: "decoder-subtitle-" + descriptor.ID,
				Checked: func() bool { return track.Selected }, Disabled: loading.Get,
				OnChange: func(bool) { selectDecoderSubtitle(d, track) }}, playerSubtitleDetails(cx, descriptor), false))
		}
		for _, track := range stream().Tracks {
			if track.Type != "subtitle" || track.URL == "" || (nativeSubtitles && !track.External) {
				continue
			}
			label := trackLabel(cx, track)
			if phoneLandscape(cx) {
				label = playerTrackTitle(cx, track)
			}
			choices = append(choices, playerRadio(cx, ui.Radio{
				Text: label, TestID: "subtitle-track-" + track.ID, Disabled: loading.Get,
				Checked: func() bool { return subtitles.Get().id == track.ID },
				OnChange: func(bool) {
					selectServerSubtitle(track)
				},
			}, playerSubtitleDetails(cx, track), false))
		}
		if selection.id == "local" {
			choices = append(choices, playerRadio(cx, ui.Radio{Text: selection.name, Checked: func() bool { return true }},
				locales.ExternalSubtitle(cx), false))
		}
		landscape := phoneLandscape(cx)
		available := selection.cues != nil || decoderSelected
		var rows []core.Node
		if landscape {
			choices = append(choices, playerPanelAction(cx, "open-subtitle-file", locales.OpenSubtitleFile(cx), lucide.FolderOpen, true, loading.Get, pick))
			rows = append(rows, playerRadioList(cx, locales.Subtitles(cx), choices))
		} else {
			rows = []core.Node{
				ui.RadioGroup{Label: locales.Subtitles(cx), Children: choices},
				ui.Button{Text: locales.OpenSubtitleFile(cx), Icon: lucide.FolderOpen, Variant: ui.ButtonOutline,
					Busy: loading.Get, TestID: "open-subtitle-file", OnPress: pick},
			}
		}
		if landscape || available {
			shift := func(delta time.Duration) {
				delay.Update(func(before time.Duration) time.Duration {
					return min(max(before+delta, -10*time.Second), 10*time.Second)
				})
			}
			if landscape {
				rows = append(rows, playerStepRow(cx, locales.PhonePlayerSubtitleDelay(cx), func() string {
					return fmt.Sprintf("%+.1fs", delay.Get().Seconds())
				}, "subtitle-earlier", locales.SubtitleEarlier(cx), "subtitle-later", locales.SubtitleLater(cx),
					func() bool { return !available }, func(delta int) { shift(time.Duration(delta) * 500 * time.Millisecond) }))
			} else {
				rows = append(rows, core.View{Class: "flex-row flex-wrap items-center gap-2", Children: []core.Node{
					core.Text{ContentFunc: func() string {
						return locales.SubtitleDelay(cx, fmt.Sprintf("%+.1f", delay.Get().Seconds()))
					}, Class: "flex-1 text-sm"},
					ui.IconButton{Label: locales.SubtitleEarlier(cx), Icon: lucide.Minus, Size: 44,
						TestID: "subtitle-earlier", OnPress: func() { shift(-500 * time.Millisecond) }},
					ui.IconButton{Label: locales.SubtitleLater(cx), Icon: lucide.Plus, Size: 44,
						TestID: "subtitle-later", OnPress: func() { shift(500 * time.Millisecond) }},
				}})
			}
		}
		if landscape && styleSettings != nil {
			rows = append(rows, playerPanelAction(cx, "player-subtitle-style", locales.SettingsSubtitleStyle(cx),
				lucide.Paintbrush, false, nil, func() { showingStyle.Set(true) }))
		}
		if failed.Get() {
			rows = append(rows, notice(locales.SubtitleLoadError(cx)))
		}
		return core.View{Class: "gap-3", Children: rows}
	})
	sheet := ui.Dialog{Open: open.Get, OnOpenChange: func(show bool, _ base.Reason) { open.Set(show) },
		Title: locales.PlayerTracks(cx), TestID: "player-tracks-dialog",
		Children: []core.Node{
			ui.Tabs[int]{Options: tabs, Value: tab.Get, OnChange: tab.Set, Equal: true, TestID: "player-track-tabs"},
			core.Scroll{StyleFunc: func() core.Style {
				return core.Style{MaxHeight: core.Pt(min(340, max(105, cx.Env().Window().Height*0.44))), FlexShrink: 1}
			}, Children: []core.Node{rows}},
		},
		Footer: []core.Node{ui.Button{Text: locales.TracksDone(cx), TestID: "close-player-tracks", OnPress: func() { open.Set(false) }}},
	}
	dialog := core.Dynamic(func(cx *core.Scope) core.Node {
		if phoneLandscape(cx) {
			title := locales.PlayerTracks(cx)
			heading := core.Dynamic(func(cx *core.Scope) core.Node {
				if showingStyle.Get() {
					return playerLabeledAction(cx, "player-subtitle-style-back", locales.SettingsSubtitleStyle(cx),
						lucide.ChevronLeft, func() { showingStyle.Set(false) })
				}
				return panelHeading(title)
			})
			panelTabs := core.Show{When: func() bool { return !showingStyle.Get() }, Then: func(cx *core.Scope) core.Node {
				return playerPanelTabs(cx, "player-track-tabs", title, tabs, tab)
			}}
			body := core.Dynamic(func(cx *core.Scope) core.Node {
				if showingStyle.Get() && styleSettings != nil {
					return styleSettings(cx)
				}
				return rows
			})
			return playerSidePanel(cx, "player-tracks-dialog", "close-player-tracks", title, open.Get, close, heading, panelTabs, body)
		}
		if cx.Env().OS() == core.OSLinux {
			// Remove the closed layer before the GTK video handles input again.
			return core.Show{When: open.Get, Then: func(*core.Scope) core.Node { return sheet }}
		}
		return sheet
	})
	return trackControls{
		button: ui.Button{Label: locales.PlayerTracks(cx), Icon: lucide.Captions, Variant: ui.ButtonSecondary,
			TestID: "player-tracks", OnPress: func() { show(trackSubtitleTab) }},
		dialog: dialog, show: show, panelOpen: open.Get, cycleSubtitle: cycleSubtitle, cycleAudio: cycleAudio,
		subtitle: func() string {
			if active := subtitles.Get(); active.name != "" {
				return active.name
			}
			if d := decoder(); d != nil {
				current := d.tracks.Get()
				if index := slices.IndexFunc(current, selectedSubtitle); index >= 0 {
					return trackLabel(cx, trackFromDecoder(current[index]))
				}
			}
			return locales.SubtitlesOff(cx)
		},
		audio: func() string {
			audio := stream().Tracks
			if d := decoder(); d != nil {
				if local := decoderAudio(d.tracks.Get()); len(local) > 0 {
					audio = local
				}
			}
			var fallback string
			for _, track := range audio {
				if track.Type != "audio" {
					continue
				}
				if track.Selected {
					return trackLabel(cx, track)
				}
				if fallback == "" {
					fallback = trackLabel(cx, track)
				}
			}
			return fallback
		},
		captions: core.Show{When: func() bool { return text.Get() != "" }, Then: func(cx *core.Scope) core.Node {
			return core.View{Class: "absolute inset-x-0 items-center px-8", PointerEvents: core.PointerEventsNone,
				StyleFunc: func() core.Style {
					bottom := cx.Env().Window().Height * core.Points(prefs().Position) / 100
					if phoneLandscape(cx) && open.Get() {
						return core.Style{Right: core.Pt(360), Bottom: core.Pt(16 + bottom)}
					}
					return core.Style{Bottom: core.Pt(104 + bottom)}
				}, Children: []core.Node{core.Text{ContentFunc: text.Get, TestID: "subtitle-caption",
					Class: "rounded px-3 py-1 text-center font-semibold", StyleFunc: func() core.Style { return prefs().captionStyle() }}}}
		}},
	}
}

func selectedSubtitle(track mpv.Track) bool {
	return track.Kind == mpv.SubtitleTrack && track.Selected
}

func decoderAudio(tracks []mpv.Track) []media.Track {
	var audio []media.Track
	for _, track := range tracks {
		if track.Kind == mpv.AudioTrack {
			audio = append(audio, trackFromDecoder(track))
		}
	}
	return audio
}

func trackFromDecoder(track mpv.Track) media.Track {
	kind := "subtitle"
	if track.Kind == mpv.AudioTrack {
		kind = "audio"
	}
	return media.Track{ID: strconv.FormatInt(track.ID, 10), Type: kind, Title: track.Title, Language: track.Language,
		Codec: track.Codec, Forced: track.Forced, External: track.External, Selected: track.Selected}
}

func audioChoices(cx *core.Scope, tracks []media.Track, defaultToFirst bool, busy, failed func() bool, choose func(media.Track)) core.Node {
	var options []media.Track
	selected := ""
	for _, track := range tracks {
		if track.Type != "audio" {
			continue
		}
		options = append(options, track)
		if track.Selected {
			selected = track.ID
		}
	}
	if len(options) == 0 {
		return hint(locales.NoAudioTracks(cx))
	}
	if selected == "" && defaultToFirst {
		selected = options[0].ID
		for _, track := range options {
			if track.Default {
				selected = track.ID
				break
			}
		}
	}
	var rows []core.Node
	for _, track := range options {
		title := trackLabel(cx, track)
		detail := strings.ToUpper(track.Codec)
		if phoneLandscape(cx) {
			title = playerTrackTitle(cx, track)
		}
		rows = append(rows, playerRadio(cx, ui.Radio{Text: title, TestID: "audio-track-" + track.ID, Disabled: busy,
			Checked: func() bool { return selected == track.ID }, OnChange: func(bool) { choose(track) }}, detail, true))
	}
	return core.View{Class: "gap-2.5", Children: []core.Node{
		playerRadioList(cx, locales.AudioTracks(cx), rows),
		core.Show{When: busy, Then: func(cx *core.Scope) core.Node { return hint(locales.Loading(cx)) }},
		core.Show{When: failed, Then: func(cx *core.Scope) core.Node { return notice(locales.AudioSwitchError(cx)) }},
	}}
}

func speedChoices(cx *core.Scope, rate *core.Signal[float32], supported func() bool) core.Node {
	var rows []core.Node
	rates := playbackRates
	if phoneLandscape(cx) {
		rates = []float32{0.5, 0.75, 1, 1.25, 1.5, 2, 2.5, 3}
	}
	for _, speed := range rates {
		label := strconv.FormatFloat(float64(speed), 'f', -1, 32)
		id := "playback-speed-" + label
		checked := func() bool { return rate.Get() == speed }
		disabled := func() bool { return !supported() }
		choose := func(bool) { rate.Set(speed) }
		if phoneLandscape(cx) {
			rows = append(rows, playerChoice(cx, id, playbackRateLabel(speed), "h-10 w-[72px]", checked, disabled, choose))
		} else {
			rows = append(rows, ui.Radio{Text: label + "×", TestID: id, Disabled: disabled, Checked: checked, OnChange: choose})
		}
	}
	var choices core.Node = ui.RadioGroup{Label: locales.PlaybackSpeed(cx), Horizontal: true, Children: rows}
	if phoneLandscape(cx) {
		choices = base.RadioGroup{Label: locales.PlaybackSpeed(cx), Class: "flex-row flex-wrap gap-2", Children: rows}
	}
	return core.View{Class: "gap-3", Children: []core.Node{
		choices,
		core.Show{When: func() bool { return phoneLandscape(cx) && supported() }, Then: func(cx *core.Scope) core.Node {
			return core.Text{Content: locales.PhonePlayerDanmakuSpeed(cx), Class: "text-xs leading-relaxed text-zinc-500"}
		}},
		core.Show{When: func() bool { return !supported() }, Then: func(cx *core.Scope) core.Node {
			return hint(locales.PlaybackSpeedUnavailable(cx))
		}},
	}}
}

func trackLabel(cx *core.Scope, track media.Track) string {
	label := track.Title
	if label == "" {
		label = track.Language
		if label == "" {
			label = locales.TrackNumber(cx, track.ID)
		}
		if track.Codec != "" {
			label += " · " + strings.ToUpper(track.Codec)
		}
	}
	if track.Forced {
		label += " · " + locales.ForcedSubtitle(cx)
	} else if track.External {
		label += " · " + locales.ExternalSubtitle(cx)
	}
	return label
}
