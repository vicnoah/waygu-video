package media

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

var ErrQuickConnectUnavailable = errors.New("Jellyfin Quick Connect is unavailable")

// QuickConnectCode represents one pending Jellyfin device authorization.
type QuickConnectCode struct {
	Code   string
	secret string
}

type QuickConnectClient interface {
	BeginQuickConnect(context.Context) (QuickConnectCode, error)
	CheckQuickConnect(context.Context, QuickConnectCode) (Session, bool, error)
}

func (m *mediaBrowser) BeginQuickConnect(ctx context.Context) (QuickConnectCode, error) {
	if m.kind != Jellyfin {
		return QuickConnectCode{}, ErrQuickConnectUnavailable
	}
	var enabled bool
	if err := m.get(ctx, "QuickConnect/Enabled", nil, &enabled); err != nil {
		if errors.Is(err, StatusError(http.StatusNotFound)) {
			return QuickConnectCode{}, ErrQuickConnectUnavailable
		}
		return QuickConnectCode{}, err
	}
	if !enabled {
		return QuickConnectCode{}, ErrQuickConnectUnavailable
	}
	var data struct{ Code, Secret string }
	err := m.post(ctx, "QuickConnect/Initiate", nil, nil, &data)
	if errors.Is(err, StatusError(http.StatusMethodNotAllowed)) || errors.Is(err, StatusError(http.StatusNotFound)) {
		err = m.get(ctx, "QuickConnect/Initiate", nil, &data)
	}
	if err != nil {
		return QuickConnectCode{}, err
	}
	if data.Code == "" || data.Secret == "" {
		return QuickConnectCode{}, ErrResponse
	}
	return QuickConnectCode{Code: data.Code, secret: data.Secret}, nil
}

func (m *mediaBrowser) CheckQuickConnect(ctx context.Context, code QuickConnectCode) (Session, bool, error) {
	if m.kind != Jellyfin || code.secret == "" {
		return Session{}, false, ErrQuickConnectUnavailable
	}
	var state struct{ Authenticated bool }
	query := url.Values{"secret": {code.secret}}
	if err := m.get(ctx, "QuickConnect/Connect", query, &state); err != nil || !state.Authenticated {
		return Session{}, false, err
	}
	var login browserLogin
	body := struct{ Secret string }{code.secret}
	if err := m.post(ctx, "Users/AuthenticateWithQuickConnect", nil, body, &login); err != nil {
		return Session{}, false, err
	}
	session, err := login.session()
	return session, err == nil, err
}
