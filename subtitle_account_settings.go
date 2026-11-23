package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/playback"
	"waygu-video/locales"
)

type openSubtitleAccountState struct {
	Enabled bool
	Name    string
}

func openSubtitleAccountStatus(cx *core.Scope, secrets core.Secrets) *core.Resource[openSubtitleAccountState] {
	return core.NewResource(cx, func() (bool, bool) { return true, true },
		func(_ context.Context, _ bool, _ core.ResourceInfo) (openSubtitleAccountState, error) {
			credentials, err := readOpenSubtitleCredentials(secrets)
			return openSubtitleAccountState{credentials.Key != "", credentials.Username}, err
		})
}

func openSubtitleAccountForm(cx *core.Scope, secrets core.Secrets, transport http.RoundTripper, after func()) core.Node {
	key := core.NewSignal(cx, "")
	username := core.NewSignal(cx, "")
	password := core.NewSignal(cx, "")
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal[error](cx, nil)
	existing := openSubtitleAccountStatus(cx, secrets)
	valid := func() bool {
		return strings.TrimSpace(key.Get()) != "" && (strings.TrimSpace(username.Get()) != "") == (password.Get() != "")
	}

	update := func(clear bool) {
		if busy.GetUntracked() || !clear && !core.Untracked(cx, valid) {
			return
		}
		busy.Set(true)
		failure.Set(nil)
		creds := playback.OpenSubtitleCredentials{
			Key: strings.TrimSpace(key.GetUntracked()), Username: strings.TrimSpace(username.GetUntracked()), Password: password.GetUntracked(),
		}
		view := cx.Context()
		go func() {
			var err error
			if clear {
				err = secrets.Delete(openSubtitleSecret)
			} else {
				ctx, cancel := context.WithTimeout(view, 15*time.Second)
				services := playback.SubtitleServices{Open: creds, HTTP: &http.Client{Transport: transport}}
				err = services.VerifyOpenAccount(ctx)
				cancel()
				if err == nil && view.Err() == nil {
					data, _ := json.Marshal(creds)
					err = secrets.Set(openSubtitleSecret, string(data))
				}
			}
			cx.Post(func() {
				if view.Err() != nil {
					return
				}
				busy.Set(false)
				if err == nil {
					key.Set("")
					username.Set("")
					password.Set("")
					after()
				} else {
					failure.Set(err)
				}
			})
		}()
	}
	return core.View{Class: "gap-3", Children: []core.Node{
		hint(locales.OnlineSubtitleCredentialHelp(cx)),
		ui.Input{Label: locales.OnlineSubtitleKey(cx), Placeholder: locales.OnlineSubtitleKey(cx), Masked: true, NoAutoCorrect: true,
			TestID: "online-subtitle-api-key", Value: key.Get, OnChange: key.Set, Disabled: busy.Get},
		ui.Input{Label: locales.OnlineSubtitleUsername(cx), Placeholder: locales.OnlineSubtitleUsername(cx), NoAutoCorrect: true,
			TestID: "online-subtitle-user", Value: username.Get, OnChange: username.Set, Disabled: busy.Get},
		ui.Input{Label: locales.OnlineSubtitlePassword(cx), Placeholder: locales.OnlineSubtitlePassword(cx), Masked: true,
			TestID: "online-subtitle-password", Value: password.Get, OnChange: password.Set, Disabled: busy.Get},
		ui.Button{Text: locales.OnlineSubtitleSave(cx), TestID: "online-subtitle-save", Busy: busy.Get, OnPress: func() { update(false) },
			Disabled: func() bool { return !valid() || busy.Get() }},
		core.Show{When: func() bool {
			config, _ := existing.Get()
			return config.Enabled
		}, Then: func(cx *core.Scope) core.Node {
			return ui.Button{Text: locales.OnlineSubtitleRemove(cx), TestID: "online-subtitle-remove", Variant: ui.ButtonOutline,
				Disabled: busy.Get, OnPress: func() { update(true) }}
		}},
		core.Dynamic(func(cx *core.Scope) core.Node {
			if err := failure.Get(); err != nil {
				return notice(onlineSubtitleError(cx, err))
			}
			return nil
		}),
	}}
}
