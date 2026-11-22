package main

import (
	"slices"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) settings(cx *core.Scope) core.Node {
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
				if err := a.storage.SetItem(bitrateKey, strconv.Itoa(bitrate)); err != nil {
					failure.Set(err)
					return
				}
				a.bitrateLimit.Set(bitrate)
			}},
		hint(locales.StreamingQualityProviders(cx)),
		hint(locales.PreferredPlaybackVersion(cx)),
		playbackChoice(cx, locales.PreferredPlaybackVersion(cx), "playback-version-policy", versionPolicyOptions(cx),
			a.versionPolicy.Get, func(policy media.VersionPolicy) {
				if err := a.storage.SetItem(versionPolicyKey, string(policy)); err != nil {
					failure.Set(err)
					return
				}
				a.versionPolicy.Set(policy)
			}),
		ui.Checkbox{Text: locales.AutoNextEpisode(cx), Checked: a.autoNext.Get, OnChange: func(enabled bool) {
			if err := a.storage.SetItem(autoNextKey, strconv.FormatBool(enabled)); err != nil {
				failure.Set(err)
				return
			}
			a.autoNext.Set(enabled)
		}},
	}
	if cx.Env().Desktop() {
		playbackControls = append(playbackControls,
			ui.Checkbox{Text: locales.PreferMPV(cx), Checked: a.mpvPreferred.Get, OnChange: func(enabled bool) {
				if err := a.storage.SetItem(mpvKey, strconv.FormatBool(enabled)); err != nil {
					failure.Set(err)
					return
				}
				a.mpvPreferred.Set(enabled)
			}},
		)
	}
	return core.Scroll{Class: "flex-1", Children: []core.Node{core.View{
		Class: "gap-5 p-5 md:p-8", StyleFunc: func() core.Style {
			return core.Style{PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Bottom: core.EnvKeyboard},
				Padding: core.Edges{Bottom: 24 + ui.TabBarInset(cx)}}
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
								current := slices.Clone(a.sources.GetUntracked())
								current = slices.DeleteFunc(current, func(entry source) bool { return entry.ID == selected.ID })
								if err := a.saveSources(current); err != nil {
									failure.Set(err)
									return
								}
								key := selected.credentialKey()
								a.tokens.Delete(key)
								if a.sourceFilter.GetUntracked() == selected.ID {
									a.sourceFilter.Set("")
								}
								confirm.Set("")
								go func() { _ = a.secrets.Delete(key) }()
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
			core.View{Class: "gap-3 rounded-2xl border border-border bg-card p-5", Children: playbackControls},
		},
	}}}
}

func (a *application) moveSource(id string, delta int) error {
	entries := slices.Clone(a.sources.GetUntracked())
	index := slices.IndexFunc(entries, func(entry source) bool { return entry.ID == id })
	other := index + delta
	if index < 0 || other < 0 || other >= len(entries) {
		return nil
	}
	entries[index], entries[other] = entries[other], entries[index]
	return a.saveSources(entries)
}
