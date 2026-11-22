package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
	"waygu-video/internal/mediatls"
	"waygu-video/internal/metadata"
	"waygu-video/locales"
)

type screen string

const (
	screenWelcome            screen = "welcome"
	screenSources            screen = "sources"
	screenConnect            screen = "connect"
	screenPlexLink           screen = "plex-link"
	screenHome               screen = "home"
	screenLibrary            screen = "library"
	screenSearch             screen = "search"
	screenFavorites          screen = "favorites"
	screenHistory            screen = "history"
	screenSettings           screen = "settings"
	screenSourceSettings     screen = "settings-sources"
	screenDanmakuSettings    screen = "settings-danmaku"
	screenPlaybackSettings   screen = "settings-playback"
	screenSubtitleSettings   screen = "settings-subtitles"
	screenSubtitleLanguages  screen = "settings-subtitle-languages"
	screenManageLibraries    screen = "manage-libraries"
	screenDetail             screen = "detail"
	screenPlayer             screen = "player"
	screenTVSettings         screen = "tv-settings"
	screenTVPairPhone        screen = "tv-pair-phone"
	screenSyncAccounts       screen = "settings-sync"
	screenDownloads          screen = "downloads"
	screenParentalSettings   screen = "settings-parental"
	screenAboutSettings      screen = "settings-about"
	screenLibrarySettings    screen = "settings-library-metadata"
	screenAppearanceSettings screen = "settings-appearance"
)

type route struct {
	screen        screen
	provider      media.Provider
	source        source
	item          media.Item
	resume        bool
	resumeLocal   bool
	versionID     string
	audioTrack    string
	subtitleTrack string
	downloadID    string
	castOnOpen    bool
	recovery      *playerRecovery
	match         *catalogMatch
	seriesView    seriesViewState
	pairLink      string
	settingsFlow  bool
	sourceAdmin   bool
}

type source struct {
	ID                string
	Provider          media.Provider
	Address           string
	RemoteAddress     string `json:",omitempty"`
	FNID              string `json:",omitempty"`
	RemoteDisabled    bool   `json:",omitempty"`
	RemoteBitrate     int    `json:",omitempty"`
	Name              string
	Certificates      []mediatls.Trust `json:",omitempty"`
	HeaderSecret      string           `json:",omitempty"`
	headerDraft       http.Header
	ServerID          string
	AccountID         string
	Username          string
	ProgressDisabled  bool   `json:",omitempty"`
	PlexLinked        bool   `json:",omitempty"`
	CredentialKey     string `json:",omitempty"`
	Libraries         []sourceLibrary
	LibrariesSyncedAt int64 `json:",omitempty"`
	Volatile          bool  `json:"-"`
}

func (s source) credentialKey() string {
	if s.CredentialKey != "" {
		return s.CredentialKey
	}
	return s.ID
}

type mediaFactory func(media.Provider, string, media.Device, media.Session) (media.Client, error)

