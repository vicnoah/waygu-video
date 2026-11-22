package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func (a *application) phoneParentalSettings(cx *core.Scope) core.Node {
	return phoneSettingsPage(cx, locales.SettingsParental(cx), a.back, "phone-parental-settings", a.parentalSettings(cx))
}

func (a *application) parentalSettings(cx *core.Scope) core.Node {
	prefs := a.parental.Get
	pinDialog, askPIN := a.parentalPINPrompt(cx)
	failure := core.NewSignal[error](cx, nil)
	save := func(change func(*parentalPolicy)) bool {
		next := a.parental.GetUntracked()
		change(&next)
		err := storeSettings(a.storage, parentalSettingsKey, a.parental, next)
		failure.Set(err)
		if err == nil && len(a.parentalApprovals.GetUntracked()) > 0 {
			a.parentalApprovals.Set(map[copyKey]bool{})
		}
		return err == nil
	}
	admin := core.NewSignal(cx, false)
	edit := func(action func()) {
		p := a.parental.GetUntracked()
		if admin.GetUntracked() || (!p.PINSet && !p.Enabled) {
			action()
			return
		}
		askPIN(false, func() { admin.Set(true); action() })
	}
	createPIN := func(enable bool) {
		askPIN(true, func() {
			if save(func(p *parentalPolicy) { p.PINSet = true; p.Enabled = p.Enabled || enable }) {
				admin.Set(true)
			}
		})
	}
	change := func(update func(*parentalPolicy)) { edit(func() { save(update) }) }

	enabled := phoneSetting{locales.ParentalEnabled(cx), "", "bg-amber-500", lucide.ShieldCheck}
	pin := phoneSetting{locales.ParentalPIN(cx), "", "bg-zinc-500", lucide.KeyRound}
	rating := phoneSetting{locales.ParentalMaximumRating(cx), "", "bg-rose-500", lucide.BadgeAlert}
	hidden := phoneSetting{locales.ParentalHiddenLibraries(cx), "", "bg-zinc-500", lucide.EyeOff}
	allowed := phoneSetting{locales.ParentalAllowedSources(cx), "", "bg-emerald-500", lucide.Server}
	kids := phoneSetting{locales.ParentalKidsMode(cx), locales.ParentalKidsHint(cx), "bg-pink-500", lucide.Baby}
	limit := phoneSetting{locales.ParentalDailyLimit(cx), "", "bg-indigo-500", lucide.Hourglass}
	rangeLabel := locales.ParentalQuietHours(cx)
	info := core.View{Class: "mx-4 flex-row items-center gap-3 rounded-2xl border border-amber-500/25 bg-amber-500/10 p-4", Children: []core.Node{
		core.View{Class: "size-10 shrink-0 items-center justify-center rounded-full bg-amber-500 shadow-md shadow-amber-500/30",
			Children: []core.Node{core.Icon{SVG: lucide.Shield(), Class: "size-5 text-white"}}},
		core.Text{Content: locales.ParentalIntroduction(cx), Class: "min-w-0 flex-1 text-xs leading-relaxed text-zinc-900/80 dark:text-zinc-50/80"},
	}}
	var ratings []ui.Option[parentalRating]
	for _, option := range []struct {
		label string
		value parentalRating
	}{{"G", ratingG}, {"PG", ratingPG}, {"PG-13", ratingPG13}, {"R", ratingR}, {locales.ParentalUnrestricted(cx), ratingUnlimited}} {
		ratings = append(ratings, ui.Option[parentalRating]{Label: option.label, Value: option.value})
	}
	durations := []ui.Option[int]{}
	for _, minutes := range []int{15, 30, 45, 60, 90, 120, 180, 240} {
		label := locales.ParentalMinutes(cx, strconv.Itoa(minutes))
		if minutes%60 == 0 {
			label = locales.ParentalHours(cx, strconv.Itoa(minutes/60))
		}
		durations = append(durations, ui.Option[int]{Label: label, Value: minutes})
	}
	days := func(id, title string, get func(parentalPolicy) int, update func(*parentalPolicy, int)) core.Node {
		return phoneSettingChoice(cx, phoneSetting{label: title}, id, durations, func() int { return get(prefs()) },
			func(value int) { change(func(p *parentalPolicy) { update(p, value) }) })
	}

	librariesOpen := core.NewSignal(cx, false)
	sourcesOpen := core.NewSignal(cx, false)
	quietOpen := core.NewSignal(cx, false)
	enabledTime := func() bool { return prefs().TimeLimit }
	return core.View{Class: "gap-6", Children: []core.Node{
		info,
		setupGroup("", "",
			enabled.toggle(cx, "parental-enabled", func() bool { return prefs().Enabled }, func(value bool) {
				p := a.parental.GetUntracked()
				if value && !p.Enabled && !p.PINSet {
					createPIN(true)
					return
				}
				change(func(p *parentalPolicy) { p.Enabled = value })
			}),
			pin.linkValue(cx, "parental-change-pin", func() string {
				if prefs().PINSet {
					return locales.ParentalPINSet(cx)
				}
				return locales.ParentalPINUnset(cx)
			}, func() { edit(func() { createPIN(false) }) }),
		),
		setupGroup(locales.ParentalRestrictions(cx), "",
			phoneSettingChoice(cx, rating, "parental-rating", ratings, func() parentalRating { return prefs().Rating },
				func(value parentalRating) { change(func(p *parentalPolicy) { p.Rating = value }) }),
			hidden.linkValue(cx, "parental-hidden-libraries", func() string {
				return locales.ParentalCount(cx, strconv.Itoa(len(prefs().Hidden)))
			}, func() { edit(func() { librariesOpen.Set(true) }) }),
			allowed.linkValue(cx, "parental-allowed-sources", func() string {
				p := prefs()
				if !p.RestrictSources {
					return locales.AllSources(cx)
				}
				var names []string
				for _, source := range a.sources.Get() {
					if slices.Contains(p.Sources, source.ID) {
						names = append(names, source.Name)
					}
				}
				if len(names) == 1 {
					return names[0]
				}
				return locales.ParentalCount(cx, strconv.Itoa(len(names)))
			}, func() { edit(func() { sourcesOpen.Set(true) }) }),
			kids.toggle(cx, "parental-kids", func() bool { return prefs().Kids },
				func(value bool) { change(func(p *parentalPolicy) { p.Kids = value }) }),
		),
		setupGroup(locales.ParentalTime(cx), locales.ParentalTimeHint(cx),
			limit.toggle(cx, "parental-time-limit", enabledTime,
				func(value bool) { change(func(p *parentalPolicy) { p.TimeLimit = value }) }),
			days("parental-weekday", locales.ParentalWeekdays(cx), func(p parentalPolicy) int { return p.Weekday },
				func(p *parentalPolicy, value int) { p.Weekday = value }),
			days("parental-weekend", locales.ParentalWeekends(cx), func(p parentalPolicy) int { return p.Weekend },
				func(p *parentalPolicy, value int) { p.Weekend = value }),
			(phoneSetting{label: rangeLabel}).linkValue(cx, "parental-quiet-hours", func() string {
				p := prefs()
				if p.Quiet {
					return parentalClock(p.QuietFrom) + " – " + parentalClock(p.QuietUntil)
				}
				return locales.SettingsDisabled(cx)
			}, func() { edit(func() { quietOpen.Set(true) }) }),
		),
		settingsFailure(cx, failure),
		pinDialog,
		a.parentalLibrariesDialog(cx, librariesOpen, save),
		a.parentalSourcesDialog(cx, sourcesOpen, save),
		a.parentalQuietDialog(cx, quietOpen, save),
	}}
}

