package main

import (
	"errors"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type parentalPINRequest struct {
	setup bool
	done  func()
}

func (a *application) parentalPINPrompt(cx *core.Scope) (core.Node, func(bool, func())) {
	request := core.NewSignal[*parentalPINRequest](cx, nil)
	pin, repeated := core.NewSignal(cx, ""), core.NewSignal(cx, "")
	message := core.NewSignal(cx, "")
	busy := core.NewSignal(cx, false)
	reset := func() { pin.Set(""); repeated.Set(""); message.Set("") }
	cancel := func() { request.Set(nil); reset() }
	open := func(setup bool, done func()) {
		reset()
		request.Set(&parentalPINRequest{setup, done})
	}
	submit := func() {
		task := request.GetUntracked()
		if task == nil || busy.GetUntracked() {
			return
		}
		value := pin.GetUntracked()
		if !parentalPINValid(value) {
			message.Set(locales.ParentalPINFormat(cx))
			return
		}
		if task.setup && value != repeated.GetUntracked() {
			message.Set(locales.ParentalPINMismatch(cx))
			return
		}
		if !task.setup && cx.Now().Before(a.parentalPINRetry) {
			message.Set(locales.ParentalPINRetry(cx))
			return
		}
		busy.Set(true)
		message.Set("")
		go func() {
			var err error
			if task.setup {
				var record string
				record, err = newParentalPIN(value)
				if err == nil {
					err = a.secrets.Set(parentalPINKey, record)
				}
			} else {
				var record string
				var found bool
				record, found, err = a.secrets.Get(parentalPINKey)
				if err == nil {
					if !found {
						err = core.ErrSecretsUnavailable
					} else if !verifyParentalPIN(record, value) {
						err = errParentalPINMismatch
					}
				}
			}
			a.scope.Post(func() {
				if !task.setup {
					if errors.Is(err, errParentalPINMismatch) {
						a.parentalPINFailures++
						if a.parentalPINFailures >= 5 {
							a.parentalPINRetry = a.scope.Now().Add(30 * time.Second)
							a.parentalPINFailures = 0
						}
					} else if err == nil {
						a.parentalPINFailures = 0
					}
				}
				if cx.Context().Err() != nil {
					return
				}
				busy.Set(false)
				if request.GetUntracked() != task {
					return
				}
				if err == nil {
					cancel()
					task.done()
				} else if errors.Is(err, errParentalPINMismatch) {
					pin.Set("")
					message.Set(locales.ParentalPINIncorrect(cx))
				} else {
					message.Set(locales.SecretUnavailable(cx))
				}
			})
		}()
	}
	active := func() bool { return request.Get() != nil }
	numeric := func(input string) string {
		var digits []byte
		for i := 0; i < len(input) && len(digits) < 4; i++ {
			if input[i] >= '0' && input[i] <= '9' {
				digits = append(digits, input[i])
			}
		}
		return string(digits)
	}
	field := func(id, title string, value *core.Signal[string]) core.Node {
		return ui.Input{Label: title, Placeholder: title, Value: value.Get,
			OnChange: func(input string) { value.Set(numeric(input)) }, Masked: true, KeyboardType: core.KeyboardNumber, MaxLength: 4, TestID: id,
			Disabled: busy.Get, OnSubmit: func(string) { submit() }}
	}
	return ui.Dialog{Open: active, OnOpenChange: func(value bool, _ base.Reason) {
		if !value && !busy.GetUntracked() {
			cancel()
		}
	}, TitleFunc: func() string {
		if item := request.Get(); item != nil && item.setup {
			return locales.ParentalSetPIN(cx)
		}
		return locales.ParentalEnterPIN(cx)
	}, TestID: "parental-pin-dialog", Children: []core.Node{
		core.Show{When: active, Then: func(cx *core.Scope) core.Node {
			return core.View{Class: "gap-3", Children: []core.Node{
				field("parental-pin-input", locales.ParentalPIN(cx), pin),
				core.Show{When: func() bool { item := request.Get(); return item != nil && item.setup }, Then: func(cx *core.Scope) core.Node {
					return field("parental-pin-repeat", locales.ParentalRepeatPIN(cx), repeated)
				}},
				core.Show{When: func() bool { return message.Get() != "" }, Then: func(cx *core.Scope) core.Node {
					return core.Text{ContentFunc: message.Get, Class: "text-sm text-rose-600 dark:text-rose-400", TestID: "parental-pin-error"}
				}},
			}}
		}},
	}, Footer: []core.Node{
		ui.Button{Text: locales.DesktopSettingsCancel(cx), Variant: ui.ButtonGhost, Disabled: busy.Get, OnPress: cancel},
		ui.Button{Text: locales.ParentalConfirm(cx), TestID: "parental-pin-confirm", Busy: busy.Get, OnPress: submit},
	}}, open
}

func (a *application) parentalMediaRoute(cx *core.Scope, current route, content func(*core.Scope, route) core.Node) core.Node {
	eligible := core.NewMemo(cx, func() parentalDecision {
		decision := a.parentalRules().assess(current.source.ID, current.item)
		if decision == parentalUnrated && a.parentalApprovals.Get()[copyKey{current.source.ID, current.item.ID}] {
			return parentalAllowed
		}
		return decision
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		decision := eligible.Get()
		if decision == parentalAllowed {
			return content(cx, current)
		}
		rows := []core.Node{
			core.Icon{SVG: lucide.Shield(), Class: "size-12 text-violet-500"},
			heading(locales.SettingsParental(cx)),
		}
		if decision == parentalUnrated {
			dialog, askPIN := a.parentalPINPrompt(cx)
			rows = append(rows, hint(locales.ParentalUnrated(cx)),
				ui.Button{Text: locales.ParentalApprove(cx), TestID: "parental-approve-title", OnPress: func() {
					askPIN(false, func() {
						key := copyKey{current.source.ID, current.item.ID}
						current := maps.Clone(a.parentalApprovals.GetUntracked())
						current[key] = true
						a.parentalApprovals.Set(current)
					})
				}}, dialog)
		} else {
			rows = append(rows, hint(locales.ParentalBlockedTitle(cx)))
		}
		return pageFrame(pageHeader(cx, locales.Back(cx), a.back), core.View{
			Class: "flex-1 items-center justify-center gap-4 px-6", TestID: "parental-blocked-media", Children: rows,
		})
	})
}

func (a *application) parentalVisibleItems(source string, items []media.Item) []media.Item {
	rules := a.parentalRules()
	if !rules.Enabled {
		return items
	}
	visible := make([]media.Item, 0, len(items))
	for _, item := range items {
		if rules.visible(source, item) {
			visible = append(visible, item)
		}
	}
	return visible
}

func (a *application) parentalSourceRoute(cx *core.Scope, current route, content func(*core.Scope) core.Node) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if current.sourceAdmin || !a.parentalRules().Enabled {
			return content(cx)
		}
		dialog, askPIN := a.parentalPINPrompt(cx)
		return pageFrame(pageHeader(cx, locales.Back(cx), a.back), core.View{
			Class: "flex-1 items-center justify-center gap-4 px-6", TestID: "parental-source-lock", Children: []core.Node{
				core.Icon{SVG: lucide.Shield(), Class: "size-12 text-violet-500"},
				heading(locales.SettingsParental(cx)),
				ui.Button{Text: locales.ParentalEnterPIN(cx), TestID: "parental-authorize-source", OnPress: func() {
					askPIN(false, func() {
						routes := slices.Clone(a.routes.GetUntracked())
						routes[len(routes)-1].sourceAdmin = true
						a.routes.Set(routes)
					})
				}},
				dialog,
			},
		})
	})
}

func (a *application) parentalUnratedResults(cx *core.Scope, entries []catalogItem) core.Node {
	const maxResults = 24
	rows := make([]core.Node, 0, min(len(entries), maxResults))
	seen := make(map[copyKey]bool)
	for _, entry := range entries {
		key := (catalogCopy{source: entry.source, item: entry.item}).key()
		if seen[key] {
			continue
		}
		seen[key] = true
		index := strconv.Itoa(len(rows) + 1)
		rows = append(rows, ui.Button{Text: locales.ParentalUnratedResult(cx, index), Icon: lucide.LockKeyhole,
			TestID: "parental-unrated-result-" + index, Variant: ui.ButtonOutline, OnPress: func() { a.openCatalogEntry(entry) }})
		if len(rows) == maxResults {
			break
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return core.View{Class: "items-start gap-2", Children: append([]core.Node{hint(locales.ParentalUnrated(cx))}, rows...)}
}
