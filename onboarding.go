package main

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

//go:embed design/assets/posters/*.jpg
var welcomePosters embed.FS

//go:embed assets/Icon.png
var welcomeMark []byte

var welcomeAmbientLight, _ = core.ParseSVG([]byte(`<svg viewBox="0 0 1000 1000" preserveAspectRatio="none"><defs>
<radialGradient id="light" cx="0.5" cy="0" r="0.8"><stop offset="0" stop-color="#8b5cf6" stop-opacity="0.06"/><stop offset="0.5" stop-color="#8b5cf6" stop-opacity="0.02"/><stop offset="1" stop-color="#8b5cf6" stop-opacity="0"/></radialGradient>
<linearGradient id="fade" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#f4f4f7" stop-opacity="0"/><stop offset="0.6" stop-color="#f4f4f7" stop-opacity="0.4"/><stop offset="1" stop-color="#f4f4f7" stop-opacity="1"/></linearGradient></defs>
<rect width="1000" height="1000" fill="url(#light)"/><rect width="1000" height="1000" fill="url(#fade)"/></svg>`))

var welcomeAmbientDark, _ = core.ParseSVG([]byte(`<svg viewBox="0 0 1000 1000" preserveAspectRatio="none"><defs>
<radialGradient id="dark" cx="0.5" cy="0" r="0.8"><stop offset="0" stop-color="#8b5cf6" stop-opacity="0.08"/><stop offset="0.5" stop-color="#8b5cf6" stop-opacity="0.03"/><stop offset="1" stop-color="#8b5cf6" stop-opacity="0"/></radialGradient>
<linearGradient id="fade" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#0f1017" stop-opacity="0"/><stop offset="0.6" stop-color="#0f1017" stop-opacity="0.4"/><stop offset="1" stop-color="#0f1017" stop-opacity="1"/></linearGradient></defs>
<rect width="1000" height="1000" fill="url(#dark)"/><rect width="1000" height="1000" fill="url(#fade)"/></svg>`))

