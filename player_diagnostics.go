package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
	"waygu-video/internal/playback/mpv"
	"waygu-video/locales"
)

func (a *application) playerDiagnostics(cx *core.Scope, active func() media.Stream, picture func() core.Size,
	stats func() (mpv.Stats, bool), buffering func() bool) core.Node {
	return core.Show{When: func() bool { return a.diagnosticPrefs.Get().Overlay }, Then: func(cx *core.Scope) core.Node {
		return core.View{Class: "absolute top-16 left-4 max-w-[80%] rounded-lg bg-black/80 px-3 py-2",
			TestID: "player-diagnostics-overlay", PointerEvents: core.PointerEventsNone, Children: []core.Node{
				core.Text{Class: "text-xs leading-relaxed text-white", ContentFunc: func() string {
					stream, size := active(), picture()
					metric, libmpv := stats()
					codec, bitrate := "", int64(0)
					if stream.Method != media.Transcode {
						codec, bitrate = stream.Rendition.VideoCodec, int64(stream.Rendition.Bitrate)
					}
					engine := locales.DesktopPlayerNativeEngine(cx)
					if libmpv {
						engine = "libmpv"
						if metric.VideoCodec != "" {
							codec = metric.VideoCodec
						}
						if metric.Bitrate > 0 {
							bitrate = metric.Bitrate
						}
					}
					rows := []string{locales.DesktopPlayerEngine(cx, engine)}
					if codec != "" {
						rows = append(rows, locales.DesktopPlayerCodec(cx)+" "+strings.ToUpper(codec))
					}
					if size.Width > 0 && size.Height > 0 {
						rows = append(rows, fmt.Sprintf("%d×%d", int(size.Width), int(size.Height)))
					}
					if bitrate > 0 {
						rows = append(rows, locales.AboutOverlayBitrate(cx)+" "+locales.BitrateMegabits(cx,
							strconv.FormatFloat(float64(bitrate)/1e6, 'f', 2, 64)))
					}
					if libmpv {
						rows = append(rows, locales.DesktopPlayerDropped(cx)+" "+strconv.FormatInt(metric.DroppedFrames, 10))
					}
					switch {
					case buffering():
						rows = append(rows, locales.AboutOverlayBuffering(cx))
					case metric.Buffered > 0:
						rows = append(rows, locales.DesktopPlayerBuffer(cx)+" "+playbackClock(metric.Buffered))
					}
					return strings.Join(rows, "\n")
				}},
			}}
	}}
}
