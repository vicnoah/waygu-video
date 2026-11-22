package casting

import (
	"context"
	"net/http"
	"time"

	"waygu-video/internal/mediatls"
	"waygu-video/internal/playback"
)

type Stream struct {
	URL     string
	Headers http.Header
	MIME    string
	Trust   mediatls.Trust
}

type Session struct {
	Renderer Renderer
	address  string
	release  func()
}

// Start hands the receiver its scoped URL and starts playback at the
// sender's bookmark. A failed handoff leaves the local player in control.
func Start(ctx context.Context, renderer Renderer, source Stream, title string, at time.Duration, paused bool) (*Session, error) {
	address, release, err := playback.OpenForRenderer(source.URL, source.Headers, renderer.Address, source.MIME, source.Trust)
	if err != nil {
		return nil, err
	}
	if err := renderer.Load(ctx, address, source.MIME, title); err != nil {
		release()
		return nil, err
	}
	session := &Session{Renderer: renderer, address: address, release: release}
	if err := session.begin(ctx, at, paused); err != nil {
		session.Close()
		return nil, err
	}
	return session, nil
}

func (s *Session) begin(ctx context.Context, at time.Duration, paused bool) error {
	if err := s.Renderer.Play(ctx, true); err != nil {
		return err
	}
	if at >= time.Second {
		for attempt := 0; ; attempt++ {
			if err := s.Renderer.Seek(ctx, at); err == nil {
				break
			} else if attempt >= 3 || ctx.Err() != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(280 * time.Millisecond):
			}
		}
	}
	if paused {
		return s.Renderer.Play(ctx, false)
	}
	return nil
}

func (s *Session) Position(ctx context.Context) (Position, error) {
	position, err := s.Renderer.Position(ctx)
	if err == nil && position.URI != "" && position.URI != s.address {
		return Position{}, ErrChanged
	}
	return position, err
}

// Close revokes the stream and stops playback if the renderer still owns it.
func (s *Session) Close() {
	defer s.release()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := s.Position(ctx); err == nil {
		_ = s.Renderer.Stop(ctx)
	}
}
