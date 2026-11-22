package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/internal/mediatls"
	"waygu-video/internal/tvpair"
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
	endpoint  sourceEndpoint
	latency   time.Duration
}

func (a *application) connect(cx *core.Scope, current route) core.Node {
	provider := core.NewSignal(cx, current.provider)
	candidate := core.NewSignal[*candidateSource](cx, nil)
	saving := core.NewSignal(cx, false)
	fnConnect := core.NewSignal(cx, false)
	back := func() {
		if saving.GetUntracked() {
			return
		}
		if candidate.GetUntracked() != nil {
			candidate.Set(nil)
			return
		}
		a.back()
	}
	cx.OnBackPress(func() bool {
		if candidate.GetUntracked() == nil && !saving.GetUntracked() {
			return false
		}
		back()
		return true
	})
	tabs := make([]core.Node, 0, len(sourceProviders))
	for _, option := range sourceProviders {
		name := providerName(cx, option.provider)
		selected := func() bool { return provider.Get() == option.provider }
		tabs = append(tabs, base.Button{Label: name, Role: core.RoleTab, Selected: selected, TestID: "connect-" + string(option.provider),
			Class: "group flex-1 items-center justify-center rounded-lg py-2 selected:bg-white selected:shadow-sm dark:selected:bg-[#1a1b25]",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
			Disabled: func() bool {
				return saving.Get() || fnConnect.Get() || (current.source.ID != "" && option.provider != current.provider)
			},
			OnPress: func(core.PressEvent) {
				if provider.GetUntracked() != option.provider {
					cx.Batch(func() {
						candidate.Set(nil)
						provider.Set(option.provider)
					})
				}
			},
			Children: []core.Node{core.Text{Content: name, Class: "text-sm group-selected:font-semibold group-selected:text-zinc-900 dark:group-selected:text-zinc-50 " + setupMuted}},
		})
	}
	return setupPage(cx, locales.ConnectServer(cx), back, core.View{Class: "w-full max-w-[640px] self-center gap-6 pt-3 pb-10", Children: []core.Node{
		core.View{Role: core.RoleTabList, TestID: "connection-providers", Class: "mx-4 flex-row rounded-xl bg-zinc-900/5 p-1 dark:bg-zinc-50/5", Children: tabs},
		core.Dynamic(func(cx *core.Scope) core.Node {
			kind := provider.Get()
			if kind == current.provider {
				return a.connectionForm(cx, kind, current.source, candidate, saving, fnConnect)
			}
			return a.connectionForm(cx, kind, source{}, candidate, saving, fnConnect)
		}),
	}})
}