type application struct {
	scope               *core.Scope
	lifetime            context.Context
	parental            *core.Signal[parentalPolicy]
	parentalUsage       *core.Signal[parentalUsage]
	parentalApprovals   *core.Signal[map[copyKey]bool]
	parentalPINFailures int
	parentalPINRetry    time.Time
	device              media.Device
	storage             core.Storage
	secrets             core.Secrets
	newMedia            mediaFactory
	newDandan           danmakuFactory
	sources             *core.Signal[[]source]
	routes              *core.Signal[[]route]
	tab                 *core.Signal[int]
	libraryView         *core.Signal[screen]
	libraryChanges      *core.Signal[uint64]
	phoneLibrary        *core.Signal[phoneLibraryState]
	desktopCategory     *core.Signal[libraryCategory]
	desktopCollapsed    *core.Signal[bool]
	desktopPlayerVolume *core.Signal[float32]
	diagnostics         *networkDiagnostics
	diagnosticPrefs     *core.Signal[diagnosticPreferences]
	desktopPrefs        *core.Signal[desktopPreferences]
	desktopSettingsTab  *core.Signal[desktopSettingsPage]
	desktopSettingsWin  *core.Window
	desktopLibraryState *core.Signal[desktopLibraryState]
	desktopCollections  *core.Signal[[]desktopCollection]
	phoneSearch         phoneSearchState
	sourceFilter        *core.Signal[string]
	sourceStatus        *core.Signal[map[string]homeSourceStatus]
	sourceChecks        *core.Signal[map[string]sourceCheck]
	sourceTimeout       *core.Signal[int]
	sourceFailover      *core.Signal[bool]
	routesEnabled       atomic.Bool
	refreshFNConnect    refreshFNConnect
	sourceCheckSerial   uint64
	sourceArtwork       sourceArtwork
	sourceHeaders       sync.Map
	libraryPrefs        *core.Signal[libraryPreferences]
	metadata            *metadata.Cache
	pairedDevices       *core.Signal[[]pairedDevice]
	mergeTitles         *core.Signal[bool]
	mpvPreferred        *core.Signal[bool]
	appearance          *core.Signal[string]
	appearancePrefs     *core.Signal[appearancePreferences]
	effectiveAppearance *core.Memo[string]
	bitrateLimit        *core.Signal[int]
	versionPolicy       *core.Signal[media.VersionPolicy]
	autoNext            *core.Signal[bool]
	playbackPrefs       *core.Signal[playbackPreferences]
	audioSamples        audioMarkerCache
	sleepTimer          *core.Signal[playerSleepTimer]
	subtitlePrefs       *core.Signal[subtitlePreferences]
	danmakuPrefs        *core.Signal[danmakuPreferences]
	progress            *progressOutbox
	downloads           *downloadQueue
	tokens              sync.Map
}

const (
	sourcesKey     = "media-sources-v1"
	deviceKey      = "media-device-v1"
	seenKey        = "onboarding-seen-v1"
	mpvKey         = "playback-libmpv-v1"
	bitrateKey     = "playback-bitrate-v1"
	autoNextKey    = "playback-auto-next-v1"
	mergeTitlesKey = "media-merge-identical-titles-v1"
)

var playbackBitrates = []int{0, 4_000_000, 8_000_000, 20_000_000}

func Root(cx *core.Scope) core.Node {
	return appRoot(cx, nil, nil)
}

func appRoot(cx *core.Scope, factory mediaFactory, plexAccount plexAuthorizer) core.Node {
	return appRootWithDandan(cx, factory, plexAccount, func(credentials danmaku.DandanCredentials) *danmaku.DandanClient {
		return danmaku.NewDandanClient(credentials, nil)
	})
}

func appRootWithDandan(_ *core.Scope, factory mediaFactory, plexAccount plexAuthorizer, dandan danmakuFactory) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		return core.Untracked(cx, func() core.Node { return mountApp(cx, factory, plexAccount, dandan) })
	})
}

