package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

const versionPolicyKey = "playback-version-policy-v1"

func playbackVersionPolicy(value string) media.VersionPolicy {
	switch policy := media.VersionPolicy(value); policy {
	case media.VersionHighest, media.VersionFitBitrate, media.VersionRemember:
		return policy
	default:
		return media.VersionServerOrder
	}
}

func versionPolicyOptions(cx *core.Scope) []ui.Option[media.VersionPolicy] {
	return []ui.Option[media.VersionPolicy]{
		{Label: locales.ServerVersionOrder(cx), Value: media.VersionServerOrder},
		{Label: locales.HighestVersionQuality(cx), Value: media.VersionHighest},
		{Label: locales.MatchStreamingBitrate(cx), Value: media.VersionFitBitrate},
		{Label: locales.RememberLastVersion(cx), Value: media.VersionRemember},
	}
}

func versionHistoryKey(current route) string {
	return "playback-last-version-v1:" + current.source.ID + ":" + current.item.ID
}

func versionSelectionKey(current route) string {
	return "playback-version-selection-v1:" + current.source.ID + ":" + current.item.ID
}

func (a *application) initialPlaybackOptions(current route) (media.PlaybackOptions, bool) {
	options, remembered := a.automaticPlaybackOptions(current)
	if current.versionID != "" {
		options.SourceID, remembered = current.versionID, false
	} else if selected := a.savedVersion(versionSelectionKey(current)); selected != "" {
		options.SourceID, remembered = selected, true
	}
	if index, err := strconv.Atoi(current.audioTrack); err == nil {
		options.AudioIndex = &index
	}
	return options, remembered
}

func (a *application) automaticPlaybackOptions(current route) (media.PlaybackOptions, bool) {
	options := media.PlaybackOptions{MaxBitrate: a.bitrateLimit.GetUntracked(), VersionPolicy: a.versionPolicy.GetUntracked()}
	if options.VersionPolicy == media.VersionRemember {
		options.SourceID = a.savedVersion(versionHistoryKey(current))
	}
	return options, options.SourceID != ""
}

func (a *application) savedVersion(key string) string {
	// Version preferences are sampled on opening a screen. A successful
	// load also writes the history and must leave the player mounted.
	return core.Untracked(a.scope, func() string {
		version, _ := a.storage.GetItem(key)
		return version
	})
}

func openPlaybackVersion(ctx context.Context, client media.Client, item media.Item, options media.PlaybackOptions,
	remembered bool) (media.Stream, error) {
	stream, err := client.Playback(ctx, item, options)
	if remembered && (errors.Is(err, media.ErrNoStream) || errors.Is(err, media.StatusError(http.StatusNotFound))) {
		options.SourceID = ""
		return client.Playback(ctx, item, options)
	}
	return stream, err
}

func (a *application) rememberPlaybackVersion(current route, client media.Client, id string) {
	if _, ok := client.(media.VersionClient); !ok || id == "" {
		return
	}
	key := versionHistoryKey(current)
	if previous, _ := a.storage.GetItem(key); previous != id {
		_ = a.storage.SetItem(key, id)
	}
}

func (a *application) versionPicker(cx *core.Scope, current route, selected *core.Signal[string]) core.Node {
	if !playable(current.item) {
		return nil
	}
	available := a.versionResource(cx, current)
	return core.Dynamic(func(cx *core.Scope) core.Node {
		versions, ready := available.Get()
		if !ready || len(versions) < 2 {
			if available.Err() != nil {
				return ui.Button{Text: locales.ReloadPlaybackVersions(cx), Variant: ui.ButtonOutline, OnPress: available.Refetch}
			}
			return nil
		}
		options := []ui.Option[string]{{Label: locales.AutomaticPlaybackVersion(cx)}}
		for i, version := range versions {
			options = append(options, ui.Option[string]{Label: versionLabel(cx, version, i+1), Value: version.ID})
		}
		return core.View{Class: "flex-row flex-wrap items-center gap-3", Children: []core.Node{
			core.Text{Content: locales.PlaybackVersion(cx), Class: "text-sm font-medium"},
			playbackChoice(cx, locales.PlaybackVersion(cx), "playback-version", options, selected.Get, selected.Set),
		}}
	})
}

func (a *application) versionResource(cx *core.Scope, current route) *core.Resource[[]media.Version] {
	return core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) ([]media.Version, error) {
		client, err := a.client(current.source)
		if err != nil {
			return nil, err
		}
		if versioned, ok := client.(media.VersionClient); ok {
			return versioned.Versions(ctx, current.item)
		}
		return nil, nil
	})
}

func playbackChoice[T comparable](cx *core.Scope, label, id string, options []ui.Option[T], value func() T, change func(T)) core.Node {
	if cx.Env().Idiom() != core.IdiomTV {
		return ui.Select[T]{Label: label, Options: options, Value: value, TestID: id, OnChange: change,
			Style: core.Style{MinWidth: core.Pt(240)}}
	}
	// Keep each option in the native focus order for TV remotes.
	radios := make([]core.Node, 0, len(options))
	for i, option := range options {
		radios = append(radios, ui.Radio{Text: option.Label, TestID: id + "-" + strconv.Itoa(i),
			Checked:  func() bool { return value() == option.Value },
			OnChange: func(bool) { change(option.Value) }})
	}
	return core.View{Class: "flex-row flex-wrap gap-3", Role: core.RoleRadioGroup, Label: label, TestID: id, Children: radios}
}

func versionLabel(cx *core.Scope, version media.Version, number int) string {
	parts := make([]string, 0, 4)
	if version.Original {
		parts = append(parts, locales.OriginalQuality(cx))
	}
	if name := strings.TrimSpace(version.Name); name != "" {
		parts = append(parts, name)
	}
	if version.Height > 0 {
		resolution := strconv.Itoa(version.Height) + "p"
		if len(parts) == 0 || !strings.Contains(strings.ToLower(parts[0]), resolution) {
			parts = append(parts, resolution)
		}
	}
	if version.VideoCodec != "" {
		parts = append(parts, strings.ToUpper(version.VideoCodec))
	}
	if version.Method != "" && version.Bitrate > 0 {
		parts = append(parts, locales.BitrateMegabits(cx, strconv.FormatFloat(float64(version.Bitrate)/1_000_000, 'f', 1, 64)))
	}
	if version.Method == media.Transcode {
		parts = append(parts, playMethod(cx, version.Method))
	}
	if version.Size > 0 {
		parts = append(parts, versionSize(version.Size))
	}
	if len(parts) == 0 {
		return locales.NumberedPlaybackVersion(cx, strconv.Itoa(number))
	}
	return strings.Join(parts, " · ")
}

func versionSize(size int64) string {
	if size >= 1<<30 {
		return fmt.Sprintf("%.1f GiB", float64(size)/(1<<30))
	}
	return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
}
