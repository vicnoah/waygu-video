package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
	"waygu-video/locales"
)

const logvarConfigKey = "logvar-danmaku-v1"

func (a *application) logvarConfig() (danmaku.LogvarConfig, error) {
	var config danmaku.LogvarConfig
	raw, found, err := a.secrets.Get(logvarConfigKey)
	if errors.Is(err, core.ErrSecretsUnavailable) {
		return config, nil
	}
	if err != nil || !found {
		return config, err
	}
	err = json.Unmarshal([]byte(raw), &config)
	return config, err
}

func logvarBindingKey(current route, config danmaku.LogvarConfig) string {
	return fmt.Sprintf("logvar-binding-v1:%s:%x", danmakuItemKey(current), sha256.Sum256([]byte(config.Address)))
}

func findLogvarBinding(ctx context.Context, client *danmaku.DandanClient, item media.Item, saved danmaku.DandanEpisode) (danmakuResult, error) {
	if saved.ID > 0 {
		return danmakuResult{bound: saved}, nil
	}
	title, episode := danmakuSearchTitle(item)
	title = strings.TrimSpace(title)
	if title == "" {
		return danmakuResult{}, nil
	}
	name := title
	if item.Kind == media.Episode && item.Episode > 0 {
		name += fmt.Sprintf(" S%02dE%02d", item.Season, item.Episode)
	} else if item.Year > 0 {
		name += fmt.Sprintf(" (%d)", item.Year)
	}
	matches, err := client.Match(ctx, danmaku.FileMatch{Name: name})
	if err != nil {
		return danmakuResult{}, err
	}
	if matches.Exact {
		return danmakuResult{bound: matches.Episodes[0]}, nil
	}
	if len(matches.Episodes) == 0 {
		matches, err = client.Search(ctx, title, episode)
	}
	return danmakuResult{matches: matches}, err
}

func (a *application) logvarSettings(cx *core.Scope, onSave func()) core.Node {
	address := core.NewSignal(cx, "")
	token := core.NewSignal(cx, "")
	keepToken := core.NewSignal(cx, false)
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal[error](cx, nil)
	saved := core.NewResource(cx, nil, func(context.Context, struct{}, core.ResourceInfo) (danmaku.LogvarConfig, error) {
		return a.logvarConfig()
	})
	cx.Effect(func() {
		if config, ok := saved.Get(); ok {
			cx.Batch(func() { address.Set(config.Address); keepToken.Set(config.Token != "") })
		}
	})
	disabled := func() bool { return busy.Get() || saved.Loading() }
	update := func(remove bool) {
		if core.Untracked(cx, disabled) {
			return
		}
		config := danmaku.LogvarConfig{Address: strings.TrimSpace(address.GetUntracked()), Token: token.GetUntracked()}
		if !remove {
			if existing, ok := saved.Get(); ok && config.Address == existing.Address && config.Token == "" && keepToken.GetUntracked() {
				config.Token = existing.Token
			}
			if _, err := danmaku.NewLogvarClient(config); err != nil {
				failure.Set(err)
				return
			}
		}
		busy.Set(true)
		failure.Set(nil)
		go func() {
			var err error
			if remove {
				err = a.secrets.Delete(logvarConfigKey)
			} else {
				raw, _ := json.Marshal(config)
				err = a.secrets.Set(logvarConfigKey, string(raw))
			}
			cx.Post(func() {
				busy.Set(false)
				failure.Set(err)
				if err == nil {
					token.Set("")
					saved.Refetch()
					if onSave != nil {
						onSave()
					}
				}
			})
		}()
	}
	return core.View{Class: "gap-3", Children: []core.Node{
		hint(locales.LogvarHelp(cx)),
		ui.Input{Label: locales.ServerAddress(cx), Placeholder: locales.ServerAddress(cx), Value: address.Get, OnChange: address.Set,
			TestID: "logvar-address", NoAutoCorrect: true, Disabled: disabled},
		ui.Input{Label: locales.LogvarPassword(cx), Placeholder: locales.LogvarPassword(cx), Value: token.Get, OnChange: token.Set,
			TestID: "logvar-password", NoAutoCorrect: true, Masked: true, Disabled: disabled},
		core.Show{When: func() bool {
			config, _ := saved.Get()
			return config.Token != "" && config.Address == address.Get() && token.Get() == ""
		}, Then: func(cx *core.Scope) core.Node {
			return ui.Checkbox{Text: locales.LogvarKeepPassword(cx), Checked: keepToken.Get,
				OnChange: keepToken.Set, Disabled: disabled, TestID: "logvar-keep-password"}
		}},
		core.View{Class: "flex-row flex-wrap gap-2", Children: []core.Node{
			ui.Button{Text: locales.LogvarSave(cx), TestID: "logvar-save", Busy: busy.Get,
				Disabled: func() bool { return disabled() || strings.TrimSpace(address.Get()) == "" },
				OnPress:  func() { update(false) }},
			core.Show{When: func() bool { config, _ := saved.Get(); return config.Address != "" }, Then: func(cx *core.Scope) core.Node {
				return ui.Button{Text: locales.LogvarRemove(cx), TestID: "logvar-remove", Variant: ui.ButtonOutline,
					Disabled: disabled, OnPress: func() { update(true) }}
			}},
		}},
		core.Dynamic(func(cx *core.Scope) core.Node {
			if errors.Is(failure.Get(), danmaku.ErrLogvarConfig) {
				return notice(locales.LogvarInvalid(cx))
			}
			if failure.Get() != nil || saved.Err() != nil {
				return notice(locales.SecretUnavailable(cx))
			}
			return nil
		}),
	}}
}
