package danmaku

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestParseFilterScheduleAndSeek(t *testing.T) {
	comments, err := ReadXML(strings.NewReader(`<i>
<d p="2,1,25,16777215">弹幕 {\pos(0,0)}</d>
<d p="2,5,25,16777215">top</d>
<d p="2,4,25,16777215">bottom</d>
<d p="2,5,25,16711680">color</d>
<d p="2,5,25,16777215">blocked</d>
<d p="2,1,25,16777215">bot-23</d>
<d p="3.25,1,25,16777215">弹幕 {\pos(0,0)}</d>
<d p="3.75,9,25,16777215">advanced effect</d>
</i>`))
	if err != nil || len(comments) != 7 {
		t.Fatalf("XML returned %d entries, %v", len(comments), err)
	}
	settings := DefaultSettings()
	settings.HideColored = true
	settings.Keywords = []string{"BLOCKED"}
	settings.Patterns = []*regexp.Regexp{regexp.MustCompile(`^bot-\d+$`)}
	settings.Offset = time.Second
	track := Schedule(comments, settings)
	if len(track.Events) != 3 || len(track.Visible(2999*time.Millisecond)) != 0 {
		t.Fatalf("unexpected schedule: %+v", track.Events)
	}
	visible := track.Visible(4 * time.Second)
	if len(visible) != 3 || visible[0].X(4*time.Second) >= float64(track.Width) || visible[0].Y == visible[1].Y {
		t.Fatalf("unexpected comments after seek: %+v", visible)
	}
	if visible[2].Y <= visible[1].Y || visible[2].Y >= track.Height*4/5 {
		t.Fatalf("bottom comment lane: %d", visible[2].Y)
	}
	display := track.Overlay(4 * time.Second)
	if !strings.Contains(display, `\an7\pos(`) || !strings.Contains(display, `｛＼pos(0,0)｝`) || len(track.Visible(8*time.Second)) != 1 {
		t.Fatalf("unexpected overlay: %q", display)
	}
}

func TestDisplaySettingsAndDuplicatesReachASS(t *testing.T) {
	comments := []Comment{
		{At: time.Second, Mode: Scroll, Color: White, Text: "Hello"},
		{At: 1100 * time.Millisecond, Mode: Top, Color: White, Text: "hello"},
		{At: 1200 * time.Millisecond, Mode: Bottom, Color: White, Text: "Bottom"},
		{At: 1300 * time.Millisecond, Mode: Bottom, Color: White, Text: "Over the limit"},
		{At: 6 * time.Second, Mode: Top, Color: White, Text: "Later"},
	}
	settings := DefaultSettings()
	settings.Offset, settings.Speed, settings.MaxOnScreen = 500*time.Millisecond, 2, 2
	settings.Bold, settings.Opacity, settings.Outline = false, .5, 3
	settings.FontSize = 34
	track := Schedule(comments, settings)
	if len(track.Events) != 3 || track.Events[0].Start != 1500*time.Millisecond ||
		track.Events[0].End-track.Events[0].Start != 4*time.Second {
		t.Fatalf("timing or density was not applied: %+v", track.Events)
	}
	at := 2 * time.Second
	if output := track.Overlay(at); !strings.Contains(output, `\fs34\b0\bord3.0\shad0\alpha&H7F&`) ||
		!strings.Contains(output, "Hello ×2") || strings.Contains(output, "Over the limit") {
		t.Fatalf("unexpected libmpv overlay: %s", output)
	}
	reserved := track.Events[1].Y
	settings.ReserveSubtitles, settings.Deduplicate = false, false
	settings.HideScrolling = true
	track = Schedule(comments, settings)
	if len(track.Events) != 3 || track.Events[0].DisplayText() != "hello" || track.Events[1].Y <= reserved {
		t.Fatalf("subtitle reservation, type filtering or deduplication toggle was ignored: %+v", track.Events)
	}
}