func (a *application) parentalLibrariesDialog(cx *core.Scope, open *core.Signal[bool], save func(func(*parentalPolicy)) bool) core.Node {
	return parentalChoicesDialog(cx, locales.ParentalHiddenLibraries(cx), open, func(cx *core.Scope) []core.Node {
		var rows []core.Node
		for _, source := range a.sources.Get() {
			for _, library := range source.Libraries {
				key := parentalLibrary{source.ID, library.ID}
				rows = append(rows, ui.Checkbox{Text: library.Name + " · " + source.Name, TestID: "parental-hide-library-" + source.ID + "-" + library.ID,
					Checked: func() bool { return slices.Contains(a.parental.Get().Hidden, key) }, OnChange: func(checked bool) {
						save(func(p *parentalPolicy) {
							p.Hidden = slices.DeleteFunc(slices.Clone(p.Hidden), func(entry parentalLibrary) bool { return entry == key })
							if checked {
								p.Hidden = append(p.Hidden, key)
							}
						})
					}})
			}
		}
		return rows
	})
}

func (a *application) parentalSourcesDialog(cx *core.Scope, open *core.Signal[bool], save func(func(*parentalPolicy)) bool) core.Node {
	return parentalChoicesDialog(cx, locales.ParentalAllowedSources(cx), open, func(cx *core.Scope) []core.Node {
		rows := []core.Node{ui.Checkbox{Text: locales.ParentalRestrictSources(cx), TestID: "parental-restrict-sources",
			Checked: func() bool { return a.parental.Get().RestrictSources }, OnChange: func(checked bool) {
				save(func(p *parentalPolicy) {
					p.RestrictSources = checked
					if checked && p.Sources == nil {
						p.Sources = []string{}
						for _, source := range a.sources.GetUntracked() {
							p.Sources = append(p.Sources, source.ID)
						}
					}
				})
			}}}
		for _, source := range a.sources.Get() {
			rows = append(rows, ui.Checkbox{Text: source.Name, TestID: "parental-allow-source-" + source.ID,
				Disabled: func() bool { return !a.parental.Get().RestrictSources },
				Checked: func() bool {
					p := a.parental.Get()
					return !p.RestrictSources || slices.Contains(p.Sources, source.ID)
				},
				OnChange: func(checked bool) {
					save(func(p *parentalPolicy) {
						p.Sources = slices.DeleteFunc(slices.Clone(p.Sources), func(id string) bool { return id == source.ID })
						if checked {
							p.Sources = append(p.Sources, source.ID)
						}
					})
				}})
		}
		return rows
	})
}

