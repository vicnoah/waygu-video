package main

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func tvDetailVersionChooser(cx *core.Scope, l tvLayout, current route, state movieDetailState,
	copies func() []ui.Option[copyKey], selectCopy func(copyKey)) core.Node {
	if !playable(current.item) && copies == nil {
		return nil
	}
	open := core.NewSignal(cx, false)
	triggerRef := &core.Ref{}
	label := func() string {
		if resolution := detailResolution(state.active()); resolution != "" {
			return locales.TvDetailVersion(cx, resolution)
		}
		if playable(current.item) {
			return locales.PlaybackVersion(cx)
		}
		return locales.PlaybackSource(cx)
	}
	title := l.text("", 24, "font-medium text-white")
	title.ContentFunc, title.TestID = label, "playback-version-value"
	trigger := core.View{Class: "flex-row items-center justify-center rounded-full border-white/20 bg-white/15",
		TestID: "playback-version", Role: core.RoleButton, Ref: triggerRef, LabelFunc: label,
		Style: core.Style{Height: l.length(80), Gap: l.points(12), Padding: l.padding(32, 0), BorderWidth: l.points(1), FocusVisible: l.focusRing()},
		Disabled: func() bool {
			versions, _ := state.available.Get()
			return len(versions) == 0 && state.available.Err() == nil && (copies == nil || len(copies()) <= 1)
		},
		OnPress: func(core.PressEvent) { open.Set(true) }, Children: []core.Node{
			title, l.icon(lucide.ChevronDown(), 24, "text-white"),
		}}
	close := func() {
		open.Set(false)
		cx.Post(triggerRef.Focus)
	}
	return core.Group{trigger, core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node {
		return tvDetailVersionsDialog(cx, l, current, state, copies, selectCopy, open.Get, close)
	}}}
}

func tvDetailVersionsDialog(cx *core.Scope, l tvLayout, current route, state movieDetailState,
	copies func() []ui.Option[copyKey], selectCopy func(copyKey), open func() bool, close func()) core.Node {
	first := &core.Ref{}
	cx.Post(first.Focus)
	title := locales.PlaybackVersion(cx)
	if !playable(current.item) {
		title = locales.PlaybackSource(cx)
	}
	body := core.Dynamic(func(cx *core.Scope) core.Node {
		var groups []core.Node
		versions, _ := state.available.Get()
		if copies != nil {
			choices := copies()
			if len(choices) > 1 {
				currentCopy := (catalogCopy{current.source, current.item}).key()
				rows := make([]core.Node, 0, len(choices))
				for i, choice := range choices {
					option := ui.Radio{Text: choice.Label, TestID: "tv-version-source-" + strconv.Itoa(i),
						Style:    core.Style{MinHeight: l.length(52), FocusVisible: l.focusRing()},
						Checked:  func() bool { return choice.Value == currentCopy },
						OnChange: func(bool) { close(); selectCopy(choice.Value) },
					}
					if i == 0 && len(versions) == 0 {
						option.Ref = first
					}
					rows = append(rows, option)
				}
				if playable(current.item) {
					rows = append([]core.Node{heading(locales.PlaybackSource(cx))}, rows...)
				}
				groups = append(groups, core.View{Class: "gap-2", Role: core.RoleRadioGroup, Label: locales.PlaybackSource(cx), Children: rows})
			}
		}
		if len(versions) > 0 {
			row := func(id, text string) ui.Radio {
				return ui.Radio{Text: text, TestID: "tv-version-" + id,
					Style:    core.Style{MinHeight: l.length(52), FocusVisible: l.focusRing()},
					Checked:  func() bool { return state.selected.Get() == id },
					OnChange: func(bool) { state.chooseVersion(id); close() }}
			}
			auto := row("", locales.AutomaticPlaybackVersion(cx))
			auto.Ref, auto.TestID = first, "tv-version-auto"
			var parts []core.Node
			if len(groups) > 0 {
				parts = append(parts, heading(locales.PlaybackVersion(cx)))
			}
			parts = append(parts, auto)
			for i, version := range versions {
				parts = append(parts, row(version.ID, versionLabel(cx, version, i+1)))
			}
			groups = append(groups, core.View{Class: "gap-2", Role: core.RoleRadioGroup, Label: locales.PlaybackVersion(cx), Children: parts})
		}
		if state.available.Err() != nil {
			groups = append(groups, ui.Button{Text: locales.ReloadPlaybackVersions(cx), OnPress: state.available.Refetch})
		}
		return core.View{Class: "gap-5 py-2", TestID: "tv-detail-version-options", Children: groups}
	})
	return ui.Dialog{Title: title, Open: open, OnOpenChange: func(visible bool, _ base.Reason) {
		if !visible {
			close()
		}
	}, Children: []core.Node{core.Scroll{Style: core.Style{MaxHeight: l.length(600)}, Children: []core.Node{body}}},
		Footer: []core.Node{ui.Button{Text: locales.Dismiss(cx), TestID: "tv-version-close", Variant: ui.ButtonOutline, OnPress: close}}}
}

func tvDetailCast(cx *core.Scope, l tvLayout, people []media.Person) core.Node {
	cards := make([]core.Node, 0, len(people))
	for i, person := range people {
		role := person.Role
		switch {
		case person.Director:
			role = locales.Director(cx)
		case person.Kind == "composer":
			role = locales.TvComposer(cx)
		}
		portrait := core.View{Class: "relative items-center justify-center overflow-hidden rounded-full border-white dark:border-[#1a1b25]",
			Style: core.Style{Width: l.length(144), Height: l.length(144), BorderWidth: l.points(4)}, Children: []core.Node{
				artworkWithIconSize(person.Portrait, "flex-1 w-full rounded-full overflow-hidden", l.points(28)),
			}}
		opened := core.NewSignal(cx, false)
		ref := &core.Ref{}
		card := l.cardWithFocus(cx, core.View{
			Class: "shrink-0 items-center", Style: core.Style{Width: l.length(160), Gap: l.points(12)},
			TestID: "tv-cast-" + strconv.Itoa(i), Label: person.Name, Role: core.RoleButton, Ref: ref,
			OnPress: func(core.PressEvent) { opened.Set(true) },
		}, portrait, l.text(person.Name, 20, "font-medium"), l.text(role, 18, tvMuted))
		// Credits use circular frames and a slightly softer shadow than posters.
		frame := card.Children[0].(core.View)
		frame.Class = "rounded-full shadow-lg shadow-black/15"
		card.Children[0] = frame
		close := func() {
			opened.Set(false)
			cx.Post(ref.Focus)
		}
		cards = append(cards, core.View{Children: []core.Node{card, core.Show{
			When: opened.Get, Then: func(cx *core.Scope) core.Node {
				return ui.Dialog{Title: person.Name, Description: role, Open: opened.Get,
					OnOpenChange: func(visible bool, _ base.Reason) {
						if !visible {
							close()
						}
					},
					Footer: []core.Node{ui.Button{Text: locales.Dismiss(cx), TestID: "tv-credit-close", OnPress: close}},
				}
			},
		}}})
	}
	return tvShelf(l, "tv-detail-cast", locales.TvCast(cx), "bg-amber-500", nil, nil, 40, cards)
}
