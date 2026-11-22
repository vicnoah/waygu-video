package main

import (
	"context"
	"errors"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"rsc.io/qr"

	"waygu-video/internal/media"
	"waygu-video/internal/tvpair"
	"waygu-video/locales"
)

type tvPairCardState struct {
	data    []byte
	code    string
	message string
}

var tvPairListener = core.NewKey("waygu.tvPairListener", func(ctx context.Context, apply func(context.Context, tvpair.Setup) error) (tvpair.Target, error) {
	ip, err := tvpair.LocalIP()
	if err != nil {
		return tvpair.Target{}, err
	}
	return tvpair.Listen(ctx, ip, apply)
})

func (a *application) tvPairCard(cx *core.Scope, l tvLayout) core.Node {
	refresh := core.NewSignal(cx, uint64(0))
	state := core.NewSignalFunc(cx, tvPairCardState{}, nil)
	listen := tvPairListener.Use(cx)
	cx.Effect(func() {
		refresh.Get()
		ctx, cancel := context.WithTimeout(tvpair.WithDevice(cx.Context(), a.pairingDevice(cx)), tvpair.Lifetime)
		cx.Cleanup(cancel)
		receive := func(call context.Context, setup tvpair.Setup) error {
			if err := a.receiveTVSetup(call, setup); err != nil {
				return err
			}
			peer := tvpair.Peer(call)
			a.scope.Post(func() { a.rememberPairedDevice(peer) })
			cx.Post(func() {
				if ctx.Err() == nil {
					state.Set(tvPairCardState{message: locales.TvPairSuccess(cx)})
				}
			})
			return nil
		}
		state.Set(tvPairCardState{})
		unavailable, expired := locales.TvPairUnavailable(cx), locales.TvPairExpired(cx)
		go func() {
			target, err := listen(ctx, receive)
			if err == nil {
				image, err := qr.Encode(target.Link(), qr.M)
				if err == nil {
					data, code := image.PNG(), target.Code()
					cx.Post(func() {
						if ctx.Err() == nil {
							state.Set(tvPairCardState{data: data, code: code})
						}
					})
					return
				}
			}
			cx.Post(func() {
				if ctx.Err() == nil {
					state.Set(tvPairCardState{message: unavailable})
				}
			})
		}()
		cx.AfterFunc(tvpair.Lifetime, func() {
			if state.GetUntracked().code != "" {
				state.Set(tvPairCardState{message: expired})
			}
		})
	})
	description := core.Text{Content: locales.TvPairInstructions(cx), Class: "text-lg leading-relaxed " + tvSettingsMuted,
		Style: core.Style{FontSize: l.points(18)}}
	status := l.text("", 18, "font-medium text-violet-500")
	status.ContentFunc = func() string {
		current := state.Get()
		if current.code != "" {
			return locales.TvPairCode(cx, current.code)
		}
		if current.message != "" {
			return current.message
		}
		return locales.Loading(cx)
	}
	status.TestID = "tv-pair-status"
	image := core.Dynamic(func(cx *core.Scope) core.Node {
		size := core.Style{Height: l.length(128), Width: l.length(128)}
		if data := state.Get().data; len(data) > 0 {
			return core.Image{Style: size, Class: "bg-white", Source: core.ImageSource{Data: data},
				TestID: "tv-pair-code", Label: locales.TvPairPhoneScan(cx), ResizeMode: core.Contain}
		}
		return core.View{Class: "items-center justify-center", Style: size,
			Children: []core.Node{l.icon(lucide.QrCode(), 68, "text-zinc-300")}}
	})
	return core.View{Class: "flex-row items-center rounded-3xl border border-zinc-900/8 bg-white shadow-sm shadow-black/5 dark:border-white/8 dark:bg-[#1a1b25]",
		Style:  core.Style{Gap: l.points(24), Padding: core.All(l.points(24)), FocusVisible: l.focusRing()},
		TestID: "tv-pair-card", Label: locales.TvPairTitle(cx), Role: core.RoleButton,
		OnPress: func(core.PressEvent) { refresh.Update(func(value uint64) uint64 { return value + 1 }) },
		Children: []core.Node{
			core.View{Class: "shrink-0 rounded-2xl border border-zinc-900/8 bg-white", Style: core.Style{Padding: core.All(l.points(12))},
				Children: []core.Node{image}},
			core.View{Class: "min-w-0 flex-1", Style: core.Style{Gap: l.points(8)}, Children: []core.Node{
				l.text(locales.TvPairTitle(cx), 24, "font-semibold "+tvSettingsTitle), description,
				core.View{Class: "self-start max-w-full rounded-full bg-violet-500/12", Style: core.Style{Padding: l.padding(16, 6)},
					Children: []core.Node{status}},
			}},
		}}
}

