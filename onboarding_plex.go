package main

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"rsc.io/qr"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type plexChallenge struct {
	pin media.PlexPIN
	qr  []byte
}

type plexAuthorizer interface {
	Begin(context.Context) (media.PlexPIN, error)
	Servers(context.Context, media.PlexPIN) ([]media.PlexServer, error)
	Connect(context.Context, media.PlexServer) (media.PlexServerConnection, error)
}

func (a *application) linkPlex(cx *core.Scope, current source, account plexAuthorizer) core.Node {
	attempt := core.NewSignal(cx, 0)
	restart := func() { attempt.Set(attempt.GetUntracked() + 1) }
	return pageFrame(pageHeader(cx, locales.LinkPlexAccount(cx), a.back), pageBody(core.Dynamic(func(cx *core.Scope) core.Node {
		attempt.Get()
		return a.plexAuthorization(cx, current, account, restart)
	})))
}

func (a *application) plexAuthorization(cx *core.Scope, current source, account plexAuthorizer, restart func()) core.Node {
	challenge := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) (plexChallenge, error) {
		pin, err := account.Begin(ctx)
		if err != nil {
			return plexChallenge{}, err
		}
		code, err := qr.Encode(pin.URL, qr.M)
		if err != nil {
			return plexChallenge{}, err
		}
		return plexChallenge{pin: pin, qr: code.PNG()}, nil
	})
	servers := core.NewResource(cx, func() (media.PlexPIN, bool) {
		state, ok := challenge.Get()
		return state.pin, ok
	}, func(ctx context.Context, pin media.PlexPIN, _ core.ResourceInfo) ([]media.PlexServer, error) {
		return account.Servers(ctx, pin)
	})
	selected := core.NewSignal[*media.PlexServer](cx, nil)
	browserFailed := core.NewSignal(cx, false)
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if server := selected.Get(); server != nil {
			return a.plexServerImport(cx, current, account, *server, func() { selected.Set(nil) })
		}
		for _, err := range []error{challenge.Err(), servers.Err()} {
			if err != nil {
				return plexLinkPanel(notice(errorText(cx, err)), plexLinkRetry(cx, restart))
			}
		}
		if choices, ok := servers.Get(); ok {
			rows := []core.Node{heading(locales.SelectPlexServer(cx))}
			for i := range choices {
				server := &choices[i]
				if current.ServerID != "" && current.ServerID != server.ID {
					continue
				}
				rows = append(rows, ui.Button{Text: server.Name, Icon: lucide.Server, Variant: ui.ButtonOutline,
					Size: ui.SizeLarge, TestID: "plex-server-" + server.ID, OnPress: func() { selected.Set(server) }})
			}
			if len(rows) == 1 {
				message := locales.NoPlexServers(cx)
				if current.ServerID != "" {
					message = locales.PlexServerNotInAccount(cx)
				}
				rows = append(rows, notice(message))
			}
			return plexLinkPanel(append(rows, plexLinkRetry(cx, restart))...)
		}
		if state, ok := challenge.Get(); ok {
			rows := []core.Node{
				hint(locales.PlexScanQR(cx)),
				core.Image{Source: core.ImageSource{Data: state.qr}, ResizeMode: core.Contain, TestID: "plex-auth-qr",
					Class: "size-52 self-center bg-white rounded-xl sm:size-56"},
				hint(locales.PlexWaitingAuthorization(cx)),
			}
			if cx.Env().Idiom() != core.IdiomTV {
				scope := cx.Context()
				rows = append(rows, ui.Button{Text: locales.OpenPlexSignIn(cx), Icon: lucide.ExternalLink,
					Size: ui.SizeLarge, TestID: "plex-open-browser", OnPress: func() {
						cx.OpenURL(state.pin.URL, func(opened bool) {
							if scope.Err() == nil {
								browserFailed.Set(!opened)
							}
						})
					}})
				if browserFailed.Get() {
					rows = append(rows, notice(locales.PlexBrowserUnavailable(cx)))
				}
			}
			return plexLinkPanel(rows...)
		}
		return plexLinkPanel(hint(locales.Loading(cx)))
	})
}

func (a *application) plexServerImport(cx *core.Scope, current source, account plexAuthorizer, server media.PlexServer, back func()) core.Node {
	connected := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) (candidateSource, error) {
		ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
		defer cancel()
		link, err := account.Connect(ctx, server)
		if err != nil {
			return candidateSource{}, err
		}
		form := source{ID: current.ID, Provider: media.Plex, PlexLinked: true,
			ServerID: link.Info.ID, Name: link.Info.Name, Address: link.Address}
		if current.Name != "" {
			form.Name = current.Name
		}
		if current.ServerID == link.Info.ID && current.RemoteAddress != form.Address {
			form.RemoteAddress = current.RemoteAddress
			form.RemoteDisabled = current.RemoteDisabled
			form.RemoteBitrate = current.RemoteBitrate
		}
		return candidateSource{source: form, info: link.Info, session: link.Session, libraries: link.Libraries}, nil
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if result, ok := connected.Get(); ok {
			return a.importSource(cx, result, back)
		}
		rows := []core.Node{heading(server.Name)}
		if err := connected.Err(); err != nil && !connected.Loading() {
			rows = append(rows, notice(errorText(cx, err)),
				ui.Button{Text: locales.Retry(cx), Size: ui.SizeLarge, TestID: "retry-plex-server", OnPress: connected.Refetch})
		} else {
			rows = append(rows, hint(locales.Loading(cx)))
		}
		return plexLinkPanel(append(rows, ui.Button{Text: locales.Back(cx), Variant: ui.ButtonGhost, OnPress: back})...)
	})
}

func plexLinkPanel(rows ...core.Node) core.Node {
	return core.View{Class: "gap-4 rounded-3xl border border-border bg-card p-5", Children: rows}
}

func plexLinkRetry(cx *core.Scope, restart func()) core.Node {
	return ui.Button{Text: locales.RestartPlexAuthorization(cx), Variant: ui.ButtonOutline,
		Size: ui.SizeLarge, TestID: "plex-restart", OnPress: restart}
}
