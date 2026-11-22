package main

import (
	"errors"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"waygu-video/internal/media"
)

func TestServerAddressFields(t *testing.T) {
	for _, test := range []struct {
		host, port string
		https      bool
		want       string
	}{
		{"nas.example.test/jellyfin", " 8443 ", true, "https://nas.example.test:8443/jellyfin"},
		{"http://nas.example.test:8096/emby/", "8920", true, "http://nas.example.test:8096/emby"},
		{"[fd00::1]/base%20path", "8096", false, "http://[fd00::1]:8096/base%20path"},
		{"nas.example.test", "", false, "http://nas.example.test"},
		{"nas.example.test", "65536", false, ""},
		{"nas.example.test", "0", false, ""},
		{"https://user:secret@nas.example.test", "", true, ""},
	} {
		address, err := serverAddress(test.host, test.port, test.https)
		if test.want == "" {
			if !errors.Is(err, media.ErrInvalidAddress) {
				t.Errorf("invalid endpoint was accepted: %v", err)
			}
			continue
		}
		if err != nil || address.String() != test.want {
			t.Errorf("address = %v, %v; want %s", address, err, test.want)
		}
		host, port, https := splitServerAddress(test.want)
		roundTrip, err := serverAddress(host, port, https)
		if err != nil || roundTrip.String() != test.want {
			t.Errorf("prefilled address = %v, %v; want %s", roundTrip, err, test.want)
		}
	}
}

func TestServerQRCodeAndPasswordVisibility(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Environment(core.Environment{ScanCode: true}))
	app.Press("id:add-source")
	app.Press("id:choose-emby")
	app.ScanCode = func(nsgotest.CodeScan) (*core.ScannedCode, error) {
		return &core.ScannedCode{Kind: core.CodeQR, Text: "https://nas.example.test:8443/emby"}, nil
	}
	app.Press("id:scan-server-address")
	app.WaitUntil(time.Second, func() bool { return app.Find("id:server-port").Text == "8443" })
	if got := app.Find("id:server-address").Text; got != "nas.example.test/emby" {
		t.Fatalf("scanned address = %q", got)
	}
	if !app.Find("id:server-https").Props.Checked {
		t.Fatal("HTTPS was not enabled for a scanned HTTPS address")
	}
	app.Type("id:server-password", "fixture-password")
	if !app.Find("id:server-password").Input.Masked {
		t.Fatal("password was visible by default")
	}
	app.Press("id:show-server-password")
	if field := app.Find("id:server-password"); field.Input.Masked || field.Text != "fixture-password" {
		t.Fatal("password visibility changed the entered password")
	}
	app.Press("id:show-server-password")
	if !app.Find("id:server-password").Input.Masked {
		t.Fatal("password was not hidden again")
	}
}