func mountApp(cx *core.Scope, factory mediaFactory, plexAccount plexAuthorizer, dandan danmakuFactory) core.Node {
	storage := cx.Storage()
	prefs := readDiagnosticPreferences(storage)
	diagnostics := &networkDiagnostics{dir: cx.LogsDir()}
	diagnostics.enabled.Store(prefs.Network)
	if factory == nil {
		factory = func(provider media.Provider, address string, device media.Device, session media.Session) (media.Client, error) {
			return media.NewObservedClient(provider, address, device, session, func(metric media.RequestMetric) {
				diagnostics.record(time.Now(), provider, metric)
			})
		}
	}
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
	policy, _ := storage.GetItem(versionPolicyKey)
	autoNext, _ := storage.GetItem(autoNextKey)
	mergeTitles, _ := storage.GetItem(mergeTitlesKey)
	sourceTimeout, _ := storage.GetItem(sourceTimeoutKey)
	routeFailover, _ := storage.GetItem(sourceFailoverKey)
	bitrate, _ := strconv.Atoi(bitrateSetting)
	if !slices.Contains(playbackBitrates, bitrate) {
		bitrate = 0
	}
	if _, seen := storage.GetItem(seenKey); seen || len(sources) > 0 {
		first = screenHome
	}
	a := &application{
		scope: cx, lifetime: cx.Context(),
		device: media.Device{ID: deviceID, Name: "Waygu Video"}, storage: storage, secrets: cx.Secrets(), newMedia: factory, newDandan: dandan,
		sources: core.NewSignalFunc(cx, sources, nil), routes: core.NewSignalFunc(cx, []route{{screen: first}}, nil),
		tab: core.NewSignal(cx, 0), libraryView: core.NewSignal(cx, screenLibrary), libraryChanges: core.NewSignal(cx, uint64(0)),
		sourceFilter:  core.NewSignal(cx, ""),
		sourceStatus:  core.NewSignalFunc[map[string]homeSourceStatus](cx, nil, maps.Equal),
		mergeTitles:   core.NewSignal(cx, mergeTitles != "false"),
		mpvPreferred:  core.NewSignal(cx, mpvPreference == "true"),
		bitrateLimit:  core.NewSignal(cx, bitrate),
		versionPolicy: core.NewSignal(cx, playbackVersionPolicy(policy)),
		autoNext:      core.NewSignal(cx, autoNext != "false"),
		diagnostics:   diagnostics, diagnosticPrefs: core.NewSignal(cx, prefs),
		danmakuPrefs: core.NewSignal(cx, core.Untracked(cx, func() danmakuPreferences { return readDanmakuPreferences(storage) })),
	}
	a.refreshFNConnect = fnConnectRefresher.Use(cx)
	a.routesEnabled.Store(routeFailover != "false")
	a.artworkSources(sources)
	cx.Cleanup(func() { a.artworkSources(nil) })
	a.parental = core.NewSignalFunc(cx, core.Untracked(cx, func() parentalPolicy { return readParentalPolicy(storage) }), nil)
	a.parentalUsage = core.NewSignal(cx, core.Untracked(cx, func() parentalUsage { return readParentalUsage(storage) }))
	a.parentalApprovals = core.NewSignalFunc(cx, map[copyKey]bool{}, nil)
	a.applyAppearance(cx)
	a.progress = core.Untracked(cx, func() *progressOutbox { return newProgressOutbox(cx, storage) })
	a.startLibraryPreferences(cx)
	a.progress.setSources(sources)
	a.pairedDevices = core.NewSignalFunc(cx, core.Untracked(cx, func() []pairedDevice { return readPairedDevices(storage) }), nil)
	a.playbackPrefs = core.NewSignal(cx, core.Untracked(cx, func() playbackPreferences { return readPlaybackPreferences(storage) }))
	a.sleepTimer = core.NewSignal(cx, playerSleepTimer{})
	cx.Effect(func() {
		stack := a.routes.Get()
		if stack[len(stack)-1].screen != screenPlayer {
			a.sleepTimer.Set(playerSleepTimer{})
		}
	})
	a.subtitlePrefs = core.NewSignalFunc(cx, core.Untracked(cx, func() subtitlePreferences { return readSubtitlePreferences(storage) }), nil)
	a.sourceChecks = core.NewSignalFunc(cx, map[string]sourceCheck{}, nil)
	a.sourceTimeout = core.NewSignal(cx, readSourceTimeout(sourceTimeout))
	a.sourceFailover = core.NewSignal(cx, routeFailover != "false")
	a.phoneLibrary = core.NewSignal(cx, phoneLibraryState{})
	a.desktopCategory = core.NewSignal(cx, libraryCategory(""))
	a.desktopCollapsed = core.NewSignal(cx, false)
	a.desktopPlayerVolume = core.NewSignal(cx, float32(0.75))
	a.desktopLibraryState = core.NewSignal(cx, desktopLibraryState{mode: preferredDesktopLibraryMode(a.appearancePrefs.GetUntracked())})
	a.initDesktopPreferences(cx, first)
	a.desktopCollections = core.NewSignalFunc(cx, readDesktopCollections(storage), nil)
	a.phoneSearch = newPhoneSearchState(cx)
	a.scheduleProgressReplay(time.Second)
	a.downloads = core.Untracked(cx, func() *downloadQueue { return newDownloadQueue(cx, a) })
	a.downloads.pump()
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
				return a.parentalSourceRoute(screenScope, current, a.chooseSource)
			case screenConnect:
				return a.parentalSourceRoute(screenScope, current, func(cx *core.Scope) core.Node { return a.connect(cx, current) })
			case screenPlexLink:
				account := plexAccount
				if account == nil {
					account = media.NewPlexAccount(a.device)
				}
				return a.parentalSourceRoute(screenScope, current, func(cx *core.Scope) core.Node { return a.linkPlex(cx, current.source, account) })
			case screenSourceSettings:
				return a.parentalSourceRoute(screenScope, current, a.phoneSourceSettings)
			case screenDanmakuSettings:
				return a.phoneDanmakuSettings(screenScope)
			case screenManageLibraries:
				return a.parentalSourceRoute(screenScope, current, func(cx *core.Scope) core.Node { return a.manageLibraries(cx, current.source) })
			case screenDetail:
				return a.parentalMediaRoute(screenScope, current, a.detail)
			case screenPlayer:
				return a.parentalMediaRoute(screenScope, current, a.player)
			case screenSyncAccounts:
				return a.parentalSourceRoute(screenScope, current, a.phoneSyncAccounts)
			case screenTVPairPhone:
				return a.parentalSourceRoute(screenScope, current, func(cx *core.Scope) core.Node { return a.phoneTVPair(cx, current.pairLink) })
			case screenTVSettings:
				initial := tvSources
				if current.item.ID != "" {
					initial = tvDanmaku
				}
				return a.tvSettingsPage(screenScope, initial, current, a.back)
			case screenPlaybackSettings:
				return a.phonePlaybackSettings(screenScope)
			case screenSubtitleSettings:
				return a.phoneSubtitleSettings(screenScope)
			case screenSubtitleLanguages:
				return a.phoneSubtitleLanguages(screenScope)
			case screenAppearanceSettings:
				return a.phoneAppearanceSettings(screenScope)
			case screenLibrarySettings:
				return a.parentalSourceRoute(screenScope, current, a.phoneLibrarySettings)
			case screenAboutSettings:
				return a.phoneAboutSettings(screenScope)
			case screenParentalSettings:
				return a.phoneParentalSettings(screenScope)
			case screenDownloads:
				return a.downloadStorage(screenScope)
			default:
				return a.shell(screenScope)
			}
		}),
	}}
}