func (a *application) receiveTVSetup(ctx context.Context, setup tvpair.Setup) error {
	if setup.Provider == "" {
		id, secret := strings.TrimSpace(setup.AppID), strings.TrimSpace(setup.Secret)
		if ctx.Err() != nil || id == "" || secret == "" {
			return tvpair.ErrRejected
		}
		return a.saveDandanCredentials(id, secret)
	}
	provider := media.Provider(setup.Provider)
	switch provider {
	case media.Emby, media.Jellyfin, media.Plex, media.Flymoo:
	default:
		return tvpair.ErrRejected
	}
	endpoint, err := media.NormalizeAddress(setup.Address)
	if err != nil {
		return err
	}
	username := strings.TrimSpace(setup.Username)
	info, err := a.connectServer(ctx, source{Provider: provider, Address: endpoint.String(), Username: username},
		media.Credentials{Username: username, Password: setup.Password}, passwordConnection, nil)
	if err != nil {
		return err
	}
	complete := make(chan error, 1)
	a.scope.Post(func() {
		if ctx.Err() != nil {
			return
		}
		previous := a.matchSource(info.source)
		libraries := mergeLibraries(info.libraries, previous.Libraries, false)
		a.saveCandidateSource(ctx, info, libraries, func(err error) { complete <- err })
	})
	select {
	case err := <-complete:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *application) phoneTVPair(cx *core.Scope, initial string) core.Node {
	link := core.NewSignal(cx, initial)
	provider := core.NewSignal(cx, string(media.Emby))
	address := core.NewSignal(cx, "")
	username := core.NewSignal(cx, "")
	password := core.NewSignal(cx, "")
	id := core.NewSignal(cx, "")
	secret := core.NewSignal(cx, "")
	busy := core.NewSignal(cx, false)
	message := core.NewSignal(cx, "")
	done := core.NewSignal(cx, false)
	view := cx.Context()
	submit := func() {
		target, err := tvpair.Parse(link.GetUntracked())
		if err != nil {
			message.Set(locales.TvPairPhoneInvalid(cx))
			return
		}
		if busy.GetUntracked() || done.GetUntracked() {
			return
		}
		setup := tvpair.Setup{Provider: provider.GetUntracked()}
		if setup.Provider == "" {
			setup.AppID, setup.Secret = id.GetUntracked(), secret.GetUntracked()
		} else {
			setup.Address, setup.Username, setup.Password = address.GetUntracked(), username.GetUntracked(), password.GetUntracked()
		}
		busy.Set(true)
		message.Set("")
		device := a.pairingDevice(cx)
		go func() {
			peer, err := target.SendSetup(view, setup, device)
			if err == nil {
				a.scope.Post(func() { a.rememberPairedDevice(peer) })
			}
			cx.Post(func() {
				busy.Set(false)
				switch {
				case err == nil:
					password.Set("")
					secret.Set("")
					message.Set(locales.TvPairSuccess(cx))
					done.Set(true)
				case errors.Is(err, tvpair.ErrRejected):
					message.Set(locales.TvPairCredentialError(cx))
				default:
					message.Set(locales.TvPairNetworkError(cx))
				}
			})
		}()
	}
	disabled := func() bool { return busy.Get() || done.Get() }
	field := func(value *core.Signal[string], label, testID string, masked bool) core.Node {
		return ui.Input{Label: label, Placeholder: label, Value: value.Get, OnChange: value.Set, TestID: testID,
			Masked: masked, Disabled: disabled, NoAutoCorrect: true}
	}
	choices := []ui.Option[string]{
		{Label: providerName(cx, media.Emby), Value: string(media.Emby)},
		{Label: providerName(cx, media.Jellyfin), Value: string(media.Jellyfin)},
		{Label: providerName(cx, media.Plex), Value: string(media.Plex)},
		{Label: providerName(cx, media.Flymoo), Value: string(media.Flymoo)},
		{Label: locales.TvDanmakuDandan(cx), Value: ""},
	}
	var scan core.Node
	if cx.Env().CanScanCode() {
		scan = ui.Button{Text: locales.TvPairPhoneScan(cx), Icon: lucide.ScanLine, Variant: ui.ButtonOutline,
			Disabled: disabled, TestID: "phone-tv-pair-scan", OnPress: func() {
				cx.ScanCode(core.ScanOptions{}, func(code *core.ScannedCode, err error) {
					if err == nil && code != nil {
						link.Set(code.Text)
						message.Set("")
					}
				})
			}}
	}
	return phoneSettingsPage(cx, locales.TvPairPhoneTitle(cx), a.back, "phone-tv-pair", core.View{Class: "gap-3 px-4 pb-5", Children: []core.Node{
		scan, field(link, locales.TvPairPhoneCode(cx), "phone-tv-pair-address", false),
		ui.Select[string]{Label: locales.MediaSources(cx), Options: choices, Value: provider.Get, OnChange: provider.Set,
			TestID: "phone-tv-pair-provider", Disabled: disabled},
		core.Show{When: func() bool { return provider.Get() != "" }, Then: func(cx *core.Scope) core.Node {
			return core.View{Class: "gap-3", Children: []core.Node{
				field(address, locales.ServerAddress(cx), "phone-tv-pair-server", false),
				field(username, locales.Username(cx), "phone-tv-pair-user", false),
				field(password, locales.Password(cx), "phone-tv-pair-password", true),
			}}
		}},
		core.Show{When: func() bool { return provider.Get() == "" }, Then: func(cx *core.Scope) core.Node {
			return core.View{Class: "gap-3", Children: []core.Node{
				field(id, locales.DanmakuAppID(cx), "phone-tv-pair-app-id", false),
				field(secret, locales.DanmakuAppSecret(cx), "phone-tv-pair-app-secret", true),
			}}
		}},
		ui.Button{Text: locales.TvPairPhoneSend(cx), TestID: "phone-tv-pair-submit", Busy: busy.Get, OnPress: submit,
			Disabled: func() bool {
				if disabled() || strings.TrimSpace(link.Get()) == "" {
					return true
				}
				if provider.Get() == "" {
					return strings.TrimSpace(id.Get()) == "" || strings.TrimSpace(secret.Get()) == ""
				}
				return strings.TrimSpace(address.Get()) == ""
			}},
		core.Text{Class: "text-sm text-muted-foreground", ContentFunc: message.Get, TestID: "phone-tv-pair-status"},
	}})
}
