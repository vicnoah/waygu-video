package main

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

type endpointHeaders struct {
	origin string
	values http.Header
}

type requestHeaderRow struct {
	id          int
	name, value string
}

func headerEditor(cx *core.Scope, draft endpointHeaders, close func(), apply func(endpointHeaders)) core.Node {
	names := make([]string, 0, len(draft.values))
	for name := range draft.values {
		names = append(names, name)
	}
	slices.Sort(names)
	entries := make([]requestHeaderRow, 0, len(names))
	for id, name := range names {
		entries = append(entries, requestHeaderRow{id, name, draft.values.Get(name)})
	}
	if len(entries) == 0 {
		entries = append(entries, requestHeaderRow{})
	}
	rows := core.NewSignalFunc(cx, entries, nil)
	nextID := len(entries)
	invalid := core.NewSignal(cx, false)
	update := func(id int, change func(*requestHeaderRow)) {
		rows.Update(func(rows []requestHeaderRow) []requestHeaderRow {
			index := slices.IndexFunc(rows, func(row requestHeaderRow) bool { return row.id == id })
			if index >= 0 {
				rows = slices.Clone(rows)
				change(&rows[index])
			}
			return rows
		})
		invalid.Set(false)
	}
	add := func() {
		rows.Update(func(rows []requestHeaderRow) []requestHeaderRow {
			return append(slices.Clone(rows), requestHeaderRow{id: nextID})
		})
		nextID++
	}
	save := func() {
		header := make(http.Header)
		for _, row := range rows.GetUntracked() {
			name := http.CanonicalHeaderKey(strings.TrimSpace(row.name))
			value := strings.TrimSpace(row.value)
			if name == "" && value == "" {
				continue
			}
			if !validSourceHeader(name, row.value) || value == "" || header.Get(name) != "" {
				invalid.Set(true)
				return
			}
			header.Set(name, value)
		}
		apply(endpointHeaders{draft.origin, header})
		close()
	}
	var firstField *core.Ref
	if appLayoutIdiom(cx) == core.IdiomTV {
		firstField = &core.Ref{}
		cx.Post(firstField.Focus)
	}
	label := locales.SourceRequestHeaders(cx)
	return ui.Dialog{Open: func() bool { return true }, Title: label, Description: draft.origin,
		TestID: "source-header-editor", OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				close()
			}
		},
		Children: []core.Node{
			core.Scroll{Class: "max-h-[380px]", Children: []core.Node{
				core.View{Class: "gap-2", Children: []core.Node{
					core.Each(rows.Get, func(row requestHeaderRow) int { return row.id },
						func(cx *core.Scope, row func() requestHeaderRow, _ func() int) core.Node {
							id := core.Untracked(cx, row).id
							suffix := strconv.Itoa(id)
							remove := locales.SourceHeaderRemove(cx)
							var fieldRef *core.Ref
							if id == 0 {
								fieldRef = firstField
							}
							return core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
								core.View{Class: "flex-1 gap-1", Children: []core.Node{
									ui.Input{Label: locales.SourceHeaderName(cx), Placeholder: "X-Example-Token", Ref: fieldRef,
										TestID: "source-header-name-" + suffix, Value: func() string { return row().name },
										OnChange: func(value string) { update(id, func(entry *requestHeaderRow) { entry.name = value }) }},
									ui.Input{Label: locales.SourceHeaderValue(cx), Placeholder: locales.SourceHeaderValue(cx), Masked: true,
										TestID: "source-header-value-" + suffix, Value: func() string { return row().value },
										OnChange: func(value string) { update(id, func(entry *requestHeaderRow) { entry.value = value }) }},
								}},
								setupIconButton(cx, remove, "source-header-remove-"+suffix, lucide.Trash(), nil, func() {
									rows.Update(func(entries []requestHeaderRow) []requestHeaderRow {
										return slices.DeleteFunc(slices.Clone(entries), func(entry requestHeaderRow) bool { return entry.id == id })
									})
									invalid.Set(false)
								}),
							}}
						}),
				}},
			}},
			ui.Button{Text: locales.SourceHeaderAdd(cx), Variant: ui.ButtonGhost, TestID: "source-header-add",
				Disabled: func() bool { return len(rows.Get()) >= maxSourceHeaders }, OnPress: add},
			core.Show{When: invalid.Get, Then: func(cx *core.Scope) core.Node { return notice(locales.SourceHeaderInvalid(cx)) }},
		},
		Footer: []core.Node{
			ui.Button{Text: locales.DesktopSettingsCancel(cx), Variant: ui.ButtonGhost, TestID: "source-header-cancel", OnPress: close},
			ui.Button{Text: locales.SourceHeadersApply(cx), TestID: "source-header-apply", OnPress: save},
		},
	}
}
