package main

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

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
	provider := core.NewSignal(cx, current.provider)
	tabs := make([]core.Node, 0, len(sourceProviders))
	for _, option := range sourceProviders {
		name := providerName(cx, option.provider)
		selected := func() bool { return provider.Get() == option.provider }
		tabs = append(tabs, base.Button{Label: name, Role: core.RoleTab, Selected: selected, TestID: "connect-" + string(option.provider),
			Class:    "group flex-1 items-center justify-center rounded-lg py-2 selected:bg-white selected:shadow-sm dark:selected:bg-[#1a1b25]",
			Style:    core.Style{FocusVisible: ui.FocusRing(cx, -2)},
			Disabled: func() bool { return current.source.ID != "" && option.provider != current.provider },
			OnPress:  func(core.PressEvent) { provider.Set(option.provider) },
			Children: []core.Node{core.Text{Content: name, Class: "text-sm group-selected:font-semibold group-selected:text-zinc-900 dark:group-selected:text-zinc-50 " + setupMuted}},
		})
	}
	return setupPage(cx, locales.ConnectServer(cx), a.back, core.View{Class: "w-full max-w-[640px] self-center gap-6 pt-3 pb-10", Children: []core.Node{
		core.View{Role: core.RoleTabList, TestID: "connection-providers", Class: "mx-4 flex-row rounded-xl bg-zinc-900/5 p-1 dark:bg-zinc-50/5", Children: tabs},
		core.Dynamic(func(cx *core.Scope) core.Node {
			kind := provider.Get()
			if kind == current.provider {
				return a.connectionForm(cx, kind, current.source)
			}
			return a.connectionForm(cx, kind, source{})
		}),
	}})
}

