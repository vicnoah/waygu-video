package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"waygu-video/internal/danmaku"
	"waygu-video/internal/downloads"
	"waygu-video/internal/media"
	"waygu-video/locales"
)

type danmakuControls struct {
	buttons    core.Node
	overlay    core.Node
	dialog     core.Node
	openPanel  func()
	panelOpen  func() bool
	importFile func()
	enabled    func() bool
	count      func() int
	toggle     func()
	openMatch  func()
	addLocal   func(danmaku.Comment) bool
	events     func() []danmaku.Event
	rawCounts  func() (local, remote int)
	offset     func() time.Duration
	setOffset  func(time.Duration)
}

type videoSample struct {
	position time.Duration
	seen     time.Time
}

type importedDanmaku struct {
	comments []danmaku.Comment
	format   string
}

func (a *application) playerDanmaku(cx *core.Scope, current route, stream media.Stream, downloaded *downloads.CommentTrack,
	sample *core.Signal[videoSample], size *core.Signal[core.Size], paused, buffering *core.Signal[bool],
	resize func() core.ResizeMode, pictureArea func() core.Bounds, rate func() float32,
	tvSettings, useCanvas func() bool, showOnMPV func(*danmaku.Track)) danmakuControls {
	type remoteComments struct {
		comments []danmaku.Comment
		shift    float64
	}
	type input struct {
		local, authored []danmaku.Comment
		remote          remoteComments
		prefs           danmakuPreferences
		offset          time.Duration
		landscape       bool
		headerInset     int
	}
	idiom := core.Untracked(cx, func() core.Idiom { return appLayoutIdiom(cx) })
	tv := idiom == core.IdiomTV
	const tvTopInset = 150
	const phoneTopLane core.Points = 64
	phoneInset := core.NewMemo(cx, func() int {
		if !phoneLandscape(cx) {
			return 0
		}
		picture := pictureArea()
		frame := visibleDanmakuFrame(picture.Size, size.Get(), resize())
		floor := phoneTopLane + cx.Env().SafeArea().Top - picture.Origin.Y - frame.Origin.Y
		if frame.Size.Height <= 0 || floor <= 0 {
			return 0
		}
		return int(math.Ceil(float64(min(1, floor/frame.Size.Height)) * float64(danmaku.DefaultSettings().Height)))
	})
	editor := a.editDanmaku(cx)
	offset, setOffset, offsetControls := a.danmakuOffset(cx, current)
	key := "danmaku-local-notes-v1:" + danmakuItemKey(current)
	saved := core.Untracked(cx, func() []danmaku.Comment {
		var comments []danmaku.Comment
		if idiom == core.IdiomDesktop {
			if raw, ok := a.storage.GetItem(key); ok {
				_ = json.Unmarshal([]byte(raw), &comments)
			}
		}
		return comments
	})
	authored := core.NewSignalFunc(cx, saved, nil)
	local := core.NewSignalFunc(cx, importedDanmaku{}, nil)
	var initial remoteComments
	if downloaded != nil {
		initial = remoteComments{downloaded.Comments, downloaded.Shift}
	}
	remote := core.NewSignalFunc(cx, initial, nil)
	enableLocal := core.NewSignal(cx, true)
	enableRemote := core.NewSignal(cx, true)
	track := core.NewResource(cx, func() (input, bool) {
		sources := input{prefs: a.danmakuPrefs.Get(), offset: offset.Get(), authored: authored.Get(),
			landscape: phoneLandscape(cx), headerInset: phoneInset.Get()}
		if enableLocal.Get() && !sources.prefs.DisableLocal {
			sources.local = local.Get().comments
		}
		if enableRemote.Get() && !sources.prefs.DisableDandan {
			sources.remote = remote.Get()
		}
		return sources, true
	}, func(ctx context.Context, sources input, _ core.ResourceInfo) (*danmaku.Track, error) {
		if len(sources.local)+len(sources.remote.comments)+len(sources.authored) == 0 {
			return nil, nil
		}
		comments := make([]danmaku.Comment, 0, len(sources.local)+len(sources.remote.comments)+len(sources.authored))
		comments = append(comments, sources.local...)
		comments = append(comments, sources.authored...)
		shift := time.Duration(0)
		if !math.IsNaN(sources.remote.shift) {
			shift = time.Duration(max(-86400, min(86400, sources.remote.shift)) * float64(time.Second))
		}
		for _, comment := range sources.remote.comments {
			if shift > 0 && comment.At > time.Duration(math.MaxInt64)-shift {
				continue
			}
			comment.At += shift
			comments = append(comments, comment)
		}
		settings := sources.prefs.rendererSettings(sources.offset)
		if tv {
			settings.Height -= tvTopInset * settings.Height / 1080
		} else if sources.landscape {
			// Scenra's 390 pt frame uses 17 pt glyphs on 28 pt rows.
			settings.FontSize = 31 * sources.prefs.FontScale / 100
			settings.RowGap, settings.TopInset = 21, sources.headerInset
		}
		scheduled := danmaku.ScheduleContext(ctx, comments, settings)
		return &scheduled, ctx.Err()
	})
	open := core.NewSignal(cx, a.danmakuPrefs.GetUntracked().Enabled)
	if idiom == core.IdiomDesktop {
		defaultEnabled := core.NewMemo(cx, func() bool { return a.danmakuPrefs.Get().Enabled })
		cx.Effect(func() { open.Set(defaultEnabled.Get()) })
	}
	panel := core.NewSignal(cx, false)
	tab := core.NewSignal(cx, 0)
	showMatch := core.NewSignal(cx, false)
	openMatch := func() {
		cx.Batch(func() {
			showMatch.Set(true)
			tab.Set(1)
			panel.Set(true)
		})
	}
	invalid := core.NewSignal(cx, false)
	busy := core.NewSignal(cx, false)
	clock := core.NewSignal(cx, time.Duration(0))
	viewContext := cx.Context()
	var source core.Node
	if current.downloadID == "" {
		source = a.onlineDanmaku(cx, current, stream, func(comments []danmaku.Comment, shift float64) {
			remote.Set(remoteComments{comments, shift})
			enableRemote.Set(true)
		}, func() { panel.Set(false) }, openMatch)
	} else if downloaded != nil {
		source = ui.Checkbox{Text: locales.PhonePlayerDanmakuLoaded(cx, strconv.Itoa(len(downloaded.Comments))),
			TestID: "danmaku-toggle-remote", Checked: enableRemote.Get, OnChange: enableRemote.Set}
	} else {
		source = hint(locales.DownloadOfflinePlayback(cx))
	}

	choose := func() {
		if busy.GetUntracked() {
			return
		}
		cx.PickFiles(core.PickFilesOptions{Types: []string{".xml", ".json", ".ass"}, Title: locales.AddDanmaku(cx)}, func(files []core.File, err error) {
			if err != nil {
				invalid.Set(true)
				return
			}
			if len(files) == 0 {
				return
			}
			busy.Set(true)
			invalid.Set(false)
			go func() {
				file := files[0]
				if file.Temporary {
					defer os.Remove(file.Path)
				}
				var result []danmaku.Comment
				extension := strings.ToLower(filepath.Ext(file.Name))
				if reader, err := file.Open(); err == nil {
					switch extension {
					case ".json":
						result, err = danmaku.ReadJSON(reader)
					case ".ass":
						result, err = danmaku.ReadASS(reader)
					default:
						result, err = danmaku.ReadXML(reader)
					}
					reader.Close()
					if err != nil {
						result = nil
					}
				}
				cx.Post(func() {
					if viewContext.Err() != nil {
						return
					}
					busy.Set(false)
					invalid.Set(len(result) == 0)
					if len(result) > 0 {
						local.Set(importedDanmaku{comments: result, format: strings.ToUpper(strings.TrimPrefix(extension, "."))})
						enableLocal.Set(true)
						open.Set(true)
					}
				})
			}()
		})
	}

	setVisible := func(value bool) {
		if editor.save(func(p *danmakuPreferences) { p.Enabled = value }) {
			open.Set(value)
		}
	}
	tabs := []ui.Option[int]{
		{Value: 0, Label: locales.DanmakuDisplayTab(cx), TestID: "danmaku-tab-display"},
		{Value: 1, Label: locales.DanmakuSourceTab(cx), TestID: "danmaku-tab-source"},
		{Value: 2, Label: locales.DanmakuFilterTab(cx), TestID: "danmaku-tab-filters"},
	}
	body := core.Dynamic(func(cx *core.Scope) core.Node {
		var node core.Node
		compact := phoneLandscape(cx)
		switch tab.Get() {
		case 0:
			if compact {
				node = editor.playerDisplay(cx, open.Get, setVisible, offsetControls)
			} else {
				node = editor.display(cx, ui.Checkbox{Text: locales.ShowDanmaku(cx), TestID: "danmaku-display-enabled",
					Checked: open.Get, OnChange: setVisible}, offsetControls)
			}
		case 1:
			if compact {
				node = editor.playerSources(cx, current, source, showMatch, local.Get, func() int { return len(remote.Get().comments) },
					enableLocal, enableRemote, invalid.Get, busy.Get, choose)
			} else {
				node = core.View{Class: "gap-3", Children: []core.Node{
					ui.Button{Text: locales.AddDanmaku(cx), Icon: lucide.FolderOpen, Variant: ui.ButtonOutline,
						Busy: busy.Get, TestID: "danmaku-import-file", OnPress: choose},
					core.Show{When: invalid.Get, Then: func(cx *core.Scope) core.Node { return notice(locales.DanmakuFileError(cx)) }},
					source,
				}}
			}
		case 2:
			if compact {
				node = editor.playerFilters(cx)
			} else {
				node = editor.filters(cx)
			}
		}
		return core.View{Class: "gap-2.5", Children: []core.Node{editor.warning(), node}}
	})

	cx.Effect(func() {
		if showOnMPV != nil {
			var visible *danmaku.Track
			if open.Get() {
				visible, _ = track.Get()
			}
			showOnMPV(visible)
		}
	})
	cx.Effect(func() {
		value := sample.Get()
		clock.Set(value.position)
		active, _ := track.Get()
		if active == nil || !open.Get() || !useCanvas() || paused.Get() || buffering.Get() || value.seen.IsZero() {
			return
		}
		speed := float64(rate())
		var next func()
		var animate func(time.Time)
		animate = func(now time.Time) {
			elapsed := min(max(0, now.Sub(value.seen)), 250*time.Millisecond)
			clock.Set(value.position + time.Duration(float64(elapsed)*speed))
			if elapsed < 250*time.Millisecond {
				next = cx.RequestAnimationFrame(animate)
			}
		}
		next = cx.RequestAnimationFrame(animate)
		cx.Cleanup(func() { next() })
	})

	title := locales.Danmaku(cx)
	close := func() { panel.Set(false) }
	dialog := core.Dynamic(func(cx *core.Scope) core.Node {
		if phoneLandscape(cx) {
			heading := core.View{Class: "min-w-0 flex-1 flex-row items-center gap-2", Children: []core.Node{
				playerDanmakuBadge(cx), panelHeading(title),
				core.Text{ContentFunc: func() string {
					return locales.PhonePlayerDanmakuLoaded(cx, strconv.Itoa(len(local.Get().comments)+len(remote.Get().comments)))
				}, Class: "text-xs text-zinc-500", MaxLines: 1},
			}}
			return playerSidePanel(cx, "danmaku-sources-dialog", "close-danmaku-sources", title, panel.Get, close,
				heading, playerPanelTabs(cx, "danmaku-tabs", title, tabs, tab), body)
		}
		return ui.Dialog{Open: panel.Get, OnOpenChange: func(visible bool, _ base.Reason) { panel.Set(visible) },
			Title: title, TestID: "danmaku-sources-dialog",
			Trailing: []core.Node{ui.IconButton{Label: locales.TracksDone(cx), Icon: lucide.X, Size: 36,
				TestID: "close-danmaku-sources", OnPress: close}},
			Children: []core.Node{
				ui.Tabs[int]{Options: tabs, Value: tab.Get, OnChange: tab.Set, Equal: true, TestID: "danmaku-tabs"},
				core.Scroll{StyleFunc: func() core.Style {
					return core.Style{MaxHeight: core.Pt(min(365, max(105, cx.Env().Window().Height*0.46))), FlexShrink: 1}
				}, Children: []core.Node{body}},
			}}
	})
	return danmakuControls{
		dialog: dialog, openPanel: func() { panel.Set(true) }, panelOpen: panel.Get, importFile: choose,
		openMatch: openMatch,
		offset:    offset.Get, setOffset: setOffset,
		events: func() []danmaku.Event {
			if value, _ := track.Get(); value != nil {
				return value.Events
			}
			return nil
		},
		rawCounts: func() (int, int) { return len(local.Get().comments) + len(authored.Get()), len(remote.Get().comments) },
		addLocal: func(comment danmaku.Comment) bool {
			before := authored.GetUntracked()
			items := append(slices.Clone(before[max(0, len(before)-999):]), comment)
			data, err := json.Marshal(items)
			if err != nil || a.storage.SetItem(key, string(data)) != nil {
				return false
			}
			authored.Set(items)
			open.Set(true)
			return true
		},
		count: func() int {
			value, _ := track.Get()
			if value == nil {
				return 0
			}
			return len(value.Events)
		},
		enabled: open.Get, toggle: func() { open.Update(func(value bool) bool { return !value }) },
		buttons: core.View{Class: "flex-row items-center gap-1", Children: []core.Node{
			ui.Button{Label: locales.Danmaku(cx), Icon: lucide.SlidersHorizontal, Variant: ui.ButtonSecondary,
				TestID: "danmaku-sources", OnPress: func() { panel.Set(true) }},
			ui.Button{Label: locales.AddDanmaku(cx), Icon: lucide.FolderOpen, Variant: ui.ButtonSecondary,
				Busy: busy.Get, TestID: "load-danmaku", OnPress: choose},
			core.Show{When: func() bool { value, _ := track.Get(); return value != nil }, Then: func(cx *core.Scope) core.Node {
				return core.Dynamic(func(cx *core.Scope) core.Node {
					label, variant := locales.ShowDanmaku(cx), ui.ButtonSecondary
					if open.Get() {
						label, variant = locales.HideDanmaku(cx), ui.ButtonPrimary
					}
					return ui.Button{Label: label, Icon: lucide.MessageSquareText, Variant: variant,
						TestID: "toggle-danmaku", OnPress: func() { open.Update(func(value bool) bool { return !value }) }}
				})
			}},
		}},
		overlay: core.View{Class: "absolute inset-0", PointerEvents: core.PointerEventsNone, Children: []core.Node{
			core.Show{When: func() bool { value, _ := track.Get(); return open.Get() && value != nil && useCanvas() }, Then: func(cx *core.Scope) core.Node {
				canvas := core.Canvas{Class: "absolute inset-0", TestID: "danmaku-canvas", Draw: func(d *core.Drawing, bounds core.Size) {
					comments, _ := track.Get()
					if comments != nil {
						frame := resize().Fit(size.Get(), bounds, core.ObjectPosition{})
						switch {
						case phoneLandscape(cx):
							frame = visibleDanmakuFrame(bounds, size.Get(), resize())
							d.Save().BeginPath().Rect(float32(frame.Origin.X), float32(frame.Origin.Y),
								float32(frame.Size.Width), float32(frame.Size.Height)).Clip()
							defer d.Restore()
						case tv:
							inset := min(tvLayoutFor(cx).points(tvTopInset), frame.Size.Height/4)
							frame.Origin.Y += inset
							frame.Size.Height -= inset
						}
						drawDanmaku(d, frame, *comments, clock.Get())
					}
				}}
				if !tv {
					return canvas
				}
				canvas.Class = "absolute inset-y-0 left-0"
				canvas.StyleFunc = func() core.Style {
					return core.Style{Width: core.Pt(cx.Env().Window().Width)}
				}
				return core.View{Class: "absolute inset-0 overflow-hidden", PointerEvents: core.PointerEventsNone,
					StyleFunc: func() core.Style {
						if tvSettings() {
							return core.Style{Right: tvLayoutFor(cx).length(600)}
						}
						return core.Style{}
					}, Children: []core.Node{canvas}}
			}},
			core.Show{When: func() bool { return invalid.Get() && !panel.Get() }, Then: func(cx *core.Scope) core.Node {
				return core.View{Class: "absolute inset-0 items-center justify-center", Children: []core.Node{
					notice(locales.DanmakuFileError(cx)),
				}}
			}},
		}},
	}
}

