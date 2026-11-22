package tvpair

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPairingPinsCertificateAndAcceptsOneSuccessfulSetup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second*8)
	defer cancel()
	var requests atomic.Int32
	target, err := Listen(ctx, netip.MustParseAddr("127.0.0.1"), func(_ context.Context, data Setup) error {
		requests.Add(1)
		if data.Username != "viewer" || data.Password != "fixture" {
			return ErrRejected
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := Parse(target.Link())
	if err != nil || read.Code() != target.Code() {
		t.Fatalf("the scanned target differs from the TV target: %v", err)
	}
	setup := Setup{Username: "viewer", Password: "fixture"}
	wrong := read
	wrong.fingerprint[0] ^= 255
	if err := wrong.Send(ctx, setup); err == nil || requests.Load() != 0 {
		t.Fatal("an unpinned TLS connection sent credentials")
	}
	wrong = read
	wrong.token[0] ^= 255
	if err := wrong.Send(ctx, setup); err == nil || requests.Load() != 0 {
		t.Fatal("an invalid token changed the setup")
	}
	if _, err := Parse(strings.Replace(read.Link(), "127.0.0.1", "192.0.2.3", 1)); err == nil {
		t.Fatal("accepted a non-local endpoint")
	}
	if err := read.Send(ctx, Setup{Username: "viewer"}); !errors.Is(err, ErrRejected) {
		t.Fatalf("expected failure with invalid details: %v", err)
	}
	if err := read.Send(ctx, setup); err != nil {
		t.Fatal(err)
	}
	if err := read.Send(ctx, setup); err == nil || requests.Load() != 2 {
		t.Fatal("a consumed code accepted another request")
	}
}
