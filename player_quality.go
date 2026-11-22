package main

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func playbackPresetHeight(bitrate int) int {
	switch bitrate {
	case 4_000_000:
		return 480
	case 8_000_000:
		return 720
	case 20_000_000:
		return 1080
	default:
		return 0
	}
}

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
		original := func() media.Version { return stream().Rendition }
		if canList {
			versions := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) ([]media.Version, error) {
				return versioned.Versions(ctx, current.item)
			})
			original = func() media.Version {
				active := stream()
				if active.Rendition.Height == 0 {
					available, _ := versions.Get()
					for _, version := range available {
						if version.ID == active.VersionID() {
							return version
						}
					}
				}
				return active.Rendition
			}
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
				selected := func() bool { return bitrate() == limit }
				change := func(bool) {
					active := stream()
					choose(media.PlaybackOptions{SourceID: active.VersionID(), MaxBitrate: limit, AudioIndex: activeAudioIndex(active, limit == 0)})
				}
				label := locales.OriginalQuality(cx)
				if limit > 0 {
					label = locales.BitrateMegabits(cx, strconv.Itoa(limit/1_000_000))
				}
				model := ui.Radio{Text: label, Disabled: busy, TestID: "player-bitrate-" + strconv.Itoa(limit), Checked: selected, OnChange: change}
				if compact {
					options = append(options, phoneQualityRow(cx, model, limit, original, func() media.PlayMethod { return stream().Method }))
				} else {
					options = append(options, model)
				}
			}
			if !compact {
				rows = append(rows, hint(locales.StreamingQuality(cx)))
			}
			rows = append(rows, qualityRadios(cx, locales.StreamingQuality(cx), options))
		}
		if compact {
			rows = append(rows, core.Dynamic(func(cx *core.Scope) core.Node {
				color := "border-white/10 bg-white/5 text-zinc-300"
				icon, iconClass := lucide.Info, "size-4 text-zinc-400"
				method := stream().Method
				if method == media.DirectPlay {
					color = "border-emerald-500/20 bg-emerald-500/10 text-emerald-300"
					icon, iconClass = lucide.CircleCheck, "size-4 text-emerald-400"
				}
				return core.View{Class: "flex-row items-center gap-2 rounded-xl border px-3 py-2 " + color,
					Children: []core.Node{core.Icon{SVG: icon(), Class: iconClass},
						core.Text{Content: playMethod(cx, method), Class: "text-xs"}}}
			}), versionsPanel)
		}
		rows = append(rows, core.Show{When: busy, Then: func(cx *core.Scope) core.Node { return hint(locales.Loading(cx)) }},
			core.Show{When: failed, Then: func(cx *core.Scope) core.Node { return notice(locales.QualitySwitchError(cx)) }})
		class := "gap-3"
		if compact {
			class = "gap-2.5"
		}
		return core.View{Class: class, Children: rows}
	}
}

func phoneQualityRow(cx *core.Scope, model ui.Radio, bitrate int, original func() media.Version, method func() media.PlayMethod) core.Node {
	class := "h-10"
	label := model.Text
	var content []core.Node
	if bitrate == 0 {
		class = "h-12"
		content = []core.Node{core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
			core.Text{Class: "text-sm font-medium text-white", MaxLines: 1, ContentFunc: func() string {
				if height := original().Height; height > 0 {
					return model.Text + " · " + strconv.Itoa(height) + "p"
				}
				return model.Text
			}},
			core.Text{Class: "text-[11px] text-zinc-400", MaxLines: 1, ContentFunc: func() string {
				version := original()
				var detail []string
				if model.Checked() {
					detail = append(detail, playMethod(cx, method()))
				}
				codec := strings.ToUpper(version.VideoCodec)
				if version.Bitrate > 0 {
					rate := strconv.FormatFloat(float64(version.Bitrate)/1_000_000, 'f', 1, 64)
					codec = strings.TrimSpace(codec + " " + locales.BitrateMegabits(cx, rate))
				}
				if codec != "" {
					detail = append(detail, codec)
				}
				return strings.Join(detail, " · ")
			}},
		}}}
	} else {
		label = strconv.Itoa(playbackPresetHeight(bitrate)) + "p · " + label
		content = []core.Node{
			core.Text{Content: label, MaxLines: 1, Class: "min-w-0 flex-1 text-sm text-zinc-200 group-selected:font-medium group-selected:text-white"},
			core.Text{Content: locales.Transcode(cx), Class: "text-[11px] text-zinc-500"},
		}
	}
	content = append(content, core.Show{When: model.Checked, Then: func(*core.Scope) core.Node {
		return core.Icon{SVG: lucide.Check(), Class: "size-4 text-violet-400"}
	}})
	return base.Radio{Label: label, TestID: model.TestID, Checked: model.Checked, Disabled: model.Disabled, OnChange: model.OnChange,
		Class: "group flex-row items-center gap-3 bg-zinc-800/70 px-3 selected:bg-violet-500/20 disabled:opacity-50 " + class,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, Children: content}
}

func qualityRadios(cx *core.Scope, label string, options []core.Node) core.Node {
	if cx.Env().Idiom() == core.IdiomTV {
		return core.View{Class: "gap-2", Role: core.RoleRadioGroup, Label: label, Children: options}
	}
	return playerRadioList(cx, label, options)
}

func activeAudioIndex(stream media.Stream, original bool) *int {
	for _, track := range stream.Tracks {
		if track.Type == "audio" && track.Selected && (!original || !track.Default) {
			if index, err := strconv.Atoi(track.ID); err == nil {
				return &index
			}
		}
	}
	return nil
}