const maxDanmakuOffset = 60 * time.Second

func (a *application) danmakuOffsetEditor(cx *core.Scope, current route) (*core.Signal[time.Duration], func(time.Duration), *core.Signal[bool]) {
	key := "danmaku-offset-v1:" + danmakuItemKey(current)
	stored := core.Untracked(cx, func() string { value, _ := a.storage.GetItem(key); return value })
	ms, _ := strconv.ParseInt(stored, 10, 64)
	offset := core.NewSignal(cx, time.Duration(min(maxDanmakuOffset.Milliseconds(), max(-maxDanmakuOffset.Milliseconds(), ms)))*time.Millisecond)
	failed := core.NewSignal(cx, false)
	set := func(value time.Duration) {
		value = min(maxDanmakuOffset, max(-maxDanmakuOffset, value))
		if value == 0 {
			a.storage.RemoveItem(key)
		} else if err := a.storage.SetItem(key, strconv.FormatInt(value.Milliseconds(), 10)); err != nil {
			failed.Set(true)
			return
		}
		failed.Set(false)
		offset.Set(value)
	}
	return offset, set, failed
}

func (a *application) danmakuOffset(cx *core.Scope, current route) (*core.Signal[time.Duration], func(time.Duration), core.Node) {
	offset, set, failed := a.danmakuOffsetEditor(cx, current)
	regular := core.View{Class: "gap-1", Children: []core.Node{
		core.View{Class: "flex-row flex-wrap items-center gap-2", Children: []core.Node{
			core.Text{ContentFunc: func() string {
				return locales.DanmakuOffset(cx, fmt.Sprintf("%+.1f", offset.Get().Seconds()))
			}, Class: "flex-1 text-sm font-medium", TestID: "danmaku-offset"},
			ui.IconButton{Label: locales.DanmakuEarlier(cx), Icon: lucide.Minus, Size: 44, TestID: "danmaku-earlier",
				Disabled: func() bool { return offset.Get() <= -maxDanmakuOffset },
				OnPress:  func() { set(offset.GetUntracked() - 500*time.Millisecond) }},
			ui.IconButton{Label: locales.DanmakuLater(cx), Icon: lucide.Plus, Size: 44, TestID: "danmaku-later",
				Disabled: func() bool { return offset.Get() >= maxDanmakuOffset },
				OnPress:  func() { set(offset.GetUntracked() + 500*time.Millisecond) }},
			ui.Button{Text: locales.DanmakuResetOffset(cx), Variant: ui.ButtonGhost, TestID: "danmaku-reset-offset",
				Disabled: func() bool { return offset.Get() == 0 }, OnPress: func() { set(0) }},
		}},
		core.Show{When: failed.Get, Then: func(cx *core.Scope) core.Node { return notice(locales.DanmakuPreferencesError(cx)) }},
	}}
	return offset, set, core.Dynamic(func(cx *core.Scope) core.Node {
		if phoneLandscape(cx) {
			return core.View{Class: "gap-1", Children: []core.Node{
				playerStepRow(cx, locales.PhonePlayerDanmakuTiming(cx), func() string {
					return fmt.Sprintf("%+.1fs", offset.Get().Seconds())
				}, "danmaku-earlier", locales.DanmakuEarlier(cx), "danmaku-later", locales.DanmakuLater(cx), nil,
					func(change int) { set(offset.GetUntracked() + time.Duration(change)*500*time.Millisecond) }),
				core.Show{When: failed.Get, Then: func(cx *core.Scope) core.Node { return notice(locales.DanmakuPreferencesError(cx)) }},
			}}
		}
		return regular
	})

}

