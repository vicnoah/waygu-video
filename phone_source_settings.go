package main

import (
	"errors"
	"slices"
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

type sourceCardDrag struct {
	id     string
	offset core.Points
}

func (a *application) phoneSourceSettings(cx *core.Scope) core.Node {
	editing := core.NewSignal(cx, false)
	revealed := core.NewSignal(cx, "")
	confirm := core.NewSignal(cx, "")
	failure := core.NewSignal[error](cx, nil)
	drag := core.NewSignal(cx, sourceCardDrag{})
	scrollRef := &core.Ref{}
	frames := map[string]core.Bounds{}
	a.watchSourceConnections(cx)
	toggleEditing := func() {
		revealed.Set("")
		drag.Set(sourceCardDrag{})
		editing.Update(func(enabled bool) bool { return !enabled })
	}
	editLabel := func() string {
		if editing.Get() {
			return locales.TracksDone(cx)
		}
		return locales.SettingsEdit(cx)
	}
	header := base.Button{LabelFunc: editLabel, TestID: "source-settings-edit",
		Class: "h-11 justify-center rounded px-3", Disabled: func() bool { return len(a.sources.Get()) == 0 },
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { toggleEditing() },
		Children: []core.Node{core.Text{ContentFunc: editLabel, Class: "text-[17px] " + setupAccent}}}

	cards := core.Each(a.sources.Get, func(entry source) string { return entry.ID },
		func(cx *core.Scope, entry func() source, index func() int) core.Node {
			id := core.Untracked(cx, entry).ID
			cx.Cleanup(func() { delete(frames, id) })
			status := func() sourceCheck {
				check := a.sourceChecks.Get()[id]
				if check.connection != entry().connection() {
					return sourceCheck{checking: true}
				}
				return check
			}
			edit := func() {
				selected := core.Untracked(cx, entry)
				a.push(route{screen: screenConnect, provider: selected.Provider, source: selected})
			}
			move := func(delta int) { failure.Set(a.moveSource(id, delta)) }
			grip := core.View{TestID: "source-reorder-" + id, Label: locales.SettingsOrder(cx), Role: core.RoleButton,
				Class:         "size-8 shrink-0 items-center justify-center rounded",
				Style:         core.Style{FocusVisible: ui.FocusRing(cx, -2)},
				OnPress:       func(core.PressEvent) { editing.Set(true) },
				PanActivation: core.PanActivation{ActiveOffsetY: 10, FailOffsetX: 20},
				OnPan: func(e core.PanEvent) {
					switch e.Phase {
					case core.GestureBegan, core.GestureChanged:
						revealed.Set("")
						drag.Set(sourceCardDrag{id, e.Translation.Y})
					case core.GestureEnded:
						drag.Set(sourceCardDrag{})
						origin := frames[id]
						target := origin.Origin.Y + origin.Size.Height/2 + e.Translation.Y
						entries := a.sources.GetUntracked()
						from, to := index(), index()
						for i, other := range entries {
							box := frames[other.ID]
							middle := box.Origin.Y + box.Size.Height/2
							if i > from && target > middle && e.Translation.Y > 0 {
								to = i
							}
							if i < from && target < middle && e.Translation.Y < 0 {
								to = i
								break
							}
						}
						move(to - from)
					default:
						drag.Set(sourceCardDrag{})
					}
				},
				Children: []core.Node{core.Icon{SVG: lucide.GripVertical(), Class: "size-4 opacity-70 " + setupMuted}},
			}
			name := func() string { return entry().Name }
			variant := sourceCardStyle(core.Untracked(cx, entry).Provider)
			top := core.View{Class: "flex-row items-center gap-3", Children: []core.Node{
				core.View{Class: "size-11 items-center justify-center rounded-xl shadow-md shadow-black/10 " + variant.badge,
					Children: []core.Node{core.Icon{SVG: variant.icon(), Class: "size-5 text-white"}}},
				core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
					core.View{Class: "min-w-0 flex-row items-center gap-2", Children: []core.Node{
						core.Text{ContentFunc: name, MaxLines: 1, Class: "min-w-0 shrink text-base font-semibold " + setupTitle},
						core.Text{Content: variant.title(cx), Class: "shrink-0 rounded px-1.5 py-0.5 text-[10px] " + variant.tag},
					}},
					core.Text{ContentFunc: func() string {
						entry := entry()
						check := status()
						if address := check.endpoint.address; check.err == nil && address != "" && address != entry.Address && address != entry.RemoteAddress {
							return strings.TrimSuffix(address, "/")
						}
						if entry.RemoteAddress != "" {
							return locales.SourceAddresses(cx, entry.Address, entry.RemoteAddress)
						}
						return strings.TrimSuffix(entry.Address, "/")
					},
						MaxLines: 1, Class: "text-xs " + setupMuted},
				}},
				grip,
			}}
			connectionText := func() string {
				s := status()
				switch {
				case s.checking:
					return locales.SettingsCheckingSource(cx)
				case errors.Is(s.err, media.ErrAuthentication):
					return locales.SettingsSourceNeedsLogin(cx)
				case s.err != nil:
					return locales.SettingsSourceOffline(cx)
				case s.endpoint.remote:
					return locales.SourceOnlineRemote(cx)
				case entry().automaticRoute() && a.sourceFailover.Get():
					return locales.SourceOnlineLocal(cx)
				default:
					return locales.SettingsSourceOnline(cx)
				}
			}
			statusTint := func() string {
				s := status()
				switch {
				case s.checking:
					return "text-zinc-500 dark:text-zinc-400"
				case s.err != nil:
					return "text-amber-600 dark:text-amber-400"
				default:
					return "text-emerald-600 dark:text-emerald-400"
				}
			}
			info := core.View{Class: "flex-row items-center gap-2 border-t pt-3 " + setupBorder, Children: []core.Node{
				core.View{Class: "min-w-0 shrink flex-row items-center gap-1.5", Children: []core.Node{
					core.View{Class: "size-2 shrink-0 rounded-full", ClassFunc: func() string {
						s := status()
						if s.checking {
							return "bg-zinc-400"
						}
						if s.err != nil {
							return "bg-amber-500"
						}
						return "bg-emerald-500"
					}},
					core.Text{ContentFunc: connectionText, TestID: "source-settings-status-" + id,
						Class: "shrink text-xs", MaxLines: 1, ClassFunc: statusTint},
				}},
				core.Text{ContentFunc: func() string {
					if username := entry().Username; username != "" {
						return locales.SettingsSourceUser(cx, username)
					}
					return ""
				}, Class: "min-w-0 shrink text-xs " + setupMuted, MaxLines: 1},
				core.Text{ContentFunc: func() string {
					if stamp := entry().LibrariesSyncedAt; stamp > 0 {
						return locales.SettingsSourceSyncedAt(cx, time.Unix(stamp, 0).In(time.Local).Format("15:04"))
					}
					return locales.SettingsSourceNotSynced(cx)
				}, TestID: "source-settings-last-sync-" + id, Class: "min-w-0 flex-1 text-right text-xs " + setupMuted},
			}}
			syncLabel := func() string {
				if current := status(); current.syncing {
					return locales.SettingsSourceSyncing(cx)
				} else if current.err != nil {
					return locales.SettingsSourceRetry(cx)
				}
				return locales.SettingsSourceSync(cx)
			}
			actions := core.View{Class: "flex-row gap-2", Children: []core.Node{
				compactSourceAction(cx, "source-sync-"+id, syncLabel, lucide.RefreshCw, func() bool { return status().syncing },
					func() {
						if errors.Is(core.Untracked(cx, status).err, media.ErrAuthentication) {
							edit()
						} else {
							a.checkSource(core.Untracked(cx, entry), true)
						}
					}),
				compactSourceAction(cx, "manage-libraries-"+id, func() string {
					return locales.SettingsSourceLibraries(cx, strconv.Itoa(len(entry().Libraries)))
				}, lucide.Library, func() bool { return status().syncing }, func() {
					a.push(route{screen: screenManageLibraries, source: core.Untracked(cx, entry)})
				}),
				compactSourceAction(cx, "source-edit-"+id, func() string { return locales.SettingsEdit(cx) },
					lucide.Pencil, nil, edit),
			}}
			syncError := core.Show{When: func() bool { return status().saveError != nil }, Then: func(cx *core.Scope) core.Node {
				return core.Text{Content: locales.LibrarySaveError(cx), Class: "text-xs text-rose-500"}
			}}
			editControls := core.Show{When: func() bool { return editing.Get() || revealed.Get() == id }, Then: func(cx *core.Scope) core.Node {
				return core.View{Class: "flex-row items-center justify-end gap-1", Children: []core.Node{
					ui.IconButton{LabelFunc: func() string { return locales.MoveSourceUp(cx, name()) }, Icon: lucide.ArrowUp,
						Size: 44, TestID: "move-source-up-" + id, Disabled: func() bool { return index() == 0 }, OnPress: func() { move(-1) }},
					ui.IconButton{LabelFunc: func() string { return locales.MoveSourceDown(cx, name()) }, Icon: lucide.ArrowDown,
						Size: 44, TestID: "move-source-down-" + id, Disabled: func() bool { return index()+1 == len(a.sources.Get()) }, OnPress: func() { move(1) }},
					ui.Button{Text: locales.RemoveSource(cx), TestID: "source-remove-" + id, Variant: ui.ButtonGhost,
						Color: func() core.Color { return cx.Palette().Danger }, OnPress: func() { confirm.Set(id) }},
				}}
			}}
			return core.View{TestID: "source-settings-card-" + id,
				Class:         "gap-3 rounded-2xl bg-white p-4 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder,
				OnLayout:      func(box core.Bounds) { frames[id] = box },
				OnLongPress:   func(core.PressEvent) { editing.Set(true) },
				PanActivation: core.PanActivation{ActiveOffsetX: 12, FailOffsetY: 14},
				OnPan: func(event core.PanEvent) {
					if event.Phase == core.GestureEnded {
						if event.Translation.X < -38 {
							revealed.Set(id)
						}
						if event.Translation.X > 38 && revealed.GetUntracked() == id {
							revealed.Set("")
						}
					}
				},
				StyleFunc: func() core.Style {
					if current := drag.Get(); current.id == id {
						return core.Style{ZIndex: 2, Transform: core.Transform{TranslateY: current.offset}}
					}
					return core.Style{}
				}, Children: []core.Node{top, info, actions, syncError, editControls},
			}
		})

	add := base.Button{Label: locales.AddSource(cx), TestID: "source-settings-add", Class: "h-12 flex-row items-center justify-center gap-2 rounded-2xl border border-dashed border-violet-500/50 bg-violet-500/8",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { a.push(route{screen: screenSources}) },
		Children: []core.Node{core.Icon{SVG: lucide.Plus(), Class: "size-5 " + setupAccent},
			core.Text{Content: locales.AddSource(cx), Class: "text-[15px] font-medium " + setupAccent}},
	}
	timeouts := make([]ui.Option[int], 0, len(sourceTimeouts))
	for _, seconds := range sourceTimeouts {
		timeouts = append(timeouts, ui.Option[int]{Value: seconds, Label: locales.SettingsSeconds(cx, strconv.Itoa(seconds))})
	}
	order := phoneSetting{locales.SettingsOrder(cx), "", "bg-indigo-500", lucide.ListOrdered}
	body := core.View{Class: "w-full max-w-[720px] self-center gap-6 pt-2 pb-8", Children: []core.Node{
		core.View{Class: "gap-3 px-4", Children: []core.Node{
			core.Show{When: func() bool { return len(a.sources.Get()) == 0 }, Then: func(cx *core.Scope) core.Node {
				return notice(locales.NoSources(cx))
			}}, cards, add,
		}},
		setupGroup(locales.SettingsConnection(cx), "",
			(phoneSetting{locales.SettingsConnectOnLaunch(cx), "", "bg-emerald-500", lucide.Power}).
				toggle(cx, "", func() bool { return true }, nil),
			(phoneSetting{locales.SettingsConnectionFailover(cx), locales.SettingsConnectionFailoverDetail(cx), "bg-sky-500", lucide.Network}).
				toggle(cx, "source-settings-failover", a.sourceFailover.Get, func(enabled bool) { failure.Set(a.setSourceFailover(enabled)) }),
			phoneSettingChoice(cx, phoneSetting{locales.SettingsConnectionTimeout(cx), "", "bg-zinc-500", lucide.Timer},
				"source-settings-timeout", timeouts, a.sourceTimeout.Get, func(value int) {
					failure.Set(storeSettingValue(a.storage, sourceTimeoutKey, a.sourceTimeout, value, strconv.Itoa(value)))
				}),
		),
		setupGroup(locales.SettingsMultipleSources(cx), locales.SettingsSourcePriorityFooter(cx),
			(phoneSetting{locales.MergeIdenticalTitles(cx), locales.SettingsSourceMatchDetail(cx), "bg-violet-500", lucide.Layers}).
				toggle(cx, "merge-identical-titles", a.mergeTitles.Get, func(enabled bool) {
					failure.Set(storeSettingValue(a.storage, mergeTitlesKey, a.mergeTitles, enabled, strconv.FormatBool(enabled)))
				}),
			core.Dynamic(func(cx *core.Scope) core.Node {
				entries := a.sources.Get()
				if len(entries) == 0 {
					return order.row(core.Text{Content: locales.SettingsNoSource(cx), Class: "text-[15px] " + setupMuted})
				}
				names := make([]string, 0, len(entries))
				for _, entry := range entries {
					names = append(names, providerName(cx, entry.Provider))
				}
				return order.link(cx, "source-settings-order", strings.Join(names, " › "), func() {
					editing.Set(true)
					scrollRef.ScrollTo(core.Point{}, true)
				})
			}),
		),
		settingsFailure(cx, failure),
	}}
	confirmRemoval := core.Dynamic(func(cx *core.Scope) core.Node {
		id := confirm.Get()
		if id == "" {
			return nil
		}
		entries := a.sources.Get()
		i := slices.IndexFunc(entries, func(s source) bool { return s.ID == id })
		if i < 0 {
			return nil
		}
		selected := entries[i]
		return ui.Dialog{Title: locales.RemoveSource(cx), Description: locales.SettingsSourceRemoveConfirm(cx, selected.Name),
			Open: func() bool { return confirm.Get() == id }, OnOpenChange: func(open bool, _ base.Reason) {
				if !open {
					confirm.Set("")
				}
			},
			TestID: "source-settings-remove-dialog", Footer: []core.Node{
				ui.Button{Text: locales.Back(cx), Variant: ui.ButtonGhost, OnPress: func() { confirm.Set("") }},
				ui.Button{Text: locales.RemoveSource(cx), TestID: "confirm-remove-source", Color: func() core.Color { return cx.Palette().Danger },
					OnPress: func() {
						failure.Set(a.removeSource(selected))
						if len(a.sources.GetUntracked()) == 0 {
							editing.Set(false)
						}
						confirm.Set("")
						revealed.Set("")
					}},
			},
		}
	})
	return core.View{Class: "flex-1", Children: []core.Node{
		phoneSettingsFrameAction(cx, locales.MediaSources(cx), a.back, "phone-source-settings", body, header, scrollRef),
		confirmRemoval,
	}}
}