func (a *application) welcome(cx *core.Scope) core.Node {
	cx.SetStatusBar(core.StatusBarSetting{Foreground: core.StatusBarLight})
	cx.Cleanup(func() {
		cx.SetStatusBar(core.StatusBarSetting{Foreground: core.StatusBarAuto})
	})
	wall := make([]core.Node, 0, 4)
	for col, files := range [][2]string{
		{"wandering-earth-2.jpg", "three-body.jpg"},
		{"suzume.jpg", "our-planet.jpg"},
		{"hero.jpg", "dune-2.jpg"},
		{"past-lives.jpg", "john-wick-4.jpg"},
	} {
		pictures := make([]core.Node, 0, 2)
		for _, file := range files {
			data, err := welcomePosters.ReadFile("design/assets/posters/" + file)
			if err == nil {
				pictures = append(pictures, core.Image{Source: core.ImageSource{Data: data}, ResizeMode: core.Cover,
					Class: "h-44 w-28 rounded-2xl shadow-lg shadow-black/20", ElementsHidden: true})
			}
		}
		offset := []core.Points{40, 0, 64, 16}[col]
		wall = append(wall, core.View{Class: "gap-3", Style: core.Style{Padding: core.Edges{Top: offset}}, Children: pictures})
	}
	features := []core.Node{
		welcomeFeature(lucide.Server(), "bg-violet-500/12", "text-violet-500 dark:text-violet-400",
			locales.WelcomeMediaSources(cx), locales.WelcomeMediaSourcesDetail(cx, locales.Flymoo(cx))),
		welcomeFeature(lucide.MessageSquareText(), "bg-fuchsia-500/12", "text-fuchsia-500 dark:text-fuchsia-400",
			locales.WelcomeDanmaku(cx), locales.WelcomeDanmakuDetail(cx)),
		welcomeFeature(lucide.Sparkles(), "bg-amber-500/12", "text-amber-500 dark:text-amber-400",
			locales.WelcomeOriginalPlayback(cx), locales.WelcomeOriginalPlaybackDetail(cx)),
		welcomeFeature(lucide.Cloud(), "bg-sky-500/12", "text-sky-500 dark:text-sky-400",
			locales.WelcomeContinueWatching(cx), locales.WelcomeContinueWatchingDetail(cx)),
	}
	add, later := locales.AddSource(cx), locales.Later(cx)
	return core.View{Class: "relative flex-1 bg-[#f4f4f7] dark:bg-[#0f1017]", Style: core.Style{FlexShrink: 1}, Children: []core.Node{
		core.SVGView{Class: "absolute inset-0 w-full h-full dark:hidden", SVG: welcomeAmbientLight},
		core.SVGView{Class: "absolute inset-0 hidden w-full h-full dark:flex", SVG: welcomeAmbientDark},
		core.Scroll{Class: "flex-1", Children: []core.Node{
			core.View{Class: "w-full max-w-[640px] self-center", StyleFunc: func() core.Style {
				return core.Style{MinHeight: core.Pt(cx.Env().Window().Height)}
			}, Children: []core.Node{
				core.View{TestID: "welcome-hero", Class: "relative h-[250px] overflow-hidden", Children: []core.Node{
					core.View{Class: "absolute top-0 left-0 flex-row gap-3", Children: wall},
					core.View{Class: "absolute inset-0 bg-linear-to-b from-transparent via-[#f4f4f7]/40 to-[#f4f4f7] dark:via-[#0f1017]/40 dark:to-[#0f1017]"},
				}},
				core.View{Class: "flex-1 gap-5 px-7", Children: []core.Node{
					core.View{Class: "gap-3", Children: []core.Node{
						core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
							core.Image{Class: "size-10", Source: core.ImageSource{Data: welcomeMark}, ResizeMode: core.Contain, ElementsHidden: true},
							core.Text{Content: locales.AppName(cx), Class: "text-3xl font-bold tracking-tight text-zinc-900 dark:text-zinc-50"},
						}},
						core.Text{Content: locales.WelcomeTitle(cx), Class: "text-[26px] leading-tight font-bold tracking-tight text-zinc-900 dark:text-zinc-50"},
						core.Text{Content: locales.WelcomeBody(cx), Class: "text-sm leading-relaxed text-zinc-500 dark:text-zinc-400"},
					}},
					core.View{TestID: "welcome-features", Class: "gap-px overflow-hidden rounded-2xl border border-zinc-900/8 bg-zinc-200 shadow-sm shadow-black/5 dark:border-white/8 dark:bg-white/7",
						Children: features},
					core.View{Class: "mt-auto gap-1 pb-6", Style: core.Style{PaddingEnv: core.EnvEdges{Bottom: core.EnvSafeArea}}, Children: []core.Node{
						base.Button{Label: add, TestID: "add-source",
							Class:   "h-[52px] flex-row items-center justify-center gap-2 rounded-2xl bg-linear-to-r from-violet-500 to-fuchsia-500 shadow-lg shadow-violet-500/30",
							Style:   core.Style{FocusVisible: ui.FocusRing(cx, 3)},
							OnPress: func(core.PressEvent) { a.push(route{screen: screenSources}) },
							Children: []core.Node{core.Icon{SVG: lucide.Plus(), Class: "size-5 text-white"},
								core.Text{Content: add, Class: "text-base font-semibold text-white"}}},
						base.Button{Label: later, TestID: "onboarding-later", Class: "items-center py-2",
							Style: core.Style{FocusVisible: ui.FocusRing(cx, 3)}, OnPress: func(core.PressEvent) { a.enter() },
							Children: []core.Node{core.Text{Content: later, Class: "text-sm font-medium text-zinc-500 dark:text-zinc-400"}}},
					}},
				}},
			}},
		}},
	}}
}

func welcomeFeature(icon *core.SVG, background, tint, title, detail string) core.Node {
	return core.View{Class: "flex-row items-center gap-3 bg-white px-4 py-2.5 dark:bg-[#1a1b25]", Children: []core.Node{
		core.View{Class: "size-9 items-center justify-center rounded-xl " + background, Children: []core.Node{
			core.Icon{SVG: icon, Class: "size-4 " + tint},
		}},
		core.View{Class: "flex-1", Children: []core.Node{
			core.Text{Content: title, Class: "text-sm font-medium text-zinc-900 dark:text-zinc-50"},
			core.Text{Content: detail, Class: "text-xs text-zinc-500 dark:text-zinc-400"},
		}},
	}}
}