func (a *application) connectionForm(cx *core.Scope, provider media.Provider, current source) core.Node {
	host, servicePort, useHTTPS := splitServerAddress(current.Address)
	address := core.NewSignal(cx, host)
	port := core.NewSignal(cx, servicePort)
	https := core.NewSignal(cx, useHTTPS)
	name := core.NewSignal(cx, current.Name)
	user := core.NewSignal(cx, current.Username)
	password := core.NewSignal(cx, "")
	showPassword := core.NewSignal(cx, false)
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal[error](cx, nil)
	preview := core.NewSignal[*media.ServerInfo](cx, nil)
	candidate := core.NewSignal[*candidateSource](cx, nil)
	quickCode := core.NewSignal(cx, "")
	viewContext := cx.Context()
	plex := provider == media.Plex

	clearResult := func() {
		failure.Set(nil)
		preview.Set(nil)
	}
	submit := func(method connectionMethod) {
		if busy.GetUntracked() || candidate.GetUntracked() != nil {
			return
		}
		base, err := serverAddress(address.GetUntracked(), port.GetUntracked(), https.GetUntracked())
		if err != nil {
			failure.Set(err)
			return
		}
		form := source{ID: current.ID, Address: base.String(), Provider: provider,
			Name: strings.TrimSpace(name.GetUntracked()), Username: strings.TrimSpace(user.GetUntracked())}
		input := media.Credentials{Username: form.Username, Password: password.GetUntracked()}
		if plex {
			input.Token, input.Password = input.Password, ""
		}
		timeout := time.Duration(a.sourceTimeout.GetUntracked()) * time.Second
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
				cx.Batch(func() {
					busy.Set(false)
					quickCode.Set("")
					failure.Set(err)
					if err == nil {
						preview.Set(&result.info)
						if method != probeConnection {
							password.Set("")
							showPassword.Set(false)
							candidate.Set(&result)
						}
					}
				})
			})
		}()
	}
	disabled := func() bool { return busy.Get() || candidate.Get() != nil }
	httpsLabel := locales.UseHTTPS(cx)
	updateAddress := func(value string) {
		address.Set(value)
		clearResult()
	}
	setEndpoint := func(raw string) {
		host, servicePort, useHTTPS := splitServerAddress(raw)
		cx.Batch(func() {
			updateAddress(host)
			port.Set(servicePort)
			https.Set(useHTTPS)
		})
	}
	var scan core.Node
	if cx.Env().CanScanCode() {
		label := locales.ScanServerAddress(cx)
		scan = setupIconButton(cx, label, "scan-server-address", lucide.ScanLine(), disabled, func() {
			cx.ScanCode(core.ScanOptions{}, func(code *core.ScannedCode, err error) {
				if err != nil {
					failure.Set(err)
					return
				}
				if code == nil || disabled() {
					return
				}
				base, err := media.NormalizeAddress(code.Text)
				if err != nil {
					failure.Set(err)
					return
				}
				setEndpoint(base.String())
			})
		})
	}
	entry := func(id, label, example string, signal *core.Signal[string], keyboard core.KeyboardType, autocomplete core.AutoComplete) core.Input {
		return core.Input{Label: label, TestID: id, Placeholder: example, KeyboardType: keyboard, AutoComplete: autocomplete,
			Class: "h-10 flex-1 px-0 text-[15px] text-zinc-900/80 dark:text-zinc-50/80",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, -1)}, Value: signal.Get, OnChange: signal.Set, NoAutoCorrect: true, Disabled: disabled}
	}
	addressEntry := entry("server-address", locales.ServerAddress(cx), "home.example.com", address, core.KeyboardURL, 0)
	addressEntry.OnChange = updateAddress
	portEntry := entry("server-port", locales.ServerPort(cx), "", port, core.KeyboardNumber, 0)
	portEntry.OnChange = func(value string) { port.Set(value); clearResult() }
	portEntry.PlaceholderFunc = func() string {
		if https.Get() {
			return "443"
		}
		return "80"
	}
	server := setupGroup(locales.ServerGroup(cx), "",
		setupInputRow(locales.ServerAddressShort(cx), addressEntry, scan),
		setupInputRow(locales.ServerPort(cx), portEntry, nil),
		base.Switch{Label: httpsLabel, TestID: "server-https", Checked: https.Get, Disabled: disabled,
			Class: setupRow + " flex-row-reverse justify-between", Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
			TrackClass: "h-[31px] w-[51px] flex-row items-center justify-start rounded-full bg-zinc-900/15 px-0.5 checked:justify-end checked:bg-violet-500 dark:bg-white/15 dark:checked:bg-violet-500",
			ThumbClass: "size-[27px] rounded-full bg-white shadow-md", Children: []core.Node{core.Text{Content: httpsLabel, Class: "text-[15px] " + setupTitle}},
			OnChange: func(enabled bool) {
				if raw := address.GetUntracked(); strings.Contains(raw, "://") {
					host, explicitPort, _ := splitServerAddress(raw)
					address.Set(host)
					port.Set(explicitPort)
				}
				https.Set(enabled)
				clearResult()
			},
		},
		setupInputRow(locales.SourceNameShort(cx), entry("server-name", locales.SourceName(cx), providerName(cx, provider), name, 0, 0), nil),
	)
	passwordLabel := locales.Password(cx)
	passwordTitle := passwordLabel
	if plex {
		passwordLabel = locales.PlexToken(cx)
		passwordTitle = locales.PlexTokenShort(cx)
	}
	passwordEntry := entry("server-password", passwordLabel, "", password, 0, core.AutoCompletePassword)
	passwordRow := core.Dynamic(func(cx *core.Scope) core.Node {
		input := passwordEntry
		input.Masked = !showPassword.Get()
		label, icon := locales.ShowPassword(cx), lucide.EyeOff()
		if !input.Masked {
			label, icon = locales.HidePassword(cx), lucide.Eye()
		}
		return setupInputRow(passwordTitle, input, setupIconButton(cx, label, "show-server-password", icon, disabled,
			func() { showPassword.Set(!showPassword.GetUntracked()) }))
	})
	loginRows := []core.Node{passwordRow}
	var footer string
	if !plex {
		loginRows = append([]core.Node{setupInputRow(locales.Username(cx), entry("server-username", locales.Username(cx), "", user, 0, core.AutoCompleteUsername), nil)}, loginRows...)
	}
	if provider == media.Jellyfin {
		footer = locales.QuickConnectHelp(cx)
		loginRows = append(loginRows, setupLoginOption(cx, "quick-connect", locales.QuickConnect(cx), "Quick Connect", lucide.KeyRound(), disabled,
			func() { submit(quickConnection) }))
	}
	if plex {
		loginRows = append(loginRows, setupLoginOption(cx, "link-plex-account", locales.LinkPlexAccount(cx), "", lucide.Link(), disabled, func() {
			a.push(route{screen: screenPlexLink, source: current})
		}))
	}
	return core.View{Class: "gap-6", Children: []core.Node{
		server,
		setupGroup(locales.LoginGroup(cx), footer, loginRows...),
		core.Dynamic(func(cx *core.Scope) core.Node {
			if err := failure.Get(); err != nil {
				return core.View{Class: "mx-4", Children: []core.Node{notice(errorText(cx, err))}}
			}
			if code := quickCode.Get(); code != "" {
				return core.View{Class: "mx-4", Children: []core.Node{notice(locales.QuickConnectInstruction(cx, code))}}
			}
			if info := preview.Get(); info != nil {
				name := info.Name
				if name == "" {
					name = providerName(cx, provider)
				}
				label := locales.Connected(cx, strings.TrimSpace(name+" "+info.Version))
				detail := locales.ConnectionVerified(cx)
				if result := candidate.Get(); result != nil && result.source.Username != "" {
					detail = locales.SignedInAs(cx, result.source.Username)
				}
				return core.View{TestID: "connection-verified", Class: "mx-4 flex-row items-center gap-3 rounded-2xl border border-emerald-500/30 bg-emerald-500/10 p-4", Children: []core.Node{
					core.Icon{SVG: lucide.CircleCheck(), Class: "size-5 text-emerald-500"},
					core.View{Class: "flex-1", Children: []core.Node{
						core.Text{Content: label, Class: "text-sm font-medium " + setupTitle},
						core.Text{Content: detail, Class: "text-xs " + setupMuted},
					}},
				}}
			}
			return nil
		}),
		core.Dynamic(func(cx *core.Scope) core.Node {
			if result := candidate.Get(); result != nil {
				return a.importSource(cx, *result, func() { candidate.Set(nil) })
			}
			return core.View{Class: "mx-4 gap-2", Children: []core.Node{
				ui.Button{Text: locales.TestConnection(cx), Variant: ui.ButtonGhost, Disabled: busy.Get,
					TestID: "test-connection", OnPress: func() { submit(probeConnection) }},
				setupPrimary(cx, "sign-in", locales.SignIn(cx), busy.Get, nil, func() { submit(passwordConnection) }),
			}}
		}),
	}}
}

