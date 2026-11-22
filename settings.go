package main

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) settings(cx *core.Scope) core.Node {
	if appLayoutIdiom(cx) == core.IdiomPhone {
		return a.phoneSettingsHome(cx)
	}
	confirm := core.NewSignal(cx, "")
	failure := core.NewSignal[error](cx, nil)
	bitrateOptions := []ui.Option[int]{{Label: locales.OriginalQuality(cx)}}
	for _, bitrate := range playbackBitrates[1:] {
		bitrateOptions = append(bitrateOptions, ui.Option[int]{
			Label: locales.BitrateMegabits(cx, strconv.Itoa(bitrate/1_000_000)), Value: bitrate,
		})
	}
	playbackControls := []core.Node{
		heading(locales.PlaybackSettings(cx)),
		hint(locales.StreamingQuality(cx)),
		ui.Select[int]{Label: locales.StreamingQuality(cx), Options: bitrateOptions,
			TestID: "bitrate-limit", Value: a.bitrateLimit.Get, OnChange: func(bitrate int) {
				failure.Set(storeSettingValue(a.storage, bitrateKey, a.bitrateLimit, bitrate, strconv.Itoa(bitrate)))
			}},
		hint(locales.StreamingQualityProviders(cx)),
		hint(locales.PreferredPlaybackVersion(cx)),
		playbackChoice(cx, locales.PreferredPlaybackVersion(cx), "playback-version-policy", versionPolicyOptions(cx),
			a.versionPolicy.Get, func(policy media.VersionPolicy) {
				failure.Set(storeSettingValue(a.storage, versionPolicyKey, a.versionPolicy, policy, string(policy)))
			}),
		ui.Checkbox{Text: locales.AutoNextEpisode(cx), Checked: a.autoNext.Get, OnChange: func(enabled bool) {
			failure.Set(storeSettingValue(a.storage, autoNextKey, a.autoNext, enabled, strconv.FormatBool(enabled)))
		}},
	}
	if cx.Env().Desktop() {
		playbackControls = append(playbackControls,
			ui.Checkbox{Text: locales.PreferMPV(cx), Checked: a.mpvPreferred.Get, OnChange: func(enabled bool) {
				failure.Set(storeSettingValue(a.storage, mpvKey, a.mpvPreferred, enabled, strconv.FormatBool(enabled)))
			}},
		)
	}
	return core.Scroll{Class: "flex-1", Children: []core.Node{core.View{
		Class: "gap-5 p-5 md:p-8", StyleFunc: func() core.Style {
			return core.Style{PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Bottom: core.EnvKeyboard},
				Padding: core.Edges{Bottom: 24 + tabBarInset(cx)}}
		}, Children: []core.Node{
			heading(locales.Settings(cx)),
			core.View{Class: "flex-row flex-wrap items-center justify-between gap-3", Children: []core.Node{
				heading(locales.MediaSources(cx)),
				ui.Button{Text: locales.AddSource(cx), Icon: lucide.Plus, TestID: "settings-add-source",
					OnPress: func() { a.push(route{screen: screenSources}) }},
			}},
			core.Dynamic(func(cx *core.Scope) core.Node {
				sources := a.sources.Get()
				var cards []core.Node
				if len(sources) == 0 {
					cards = append(cards, notice(locales.NoSources(cx)))
				}
				for i, selected := range sources {
					content := []core.Node{
						heading(selected.Name),
						hint(providerName(cx, selected.Provider) + " · " + selected.Address),
					}
					if selected.Volatile {
						content = append(content, hint(locales.SecretUnavailable(cx)))
					}
					var actions []core.Node
					if confirm.Get() == selected.ID {
						actions = []core.Node{
							ui.Button{Text: locales.Back(cx), Variant: ui.ButtonGhost, OnPress: func() { confirm.Set("") }},
							ui.Button{Text: locales.RemoveSource(cx), Color: func() core.Color { return cx.Palette().Danger }, TestID: "confirm-remove-source", OnPress: func() {
								if err := a.removeSource(selected); err != nil {
									failure.Set(err)
									return
								}
								confirm.Set("")
							}},
						}
					} else {
						actions = []core.Node{
							ui.Button{Text: locales.ManageLibraries(cx), Icon: lucide.LibraryBig, Variant: ui.ButtonOutline,
								TestID: "manage-libraries-" + selected.ID, OnPress: func() {
									a.push(route{screen: screenManageLibraries, source: selected})
								}},
							ui.Button{Text: locales.Reconnect(cx), Variant: ui.ButtonOutline, OnPress: func() {
								a.push(route{screen: screenConnect, provider: selected.Provider, source: selected})
							}},
							ui.Button{Text: locales.RemoveSource(cx), Variant: ui.ButtonGhost, OnPress: func() { confirm.Set(selected.ID) }},
						}
						if len(sources) > 1 {
							move := func(delta int) {
								failure.Set(a.moveSource(selected.ID, delta))
							}
							actions = append(actions,
								ui.IconButton{Label: locales.MoveSourceUp(cx, selected.Name), Icon: lucide.ArrowUp, Size: 44,
									TestID: "move-source-up-" + selected.ID, Disabled: func() bool { return i == 0 }, OnPress: func() { move(-1) }},
								ui.IconButton{Label: locales.MoveSourceDown(cx, selected.Name), Icon: lucide.ArrowDown, Size: 44,
									TestID: "move-source-down-" + selected.ID, Disabled: func() bool { return i == len(sources)-1 }, OnPress: func() { move(1) }})
						}
					}
					content = append(content, core.View{Class: "flex-row flex-wrap gap-3 pt-2", Children: actions})
					cards = append(cards, core.View{Class: "gap-2 rounded-2xl border border-border bg-card p-5", Children: content})
				}
				cards = append(cards, core.View{Class: "gap-2 rounded-2xl border border-border bg-card p-5", Children: []core.Node{
					ui.Checkbox{Text: locales.MergeIdenticalTitles(cx), Checked: a.mergeTitles.Get, TestID: "merge-identical-titles", OnChange: func(enabled bool) {
						if err := a.storage.SetItem(mergeTitlesKey, strconv.FormatBool(enabled)); err != nil {
							failure.Set(err)
							return
						}
						a.mergeTitles.Set(enabled)
					}},
					hint(locales.MergeTitlesDescription(cx)),
				}})
				if err := failure.Get(); err != nil {
					cards = append(cards, notice(errorText(cx, err)))
				}
				return core.View{Class: "gap-4", Children: cards}
			}),
			core.Show{When: a.downloads.available, Then: func(cx *core.Scope) core.Node {
				return ui.Button{Text: locales.StorageDownloads(cx), Icon: lucide.HardDrive, TestID: "settings-downloads",
					Variant: ui.ButtonOutline, OnPress: func() { a.push(route{screen: screenDownloads}) }}
			}},
			core.View{Class: "gap-3 rounded-2xl border border-border bg-card p-5", Children: playbackControls},
			a.danmakuSettings(cx),
			a.danmakuDefaults(cx),
			ui.Button{Text: locales.SettingsAppearance(cx), Icon: lucide.Palette, TestID: "open-appearance-settings",
				Variant: ui.ButtonOutline, OnPress: func() { a.push(route{screen: screenAppearanceSettings}) }},
			ui.Button{Text: locales.SettingsParental(cx), Icon: lucide.Shield, TestID: "open-parental-settings",
				Variant: ui.ButtonOutline, OnPress: func() { a.push(route{screen: screenParentalSettings}) }},
		},
	}}}
}
