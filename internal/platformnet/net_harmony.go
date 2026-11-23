//go:build js && wasm && nsgoharmony

package platformnet

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall/js"
	"time"
)

const chunkSize = 32 << 10

var (
	native    = js.Global().Get("nsgoHarmony")
	sockets   sync.Map
	listeners sync.Map
	nextID    atomic.Int32
	onEvent   js.Func
)

func init() {
	onEvent = js.FuncOf(func(_ js.Value, args []js.Value) any {
		event(args[0].String(), args[1].Int(), args[2].Int(), args[3].String())
		return nil
	})
	js.Global().Set("wayguTCPEvent", onEvent)
	DialContext, Listen = dial, listen
	DialTLSContext = dialTLS
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dial
	transport.DialTLSContext = dialTLS
	http.DefaultTransport = transport
}

func call(method string, args ...any) bool {
	return native.Type() == js.TypeObject && native.Get(method).Type() == js.TypeFunction &&
		native.Call(method, args...).Bool()
}

func event(kind string, id, number int, data string) {
	if kind == "verify" {
		if reply, ok := verifications.LoadAndDelete(id); ok {
			reply.(chan int) <- number
		}
		return
	}
	if listener, ok := listeners.Load(id); ok {
		listener.(*tcpListener).event(kind, number, data)
		return
	}
	if kind == "accept" {
		call("wayguTCPClose", number)
		return
	}
	if socket, ok := sockets.Load(id); ok {
		socket.(*tcpConn).event(kind, number, data)
	}
}

type tcpAddr string

func (a tcpAddr) Network() string { return "tcp" }
func (a tcpAddr) String() string  { return string(a) }

