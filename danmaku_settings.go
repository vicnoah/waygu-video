package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/danmaku"
	"waygu-video/locales"
)

func (a *application) danmakuSettings(cx *core.Scope) core.Node {
	id := core.NewSignal(cx, "")
	secret := core.NewSignal(cx, "")
	busy := core.NewSignal(cx, false)
	failed := core.NewSignal(cx, false)
	view := cx.Context()
	saved := core.NewResource(cx, nil, func(context.Context, struct{}, core.ResourceInfo) (string, error) {
		credentials, err := a.dandanCredentials()
		return credentials.AppID, err
	})
	cx.Effect(func() {
		if value, ok := saved.Get(); ok {
			id.Set(value)
		}
	})
	disabled := func() bool { return busy.Get() || saved.Loading() }
	update := func(remove bool) {
		if busy.GetUntracked() {
			return
		}
		credentials := danmaku.DandanCredentials{AppID: strings.TrimSpace(id.GetUntracked()), Secret: strings.TrimSpace(secret.GetUntracked())}
		busy.Set(true)
		failed.Set(false)
		go func() {
			var err error
			if remove {
				err = a.secrets.Delete(dandanCredentialKey)
			} else {
				data, _ := json.Marshal(credentials)
				err = a.secrets.Set(dandanCredentialKey, string(data))
			}
			cx.Post(func() {
				if view.Err() != nil {
					return
				}
				busy.Set(false)
				failed.Set(err != nil)
				if err == nil {
					secret.Set("")
					saved.Refetch()
				}
			})
		}()
	}
	return core.View{Class: "gap-3 rounded-2xl border border-border bg-card p-5", Children: []core.Node{
		heading(locales.DanmakuSources(cx)),
		hint(locales.DanmakuCredentialsHelp(cx)),
		ui.Input{Label: locales.DanmakuAppID(cx), Placeholder: locales.DanmakuAppID(cx), Value: id.Get, OnChange: id.Set,
			TestID: "dandan-app-id", NoAutoCorrect: true, Disabled: disabled},
		ui.Input{Label: locales.DanmakuAppSecret(cx), Placeholder: locales.DanmakuAppSecret(cx), Value: secret.Get, OnChange: secret.Set,
			TestID: "dandan-app-secret", Masked: true, NoAutoCorrect: true, Disabled: disabled},
		core.View{Class: "flex-row flex-wrap gap-3", Children: []core.Node{
			ui.Button{Text: locales.DanmakuConnect(cx), TestID: "dandan-save", Busy: busy.Get,
				Disabled: func() bool {
					return disabled() || strings.TrimSpace(id.Get()) == "" || strings.TrimSpace(secret.Get()) == ""
				},
				OnPress: func() { update(false) }},
			core.Show{When: func() bool { value, _ := saved.Get(); return value != "" }, Then: func(cx *core.Scope) core.Node {
				return ui.Button{Text: locales.DanmakuDisconnect(cx), TestID: "dandan-remove", Disabled: disabled, Variant: ui.ButtonOutline,
					OnPress: func() { update(true) }}
			}},
		}},
		core.Show{When: func() bool { return failed.Get() || saved.Err() != nil }, Then: func(cx *core.Scope) core.Node {
			return notice(locales.SecretUnavailable(cx))
		}},
	}}
}

func (a *application) danmakuDefaults(cx *core.Scope) core.Node {
	editor := a.editDanmaku(cx)
	return core.View{Class: "gap-3 rounded-2xl border border-border bg-card p-5", Children: []core.Node{
		heading(locales.Danmaku(cx)),
		editor.display(cx, editor.toggle(locales.DanmakuEnabledByDefault(cx), "danmaku-default-enabled",
			func(p *danmakuPreferences) *bool { return &p.Enabled }), nil),
		editor.toggle(locales.DanmakuAutoMatch(cx), "danmaku-auto-match", func(p *danmakuPreferences) *bool { return &p.AutoMatch }),
		editor.filters(cx),
		editor.warning(),
	}}
}
