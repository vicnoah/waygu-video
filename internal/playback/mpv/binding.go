//go:build (linux && !android) || (darwin && !ios) || windows

package mpv

import (
	"fmt"
	"image"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"waygu-video/internal/danmaku"
)

type api struct {
	create            func() uintptr
	initialize        func(uintptr) int32
	destroy           func(uintptr)
	setOptionString   func(uintptr, string, string) int32
	setPropertyString func(uintptr, string, string) int32
	getProperty       func(uintptr, string, int32, unsafe.Pointer) int32
	getPropertyString func(uintptr, string) *byte
	free              func(unsafe.Pointer)
	freeNode          func(*propertyNode)
	command           func(uintptr, **byte) int32
	waitEvent         func(uintptr, float64) *event
	createRender      func(*uintptr, uintptr, *renderParam) int32
	freeRender        func(uintptr)
	updateRender      func(uintptr) uint64
	render            func(uintptr, *renderParam) int32
}

type event struct {
	ID, Error int32
	Reply     uint64
	Data      unsafe.Pointer
}

type renderParam struct {
	Kind int32
	Data unsafe.Pointer
}

const (
	formatFlag        = 3
	formatInt64       = 4
	formatDouble      = 5
	eventEndFile      = 7
	eventFileLoaded   = 8
	endEOF            = 0
	endError          = 4
	renderAPIType     = 1
	renderBlock       = 12
	renderSize        = 17
	renderFormat      = 18
	renderStride      = 19
	renderPixels      = 20
	renderUpdateFrame = 1
)

var loadAPI = sync.OnceValues(bind)

func bind() (*api, error) {
	handle, err := openLibrary()
	if err != nil {
		return nil, err
	}
	a := &api{}
	for _, symbol := range []struct {
		name string
		fn   any
	}{
		{"mpv_create", &a.create}, {"mpv_initialize", &a.initialize}, {"mpv_terminate_destroy", &a.destroy},
		{"mpv_set_option_string", &a.setOptionString}, {"mpv_set_property_string", &a.setPropertyString},
		{"mpv_get_property", &a.getProperty}, {"mpv_command", &a.command}, {"mpv_wait_event", &a.waitEvent},
		{"mpv_get_property_string", &a.getPropertyString}, {"mpv_free", &a.free},
		{"mpv_free_node_contents", &a.freeNode},
		{"mpv_render_context_create", &a.createRender}, {"mpv_render_context_free", &a.freeRender},
		{"mpv_render_context_update", &a.updateRender}, {"mpv_render_context_render", &a.render},
	} {
		address, err := symbolAddress(handle, symbol.name)
		if err != nil {
			closeLibrary(handle)
			return nil, fmt.Errorf("%w: missing %s", ErrUnavailable, symbol.name)
		}
		purego.RegisterFunc(symbol.fn, address)
	}
	return a, nil
}

type backend struct {
	api     *api
	movie   uintptr
	surface uintptr
	track   *danmaku.Track
	overlay string
	buffer  []byte
}

func open() (*Player, error) {
	a, err := loadAPI()
	if err != nil {
		return nil, err
	}
	ctx := a.create()
	if ctx == 0 {
		return nil, ErrUnavailable
	}
	p := &Player{backend: backend{api: a, movie: ctx}}
	for _, option := range [][2]string{{"config", "no"}, {"load-scripts", "no"}, {"terminal", "no"},
		{"vo", "libmpv"}, {"hwdec", "no"}, {"idle", "yes"}, {"keep-open", "yes"},
		{"sid", "no"}, {"sub-auto", "no"}} {
		if err := check(a.setOptionString(ctx, option[0], option[1]), option[0]); err != nil {
			p.Close()
			return nil, err
		}
	}
	if err := check(a.initialize(ctx), "initialize"); err != nil {
		p.Close()
		return nil, err
	}
	kind := [3]byte{'s', 'w', 0}
	params := [...]renderParam{{renderAPIType, unsafe.Pointer(&kind[0])}, {}}
	if err := check(a.createRender(&p.surface, ctx, &params[0]), "software renderer"); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func (p *backend) close() {
	if p.surface != 0 {
		p.api.freeRender(p.surface)
		p.surface = 0
	}
	if p.movie != 0 {
		p.api.destroy(p.movie)
		p.movie = 0
	}
	p.buffer = nil
}

func check(code int32, operation string) error {
	if code >= 0 {
		return nil
	}
	return fmt.Errorf("libmpv %s failed (%d)", operation, code)
}

func (p *backend) command(args ...string) error {
	values := make([]*byte, len(args)+1)
	for i, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("libmpv command has a NUL argument")
		}
		data := append([]byte(arg), 0)
		values[i] = &data[0]
	}
	return check(p.api.command(p.movie, &values[0]), args[0])
}

func (p *backend) property(name, value string) error {
	return check(p.api.setPropertyString(p.movie, name, value), name)
}

func (p *backend) seek(position time.Duration) error {
	return p.command("seek", strconv.FormatFloat(position.Seconds(), 'f', 3, 64), "absolute+exact")
}

func (p *backend) poll() Event {
	var events Event
	for {
		e := p.api.waitEvent(p.movie, 0)
		if e == nil || e.ID == 0 {
			return events
		}
		switch e.ID {
		case eventFileLoaded:
			events.Loaded = true
		case eventEndFile:
			if e.Data != nil {
				reason := *(*int32)(e.Data)
				events.Ended = reason == endEOF
				events.Failed = reason == endError
			}
		}
	}
}

