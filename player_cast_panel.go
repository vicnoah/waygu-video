package main

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/casting"
	"waygu-video/locales"
)

func (c *playerCast) devicePanel(cx *core.Scope, ready, paused func() bool) core.Node {
	scan := core.NewSignal(cx, uint64(0))
	refresh := func() { scan.Update(func(n uint64) uint64 { return n + 1 }) }
	auto := core.NewResource(cx, func() (uint64, bool) {
		return scan.Get(), c.open.Get() && !c.active()
	}, func(ctx context.Context, _ uint64, _ core.ResourceInfo) ([]casting.Renderer, error) {
		ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
		defer cancel()
		return casting.Discover(ctx), nil
	})
	cx.Effect(func() {
		if !c.open.Get() || c.active() {
			return
		}
		var tick func()
		tick = func() {
			refresh()
			cx.AfterFunc(10*time.Second, tick)
		}
		cx.AfterFunc(10*time.Second, tick)
	})
	type lookup struct {
		address  string
		revision uint64
	}
	address := core.NewSignal(cx, "")
	query := core.NewSignal(cx, lookup{})
	manual := core.NewResource(cx, func() (lookup, bool) {
		q := query.Get()
		return q, q.address != "" && c.open.Get() && !c.active()
	}, func(ctx context.Context, q lookup, _ core.ResourceInfo) ([]casting.Renderer, error) {
		ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
		defer cancel()
		return casting.Resolve(ctx, q.address)
	})
	find := func() {
		input := strings.TrimSpace(address.GetUntracked())
		query.Update(func(before lookup) lookup { return lookup{input, before.revision + 1} })
	}
	label := locales.PhonePlayerCast(cx)
	list := core.Dynamic(func(cx *core.Scope) core.Node {
		detected, _ := auto.Get()
		added, _ := manual.Get()
		rows := append(slices.Clone(added), detected...)
		slices.SortStableFunc(rows, func(a, b casting.Renderer) int { return cmp.Compare(a.Name, b.Name) })
		seen := make(map[string]bool)
		var buttons []core.Node
		for _, renderer := range rows {
			key := renderer.ID + "@" + renderer.Address.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			buttons = append(buttons, playerPanelAction(cx, "cast-device-"+strconv.Itoa(len(buttons)), renderer.Name,
				lucide.TvMinimal, true, func() bool { return !ready() || c.busy.Get() }, func() { c.connect(renderer) }))
		}
		if len(buttons) == 0 {
			message := locales.CastNoDevices(cx)
			if auto.Loading() || manual.Loading() {
				message = locales.CastSearching(cx)
			}
			return core.Text{Content: message, TestID: "cast-discovery-status", Class: "text-sm text-zinc-300"}
		}
		return core.View{Class: "gap-px overflow-hidden rounded-xl border border-white/10 bg-white/10", Children: buttons}
	})
	notCasting := core.View{Class: "gap-3", Children: []core.Node{
		list,
		playerPanelAction(cx, "cast-refresh", locales.CastRescan(cx), lucide.RefreshCw, false, auto.Loading, refresh),
		ui.Input{Label: locales.CastManualAddress(cx), Placeholder: locales.CastManualAddress(cx), TestID: "cast-manual-address",
			Value: address.Get, OnChange: address.Set, OnSubmit: func(string) { find() }},
		playerPanelAction(cx, "cast-lookup", locales.CastFindDevice(cx), lucide.Search, false,
			func() bool { return manual.Loading() || strings.TrimSpace(address.Get()) == "" }, find),
		core.Show{When: func() bool { return manual.Err() != nil }, Then: func(*core.Scope) core.Node {
			return core.Text{Content: locales.CastDeviceUnavailable(cx), Class: "text-sm text-rose-300"}
		}},
	}}
	detail := core.Dynamic(func(cx *core.Scope) core.Node {
		if !c.active() {
			return notCasting
		}
		device := c.receiver.Get()
		action, icon := locales.Pause(cx), lucide.Pause
		if paused() {
			action, icon = locales.Play(cx), lucide.Play
		}
		rows := []core.Node{
			core.Text{Content: locales.CastConnected(cx, device.Name), TestID: "cast-connected",
				Class: "rounded-lg bg-violet-500/15 px-3 py-2 text-sm font-semibold text-violet-100"},
			playerPanelAction(cx, "cast-transport", action, icon, false, c.busy.Get,
				func() { c.command(castCommand{action: castToggle, playing: paused()}) }),
		}
		if device.HasVolume() {
			rows = append(rows, playerStepRow(cx, locales.DesktopPlayerVolume(cx),
				func() string { return strconv.Itoa(c.volume.Get()) + "%" },
				"cast-volume-down", locales.CastVolumeDown(cx), "cast-volume-up", locales.CastVolumeUp(cx), c.busy.Get, func(delta int) {
					c.command(castCommand{action: castVolume, value: min(100, max(0, c.volume.GetUntracked()+delta*5))})
				}))
		}
		rows = append(rows, playerPanelAction(cx, "cast-disconnect", locales.CastDisconnect(cx), lucide.Unplug, false, c.busy.Get,
			func() { c.command(castCommand{action: castStop}) }))
		return core.View{Class: "gap-3", Children: rows}
	})
	body := core.Group([]core.Node{
		detail,
		core.Show{When: func() bool { return c.busy.Get() && !c.active() }, Then: func(*core.Scope) core.Node {
			return core.Text{Content: locales.CastConnecting(cx), Class: "text-sm text-zinc-300"}
		}},
		core.Show{When: func() bool { return c.problem.Get() != "" }, Then: func(*core.Scope) core.Node {
			return core.Text{ContentFunc: c.problem.Get, TestID: "cast-error", Class: "text-sm text-rose-300"}
		}},
	})
	return playerSidePanel(cx, "cast-panel", "cast-close", label, c.open.Get, func() { c.open.Set(false) }, panelHeading(label), nil, body)
}

func (c *playerCast) indicator(cx *core.Scope) core.Node {
	return core.Show{When: func() bool { return c.active() && !c.open.Get() }, Then: func(cx *core.Scope) core.Node {
		return core.View{Class: "absolute inset-x-0 top-20 items-center", PointerEvents: core.PointerEventsNone, Children: []core.Node{
			core.Text{ContentFunc: func() string { return locales.CastConnected(cx, c.receiver.Get().Name) },
				TestID: "player-cast-status", Class: "rounded-full bg-black/75 px-3 py-2 text-sm font-semibold text-white"},
		}}
	}}
}
