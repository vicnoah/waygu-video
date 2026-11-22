package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/casting"
	"waygu-video/internal/media"
)

type testRenderer struct {
	server     *httptest.Server
	mu         sync.Mutex
	uri, state string
	at         time.Duration
	volume     int
	format     string
}

func newTestRenderer(t *testing.T, format string) *testRenderer {
	t.Helper()
	r := &testRenderer{state: "STOPPED", volume: 35, format: format}
	r.server = httptest.NewServer(http.HandlerFunc(r.request))
	t.Cleanup(r.server.Close)
	return r
}

func (r *testRenderer) request(w http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/description.xml" {
		io.WriteString(w, `<root xmlns="urn:schemas-upnp-org:device-1-0"><device><deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
<friendlyName>Waygu Test TV</friendlyName><UDN>uuid:waygu-test-tv</UDN><serviceList>
<service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/av</controlURL></service>
<service><serviceType>urn:schemas-upnp-org:service:ConnectionManager:1</serviceType><controlURL>/cm</controlURL></service>
<service><serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType><controlURL>/rc</controlURL></service>
</serviceList></device></root>`)
		return
	}
	header := strings.Trim(request.Header.Get("SOAPAction"), "\"")
	service, action, ok := strings.Cut(header, "#")
	if request.Method != http.MethodPost || !ok {
		http.NotFound(w, request)
		return
	}
	var message struct {
		Body struct {
			Action struct {
				Instance string `xml:"InstanceID"`
				URI      string `xml:"CurrentURI"`
				Metadata string `xml:"CurrentURIMetaData"`
				Target   string `xml:"Target"`
				Volume   int    `xml:"DesiredVolume"`
			} `xml:",any"`
		} `xml:"Body"`
	}
	if xml.NewDecoder(request.Body).Decode(&message) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	args := message.Body.Action
	r.mu.Lock()
	defer r.mu.Unlock()
	var value string
	switch action {
	case "GetProtocolInfo":
		value = "<Sink>http-get:*:" + r.format + ":*</Sink>"
	case "SetAVTransportURI":
		if args.Instance != "0" || !strings.Contains(args.Metadata, args.URI) || !strings.Contains(args.Metadata, r.format) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.uri, r.at = args.URI, 0
	case "Play":
		r.state = "PLAYING"
	case "Pause":
		r.state = "PAUSED_PLAYBACK"
	case "Stop":
		r.state = "STOPPED"
	case "Seek":
		if parsed, err := time.Parse("15:04:05", args.Target); err == nil {
			r.at = time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute + time.Duration(parsed.Second())*time.Second
		}
	case "GetPositionInfo":
		value = fmt.Sprintf("<TrackURI>%s</TrackURI><RelTime>%s</RelTime><TrackDuration>00:00:24</TrackDuration>", r.uri, landscapeClock(r.at))
	case "GetTransportInfo":
		value = "<CurrentTransportState>" + r.state + "</CurrentTransportState><CurrentTransportStatus>OK</CurrentTransportStatus>"
	case "GetVolume":
		value = "<CurrentVolume>" + strconv.Itoa(r.volume) + "</CurrentVolume>"
	case "SetVolume":
		r.volume = args.Volume
	default:
		http.NotFound(w, request)
		return
	}
	w.Header().Set("Content-Type", "text/xml")
	fmt.Fprintf(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body></s:Envelope>`,
		action, service, value, action)
}

func (r *testRenderer) snapshot() (string, string, time.Duration, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.uri, r.state, r.at, r.volume
}

func decodeRendererFrame(t *testing.T, address string) {
	t.Helper()
	if strings.ContainsAny(address, "?&") {
		t.Fatal("receiver was given upstream parameters")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", address, "-frames:v", "1", "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("receiver could not decode its proxied video: %v: %s", err, out)
	}
}

func TestDockerPhoneDLNACasting(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires Docker fixture media")
	}
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("loopback receiver runs alongside the Linux native test host")
	}
	box := newTestRenderer(t, "video/mp4")
	config, secrets := homeReviewSources(t)
	_, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	height := core.Points(800)
	if target == "linux" {
		height += 37
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.Size(390, height), nsgotest.Storage(config), nsgotest.Secrets(secrets),
		nsgotest.Environment(core.Environment{Locale: "zh-Hans"}))
	app.Press("id:tab-search")
	app.Type("id:search-query", "Waygu Track Test")
	app.Press("id:search-source-emby")
	card := "id:title-emby-" + movie.ID
	app.WaitFor(card, 10*time.Second)
	app.Press(card)
	app.WaitFor("id:detail-cast-device", 5*time.Second)
	app.Press("id:detail-cast-device")
	app.Resize(844, 390)
	app.WaitFor("id:video-player", 10*time.Second)
	if target == "" {
		app.Play("id:video-player", core.VideoInfo{Duration: 24 * time.Second})
	}
	app.WaitFor("id:cast-panel", time.Second)
	if target == "linux" {
		const empty = "text:当前网络未发现设备，请确认设备已开机且与本机在同一 Wi-Fi。"
		app.WaitUntil(6*time.Second, func() bool { return len(app.Query(empty)) > 0 || len(app.Query("id:cast-device-0")) > 0 })
		if len(app.Query(empty)) > 0 {
			app.Screenshot("cast-no-devices")
		}
	}
	app.Type("id:cast-manual-address", box.server.URL+"/description.xml")
	app.Press("id:cast-lookup")
	const receiver = "role:button Waygu Test TV"
	app.WaitUntil(6*time.Second, func() bool { return len(app.Query(receiver)) == 1 && app.Enabled(receiver) })
	if target == "linux" {
		app.Screenshot("cast-devices")
	}
	app.Press(receiver)
	app.WaitFor("id:cast-connected", 8*time.Second)
	address, state, _, _ := box.snapshot()
	if state != "PLAYING" || address == "" {
		t.Fatal("receiver did not start playback")
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=0-31")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusPartialContent {
		t.Fatalf("receiver byte range returned %s", response.Status)
	}
	decodeRendererFrame(t, address)
	if target == "linux" {
		app.Screenshot("cast-connected")
	}
	app.Press("id:cast-transport")
	app.WaitUntil(3*time.Second, func() bool { _, state, _, _ := box.snapshot(); return state == "PAUSED_PLAYBACK" })
	app.WaitUntil(3*time.Second, func() bool { return app.Enabled("id:cast-transport") })
	app.Press("id:cast-transport")
	app.WaitUntil(3*time.Second, func() bool { _, state, _, _ := box.snapshot(); return state == "PLAYING" })
	app.WaitUntil(3*time.Second, func() bool { return app.Enabled("id:cast-volume-up") })
	app.Press("id:cast-volume-up")
	app.WaitUntil(3*time.Second, func() bool { _, _, _, volume := box.snapshot(); return volume == 40 })
	app.Press("id:cast-close")
	app.WaitFor("id:phone-player-cast", time.Second)
	app.Press("id:player-seek-forward")
	app.WaitUntil(3*time.Second, func() bool { _, _, at, _ := box.snapshot(); return at == 24*time.Second })
	box.mu.Lock()
	box.at = 12 * time.Second
	box.mu.Unlock()
	app.WaitUntil(8*time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:12" })
	app.Press("id:phone-player-cast")
	app.Press("id:cast-disconnect")
	app.WaitForGone("id:cast-connected", 5*time.Second)
	_, stopped, _, _ := box.snapshot()
	if stopped != "STOPPED" {
		t.Fatal("disconnect did not stop the receiver")
	}
	if target == "" {
		decoder := app.FindMounted("id:video-player")
		if decoder.Video.Paused || len(decoder.Seeks) == 0 || decoder.Seeks[len(decoder.Seeks)-1] != 12*time.Second {
			t.Fatalf("local video did not resume at the receiver position: %v", decoder.Seeks)
		}
	}
	if response, err := http.Get(address); err == nil {
		response.Body.Close()
		t.Fatal("the receiver URL remained available after disconnect")
	}
	app.Press("id:cast-close")
	if target == "linux" {
		app.WaitUntil(8*time.Second, func() bool {
			return app.Find("id:player-position").Text >= "00:00:13"
		})
	}
	app.Press("id:close-player")
}

func TestDockerDLNAUsesServerTranscode(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires Docker fixture media")
	}
	config, secrets := homeReviewSources(t)
	for _, id := range []string{"jellyfin", "emby", "plex"} {
		t.Run(id, func(t *testing.T) {
			box := newTestRenderer(t, "application/vnd.apple.mpegurl")
			devices, err := casting.Resolve(t.Context(), box.server.URL+"/description.xml")
			if err != nil || len(devices) != 1 {
				t.Fatalf("resolve renderer: %v", err)
			}
			client, movie := reviewFixtureItem(t, config, secrets, id, "Waygu Test Movie")
			stream, err := client.Playback(t.Context(), movie, media.PlaybackOptions{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			session, actual, err := startRenderer(ctx, movie, playerMedia{client: client, stream: stream}, devices[0], 0, false)
			if err != nil || actual.Method != media.Transcode {
				t.Fatalf("negotiate supported transcode: %s, %v", actual.Method, err)
			}
			defer session.Close()
			address, _, _, _ := box.snapshot()
			decodeRendererFrame(t, address)
		})
	}
}

func TestDockerDLNAExternalRenderer(t *testing.T) {
	name := os.Getenv("WAYGU_TEST_DLNA_RENDERER")
	if name == "" || os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires a dedicated DLNA receiver and Docker fixture media")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	var device casting.Renderer
	for _, candidate := range casting.Discover(ctx) {
		if candidate.Name == name {
			device = candidate
			break
		}
	}
	if !device.Address.IsValid() {
		t.Fatal("the local DLNA receiver did not answer multicast discovery")
	}
	config, secrets := homeReviewSources(t)
	client, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Test Movie")
	stream, err := client.Playback(ctx, movie, media.PlaybackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	session, selected, err := startRenderer(ctx, movie, playerMedia{client: client, stream: stream}, device, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	t.Logf("receiver negotiated %s", selected.Method)
	waitAt := func(minimum time.Duration) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for {
			at, err := session.Position(ctx)
			state, stateErr := device.Status(ctx)
			if err == nil && stateErr == nil && state == "PLAYING" && at.Available && at.At >= minimum {
				return
			}
			if !time.Now().Before(deadline) {
				t.Fatalf("receiver trace at %v: position %v, state %s, errors %v and %v", minimum, at.At, state, err, stateErr)
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	waitAt(time.Second)
	if err := device.Seek(ctx, 8*time.Second); err != nil {
		t.Fatal(err)
	}
	waitAt(8 * time.Second)
	if err := device.Play(ctx, false); err != nil {
		t.Fatal(err)
	}
	if state, err := device.Status(ctx); err != nil || state != "PAUSED_PLAYBACK" {
		t.Fatalf("receiver pause state: %q, %v", state, err)
	}
	if err := device.SetVolume(ctx, 30); err != nil {
		t.Fatal(err)
	}
	if volume, err := device.Volume(ctx); err != nil || volume != 30 {
		t.Fatalf("receiver volume: %d, %v", volume, err)
	}
}