func (a *application) connectionForm(cx *core.Scope, provider media.Provider, current source, candidate *core.Signal[*candidateSource], saving, fnConnect *core.Signal[bool]) core.Node {
	host, servicePort, useHTTPS := sourceAddressFields(provider, current.Address)
	address := core.NewSignal(cx, host)
	remote := core.NewSignal(cx, current.RemoteAddress)
	port := core.NewSignal(cx, servicePort)
	https := core.NewSignal(cx, useHTTPS)
	name := core.NewSignal(cx, current.Name)
	user := core.NewSignal(cx, current.Username)
	password := core.NewSignal(cx, "")
	showPassword := core.NewSignal(cx, false)
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal[error](cx, nil)
	preview := core.NewSignal[*candidateSource](cx, nil)
	type fnSelection struct{ id, address, server string }
	resolvedFN := core.NewSignal(cx, fnSelection{})
	quickCode := core.NewSignal(cx, "")
	approved := core.NewSignalFunc(cx, slices.Clone(current.Certificates), slices.Equal)
	type certificateOffer struct {
		pin   mediatls.Trust
		retry func()
	}
	offer := core.NewSignal[*certificateOffer](cx, nil)
	canTrust := runtime.GOOS != "js"
	showError := func(err error, retry func()) {
		failure.Set(err)
		var verification *sourceCertificateError
		if canTrust && errors.As(err, &verification) {
			if pin, ok := mediatls.Candidate(verification.address, err); ok {
				offer.Set(&certificateOffer{pin, retry})
			}
		}
	}
	viewContext := cx.Context()
	headers := core.NewSignalFunc[endpointHeaders](cx, endpointHeaders{}, nil)
	storedHeaders := core.NewSignalFunc[endpointHeaders](cx, endpointHeaders{}, nil)
	storedHeadersReady := core.NewSignal(cx, current.HeaderSecret == "")
	editHeaders := core.NewSignal[*endpointHeaders](cx, nil)
	savedOrigin := sourceOrigin(current.Address)
	if canTrust && current.HeaderSecret != "" {
		go func() {
			values, err := a.requestHeaders(current, current.Address)
			cx.Post(func() {
				cx.Batch(func() {
					if err == nil {
						storedHeaders.Set(endpointHeaders{savedOrigin, values})
					}
					storedHeadersReady.Set(true)
				})
			})
		}()
	}
	plex := provider == media.Plex

	clearResult := func() {
		failure.Set(nil)
		preview.Set(nil)
	}
	updateAddress := func(value string) {
		address.Set(value)
		clearResult()
	}
	setEndpoint := func(raw string) {
		host, servicePort, useHTTPS := sourceAddressFields(provider, raw)
		cx.Batch(func() {
			updateAddress(host)
			port.Set(servicePort)
			https.Set(useHTTPS)
		})
	}
	approvedForForm := func(primary, backup string) []mediatls.Trust {
		pins := approved.GetUntracked()
		if provider == media.Flymoo {
			// A public probe may need a certificate approved at the redirected
			// port. The saved source retains only its final origin and backup.
			return pins
		}
		return routeCertificates(pins, primary, backup)
	}
	var submit func(connectionMethod)
	submit = func(method connectionMethod) {
		pending := candidate.GetUntracked()
		if busy.GetUntracked() || saving.GetUntracked() || fnConnect.GetUntracked() || (pending != nil && method != probeConnection) {
			return
		}
		base, err := sourceAddress(provider, address.GetUntracked(), port.GetUntracked(), https.GetUntracked())
		if err != nil {
			failure.Set(err)
			return
		}
		alternate, err := backupAddress(remote.GetUntracked(), base.String())
		if err != nil {
			failure.Set(err)
			return
		}
		form := source{ID: current.ID, Address: base.String(), Provider: provider, RemoteAddress: alternate,
			RemoteDisabled: current.RemoteDisabled, RemoteBitrate: current.RemoteBitrate,
			Name: strings.TrimSpace(name.GetUntracked()), Username: strings.TrimSpace(user.GetUntracked())}
		form.Certificates = approvedForForm(form.Address, form.RemoteAddress)
		if origin := sourceOrigin(form.Address); origin == savedOrigin {
			form.HeaderSecret = current.HeaderSecret
		}
		if draft := headers.GetUntracked(); draft.origin == sourceOrigin(form.Address) {
			form.headerDraft = draft.values
		}
		if pending != nil {
			form = pending.source
			form.Certificates = approvedForForm(form.Address, form.RemoteAddress)
		}
		if form.Address == current.Address {
			form.ServerID, form.FNID = current.ServerID, current.FNID
		}
		if resolved := resolvedFN.GetUntracked(); form.Address == resolved.address {
			form.FNID = resolved.id
			if resolved.id == current.FNID && current.ServerID != "" {
				form.ServerID = current.ServerID
			} else if form.ServerID == "" {
				form.ServerID = resolved.server
			}
		}
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
			if err == nil && pending != nil {
				if pending.info.ID != "" && result.info.ID != pending.info.ID {
					err = errDifferentServer
				} else if provider == media.Flymoo && pending.endpoint != result.endpoint {
					err = media.ErrFlymooGateway
				}
			}
			if method == probeConnection && errors.Is(err, media.ErrAuthentication) {
				err = errSourceProbeDenied
			}
			cx.Post(func() {
				if viewContext.Err() != nil {
					return
				}
				cx.Batch(func() {
					busy.Set(false)
					quickCode.Set("")
					showError(err, func() { submit(probeConnection) })
					if err == nil {
						if provider == media.Flymoo && pending == nil && form.Address != result.source.Address {
							setEndpoint(result.source.Address)
						}
						if provider == media.Flymoo && pending == nil && form.RemoteAddress != result.source.RemoteAddress {
							remote.Set(result.source.RemoteAddress)
						}
						preview.Set(&candidateSource{info: result.info, endpoint: result.endpoint, latency: result.latency})
						if pending != nil {
							pending.source.Certificates = result.source.Certificates
						}
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
	disabled := func() bool {
		return busy.Get() || candidate.Get() != nil || editHeaders.Get() != nil || fnConnect.Get()
	}
	httpsLabel := locales.UseHTTPS(cx)
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
				if _, err := tvpair.Parse(code.Text); err == nil {
					a.push(route{screen: screenTVPairPhone, pairLink: code.Text})
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
		if provider == media.Flymoo {
			if strings.TrimSpace(address.Get()) == "" {
				return flymooPort(https.Get())
			}
			if endpoint, err := sourceAddress(provider, address.Get(), "", https.Get()); err == nil {
				if explicit := endpoint.Port(); explicit != "" {
					return explicit
				}
				if endpoint.Scheme == "https" {
					return "443"
				}
				return "80"
			}
		}
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
	remoteEntry := entry("server-remote-address", locales.BackupServerAddress(cx), locales.BackupAddressUnset(cx),
		remote, core.KeyboardURL, 0)
	remoteEntry.Class += " text-right"
	remoteEntry.OnChange = func(value string) { remote.Set(value); clearResult() }
	approvedOnForm := func() bool {
		base, _ := sourceAddress(provider, address.Get(), port.Get(), https.Get())
		local := ""
		if base != nil {
			local = base.String()
		}
		backup, _ := backupAddress(remote.Get(), local)
		return len(routeCertificates(approved.Get(), local, backup)) != 0
	}
	var trustChange func(bool)
	if canTrust {
		trustChange = func(enabled bool) {
			if enabled {
				submit(probeConnection)
			} else {
				approved.Set(nil)
				clearResult()
			}
		}
	}
	trustLabel := locales.TrustSourceCertificate(cx)
	trustRow := base.Switch{Label: trustLabel, TestID: "source-trust-certificate", Checked: approvedOnForm, OnChange: trustChange,
		Disabled: func() bool { return !canTrust || disabled() },
		Class:    setupRow + " flex-row-reverse justify-between", Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
		TrackClass: "h-[31px] w-[51px] flex-row items-center justify-start rounded-full bg-zinc-900/15 px-0.5 checked:justify-end checked:bg-violet-500 dark:bg-white/15 dark:checked:bg-violet-500",
		ThumbClass: "size-[27px] rounded-full bg-white shadow-md", Children: []core.Node{core.Text{Content: trustLabel, Class: "text-[15px] " + setupTitle}},
	}
	formOrigin := func() string {
		base, err := sourceAddress(provider, address.Get(), port.Get(), https.Get())
		if err != nil {
			return ""
		}
		return sourceOrigin(base.String())
	}
	headerDraft := func() endpointHeaders {
		origin := formOrigin()
		if draft := headers.Get(); draft.origin == origin {
			return draft
		}
		if stored := storedHeaders.Get(); stored.origin == origin {
			return stored
		}
		return endpointHeaders{origin: origin}
	}
	label := locales.SourceRequestHeaders(cx)
	headerTrigger := &core.Ref{}
	headersRow := base.Button{Label: label, Ref: headerTrigger, TestID: "source-request-headers", Class: setupRow + " py-2.5",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
		Disabled: func() bool {
			origin := formOrigin()
			return !canTrust || disabled() || origin == "" || (origin == savedOrigin && !storedHeadersReady.Get())
		},
		OnPress: func(core.PressEvent) {
			draft := core.Untracked(cx, headerDraft)
			editHeaders.Set(&draft)
		},
		Children: []core.Node{
			core.Text{Content: label, Class: "flex-1 text-[15px] " + setupTitle},
			core.Text{ContentFunc: func() string {
				n := len(headerDraft().values)
				if n == 0 {
					return locales.BackupAddressUnset(cx)
				}
				return locales.SourceHeaderCount(cx, strconv.Itoa(n))
			}, Class: "text-[15px] " + setupMuted},
			core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 text-zinc-500/60 dark:text-zinc-400/60"},
		},
	}
	requestHeadersEditor := core.Dynamic(func(cx *core.Scope) core.Node {
		if draft := editHeaders.Get(); draft != nil {
			return headerEditor(cx, *draft, func() {
				editHeaders.Set(nil)
				if appLayoutIdiom(cx) == core.IdiomTV {
					a.scope.Post(headerTrigger.Focus)
				}
			}, func(changes endpointHeaders) {
				headers.Set(changes)
				clearResult()
			})
		}
		return nil
	})
	advancedRows := []core.Node{headersRow, trustRow, core.View{Class: setupRow, Children: []core.Node{
		core.Text{Content: locales.BackupServerAddress(cx), Class: "w-32 shrink-0 text-[15px] " + setupTitle}, remoteEntry,
	}}}
	if current.ID != "" {
		var saveHeaders func()
		saveHeaders = func() {
			draft := current
			draft.headerDraft = headers.GetUntracked().values
			draft.Certificates = routeCertificates(approved.GetUntracked(), current.Address, current.RemoteAddress)
			busy.Set(true)
			failure.Set(nil)
			a.saveExistingSourceHeaders(viewContext, current, draft, func(err error) {
				busy.Set(false)
				if err != nil {
					showError(err, saveHeaders)
					return
				}
				a.back()
			})
		}
		showHeaderSave := func() bool { return headers.Get().origin == savedOrigin && formOrigin() == savedOrigin }
		advancedRows = append(advancedRows, core.Show{When: showHeaderSave, Then: func(cx *core.Scope) core.Node {
			return core.View{Class: setupRow, Children: []core.Node{
				ui.Button{Text: locales.SaveSourceHeaders(cx), Variant: ui.ButtonGhost, TestID: "save-source-headers",
					Disabled: disabled, OnPress: saveHeaders},
			}}
		}})
		var saveRemote func()
		saveRemote = func() {
			alternate, err := backupAddress(remote.GetUntracked(), current.Address)
			if err != nil {
				failure.Set(err)
				return
			}
			clearResult()
			draft := current
			draft.RemoteAddress = alternate
			draft.Certificates = approvedForForm(draft.Address, alternate)
			save := func() error {
				return a.updateSourceRouting(current, func(saved *source) {
					saved.RemoteAddress = draft.RemoteAddress
					saved.Certificates = draft.Certificates
				})
			}
			if alternate == "" {
				draft.Certificates = routeCertificates(draft.Certificates, draft.Address)
				if err := save(); err != nil {
					failure.Set(err)
				} else {
					a.back()
				}
				return
			}
			busy.Set(true)
			go func() {
				verified, err := a.verifyConnectionAddress(viewContext, draft, alternate, current.ServerID, 4*time.Second)
				if err == nil {
					draft.RemoteAddress = verified.address
					if draft.RemoteAddress == draft.Address {
						err = media.ErrInvalidAddress
					}
					draft.Certificates = routeCertificates(draft.Certificates, draft.Address, draft.RemoteAddress)
				}
				cx.Post(func() {
					busy.Set(false)
					if err != nil {
						showError(err, saveRemote)
						return
					}
					if err := save(); err != nil {
						failure.Set(err)
						return
					}
					a.back()
				})
			}()
		}
		advancedRows = append(advancedRows, core.View{Class: setupRow, Children: []core.Node{
			ui.Button{Text: locales.SaveBackupAddress(cx), Variant: ui.ButtonGhost, TestID: "save-backup-address",
				Disabled: func() bool { return disabled() || strings.TrimSpace(remote.Get()) == current.RemoteAddress }, OnPress: saveRemote},
		}})
		savedPins := func() []mediatls.Trust {
			return routeCertificates(approved.Get(), current.Address, current.RemoteAddress)
		}
		advancedRows = append(advancedRows, core.Show{When: func() bool { return !slices.Equal(savedPins(), current.Certificates) }, Then: func(cx *core.Scope) core.Node {
			return core.View{Class: setupRow, Children: []core.Node{
				ui.Button{Text: locales.SaveSourceCertificates(cx), Variant: ui.ButtonGhost, TestID: "save-source-certificates", Disabled: disabled,
					OnPress: func() {
						pins := core.Untracked(cx, savedPins)
						if err := a.updateSourceRouting(current, func(saved *source) { saved.Certificates = pins }); err != nil {
							failure.Set(err)
							return
						}
						a.back()
					}},
			}}
		}})
	}
	review := core.Dynamic(func(cx *core.Scope) core.Node {
		current := offer.Get()
		if current == nil {
			return nil
		}
		fingerprint := current.pin.Fingerprint()
		var fields []string
		for offset := 0; offset < len(fingerprint); offset += 16 {
			fields = append(fields, fingerprint[offset:min(len(fingerprint), offset+16)])
		}
		return ui.AlertDialog{Open: func() bool { return offer.Get() != nil }, Title: locales.TrustSourceCertificate(cx),
			Description: locales.SourceCertificateDetails(cx, current.pin.Origin, strings.Join(fields, " ")),
			OnOpenChange: func(open bool, _ base.Reason) {
				if !open {
					offer.Set(nil)
				}
			},
			Actions: []ui.AlertAction{
				{Text: locales.DesktopSettingsCancel(cx), Role: core.AlertCancel, TestID: "cancel-source-certificate"},
				{Text: locales.ApproveSourceCertificate(cx), TestID: "approve-source-certificate", OnPress: func() {
					approved.Update(func(pins []mediatls.Trust) []mediatls.Trust {
						other := slices.DeleteFunc(slices.Clone(pins), func(pin mediatls.Trust) bool {
							return strings.EqualFold(pin.Origin, current.pin.Origin)
						})
						return append(other, current.pin)
					})
					offer.Set(nil)
					if current.retry != nil {
						current.retry()
					}
				}},
			},
		}
	})
	var intro core.Node
	if provider == media.Flymoo {
		trigger := &core.Ref{}
		closeFNConnect := func() {
			fnConnect.Set(false)
			if appLayoutIdiom(cx) == core.IdiomTV {
				cx.Post(trigger.Focus)
			}
		}
		intro = core.Group{
			flymooConnectionIntro(cx, trigger, disabled, func() { fnConnect.Set(true) }),
			core.Show{When: fnConnect.Get, Then: func(cx *core.Scope) core.Node {
				id := resolvedFN.GetUntracked().id
				if id == "" {
					id = current.FNID
				}
				return flymooFNConnectDialog(cx, id, closeFNConnect, func(id string, result media.FNConnectResult) {
					setEndpoint(result.Address)
					resolvedFN.Set(fnSelection{id, result.Address, result.ServerID})
					if result.BackupAddress != "" && remote.GetUntracked() == "" {
						remote.Set(result.BackupAddress)
					}
					if name.GetUntracked() == "" && result.Name != "" {
						name.Set(result.Name)
					}
					if result.Cookie != "" {
						draft := core.Untracked(cx, headerDraft)
						values := draft.values.Clone()
						if values == nil {
							values = make(http.Header)
						}
						values.Set("Cookie", result.Cookie)
						headers.Set(endpointHeaders{sourceOrigin(result.Address), values})
					}
				})
			}},
		}
	}
	return core.View{Class: "gap-6", Children: []core.Node{
		intro,
		server,
		review,
		requestHeadersEditor,
		setupGroup(locales.LoginGroup(cx), footer, loginRows...),
		setupGroup(locales.SourceAdvanced(cx), "", advancedRows...),
		core.Dynamic(func(cx *core.Scope) core.Node {
			receipt := preview.Get()
			if err := failure.Get(); err != nil {
				nodes := []core.Node{notice(errorText(cx, err))}
				if receipt != nil {
					nodes = append(nodes, sourceRetest(cx, busy.Get, func() bool { return saving.Get() || fnConnect.Get() }, func() { submit(probeConnection) }))
				}
				return core.View{Class: "mx-4 gap-2", Children: nodes}
			}
			if code := quickCode.Get(); code != "" {
				return core.View{Class: "mx-4", Children: []core.Node{notice(locales.QuickConnectInstruction(cx, code))}}
			}
			if receipt != nil {
				user := ""
				if result := candidate.Get(); result != nil {
					user = result.source.Username
				}
				return sourceConnectionReceipt(cx, provider, *receipt, user, busy.Get, func() bool { return saving.Get() || fnConnect.Get() }, func() { submit(probeConnection) })
			}
			return nil
		}),
		core.Dynamic(func(cx *core.Scope) core.Node {
			if result := candidate.Get(); result != nil {
				return a.importSource(cx, func() candidateSource { return *result }, nil, func() bool { return busy.Get() || failure.Get() != nil }, saving)
			}
			return core.View{Class: "mx-4 gap-2", Children: []core.Node{
				core.Show{When: func() bool { return preview.Get() == nil }, Then: func(cx *core.Scope) core.Node {
					return ui.Button{Text: locales.TestConnection(cx), Variant: ui.ButtonGhost, Disabled: disabled,
						TestID: "test-connection", OnPress: func() { submit(probeConnection) }}
				}},
				setupPrimary(cx, "sign-in", locales.SignIn(cx), busy.Get, disabled, func() { submit(passwordConnection) }),
			}}
		}),
	}}
}

func sourceRetest(cx *core.Scope, busy, saving func() bool, run func()) core.Node {
	label := func() string {
		if busy() {
			return locales.ConnectionTesting(cx)
		}
		return locales.ConnectionRetest(cx)
	}
	return base.Button{LabelFunc: label, TestID: "retest-connection", Disabled: func() bool { return busy() || saving() },
		Class: "shrink-0 rounded", Style: core.Style{FocusVisible: ui.FocusRing(cx, 3)},
		OnPress:  func(core.PressEvent) { run() },
		Children: []core.Node{core.Text{ContentFunc: label, Class: "text-xs text-emerald-500 dark:text-emerald-400"}}}
}

func sourceConnectionReceipt(cx *core.Scope, provider media.Provider, receipt candidateSource, user string, busy, saving func() bool, retest func()) core.Node {
	info := receipt.info
	name := info.Name
	if name == "" {
		name = providerName(cx, provider)
	}
	label := locales.Connected(cx, strings.TrimSpace(name+" "+info.Version))
	details := []string{locales.ConnectionLatency(cx, strconv.FormatInt(max(1, receipt.latency.Round(time.Millisecond).Milliseconds()), 10))}
	if receipt.endpoint.remote {
		details = append(details, locales.ConnectionRemote(cx))
	} else if endpoint, err := media.NormalizeAddress(receipt.endpoint.address); err == nil {
		ip, _ := netip.ParseAddr(endpoint.Hostname())
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
			strings.HasSuffix(strings.ToLower(endpoint.Hostname()), ".local") {
			details = append(details, locales.ConnectionLAN(cx))
		}
	}
	if user != "" {
		details = append(details, locales.SignedInAs(cx, user))
	}
	return core.View{TestID: "connection-verified", Class: "mx-4 flex-row items-center gap-3 rounded-2xl border border-emerald-500/30 bg-emerald-500/10 p-4", Children: []core.Node{
		core.Icon{SVG: lucide.CircleCheck(), Class: "size-5 text-emerald-500"},
		core.View{Class: "min-w-0 flex-1", Children: []core.Node{
			core.Text{Content: label, Class: "text-sm font-medium " + setupTitle, MaxLines: 1},
			core.Text{Content: strings.Join(details, " · "), TestID: "connection-detail", Class: "text-xs " + setupMuted},
		}},
		sourceRetest(cx, busy, saving, retest),
	}}
}

func splitServerAddress(raw string) (host, port string, https bool) {
	address, err := media.NormalizeAddress(raw)
	if err != nil {
		return raw, "", false
	}
	host = address.Hostname()
	if strings.Contains(host, ":") {
		host = strings.TrimPrefix((&url.URL{Host: "[" + host + "]"}).String(), "//")
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