func splitServerAddress(raw string) (host, port string, https bool) {
	address, err := media.NormalizeAddress(raw)
	if err != nil {
		return raw, "", false
	}
	host = address.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + address.EscapedPath(), address.Port(), address.Scheme == "https"
}

func serverAddress(host, port string, https bool) (*url.URL, error) {
	address, err := media.NormalizeAddress(host)
	if err != nil {
		return nil, err
	}
	if https && !strings.Contains(host, "://") {
		address.Scheme = "https"
	}
	if address.Port() != "" {
		port = address.Port()
	}
	if port = strings.TrimSpace(port); port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return nil, media.ErrInvalidAddress
		}
		address.Host = net.JoinHostPort(address.Hostname(), port)
	}
	return address, nil
}

func setupInputRow(label string, input, action core.Node) core.Node {
	return core.View{Class: setupRow, Children: []core.Node{
		core.Text{Content: label, Class: "w-16 text-[15px] " + setupTitle}, input, action,
	}}
}

func setupIconButton(cx *core.Scope, label, id string, icon *core.SVG, disabled func() bool, press func()) core.Node {
	return base.Button{Label: label, TestID: id, Class: "size-8 items-center justify-center rounded-md",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, Disabled: disabled, OnPress: func(core.PressEvent) { press() },
		Children: []core.Node{core.Icon{SVG: icon, Class: "size-4 " + setupMuted}}}
}

func setupLoginOption(cx *core.Scope, id, label, value string, icon *core.SVG, disabled func() bool, press func()) core.Node {
	return base.Button{Label: label, TestID: id, Class: setupRow + " py-2.5", Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
		Disabled: disabled, OnPress: func(core.PressEvent) { press() }, Children: []core.Node{
			setupBadge(icon, "bg-slate-500"), core.Text{Content: label, Class: "flex-1 text-[15px] " + setupTitle},
			core.Text{Content: value, Class: "text-[15px] " + setupMuted},
			core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 text-zinc-500/60 dark:text-zinc-400/60"},
		}}
}

func setupPrimary(cx *core.Scope, id, label string, busy, disabled func() bool, press func()) core.Node {
	return base.Button{Label: label, TestID: id,
		Class: "h-[52px] items-center justify-center rounded-2xl bg-linear-to-r from-violet-500 to-fuchsia-500 shadow-lg shadow-violet-500/30 disabled:opacity-60",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 3)}, Busy: busy,
		Disabled: func() bool { return busy() || (disabled != nil && disabled()) }, OnPress: func(core.PressEvent) { press() },
		Children: []core.Node{core.Text{ContentFunc: func() string {
			if busy() {
				return locales.Loading(cx)
			}
			return label
		}, Class: "text-base font-semibold text-white"}}}
}