func (p *backend) intProperty(name string) int64 {
	var value int64
	_ = p.api.getProperty(p.movie, name, formatInt64, unsafe.Pointer(&value))
	return value
}

func (p *backend) flagProperty(name string) bool {
	var value int32
	_ = p.api.getProperty(p.movie, name, formatFlag, unsafe.Pointer(&value))
	return value != 0
}

func (p *backend) stringProperty(name string) string {
	first := p.api.getPropertyString(p.movie, name)
	if first == nil {
		return ""
	}
	defer p.api.free(unsafe.Pointer(first))
	return readCString(first)
}

func readCString(first *byte) string {
	if first == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(first), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(first, n))
}

func (p *backend) tracks() []Track {
	var tracks []Track
	for i := range p.intProperty("track-list/count") {
		base := "track-list/" + strconv.FormatInt(i, 10) + "/"
		kind := TrackKind(p.stringProperty(base + "type"))
		if kind != AudioTrack && kind != SubtitleTrack {
			continue
		}
		id := p.intProperty(base + "id")
		if id <= 0 {
			continue
		}
		tracks = append(tracks, Track{
			ID: id, Kind: kind, Codec: p.stringProperty(base + "codec"),
			Title: p.stringProperty(base + "title"), Language: p.stringProperty(base + "lang"),
			Selected: p.flagProperty(base + "selected"), Forced: p.flagProperty(base + "forced"),
			External: p.flagProperty(base + "external"),
		})
	}
	return tracks
}

func (p *backend) doubleProperty(name string) float64 {
	var value float64
	_ = p.api.getProperty(p.movie, name, formatDouble, unsafe.Pointer(&value))
	return value
}

func (p *backend) timeProperty(name string) time.Duration {
	return time.Duration(p.doubleProperty(name) * float64(time.Second))
}

func (p *backend) stats() Stats {
	return Stats{Buffered: p.timeProperty("demuxer-cache-duration"), CacheRanges: p.cacheRanges(), FPS: p.doubleProperty("container-fps"),
		Bitrate: int64(p.doubleProperty("video-bitrate")), DownloadRate: p.intProperty("cache-speed"),
		DroppedFrames: p.intProperty("frame-drop-count"), VideoCodec: p.stringProperty("video-codec"),
		Hardware: p.stringProperty("hwdec-current"), AudioOutput: p.stringProperty("current-ao")}
}

func (p *backend) status() Status {
	return Status{Position: p.timeProperty("time-pos"), Duration: p.timeProperty("duration"),
		Width: int(p.intProperty("dwidth")), Height: int(p.intProperty("dheight")),
		Paused: p.flagProperty("pause"), Buffering: p.flagProperty("paused-for-cache"), Ended: p.flagProperty("eof-reached")}
}

func (p *backend) frame(dst *image.RGBA, redraw bool) (bool, error) {
	changed, err := p.updateDanmaku()
	if err != nil {
		return false, err
	}
	redraw = redraw || changed
	if p.api.updateRender(p.surface)&renderUpdateFrame == 0 && !redraw {
		return false, nil
	}
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	if w <= 0 || h <= 0 {
		return false, nil
	}
	stride := uintptr((w*4 + 63) &^ 63)
	size := int(stride) * h
	if len(p.buffer) < size+63 {
		p.buffer = make([]byte, size+63)
	}
	start := int((64 - uintptr(unsafe.Pointer(&p.buffer[0]))%64) % 64)
	pixels := p.buffer[start : start+size]
	dimensions := [2]int32{int32(w), int32(h)}
	format := [...]byte{'r', 'g', 'b', '0', 0}
	var block int32
	params := [...]renderParam{
		{renderSize, unsafe.Pointer(&dimensions[0])}, {renderFormat, unsafe.Pointer(&format[0])},
		{renderStride, unsafe.Pointer(&stride)}, {renderPixels, unsafe.Pointer(&pixels[0])},
		{renderBlock, unsafe.Pointer(&block)}, {},
	}
	if err := check(p.api.render(p.surface, &params[0]), "render frame"); err != nil {
		return false, err
	}
	runtime.KeepAlive(pixels)
	for y := range h {
		row := dst.Pix[y*dst.Stride : y*dst.Stride+w*4]
		copy(row, pixels[y*int(stride):y*int(stride)+w*4])
		for i := 3; i < len(row); i += 4 {
			row[i] = 0xff
		}
	}
	return true, nil
}

func (p *backend) danmaku(track *danmaku.Track) { p.track = track }

func (p *backend) updateDanmaku() (bool, error) {
	data := ""
	if p.track != nil {
		data = p.track.Overlay(p.timeProperty("time-pos"))
	}
	if data == p.overlay {
		return false, nil
	}
	var err error
	if data == "" {
		// libmpv still requires the positional data argument when removing the overlay.
		err = p.command("osd-overlay", "1", "none", "")
	} else {
		err = p.command("osd-overlay", "1", "ass-events", data, strconv.Itoa(p.track.Width), strconv.Itoa(p.track.Height))
	}
	if err != nil {
		return false, err
	}
	p.overlay = data
	return true, nil
}
