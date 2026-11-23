package danmaku

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type LogvarConfig struct {
	Address string
	Token   string
}

var (
	ErrLogvarConfig         = errors.New("invalid custom danmaku server or password")
	ErrLogvarRequest        = errors.New("custom danmaku server request failed")
	ErrLogvarAuthentication = errors.New("custom danmaku server rejected the password")
)

// NewLogvarClient connects to a self-hosted DandanPlay-compatible Logvar
// service. Logvar carries an optional password in the request path.
func NewLogvarClient(config LogvarConfig) (*DandanClient, error) {
	address := strings.TrimSpace(config.Address)
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	base, err := url.Parse(address)
	token := config.Token
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" ||
		base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" ||
		token == "." || token == ".." || strings.ContainsAny(token, "/\\") ||
		strings.IndexFunc(token, unicode.IsControl) >= 0 {
		return nil, ErrLogvarConfig
	}
	path := strings.TrimRight(base.String(), "/")
	if token != "" {
		path += "/" + url.PathEscape(token)
	}
	return &DandanClient{logvar: &logvarEndpoint{path: path},
		http: http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}}, nil
}

type logvarEndpoint struct {
	path string
}

func (c *DandanClient) cacheLocation(episode int64) (string, string) {
	if c.logvar != nil {
		return "logvar-danmu", "v2:" + c.logvar.path + ":" + strconv.FormatInt(episode, 10)
	}
	return "dandanplay", "v2:" + c.credentials.AppID + ":" + strconv.FormatInt(episode, 10)
}
