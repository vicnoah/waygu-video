package main

import (
	"context"
	"errors"
	"runtime"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/casting"
	"waygu-video/internal/media"
	"waygu-video/locales"
)

type castAction uint8

const (
	castToggle castAction = iota
	castSeek
	castVolume
	castStop
)

type castCommand struct {
	action  castAction
	at      time.Duration
	value   int
	playing bool
}

type playerCast struct {
	available     bool
	open, busy    *core.Signal[bool]
	receiver      *core.Signal[casting.Renderer]
	problem       *core.Signal[string]
	volume        *core.Signal[int]
	castingStream media.Stream
	inbox         chan castCommand
	connect       func(casting.Renderer)
	cancel        func()
	panel         core.Node
}

func canCast(cx *core.Scope) bool {
	return runtime.GOOS != "js" && appLayoutIdiom(cx) != core.IdiomTV
}

func (c *playerCast) active() bool { return c.receiver.Get().Address.IsValid() }

func (c *playerCast) command(action castCommand) {
	if c.inbox == nil || c.busy.GetUntracked() {
		return
	}
	c.busy.Set(true)
	c.problem.Set("")
	c.inbox <- action
}

func newPlayerCast(cx *core.Scope, current route, medium *core.Signal[playerMedia],
	position *core.Signal[time.Duration], paused *core.Signal[bool], blocked func() bool,
	status func(casting.Position, string), detached func(), ready func() bool) *playerCast {
	c := &playerCast{
		available: core.Untracked(cx, func() bool { return canCast(cx) }) && current.downloadID == "",
		open:      core.NewSignal(cx, current.castOnOpen),
		busy:      core.NewSignal(cx, false),
		receiver:  core.NewSignal(cx, casting.Renderer{}),
		problem:   core.NewSignal(cx, ""),
		volume:    core.NewSignal(cx, 0),
	}
	if !c.available {
		c.open.Set(false)
		return c
	}
	var cancel context.CancelFunc
	c.cancel = func() {
		if cancel != nil {
			cancel()
		}
	}
	cx.Cleanup(func() {
		if cancel != nil {
			cancel()
		}
	})
	activeContext := cx.Context()
	cx.Effect(func() {
		if blocked() && cancel != nil {
			cancel()
		}
	})
	c.connect = func(renderer casting.Renderer) {
		if c.busy.GetUntracked() || c.active() || core.Untracked(cx, blocked) || !core.Untracked(cx, ready) {
			return
		}
		c.problem.Set("")
		c.volume.Set(0)
		c.busy.Set(true)
		input := medium.GetUntracked()
		at, wasPaused := position.GetUntracked(), paused.GetUntracked()
		if current.castOnOpen && c.castingStream.URL == "" {
			wasPaused = false
		}
		ctx, stop := context.WithCancel(activeContext)
		cancel = stop
		commands := make(chan castCommand, 1)
		go func() {
			defer stop()
			setup, finish := context.WithTimeout(ctx, 24*time.Second)
			session, stream, err := startRenderer(setup, current.item, input, renderer, at, wasPaused)
			finish()
			if err != nil {
				cx.Post(func() {
					cancel = nil
					c.busy.Set(false)
					if ctx.Err() == nil || !errors.Is(err, context.Canceled) {
						c.problem.Set(castErrorText(cx, err))
					}
				})
				return
			}
			cx.Post(func() {
				if ctx.Err() != nil {
					return
				}
				c.inbox, c.castingStream = commands, stream
				cx.Batch(func() {
					c.receiver.Set(renderer)
					c.busy.Set(false)
					status(casting.Position{At: at, Available: true}, castPlayState(!wasPaused))
				})
			})
			err = c.runRenderer(ctx, cx, session, commands, status)
			session.Close()
			cx.Post(func() {
				cancel = nil
				c.busy.Set(false)
				c.inbox = nil
				if c.active() {
					c.receiver.Set(casting.Renderer{})
					detached()
				}
				if err != nil {
					c.problem.Set(castErrorText(cx, err))
				}
			})
		}()
	}
	c.panel = c.devicePanel(cx, ready, paused.Get)
	return c
}

func (c *playerCast) runRenderer(ctx context.Context, cx *core.Scope, session *casting.Session, commands <-chan castCommand,
	update func(casting.Position, string)) error {
	renderer := session.Renderer
	if renderer.HasVolume() {
		if volume, err := renderer.Volume(ctx); err == nil {
			cx.Post(func() { c.volume.Set(min(100, max(0, volume))) })
		}
	}
	poll := time.NewTicker(5 * time.Second)
	defer poll.Stop()
	missed := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case command := <-commands:
			if command.action == castStop {
				if at, err := session.Position(ctx); err == nil {
					cx.Post(func() { update(at, "") })
				}
				return nil
			}
			c.sendRendererCommand(ctx, cx, renderer, command, update)
		case <-poll.C:
			at, err := session.Position(ctx)
			if errors.Is(err, casting.ErrChanged) {
				return err
			}
			state, statusErr := renderer.Status(ctx)
			if ctx.Err() != nil {
				return nil
			}
			if err := errors.Join(err, statusErr); err != nil {
				missed++
				if missed == 2 {
					return err
				}
				continue
			}
			missed = 0
			if state == "NO_MEDIA_PRESENT" || state == "STOPPED" {
				return nil
			}
			cx.Post(func() { update(at, state) })
		}
	}
}

func (c *playerCast) sendRendererCommand(ctx context.Context, cx *core.Scope, renderer casting.Renderer, command castCommand,
	update func(casting.Position, string)) {
	var err error
	switch command.action {
	case castToggle:
		err = renderer.Play(ctx, command.playing)
	case castSeek:
		err = renderer.Seek(ctx, command.at)
	case castVolume:
		err = renderer.SetVolume(ctx, command.value)
	}
	cx.Post(func() {
		c.busy.Set(false)
		if err != nil {
			c.problem.Set(castErrorText(cx, err))
			return
		}
		switch command.action {
		case castToggle:
			update(casting.Position{}, castPlayState(command.playing))
		case castSeek:
			update(casting.Position{At: command.at.Truncate(time.Second), Available: true}, "")
		case castVolume:
			c.volume.Set(command.value)
		}
	})
}

func castPlayState(playing bool) string {
	if playing {
		return "PLAYING"
	}
	return "PAUSED_PLAYBACK"
}

func castErrorText(cx *core.Scope, err error) string {
	switch {
	case errors.Is(err, casting.ErrFormat):
		return locales.CastFormatUnavailable(cx)
	case errors.Is(err, casting.ErrDevice):
		return locales.CastDeviceUnavailable(cx)
	case errors.Is(err, casting.ErrChanged):
		return locales.CastDeviceChanged(cx)
	default:
		return locales.CastConnectionFailed(cx)
	}
}