func visibleDanmakuFrame(bounds, video core.Size, resize core.ResizeMode) core.Bounds {
	frame := resize.Fit(video, bounds, core.ObjectPosition{})
	left, top := max(0, frame.Origin.X), max(0, frame.Origin.Y)
	right := min(bounds.Width, frame.Origin.X+frame.Size.Width)
	bottom := min(bounds.Height, frame.Origin.Y+frame.Size.Height)
	return core.Bounds{Origin: core.Point{X: left, Y: top}, Size: core.Size{
		Width: max(0, right-left), Height: max(0, bottom-top),
	}}
}

func drawDanmaku(d *core.Drawing, frame core.Bounds, track danmaku.Track, at time.Duration) {
	width, height := float32(frame.Size.Width), float32(frame.Size.Height)
	if width <= 0 || height <= 0 {
		return
	}
	x, y := float32(frame.Origin.X), float32(frame.Origin.Y)
	scaleX, scaleY := width/float32(track.Width), height/float32(track.Height)
	scale := min(scaleX, scaleY)
	font := core.Font{Size: core.Points(max(6, float32(track.FontSize)*scale)), Weight: core.FontWeightNormal}
	if track.Bold {
		font.Weight = core.FontWeightBold
	}
	for _, event := range track.Visible(at) {
		left := x + float32(event.X(at))*scaleX
		top := y + float32(event.Y)*scaleY + float32(font.Size)
		text := event.DisplayText()
		if event.Mode != danmaku.Scroll {
			left -= d.MeasureText(text, font) / 2
		}
		if track.Outline > 0 {
			d.SetStrokeColor(core.RGBA(0x000000, float32(track.Opacity))).SetLineWidth(max(.5, float32(track.Outline)*scale)).StrokeText(text, left, top, font)
		}
		d.SetFillColor(core.RGBA(event.Color, float32(track.Opacity))).FillText(text, left, top, font)
	}
}