func compactSourceAction(cx *core.Scope, id string, label func() string, icon func() *core.SVG, disabled func() bool, action func()) core.Node {
	return base.Button{LabelFunc: label, TestID: id, Disabled: disabled,
		Class: "h-8 min-w-0 flex-1 flex-row items-center justify-center gap-1.5 rounded-lg bg-zinc-900/5 px-1 dark:bg-white/7 " + setupBorder,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { action() },
		Children: []core.Node{
			core.Icon{SVG: icon(), Class: "size-3.5 shrink-0 " + setupMuted},
			core.Text{ContentFunc: label, Class: "min-w-0 shrink text-xs " + setupTitle, MaxLines: 1},
		},
	}
}

type sourceCardAppearance struct {
	name, badge, tag string
	icon             func() *core.SVG
}

func (s sourceCardAppearance) title(cx *core.Scope) string {
	if s.name == "" {
		return locales.MediaSources(cx)
	}
	return s.name
}

func sourceCardStyle(provider media.Provider) sourceCardAppearance {
	switch provider {
	case media.Emby:
		return sourceCardAppearance{"Emby", "bg-emerald-500", "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400", lucide.Server}
	case media.Jellyfin:
		return sourceCardAppearance{"Jellyfin", "bg-sky-500", "bg-sky-500/15 text-sky-700 dark:text-sky-300", lucide.Database}
	case media.Flymoo:
		return sourceCardAppearance{"fnOS", "bg-blue-600", "bg-blue-500/15 text-blue-600 dark:text-blue-400", lucide.HardDrive}
	case media.Plex:
		return sourceCardAppearance{"Plex", "bg-amber-500", "bg-amber-500/15 text-amber-700 dark:text-amber-400", lucide.Server}
	default:
		return sourceCardAppearance{string(provider), "bg-zinc-500", "bg-zinc-500/15 " + setupMuted, lucide.Server}
	}
}

func (a *application) returnToSourceSettings() bool {
	stack := a.routes.GetUntracked()
	index := slices.IndexFunc(stack, func(entry route) bool {
		return entry.screen == screenSourceSettings || entry.screen == screenTVSettings || entry.settingsFlow
	})
	if index < 0 {
		return false
	}
	if stack[index].settingsFlow {
		a.routes.Set(stack[:index])
		a.restoreDesktopSettings()
	} else {
		a.routes.Set(stack[:index+1])
	}
	return true
}
