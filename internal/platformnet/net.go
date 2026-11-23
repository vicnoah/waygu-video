package platformnet

import (
	"context"
	"net"
)

var (
	DialContext    func(context.Context, string, string) (net.Conn, error)
	DialTLSContext func(context.Context, string, string) (net.Conn, error)
	Listen         = net.Listen
)