func (a *application) chooseSource(cx *core.Scope) core.Node {
	choices := make([]core.Node, 0, 5)
	choices = append(choices, heading(locales.ChooseSource(cx)))
	for _, provider := range []media.Provider{media.Emby, media.Jellyfin, media.Flymoo, media.Plex} {
		icon := lucide.Server
		if provider == media.Flymoo {
			icon = lucide.HardDrive
		}
		choices = append(choices, ui.Button{Text: providerName(cx, provider), Icon: icon,
			Variant: ui.ButtonOutline, Size: ui.SizeLarge, TestID: "choose-" + string(provider),
			OnPress: func() { a.push(route{screen: screenConnect, provider: provider}) }})
	}
	return pageFrame(pageHeader(cx, locales.AddSource(cx), a.back), pageBody(choices...))
}

type connectionMethod uint8

const (
	probeConnection connectionMethod = iota
	passwordConnection
	quickConnection
)

type candidateSource struct {
	source    source
	info      media.ServerInfo
	session   media.Session
	libraries []media.Library
}

func (a *application) connect(cx *core.Scope, current route) core.Node {
	address := core.NewSignal(cx, current.source.Address)
	name := core.NewSignal(cx, current.source.Name)
	user := core.NewSignal(cx, current.source.Username)
	password := core.NewSignal(cx, "")
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal[error](cx, nil)
	preview := core.NewSignal(cx, media.ServerInfo{})
	candidate := core.NewSignal[*candidateSource](cx, nil)
	quickCode := core.NewSignal(cx, "")
	viewContext := cx.Context()
	plex := current.provider == media.Plex
	submit := func(method connectionMethod) {
		if busy.GetUntracked() {
			return
		}
		base, err := media.NormalizeAddress(address.GetUntracked())
		if err != nil {
			failure.Set(err)
			return
		}
		form := source{ID: current.source.ID, Address: base.String(), Provider: current.provider,
			Name: strings.TrimSpace(name.GetUntracked()), Username: strings.TrimSpace(user.GetUntracked())}
		input := media.Credentials{Username: form.Username, Password: password.GetUntracked()}
		if plex {
			input.Token = input.Password
			input.Password = ""
		}
		timeout := 35 * time.Second
		if method == quickConnection {
			timeout = 8 * time.Minute
		}
		ctx, cancel := context.WithTimeout(viewContext, timeout)
		busy.Set(true)
		failure.Set(nil)
		go func() {
			defer cancel()
			result, err := a.connectServer(ctx, form, input, method, func(code string) {
				cx.Post(func() {
					if viewContext.Err() == nil {
						quickCode.Set(code)
					}
				})
			})
			cx.Post(func() {
				if viewContext.Err() != nil {
					return
				}
				busy.Set(false)
				quickCode.Set("")
				failure.Set(err)
				if err == nil {
					preview.Set(result.info)
					if method != probeConnection {
						password.Set("")
						candidate.Set(&result)
					}
				}
			})
		}()
	}
	return pageFrame(pageHeader(cx, locales.ConnectServer(cx), a.back), pageBody(core.Dynamic(func(form *core.Scope) core.Node {
		if result := candidate.Get(); result != nil {
			return a.importSource(form, *result, func() { candidate.Set(nil) })
		}
		manual := connectionForm(form, current.provider, address, name, user, password, busy, failure, preview, quickCode, submit)
		if !plex {
			return manual
		}
		return core.View{Class: "gap-4", Children: []core.Node{
			ui.Button{Text: locales.LinkPlexAccount(form), Icon: lucide.Link, Size: ui.SizeLarge,
				TestID: "link-plex-account", Disabled: busy.Get, OnPress: func() {
					a.push(route{screen: screenPlexLink, source: current.source})
				}},
			manual,
		}}
	})))
}

