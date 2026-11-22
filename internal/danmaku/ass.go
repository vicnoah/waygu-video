package danmaku

import (
	"fmt"
	"strings"
	"time"
)

// Overlay returns the ASS event text for libmpv's osd-overlay command at the
// current video time. The normal subtitle track stays under this overlay.
func (t Track) Overlay(at time.Duration) string {
	var data strings.Builder
	alpha := uint8((1 - t.Opacity) * 255)
	for _, event := range t.Visible(at) {
		bgr := (event.Color&0xff)<<16 | event.Color&0xff00 | (event.Color>>16)&0xff
		alignment := 8
		if event.Mode == Scroll {
			alignment = 7
		}
		fmt.Fprintf(&data, `{\an%d\pos(%.1f,%d)\fs%d\b1\bord1.5\shad0\alpha&H%02X&\1c&H%06X&}%s`,
			alignment, event.X(at), event.Y, t.FontSize, alpha, bgr, assTextEscapes.Replace(event.Text))
		data.WriteByte('\n')
	}
	return strings.TrimSuffix(data.String(), "\n")
}

var assTextEscapes = strings.NewReplacer("{", "｛", "}", "｝", "\\", "＼", "\r", " ", "\n", " ", "\x00", "")