func parentalChoicesDialog(cx *core.Scope, title string, open *core.Signal[bool], choices func(*core.Scope) []core.Node) core.Node {
	return ui.Dialog{Open: open.Get, OnOpenChange: func(value bool, _ base.Reason) { open.Set(value) }, Title: title,
		Children: []core.Node{core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				rows := choices(cx)
				if len(rows) == 0 {
					return hint(locales.NoSources(cx))
				}
				return core.Scroll{Class: "max-h-64", Children: []core.Node{core.View{Class: "gap-3", Children: rows}}}
			})
		}}}, Footer: []core.Node{
			ui.Button{Text: locales.TracksDone(cx), TestID: "parental-choices-done", OnPress: func() { open.Set(false) }},
		}}
}

func parentalClock(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func (a *application) parentalQuietDialog(cx *core.Scope, open *core.Signal[bool], save func(func(*parentalPolicy)) bool) core.Node {
	return ui.Dialog{Open: open.Get, OnOpenChange: func(value bool, _ base.Reason) { open.Set(value) }, Title: locales.ParentalQuietHours(cx),
		Children: []core.Node{core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node {
			p := a.parental.GetUntracked()
			on := core.NewSignal(cx, p.Quiet)
			from := core.NewSignal(cx, parentalClock(p.QuietFrom))
			until := core.NewSignal(cx, parentalClock(p.QuietUntil))
			invalid := core.NewSignal(cx, false)
			parse := func(value string) (int, error) {
				at, err := time.Parse("15:04", strings.TrimSpace(value))
				return at.Hour()*60 + at.Minute(), err
			}
			apply := func() {
				begin, startErr := parse(from.GetUntracked())
				end, endErr := parse(until.GetUntracked())
				active := on.GetUntracked()
				if active && (startErr != nil || endErr != nil || begin == end) {
					invalid.Set(true)
					return
				}
				if save(func(p *parentalPolicy) {
					p.Quiet = active
					if startErr == nil && endErr == nil && begin != end {
						p.QuietFrom, p.QuietUntil = begin, end
					}
				}) {
					open.Set(false)
				}
			}
			return core.View{Class: "gap-3", Children: []core.Node{
				ui.Checkbox{Text: locales.ParentalQuietHours(cx), TestID: "parental-quiet-enabled", Checked: on.Get, OnChange: on.Set},
				ui.Input{Label: locales.ParentalQuietFrom(cx), Placeholder: "21:30", TestID: "parental-quiet-start", Value: from.Get, OnChange: from.Set},
				ui.Input{Label: locales.ParentalQuietUntil(cx), Placeholder: "07:00", TestID: "parental-quiet-end", Value: until.Get, OnChange: until.Set},
				core.Show{When: invalid.Get, Then: func(cx *core.Scope) core.Node { return notice(locales.ParentalQuietInvalid(cx)) }},
				ui.Button{Text: locales.ParentalConfirm(cx), TestID: "parental-quiet-save", OnPress: apply},
			}}
		}}}}
}
