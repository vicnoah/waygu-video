package danmaku

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/unicode"
)

func TestReadASSAndSchedule(t *testing.T) {
	const fixture = `[Script Info]
ScriptType: v4.00+
[V4+ Styles]
Format: PrimaryColour, Name, Alignment
Style: &H002211DD, Header, 8
Style: &H00FFFFFF, Default, 2
[Events]
Format: Start, End, Style, Name, Text
Comment: 0:00:00.00,0:00:03.00,Default,,ignored
Dialogue: 0:00:01.25,0:00:04.50,Header,viewer,置顶, 含逗号\N与换行
Dialogue: 0:00:01.50,0:00:09.50,Default,,{\move(1280,100,-80,100)\c&HFF8800&\t(\c&HFFFFFF&)}向左
Dialogue: 0:00:01.60,0:00:09.60,Header,,{\move(-30,50,1280,50,0,8000)}向右
Dialogue: 0:00:01.70,0:00:05.70,Header,,{\an2\1c&H00223344&}底部\h弹幕
Dialogue: 0:00:02.00,0:00:02.00,Default,,zero length
Dialogue: 0:00:62.00,0:00:63.00,Default,,bad timestamp
Dialogue: 0:00:03.00,0:00:08.00,Default,,{\p1}m 0 0 l 20 30
`
	want := []Comment{
		{At: 1250 * time.Millisecond, Mode: Top, Color: 0xdd1122, Text: "置顶, 含逗号 与换行", User: "viewer"},
		{At: 1500 * time.Millisecond, Mode: Scroll, Color: 0x0088ff, Text: "向左"},
		{At: 1600 * time.Millisecond, Mode: Scroll, Color: 0xdd1122, Text: "向右", Rightward: true},
		{At: 1700 * time.Millisecond, Mode: Bottom, Color: 0x443322, Text: "底部 弹幕"},
	}
	for _, encoding := range []struct {
		name string
		data []byte
	}{
		{"UTF-8 with BOM", []byte("\ufeff" + fixture)},
		{"UTF-16LE with BOM", encodeASS(t, fixture)},
	} {
		t.Run(encoding.name, func(t *testing.T) {
			got, err := ReadASS(bytes.NewReader(encoding.data))
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("ASS comments = %+v, %v", got, err)
			}
			settings := DefaultSettings()
			settings.BlockedUsers = map[string]bool{"viewer": true}
			settings.Offset = 500 * time.Millisecond
			track := Schedule(got, settings)
			now := 3 * time.Second
			events := track.Visible(now)
			if len(events) != 3 || events[0].Start != 2*time.Second ||
				events[0].X(now) >= float64(events[0].X0) || events[1].X(now) <= float64(events[1].X0) ||
				events[0].Y == events[1].Y || events[2].Y <= events[1].Y || !strings.Contains(track.Overlay(now), "向右") {
				t.Fatalf("scheduled ASS events = %+v", events)
			}
		})
	}
}

func encodeASS(t *testing.T, text string) []byte {
	t.Helper()
	data, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReadASSRejectsOversizedFile(t *testing.T) {
	file := strings.NewReader(strings.Repeat(";\n", maxCommentFileSize/2+1))
	if comments, err := ReadASS(file); len(comments) != 0 || err == nil {
		t.Fatalf("oversized file = %+v, %v", comments, err)
	}
}