func (a *application) push(next route) {
	stack := a.routes.GetUntracked()
	switch next.screen {
	case screenSources, screenConnect, screenPlexLink, screenSourceSettings, screenManageLibraries, screenTVPairPhone, screenLibrarySettings, screenSyncAccounts:
		next.sourceAdmin = stack[len(stack)-1].sourceAdmin
	}
	a.routes.Set(append(slices.Clone(stack), next))
}

func (a *application) back() {
	stack := a.routes.GetUntracked()
	if len(stack) > 1 {
		a.routes.Set(stack[:len(stack)-1])
		if stack[len(stack)-1].settingsFlow {
			a.restoreDesktopSettings()
		}
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
	a.progress.setSources(sources)
	a.artworkSources(sources)
	a.sources.Set(sources)
	a.scheduleProgressReplay(time.Second)
	return nil
}

func (a *application) clientAt(s source, address string) (media.Client, error) {
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
	return a.sourceClient(s, address, media.Session{UserID: s.AccountID, Token: value.(string)}, true)
}

func errorText(cx *core.Scope, err error) string {
	var certificate *sourceCertificateError
	switch {
	case errors.As(err, &certificate):
		return locales.SourceCertificateRejected(cx)
	case errors.Is(err, errSourceProbeDenied):
		return locales.SourceProbeDenied(cx)
	case errors.Is(err, errSourceHeadersUnavailable):
		return locales.SourceHeadersUnavailable(cx)
	case errors.Is(err, media.ErrFNConnectID):
		return locales.FlymooFNInvalid(cx)
	case errors.Is(err, media.ErrFNConnectAbsent):
		return locales.FlymooFNNotFound(cx)
	case errors.Is(err, media.ErrFNConnectLookup):
		return locales.FlymooFNLookupFailed(cx)
	case errors.Is(err, media.ErrFNConnectRoute):
		return locales.FlymooFNNoRoute(cx)
	case errors.Is(err, media.ErrFNConnectCode):
		return locales.FlymooFNCodeRejected(cx)
	case errors.Is(err, media.ErrFlymooGateway):
		return locales.FlymooGatewayRejected(cx)
	case errors.Is(err, errDifferentServer):
		return locales.SourceDifferentServer(cx)
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

func runApplication(cx *core.Scope) { nsgo.SetMainView(cx, Root) }

func main() { nsgo.Run(runApplication, nsgo.WindowSize(1440, 900)) }
