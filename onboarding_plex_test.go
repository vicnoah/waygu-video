package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestPlexLinkAndImport(t *testing.T) {
	for _, target := range []struct {
		name      string
		tv        bool
		reconnect bool
	}{{name: "browser"}, {name: "tv", tv: true}, {name: "reconnect", reconnect: true}} {
		t.Run(target.name, func(t *testing.T) {
			fixture := plexSignInFixture{authorized: make(chan struct{})}
			host := core.NewHost()
			host.HandleOpenURL(func(address string, answer func(bool)) {
				if address != fixturePlexSignIn {
					t.Error("opened an unexpected authorization page")
					answer(false)
					return
				}
				close(fixture.authorized)
				answer(true)
			})
			options := []nsgotest.Option{nsgotest.Host(host), nsgotest.Size(1280, 720)}
			if target.tv {
				options = append(options, nsgotest.Idiom(core.IdiomTV))
			}
			const oldSource = "previous-plex"
			if target.reconnect {
				options = append(options,
					nsgotest.Storage(map[string]string{sourcesKey: `[{"ID":"previous-plex","Provider":"plex",
						"Address":"https://old.example.test","Name":"Custom Plex Name","ServerID":"second","PlexLinked":true}]`}),
					nsgotest.Secrets(map[string]string{oldSource: "old-plex-token"}))
			}
			var secrets core.Secrets
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				secrets = cx.Secrets()
				return appRoot(cx, func(kind media.Provider, address string, device media.Device, session media.Session) (media.Client, error) {
					if kind != media.Plex || address != "https://second.example.test" || device.ID == "" {
						return nil, media.ErrInvalidAddress
					}
					return fixtureServer{session: session}, nil
				}, fixture)
			}, options...)
			if target.reconnect {
				app.Press("role:tab Settings")
				app.Press("role:button Sign in again")
			} else {
				app.Press("id:add-source")
				app.Press("id:choose-plex")
			}
			app.Find("id:server-password")
			app.Press("id:link-plex-account")
			app.WaitFor("id:plex-auth-qr", time.Second)
			if target.tv {
				app.WaitForGone("id:plex-open-browser", time.Second)
				close(fixture.authorized)
			} else {
				app.Press("id:plex-open-browser")
			}
			app.WaitFor("id:plex-server-second", time.Second)
			if target.reconnect {
				app.WaitForGone("id:plex-server-first", time.Second)
			} else {
				app.Press("id:plex-server-first")
				app.WaitFor("id:import-library-library-first", time.Second)
				app.Press("id:import-back")
				app.WaitFor("id:plex-server-second", time.Second)
			}
			app.Press("id:plex-server-second")
			app.WaitFor("id:import-library-library-second", time.Second)
			app.WaitForGone("id:import-library-library-first", time.Second)
			app.Press("id:import-libraries")
			app.WaitFor("id:title-test-film", time.Second)

			config, _ := app.Storage().GetItem(sourcesKey)
			var saved []source
			if err := json.Unmarshal([]byte(config), &saved); err != nil || len(saved) != 1 {
				t.Fatal("did not save a single Plex source")
			}
			if s := saved[0]; s.Provider != media.Plex || !s.PlexLinked || s.ServerID != "second" ||
				len(s.Libraries) != 1 || s.Libraries[0].ID != "library-second" {
				t.Fatal("saved a source other than the selected Plex server")
			}
			if strings.Contains(config, "fixture-token") || strings.Contains(config, fixturePlexSignIn) {
				t.Fatal("source configuration exposed Plex authorization or server credentials")
			}
			if s := saved[0]; target.reconnect && (s.ID != oldSource || s.Name != "Custom Plex Name" || s.credentialKey() == oldSource) {
				t.Fatal("reconnect did not retain source details with a new credential key")
			}
			if token, found, err := secrets.Get(saved[0].credentialKey()); err != nil || !found || token != "fixture-token" {
				t.Fatal("the selected server token was not persisted in the credential store")
			}
		})
	}
}

const fixturePlexSignIn = "https://app.plex.tv/auth#?clientID=test&code=example"

type plexSignInFixture struct {
	authorized chan struct{}
}

func (plexSignInFixture) Begin(context.Context) (media.PlexPIN, error) {
	return media.PlexPIN{URL: fixturePlexSignIn}, nil
}

func (f plexSignInFixture) Servers(ctx context.Context, _ media.PlexPIN) ([]media.PlexServer, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.authorized:
		return []media.PlexServer{{ID: "first", Name: "First Plex"}, {ID: "second", Name: "Second Plex"}}, nil
	}
}

func (plexSignInFixture) Connect(_ context.Context, server media.PlexServer) (media.PlexServerConnection, error) {
	return media.PlexServerConnection{
		Address:   "https://" + server.ID + ".example.test",
		Info:      media.ServerInfo{ID: server.ID, Name: server.Name},
		Session:   media.Session{Token: "fixture-token"},
		Libraries: []media.Library{{ID: "library-" + server.ID, Name: "Movies", Kind: "movie"}},
	}, nil
}