func connectionForm(cx *core.Scope, provider media.Provider, address, name, user, password *core.Signal[string],
	busy *core.Signal[bool], failure *core.Signal[error], preview *core.Signal[media.ServerInfo], quickCode *core.Signal[string], submit func(connectionMethod)) core.Node {
	entry := func(id, label, example string, signal *core.Signal[string], masked bool, keyboard core.KeyboardType) core.Node {
		return core.View{Class: "gap-1.5", Children: []core.Node{
			core.Text{Content: label, Class: "text-sm font-medium"},
			ui.Input{Label: label, TestID: id, Placeholder: example, Masked: masked, KeyboardType: keyboard,
				Value: signal.Get, OnChange: signal.Set, NoAutoCorrect: true, Disabled: busy.Get,
				Style: core.Style{Height: core.Pt(44), FontSize: 15}},
		}}
	}
	fields := []core.Node{
		heading(providerName(cx, provider)),
		entry("server-address", locales.ServerAddress(cx), "https://nas.example.com:8096", address, false, core.KeyboardURL),
		entry("server-name", locales.SourceName(cx), providerName(cx, provider), name, false, 0),
	}
	if provider != media.Plex {
		fields = append(fields, entry("server-username", locales.Username(cx), "", user, false, 0),
			entry("server-password", locales.Password(cx), "", password, true, 0))
	} else {
		fields = append(fields, entry("server-password", locales.PlexToken(cx), "", password, true, 0))
	}
	fields = append(fields, core.Dynamic(func(cx *core.Scope) core.Node {
		if err := failure.Get(); err != nil {
			return notice(errorText(cx, err))
		}
		if code := quickCode.Get(); code != "" {
			return notice(locales.QuickConnectInstruction(cx, code))
		}
		if info := preview.Get(); info.Name != "" {
			return notice(locales.Connected(cx, strings.TrimSpace(info.Name+" "+info.Version)))
		}
		return nil
	}), core.View{Class: "flex-row flex-wrap gap-3", Children: []core.Node{
		ui.Button{Text: locales.TestConnection(cx), Variant: ui.ButtonOutline, Size: ui.SizeLarge, Disabled: busy.Get,
			TestID: "test-connection", OnPress: func() { submit(probeConnection) }},
		ui.Button{Text: locales.SignIn(cx), Size: ui.SizeLarge, Busy: busy.Get, TestID: "sign-in", OnPress: func() { submit(passwordConnection) }},
	}})
	if provider == media.Jellyfin {
		fields = append(fields, ui.Button{Text: locales.QuickConnect(cx), Variant: ui.ButtonOutline,
			TestID: "quick-connect", Disabled: busy.Get, OnPress: func() { submit(quickConnection) }})
	}
	return core.View{Class: "gap-4 rounded-3xl border border-border bg-card p-5", Children: fields}
}

func (a *application) connectServer(ctx context.Context, form source, input media.Credentials, method connectionMethod, showCode func(string)) (candidateSource, error) {
	guest, err := a.newMedia(form.Provider, form.Address, a.device, media.Session{})
	if err != nil {
		return candidateSource{}, err
	}
	info, err := guest.Probe(ctx)
	if err != nil && (method == probeConnection || form.Provider != media.Flymoo) {
		return candidateSource{}, err
	}
	result := candidateSource{source: form, info: info}
	if method == probeConnection {
		return result, nil
	}
	var session media.Session
	if method == quickConnection {
		session, err = awaitQuickConnect(ctx, guest, showCode)
	} else {
		session, err = guest.SignIn(ctx, input)
	}
	if err != nil {
		return candidateSource{}, err
	}
	client, err := a.newMedia(form.Provider, form.Address, a.device, session)
	if err != nil {
		return candidateSource{}, err
	}
	libraries, err := client.Libraries(ctx)
	if err != nil {
		return candidateSource{}, err
	}
	if form.Name == "" {
		form.Name = info.Name
		if form.Name == "" {
			form.Name = form.Address
		}
	}
	form.AccountID, form.ServerID = session.UserID, info.ID
	return candidateSource{source: form, info: info, session: session, libraries: libraries}, nil
}