func dial(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, net.UnknownNetworkError(network)
	}
	host, text, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(text)
	if err != nil || host == "" || port < 1 || port > 65535 {
		return nil, &net.AddrError{Err: "invalid TCP address", Addr: address}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c := newConn(int(nextID.Add(1)), "", address)
	if !call("wayguTCPDial", c.id, host, port, network) {
		_ = c.Close()
		return nil, errors.New("HarmonyOS TCP transport is unavailable")
	}
	if err := c.connected(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

type tcpConn struct {
	id            int
	local, remote tcpAddr
	mu            sync.Mutex
	writeMu       sync.Mutex
	changed       chan struct{}
	ready         bool
	pulling       bool
	sending       bool
	buf           []byte
	terminal      error
	readDeadline  time.Time
	writeDeadline time.Time
}

func newConn(id int, local, remote string) *tcpConn {
	c := &tcpConn{id: id, local: tcpAddr(local), remote: tcpAddr(remote), changed: make(chan struct{})}
	sockets.Store(id, c)
	return c
}

func (c *tcpConn) wake() {
	close(c.changed)
	c.changed = make(chan struct{})
}

func (c *tcpConn) event(kind string, number int, data string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal != nil {
		return
	}
	switch kind {
	case "ready":
		c.ready = true
		c.local = tcpAddr(data)
	case "chunk":
		c.pulling = false
		var err error
		c.buf, err = base64.StdEncoding.DecodeString(data)
		if err != nil || len(c.buf) == 0 || len(c.buf) > chunkSize {
			c.buf = nil
			c.terminal = errors.New("invalid HarmonyOS TCP data")
		}
	case "sent":
		c.sending = false
	case "eof":
		c.terminal = io.EOF
	case "error":
		c.terminal = fmt.Errorf("HarmonyOS TCP socket error %d", number)
	default:
		return
	}
	c.wake()
}

func (c *tcpConn) connected(ctx context.Context) error {
	for {
		c.mu.Lock()
		ready, terminal, change := c.ready, c.terminal, c.changed
		c.mu.Unlock()
		if terminal != nil || ready {
			return terminal
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-change:
		}
	}
}

func wait(changed <-chan struct{}, deadline time.Time) error {
	if deadline.IsZero() {
		<-changed
		return nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return os.ErrDeadlineExceeded
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-changed:
		return nil
	case <-timer.C:
		return os.ErrDeadlineExceeded
	}
}

func (c *tcpConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		c.mu.Lock()
		if !c.readDeadline.IsZero() && !time.Now().Before(c.readDeadline) {
			c.mu.Unlock()
			return 0, os.ErrDeadlineExceeded
		}
		if len(c.buf) != 0 {
			n := copy(p, c.buf)
			c.buf = c.buf[n:]
			c.mu.Unlock()
			return n, nil
		}
		if err := c.terminal; err != nil {
			c.mu.Unlock()
			return 0, err
		}
		pull, change, deadline := !c.pulling, c.changed, c.readDeadline
		c.pulling = true
		c.mu.Unlock()
		if pull && !call("wayguTCPPull", c.id) {
			_ = c.Close()
			return 0, net.ErrClosed
		}
		if err := wait(change, deadline); err != nil {
			return 0, err
		}
	}
}

func (c *tcpConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	n := 0
	for len(p) != 0 {
		c.mu.Lock()
		if err := c.terminal; err != nil {
			c.mu.Unlock()
			return n, err
		}
		if !c.writeDeadline.IsZero() && !time.Now().Before(c.writeDeadline) {
			c.mu.Unlock()
			return n, os.ErrDeadlineExceeded
		}
		c.sending = true
		c.mu.Unlock()
		part := p[:min(chunkSize, len(p))]
		if !call("wayguTCPSend", c.id, base64.StdEncoding.EncodeToString(part)) {
			_ = c.Close()
			return n, net.ErrClosed
		}
		for {
			c.mu.Lock()
			done, err, change, deadline := !c.sending, c.terminal, c.changed, c.writeDeadline
			c.mu.Unlock()
			if done {
				break
			}
			if err != nil {
				return n, err
			}
			if err := wait(change, deadline); err != nil {
				_ = c.Close()
				return n, err
			}
		}
		n += len(part)
		p = p[len(part):]
	}
	return n, nil
}

func (c *tcpConn) Close() error {
	if _, ok := sockets.LoadAndDelete(c.id); ok {
		c.mu.Lock()
		c.terminal = net.ErrClosed
		c.wake()
		c.mu.Unlock()
		call("wayguTCPClose", c.id)
	}
	return nil
}

func (c *tcpConn) LocalAddr() net.Addr  { return c.local }
func (c *tcpConn) RemoteAddr() net.Addr { return c.remote }

func (c *tcpConn) SetDeadline(t time.Time) error      { return c.deadline(t, true, true) }
func (c *tcpConn) SetReadDeadline(t time.Time) error  { return c.deadline(t, true, false) }
func (c *tcpConn) SetWriteDeadline(t time.Time) error { return c.deadline(t, false, true) }

func (c *tcpConn) deadline(t time.Time, read, write bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if read {
		c.readDeadline = t
	}
	if write {
		c.writeDeadline = t
	}
	c.wake()
	return nil
}

type tcpListener struct {
	id       int
	mu       sync.Mutex
	addr     tcpAddr
	ready    bool
	terminal error
	changed  chan struct{}
	pending  []*tcpConn
}

func listen(network, address string) (net.Listener, error) {
	if network != "tcp" || address != "127.0.0.1:0" {
		return nil, &net.AddrError{Err: "HarmonyOS playback listens on the local device only", Addr: address}
	}
	l := &tcpListener{id: int(nextID.Add(1)), changed: make(chan struct{})}
	listeners.Store(l.id, l)
	if !call("wayguTCPListen", l.id) {
		_ = l.Close()
		return nil, errors.New("HarmonyOS TCP listener is unavailable")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		l.mu.Lock()
		ready, err, change := l.ready, l.terminal, l.changed
		l.mu.Unlock()
		if err != nil || ready {
			if err != nil {
				_ = l.Close()
				return nil, err
			}
			return l, nil
		}
		if err := wait(change, deadline); err != nil {
			_ = l.Close()
			return nil, err
		}
	}
}

func (l *tcpListener) wake() {
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *tcpListener) event(kind string, number int, data string) {
	l.mu.Lock()
	if kind == "accept" && (l.terminal != nil || number >= 0 || len(l.pending) >= 32) {
		l.mu.Unlock()
		call("wayguTCPClose", number)
		return
	}
	switch kind {
	case "ready":
		l.addr = tcpAddr(net.JoinHostPort("127.0.0.1", strconv.Itoa(number)))
		l.ready = true
	case "accept":
		l.pending = append(l.pending, newConn(number, string(l.addr), data))
	case "error":
		l.terminal = fmt.Errorf("HarmonyOS TCP listener error %d", number)
	}
	l.wake()
	l.mu.Unlock()
}

func (l *tcpListener) Accept() (net.Conn, error) {
	for {
		l.mu.Lock()
		if len(l.pending) != 0 {
			c := l.pending[0]
			l.pending[0] = nil
			l.pending = l.pending[1:]
			l.mu.Unlock()
			return c, nil
		}
		err, change := l.terminal, l.changed
		l.mu.Unlock()
		if err != nil {
			return nil, err
		}
		<-change
	}
}

func (l *tcpListener) Close() error {
	if _, ok := listeners.LoadAndDelete(l.id); ok {
		l.mu.Lock()
		l.terminal = net.ErrClosed
		pending := l.pending
		l.pending = nil
		l.wake()
		l.mu.Unlock()
		call("wayguTCPClose", l.id)
		for _, c := range pending {
			_ = c.Close()
		}
	}
	return nil
}

func (l *tcpListener) Addr() net.Addr { return l.addr }
