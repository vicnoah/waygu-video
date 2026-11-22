package media

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrPlexPINExpired  = errors.New("Plex authorization expired")
	ErrPlexUnreachable = errors.New("could not reach the selected Plex server")
)

type PlexAccount struct {
	device    Device
	pins      api
	resources api
}

type PlexPIN struct {
	URL     string
	id      int
	expires time.Time
}

type plexPINResponse struct {
	ID        int
	Code      string
	ExpiresIn int
	AuthToken string
}

type PlexServer struct {
	ID          string
	Name        string
	token       string
	connections []plexConnection
}

type plexConnection struct {
	URI   string
	Local bool
	Relay bool
}

func NewPlexAccount(device Device) *PlexAccount {
	return &PlexAccount{
		device:    device,
		pins:      newAPI(&url.URL{Scheme: "https", Host: "plex.tv"}),
		resources: newAPI(&url.URL{Scheme: "https", Host: "clients.plex.tv"}),
	}
}

func (a *PlexAccount) Begin(ctx context.Context) (PlexPIN, error) {
	var result plexPINResponse
	headers := plexHeaders(a.device, "")
	params := url.Values{"strong": {"true"}}
	if err := a.pins.request(ctx, http.MethodPost, "/api/v2/pins", params, headers, nil, &result); err != nil {
		return PlexPIN{}, err
	}
	validFor, err := time.ParseDuration(strconv.Itoa(result.ExpiresIn) + "s")
	if result.ID <= 0 || result.Code == "" || validFor <= 0 || err != nil {
		return PlexPIN{}, ErrResponse
	}
	fragment := url.Values{
		"clientID": {a.device.ID}, "code": {result.Code},
		"context[device][product]": {headers.Get("X-Plex-Product")},
	}
	return PlexPIN{
		URL: "https://app.plex.tv/auth#?" + fragment.Encode(),
		id:  result.ID, expires: time.Now().Add(validFor),
	}, nil
}

func (a *PlexAccount) Servers(ctx context.Context, pin PlexPIN) ([]PlexServer, error) {
	token, err := a.awaitToken(ctx, pin)
	if err != nil {
		return nil, err
	}
	var resources []struct {
		ClientIdentifier, Name, Provides, AccessToken string
		Connections                                   []plexConnection
	}
	params := url.Values{"includeHttps": {"1"}, "includeRelay": {"1"}, "includeIPv6": {"1"}}
	if err := a.resources.request(ctx, http.MethodGet, "/api/v2/resources", params, plexHeaders(a.device, token), nil, &resources); err != nil {
		return nil, err
	}
	servers := make([]PlexServer, 0, len(resources))
	seen := make(map[string]bool)
	for _, resource := range resources {
		if resource.ClientIdentifier == "" || seen[resource.ClientIdentifier] || resource.AccessToken == "" || len(resource.Connections) == 0 {
			continue
		}
		for service := range strings.SplitSeq(resource.Provides, ",") {
			if strings.TrimSpace(service) != "server" {
				continue
			}
			name := strings.TrimSpace(resource.Name)
			if name == "" {
				name = "Plex"
			}
			servers = append(servers, PlexServer{ID: resource.ClientIdentifier, Name: name, token: resource.AccessToken, connections: resource.Connections})
			seen[resource.ClientIdentifier] = true
			break
		}
	}
	sort.SliceStable(servers, func(i, j int) bool {
		return strings.ToLower(servers[i].Name) < strings.ToLower(servers[j].Name)
	})
	return servers, nil
}

func (a *PlexAccount) awaitToken(ctx context.Context, pin PlexPIN) (string, error) {
	poll, cancel := context.WithDeadline(ctx, pin.expires)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	route := "/api/v2/pins/" + strconv.Itoa(pin.id)
	headers := plexHeaders(a.device, "")
	for poll.Err() == nil {
		var result plexPINResponse
		err := a.pins.request(poll, http.MethodGet, route, nil, headers, nil, &result)
		switch {
		case errors.Is(err, StatusError(http.StatusNotFound)), errors.Is(err, StatusError(http.StatusGone)):
			return "", ErrPlexPINExpired
		case err != nil && poll.Err() == nil:
			return "", err
		case result.AuthToken != "":
			return result.AuthToken, nil
		}
		select {
		case <-poll.Done():
		case <-ticker.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", ErrPlexPINExpired
}

type PlexServerConnection struct {
	Address   string
	Info      ServerInfo
	Session   Session
	Libraries []Library
}

// Connect checks the chosen server's identity before sending its Plex token.
func (a *PlexAccount) Connect(ctx context.Context, server PlexServer) (PlexServerConnection, error) {
	if server.ID == "" || server.token == "" {
		return PlexServerConnection{}, ErrAuthentication
	}
	type option struct {
		base *url.URL
		rank int
	}
	options := make([]option, 0, len(server.connections))
	for _, connection := range server.connections {
		base, err := NormalizeAddress(connection.URI)
		if err != nil {
			continue
		}
		ip, _ := netip.ParseAddr(base.Hostname())
		if base.Scheme == "http" && (!connection.Local || connection.Relay || !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())) {
			continue
		}
		rank := 0
		switch {
		case connection.Relay:
			rank = 3
		case !connection.Local:
			rank = 2
		case base.Scheme == "http":
			rank = 1
		}
		options = append(options, option{base: base, rank: rank})
	}
	sort.SliceStable(options, func(i, j int) bool { return options[i].rank < options[j].rank })
	session := Session{Token: server.token}
	failure := ErrPlexUnreachable
	for _, option := range options {
		client := plexClient{api: newAPI(option.base), device: a.device}
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		info, err := client.identity(attempt)
		if err == nil && info.ID == server.ID {
			client.session = session
			libraries, err := client.Libraries(attempt)
			cancel()
			if err == nil {
				info.Name = server.Name
				return PlexServerConnection{Address: option.base.String(), Info: info, Session: session, Libraries: libraries}, nil
			}
			if errors.Is(err, ErrAuthentication) {
				failure = ErrAuthentication
			}
		} else {
			cancel()
		}
		if ctx.Err() != nil {
			break
		}
	}
	return PlexServerConnection{}, failure
}