func awaitQuickConnect(ctx context.Context, guest media.Client, showCode func(string)) (media.Session, error) {
	quick, ok := guest.(media.QuickConnectClient)
	if !ok {
		return media.Session{}, media.ErrQuickConnectUnavailable
	}
	code, err := quick.BeginQuickConnect(ctx)
	if err != nil {
		return media.Session{}, err
	}
	showCode(code.Code)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		session, done, err := quick.CheckQuickConnect(ctx, code)
		if err != nil || done {
			return session, err
		}
		select {
		case <-ctx.Done():
			return media.Session{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *application) importSource(cx *core.Scope, candidate candidateSource, back func()) core.Node {
	previous := a.matchSource(candidate.source)
	var saved []sourceLibrary
	if previous.ID != "" && previous.Provider == candidate.source.Provider &&
		previous.ServerID == candidate.source.ServerID &&
		(previous.ServerID != "" || previous.Address == candidate.source.Address) {
		saved = previous.Libraries
	}
	libraries := mergeLibraries(candidate.libraries, saved, false)
	draft := core.NewSignalFunc(cx, libraries, nil)
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal[error](cx, nil)
	viewContext := cx.Context()
	list := []core.Node{heading(locales.ImportLibraries(cx))}
	if len(libraries) == 0 {
		list = append(list, hint(locales.NoLibraries(cx)))
	}
	list = append(list, libraryEditor(cx, draft, true, busy.Get))
	store := func() {
		if busy.GetUntracked() {
			return
		}
		form := candidate.source
		form.Libraries = draft.GetUntracked()
		previous := a.matchSource(form)
		var previousKey string
		if previous.ID != "" {
			form.ID = previous.ID
			previousKey = previous.credentialKey()
		}
		if form.ID == "" {
			form.ID = rand.Text()
		}
		// Stage replacements under a fresh key so the saved source keeps its old
		// credential if the view closes or saving the source settings fails.
		if previousKey != "" {
			form.CredentialKey = rand.Text()
		}
		key, token := form.credentialKey(), candidate.session.Token
		busy.Set(true)
		go func() {
			var err error
			if token != "" {
				err = a.secrets.Set(key, token)
			}
			form.Volatile = errors.Is(err, core.ErrSecretsUnavailable)
			discard := func() {
				if token != "" && err == nil {
					go func() { _ = a.secrets.Delete(key) }()
				}
			}
			a.scope.Post(func() {
				if viewContext.Err() != nil {
					discard()
					return
				}
				busy.Set(false)
				if err != nil && !form.Volatile {
					failure.Set(err)
					return
				}
				others := slices.Clone(a.sources.GetUntracked())
				others = slices.DeleteFunc(others, func(entry source) bool { return entry.ID == form.ID })
				if err := a.saveSources(append(others, form)); err != nil {
					discard()
					failure.Set(err)
					return
				}
				a.tokens.Store(key, token)
				if previousKey != "" {
					a.tokens.Delete(previousKey)
					go func() { _ = a.secrets.Delete(previousKey) }()
				}
				a.sourceFilter.Set("")
				a.enter()
			})
		}()
	}
	list = append(list, core.Dynamic(func(cx *core.Scope) core.Node {
		if err := failure.Get(); err != nil {
			return notice(errorText(cx, err))
		}
		return nil
	}), core.View{Class: "flex-row flex-wrap gap-3 pt-2", Children: []core.Node{
		ui.Button{Text: locales.Back(cx), Variant: ui.ButtonGhost, Disabled: busy.Get, TestID: "import-back", OnPress: back},
		ui.Button{Text: locales.ImportAction(cx), Busy: busy.Get, Size: ui.SizeLarge, TestID: "import-libraries",
			Disabled: func() bool {
				return !slices.ContainsFunc(draft.Get(), func(library sourceLibrary) bool { return !library.Hidden })
			}, OnPress: store},
	}})
	return core.View{Class: "gap-4 rounded-3xl border border-border bg-card p-5", Children: list}
}

func (a *application) matchSource(form source) source {
	for _, existing := range a.sources.GetUntracked() {
		if form.ID != "" {
			if existing.ID == form.ID {
				return existing
			}
			continue
		}
		if existing.Provider == form.Provider && existing.Address == form.Address &&
			existing.Username == form.Username && existing.PlexLinked == form.PlexLinked {
			return existing
		}
	}
	return source{}
}
