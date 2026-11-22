package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync"

	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type screen string

const (
	screenWelcome         screen = "welcome"
	screenSources         screen = "sources"
	screenConnect         screen = "connect"
	screenPlexLink        screen = "plex-link"
	screenHome            screen = "home"
	screenLibrary         screen = "library"
	screenSearch          screen = "search"
	screenSettings        screen = "settings"
	screenManageLibraries screen = "manage-libraries"
	screenDetail          screen = "detail"
	screenPlayer          screen = "player"
)

type route struct {
	screen   screen
	provider media.Provider
	source   source
	item     media.Item
	resume   bool
}

type source struct {
	ID            string
	Provider      media.Provider
	Address       string
	Name          string
	ServerID      string
	AccountID     string
	Username      string
	PlexLinked    bool   `json:",omitempty"`
	CredentialKey string `json:",omitempty"`
	Libraries     []sourceLibrary
	Volatile      bool `json:"-"`
}

func (s source) credentialKey() string {
	if s.CredentialKey != "" {
		return s.CredentialKey
	}
	return s.ID
}

type mediaFactory func(media.Provider, string, media.Device, media.Session) (media.Client, error)

type application struct {
	scope        *core.Scope
	device       media.Device
	storage      core.Storage
	secrets      core.Secrets
	newMedia     mediaFactory
	sources      *core.Signal[[]source]
	routes       *core.Signal[[]route]
	tab          *core.Signal[int]
	sourceFilter *core.Signal[string]
	mpvPreferred *core.Signal[bool]
	bitrateLimit *core.Signal[int]
	autoNext     *core.Signal[bool]
	tokens       sync.Map
}

const (
	sourcesKey  = "media-sources-v1"
	deviceKey   = "media-device-v1"
	seenKey     = "onboarding-seen-v1"
	mpvKey      = "playback-libmpv-v1"
	bitrateKey  = "playback-bitrate-v1"
	autoNextKey = "playback-auto-next-v1"
)

var playbackBitrates = []int{0, 4_000_000, 8_000_000, 20_000_000}

func Root(cx *core.Scope) core.Node {
	return appRoot(cx, media.NewClient, nil)
}

func appRoot(cx *core.Scope, factory mediaFactory, plexAccount plexAuthorizer) core.Node {
	cx.SetTheme(wayguTheme())
	storage := cx.Storage()
	var sources []source
	if data, ok := storage.GetItem(sourcesKey); ok {
		_ = json.Unmarshal([]byte(data), &sources)
	}
	deviceID, _ := storage.GetItem(deviceKey)
	if deviceID == "" {
		deviceID = rand.Text()
		_ = storage.SetItem(deviceKey, deviceID)
	}
	first := screenWelcome
	mpvPreference, _ := storage.GetItem(mpvKey)
	bitrateSetting, _ := storage.GetItem(bitrateKey)
	autoNext, _ := storage.GetItem(autoNextKey)
	bitrate, _ := strconv.Atoi(bitrateSetting)
	if !slices.Contains(playbackBitrates, bitrate) {
		bitrate = 0
	}
	if _, seen := storage.GetItem(seenKey); seen || len(sources) > 0 {
		first = screenHome
	}
	a := &application{
		scope:  cx,
		device: media.Device{ID: deviceID, Name: "Waygu Video"}, storage: storage, secrets: cx.Secrets(), newMedia: factory,
		sources: core.NewSignalFunc(cx, sources, nil), routes: core.NewSignalFunc(cx, []route{{screen: first}}, nil),
		tab: core.NewSignal(cx, 0), sourceFilter: core.NewSignal(cx, ""),
		mpvPreferred: core.NewSignal(cx, mpvPreference == "true"),
		bitrateLimit: core.NewSignal(cx, bitrate),
		autoNext:     core.NewSignal(cx, autoNext != "false"),
	}
	cx.OnBackPress(func() bool {
		if len(a.routes.GetUntracked()) <= 1 {
			return false
		}
		a.back()
		return true
	})
	return core.View{Class: "flex-1 bg-background", Style: core.Style{FlexShrink: 1}, Children: []core.Node{
		core.Dynamic(func(screenScope *core.Scope) core.Node {
			stack := a.routes.Get()
			current := stack[len(stack)-1]
			switch current.screen {
			case screenWelcome:
				return a.welcome(screenScope)
			case screenSources:
				return a.chooseSource(screenScope)
			case screenConnect:
				return a.connect(screenScope, current)
			case screenPlexLink:
				account := plexAccount
				if account == nil {
					account = media.NewPlexAccount(a.device)
				}
				return a.linkPlex(screenScope, current.source, account)
			case screenManageLibraries:
				return a.manageLibraries(screenScope, current.source)
			case screenDetail:
				return a.detail(screenScope, current)
			case screenPlayer:
				return a.player(screenScope, current)
			default:
				return a.shell(screenScope)
			}
		}),
	}}
}

func (a *application) push(next route) {
	stack := a.routes.GetUntracked()
	a.routes.Set(append(slices.Clone(stack), next))
}

func (a *application) back() {
	stack := a.routes.GetUntracked()
	if len(stack) > 1 {
		a.routes.Set(stack[:len(stack)-1])
	}
}

func (a *application) enter() {
	_ = a.storage.SetItem(seenKey, "true")
	a.tab.Set(0)
	a.routes.Set([]route{{screen: screenHome}})
}

func (a *application) saveSources(sources []source) error {
	data, err := json.Marshal(sources)
	if err != nil {
		return err
	}
	if err := a.storage.SetItem(sourcesKey, string(data)); err != nil {
		return err
	}
	a.sources.Set(sources)
	return nil
}

func (a *application) client(s source) (media.Client, error) {
	key := s.credentialKey()
	value, exists := a.tokens.Load(key)
	if !exists {
		token, found, err := a.secrets.Get(key)
		if err != nil && !errors.Is(err, core.ErrSecretsUnavailable) {
			return nil, err
		}
		if !found && (s.Provider != media.Plex || s.PlexLinked) {
			return nil, media.ErrAuthentication
		}
		value = token
		a.tokens.Store(key, token)
	}
	return a.newMedia(s.Provider, s.Address, a.device, media.Session{UserID: s.AccountID, Token: value.(string)})
}

func errorText(cx *core.Scope, err error) string {
	switch {
	case errors.Is(err, media.ErrInvalidAddress):
		return locales.InvalidAddress(cx)
	case errors.Is(err, media.ErrAuthentication):
		return locales.AuthenticationError(cx)
	case errors.Is(err, media.ErrQuickConnectUnavailable):
		return locales.QuickConnectUnavailable(cx)
	case errors.Is(err, media.ErrPlexPINExpired):
		return locales.PlexAuthorizationExpired(cx)
	case errors.Is(err, media.ErrPlexUnreachable):
		return locales.PlexServerUnreachable(cx)
	case errors.Is(err, core.ErrSecretsUnavailable):
		return locales.SecretUnavailable(cx)
	default:
		return locales.ServerError(cx)
	}
}

func providerName(cx *core.Scope, provider media.Provider) string {
	switch provider {
	case media.Emby:
		return "Emby"
	case media.Jellyfin:
		return "Jellyfin"
	case media.Plex:
		return "Plex"
	case media.Flymoo:
		return locales.Flymoo(cx)
	default:
		return string(provider)
	}
}

func main() { nsgo.Run(Root, nsgo.WindowSize(1440, 900)) }
