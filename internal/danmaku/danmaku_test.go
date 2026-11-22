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
