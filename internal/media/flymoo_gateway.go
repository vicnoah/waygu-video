package media

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// ErrFlymooGateway marks a public gateway change that cannot be used for login.
var ErrFlymooGateway = errors.New("the Flymoo gateway could not be verified")

type FlymooGatewayRedirect struct {
	Address string
}

func (*FlymooGatewayRedirect) Error() string { return "the Flymoo public gateway moved" }

// FlymooGatewayProber reports a new gateway without following its redirect.
// The caller can configure a new client and verify the new endpoint separately.
type FlymooGatewayProber interface {
	ProbeFlymooGateway(context.Context) (ServerInfo, error)
}

func (f *flymooClient) ProbeFlymooGateway(ctx context.Context) (ServerInfo, error) {
	probe := *f
	client := *f.client
	client.CheckRedirect = func(next *http.Request, previous []*http.Request) error {
		const route = "/api/v1/sys/config"
		if !strings.HasSuffix(previous[0].URL.Path, route) {
			return f.client.CheckRedirect(next, previous)
		}
		target := next.URL
		if !isHTTP(target) || target.User != nil || target.Fragment != "" || target.RawQuery != "" || target.ForceQuery ||
			!strings.EqualFold(target.Hostname(), f.base.Hostname()) || (f.base.Scheme == "https" && target.Scheme != "https") {
			return ErrFlymooGateway
		}
		prefix, ok := strings.CutSuffix(target.Path, route)
		rawPrefix, rawOK := strings.CutSuffix(target.EscapedPath(), route)
		if !ok || !rawOK {
			return ErrFlymooGateway
		}
		address := *target
		address.Path, address.RawPath = prefix, rawPrefix
		return &FlymooGatewayRedirect{Address: address.String()}
	}
	probe.client = &client
	return probe.Probe(ctx)
}
