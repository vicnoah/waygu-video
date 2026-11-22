package main

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func flymooLocalHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	_, err := netip.ParseAddr(host)
	return err == nil || !strings.Contains(host, ".") || strings.HasSuffix(host, ".local")
}

func flymooPort(https bool) string {
	if https {
		return "5667"
	}
	return "5666"
}

// Keep the scheme in a stored bare-port URL if splitting it would turn an
// explicitly supplied HTTP or HTTPS origin into a Flymoo default-port address.
func sourceAddressFields(provider media.Provider, raw string) (host, port string, https bool) {
	host, port, https = splitServerAddress(raw)
	if provider == media.Flymoo && port == "" {
		if original, err := media.NormalizeAddress(raw); err == nil && flymooLocalHost(original.Hostname()) {
			host = raw
		}
	}
	return
}

func sourceAddress(provider media.Provider, host, port string, https bool) (*url.URL, error) {
	address, err := serverAddress(host, port, https)
	if err == nil && provider == media.Flymoo && !strings.Contains(host, "://") &&
		address.Port() == "" && flymooLocalHost(address.Hostname()) {
		address.Host = net.JoinHostPort(address.Hostname(), flymooPort(address.Scheme == "https"))
	}
	return address, err
}

type resolveFNConnect func(context.Context, string, string) (media.FNConnectResult, error)

var fnConnectResolver = core.NewKey("waygu.fnConnectResolver", resolveFNConnect(media.FNConnectClient{}.Resolve))

func flymooRemoteAddress(raw string) string {
	id := media.FNConnectID(raw)
	if id == "" {
		return ""
	}
	return "https://" + id + ".fnos.net/v"
}

func flymooConnectionIntro(cx *core.Scope, ref *core.Ref, disabled func() bool, show func()) core.Node {
	return base.Button{
		TestID: "flymoo-fn-connect", Ref: ref, Label: locales.FlymooFNConnect(cx), Disabled: disabled,
		Class: "mx-4 items-center gap-2 rounded-2xl border border-zinc-900/8 bg-white p-6 shadow-sm shadow-black/5 dark:border-white/8 dark:bg-[#1a1b25]",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { show() },
		Children: []core.Node{
			core.Icon{SVG: lucide.HardDrive(), Class: "size-8 text-blue-500"},
			core.Text{Content: locales.FlymooLoginIntro(cx), Class: "text-sm " + setupTitle},
			core.Text{Content: locales.FlymooFNConnect(cx), Class: "text-xs " + setupMuted},
		},
	}
}

func flymooFNConnectDialog(cx *core.Scope, savedID string, back func(), apply func(string, media.FNConnectResult)) core.Node {
	id := core.NewSignal(cx, savedID)
	code := core.NewSignal(cx, "")
	failure := core.NewSignal[error](cx, nil)
	resolving := core.NewSignal(cx, false)
	resolver := fnConnectResolver.Use(cx)
	lifetime := cx.Context()
	var field *core.Ref
	if appLayoutIdiom(cx) == core.IdiomTV {
		field = &core.Ref{}
		cx.Post(field.Focus)
	}
	commit := func() {
		key := media.FNConnectID(id.GetUntracked())
		if key == "" {
			failure.Set(media.ErrFNConnectID)
			return
		}
		if resolving.GetUntracked() {
			return
		}
		access := code.GetUntracked()
		resolving.Set(true)
		failure.Set(nil)
		go func() {
			ctx, cancel := context.WithTimeout(lifetime, 15*time.Second)
			defer cancel()
			resolved, err := resolver(ctx, key, access)
			cx.Post(func() {
				if lifetime.Err() != nil {
					return
				}
				resolving.Set(false)
				if err != nil {
					failure.Set(err)
					return
				}
				code.Set("")
				apply(key, resolved)
				back()
			})
		}()
	}
	fallback := func() {
		address := flymooRemoteAddress(id.GetUntracked())
		if address == "" {
			failure.Set(media.ErrFNConnectID)
			return
		}
		apply(media.FNConnectID(id.GetUntracked()), media.FNConnectResult{Address: address})
		back()
	}
	return ui.Dialog{TestID: "flymoo-fn-dialog", Title: locales.FlymooFNTitle(cx), Open: func() bool { return true },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				back()
			}
		},
		Children: []core.Node{
			ui.Input{Ref: field, TestID: "flymoo-fn-id", Label: locales.FlymooFNID(cx), Placeholder: locales.FlymooFNID(cx),
				NoAutoCorrect: true, Value: id.Get, Disabled: resolving.Get, OnChange: func(value string) { id.Set(value); failure.Set(nil) },
				OnSubmit: func(string) { commit() }},
			ui.Input{TestID: "flymoo-fn-code", Label: locales.FlymooFNAccessCode(cx), Placeholder: locales.FlymooFNAccessCode(cx),
				Masked: true, NoAutoCorrect: true, Value: code.Get, Disabled: resolving.Get,
				OnChange: func(value string) { code.Set(value); failure.Set(nil) }, OnSubmit: func(string) { commit() }},
			core.Dynamic(func(cx *core.Scope) core.Node {
				if err := failure.Get(); err != nil {
					return notice(errorText(cx, err))
				}
				return nil
			}),
		},
		Footer: []core.Node{
			ui.Button{Text: locales.DesktopSettingsCancel(cx), TestID: "flymoo-fn-cancel", Variant: ui.ButtonGhost, OnPress: back},
			core.Show{When: func() bool { return code.Get() == "" }, Then: func(cx *core.Scope) core.Node {
				return ui.Button{Text: locales.FlymooFNDefaultAddress(cx), TestID: "flymoo-fn-default",
					Variant: ui.ButtonGhost, Disabled: resolving.Get, OnPress: fallback}
			}},
			ui.Button{Text: locales.SourceHeadersApply(cx), TestID: "flymoo-fn-apply", Busy: resolving.Get,
				BusyLabel: locales.FlymooFNResolving(cx), OnPress: commit},
		},
	}
}
