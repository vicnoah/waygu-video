package main

import (
	"context"
	"slices"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func playerQuality(current route, client media.Client, stream func() media.Stream,
	bitrate func() int, busy, failed func() bool, choose func(media.PlaybackOptions)) func(*core.Scope) core.Node {
	versioned, canList := client.(media.VersionClient)
	canLimit := current.source.Provider == media.Emby || current.source.Provider == media.Jellyfin || current.source.Provider == media.Plex
	if !canList && !canLimit {
		return nil
	}
	return func(cx *core.Scope) core.Node {
		compact := phoneLandscape(cx)
		var rows []core.Node
		if !compact {
			rows = append(rows, core.Text{Class: "text-sm text-muted-foreground", ContentFunc: func() string { return playMethod(cx, stream().Method) }})
		}
		var versionsPanel core.Node
		if canList {
			versions := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) ([]media.Version, error) {
				return versioned.Versions(ctx, current.item)
			})
			versionsPanel = core.Dynamic(func(cx *core.Scope) core.Node {
				available, ready := versions.Get()
				if versions.Err() != nil {
					return ui.Button{Text: locales.ReloadPlaybackVersions(cx), Variant: ui.ButtonOutline, OnPress: versions.Refetch}
				}
				if !ready {
					return hint(locales.Loading(cx))
				}
				if len(available) < 2 {
					return nil
				}
				options := make([]core.Node, 0, len(available))
				for i, version := range available {
					options = append(options, playerRadio(cx, ui.Radio{Text: versionLabel(cx, version, i+1), Disabled: busy,
						TestID: "player-version-" + strconv.Itoa(i), Checked: func() bool { return stream().VersionID() == version.ID },
						OnChange: func(bool) { choose(media.PlaybackOptions{SourceID: version.ID, MaxBitrate: bitrate()}) }}, "", false))
				}
				return core.View{Class: "gap-2", Children: []core.Node{
					hint(locales.PlaybackVersion(cx)), qualityRadios(cx, locales.PlaybackVersion(cx), options),
				}}
			})
			if !compact {
				rows = append(rows, versionsPanel)
			}
		}
		if canLimit {
			options := make([]core.Node, 0, len(playbackBitrates))
			limits := playbackBitrates
			if compact {
				limits = slices.Clone(playbackBitrates)
				slices.Reverse(limits[1:])
			}
			for _, limit := range limits {
				label := locales.OriginalQuality(cx)
				if limit > 0 {
					label = locales.BitrateMegabits(cx, strconv.Itoa(limit/1_000_000))
				}
				detail := ""
				if compact && limit > 0 {
					detail = locales.Transcode(cx)
				}
				options = append(options, playerRadio(cx, ui.Radio{Text: label, Disabled: busy, TestID: "player-bitrate-" + strconv.Itoa(limit),
					Checked: func() bool { return bitrate() == limit }, OnChange: func(bool) {
						active := stream()
						choose(media.PlaybackOptions{SourceID: active.VersionID(), MaxBitrate: limit, AudioIndex: activeAudioIndex(active)})
					}}, detail, false))
			}
			if !compact {
				rows = append(rows, hint(locales.StreamingQuality(cx)))
			}
			rows = append(rows, qualityRadios(cx, locales.StreamingQuality(cx), options))
		}
		if compact {
			rows = append(rows, core.Dynamic(func(cx *core.Scope) core.Node {
				color := "border-white/10 bg-white/5 text-zinc-300"
				if stream().Method == media.DirectPlay {
					color = "border-emerald-500/20 bg-emerald-500/10 text-emerald-300"
				}
				return core.View{Class: "flex-row items-center gap-2 rounded-xl border px-3 py-2 " + color,
					Children: []core.Node{core.Icon{SVG: lucide.CircleCheck(), Class: "size-4"},
						core.Text{Content: playMethod(cx, stream().Method), Class: "text-xs"}}}
			}), versionsPanel)
		}
		rows = append(rows, core.Show{When: busy, Then: func(cx *core.Scope) core.Node { return hint(locales.Loading(cx)) }},
			core.Show{When: failed, Then: func(cx *core.Scope) core.Node { return notice(locales.QualitySwitchError(cx)) }})
		return core.View{Class: "gap-3", Children: rows}
	}
}

func qualityRadios(cx *core.Scope, label string, options []core.Node) core.Node {
	if cx.Env().Idiom() == core.IdiomTV {
		return core.View{Class: "gap-2", Role: core.RoleRadioGroup, Label: label, Children: options}
	}
	return playerRadioList(cx, label, options)
}

func activeAudioIndex(stream media.Stream) *int {
	for _, track := range stream.Tracks {
		if track.Type == "audio" && track.Selected {
			if index, err := strconv.Atoi(track.ID); err == nil {
				return &index
			}
		}
	}
	return nil
}
