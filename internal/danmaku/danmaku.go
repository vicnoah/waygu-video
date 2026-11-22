// Package danmaku parses, filters, and schedules comments in video time.
package danmaku

import (
	"cmp"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Mode uint8

const (
	Scroll Mode = iota + 1
	Top
	Bottom
)

const (
	White      = 0xffffff
	MaxXMLSize = 16 << 20
	maxEntries = 100_000
)

type Comment struct {
	At    time.Duration
	Mode  Mode
	Color uint32
	Text  string
}

// ReadXML reads the Bilibili-compatible XML format used by locally saved
// comment tracks. Unsupported overlay effects are skipped.
func ReadXML(reader io.Reader) ([]Comment, error) {
	limited := &io.LimitedReader{R: reader, N: MaxXMLSize + 1}
	decoder := xml.NewDecoder(limited)
	var comments []Comment
	for {
		token, err := decoder.Token()
		if limited.N <= 0 {
			return nil, errors.New("comment file exceeds the size limit")
		}
		if errors.Is(err, io.EOF) {
			return comments, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "d" {
			continue
		}
		var entry struct {
			Parameters string `xml:"p,attr"`
			Text       string `xml:",chardata"`
		}
		if err := decoder.DecodeElement(&entry, &start); err != nil {
			return nil, err
		}
		if line, ok := ParseComment(entry.Parameters, entry.Text, true); ok {
			comments = append(comments, line)
			if len(comments) > maxEntries {
				return nil, errors.New("comment file contains too many entries")
			}
		}
	}
}

// ParseComment decodes the parameter string of a Bilibili XML or DandanPlay
// comment. XML has a font-size field before color, while DandanPlay omits it.
func ParseComment(parameters, text string, xmlFontSize bool) (Comment, bool) {
	fields := strings.SplitN(parameters, ",", 5)
	colorIndex := 2
	if xmlFontSize {
		colorIndex = 3
	}
	text = strings.TrimSpace(text)
	if len(fields) <= colorIndex || text == "" {
		return Comment{}, false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(seconds) || seconds < 0 || seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return Comment{}, false
	}
	mode := Scroll
	switch fields[1] {
	case "1", "2", "3":
	case "4":
		mode = Bottom
	case "5":
		mode = Top
	default:
		return Comment{}, false
	}
	color, err := strconv.ParseUint(fields[colorIndex], 10, 24)
	if err != nil {
		color = White
	}
	return Comment{At: time.Duration(seconds * float64(time.Second)), Mode: mode, Color: uint32(color), Text: text}, true
}

type Settings struct {
	Width, Height       int
	FontSize            int
	Opacity             float64
	Area                float64
	Speed               float64
	MaxOnScreen         int
	Offset              time.Duration
	Deduplicate         bool
	HideTop, HideBottom bool
	HideColored         bool
	Keywords            []string
	Patterns            []*regexp.Regexp
}

func DefaultSettings() Settings {
	return Settings{Width: 1280, Height: 720, FontSize: 28, Opacity: .9, Area: .6,
		Speed: 1, MaxOnScreen: 32, Deduplicate: true}
}

type Event struct {
	Comment
	Start, End time.Duration
	X0, X1, Y  int
}

func (e Event) X(at time.Duration) float64 {
	if e.X0 == e.X1 || e.End <= e.Start {
		return float64(e.X0)
	}
	progress := float64(max(0, min(at-e.Start, e.End-e.Start))) / float64(e.End-e.Start)
	return float64(e.X0) + float64(e.X1-e.X0)*progress
}

type Track struct {
	Width, Height int
	FontSize      int
	Opacity       float64
	Events        []Event
	longest       time.Duration
}

func (t Track) Visible(at time.Duration) []Event {
	first, _ := slices.BinarySearchFunc(t.Events, at-t.longest, func(event Event, time time.Duration) int {
		return cmp.Compare(event.Start, time)
	})
	last, _ := slices.BinarySearchFunc(t.Events, at+1, func(event Event, time time.Duration) int {
		return cmp.Compare(event.Start, time)
	})
	visible := make([]Event, 0, last-first)
	for _, event := range t.Events[first:last] {
		if event.End > at {
			visible = append(visible, event)
		}
	}
	return visible
}

func Schedule(comments []Comment, settings Settings) Track {
	settings = checkedSettings(settings)
	track := Track{Width: settings.Width, Height: settings.Height, FontSize: settings.FontSize, Opacity: settings.Opacity}
	comments = slices.Clone(comments)
	slices.SortStableFunc(comments, func(a, b Comment) int { return cmp.Compare(a.At, b.At) })
	rowHeight := settings.FontSize + 8
	rows := (settings.Height * 4 / 5) / rowHeight
	scrollRows := min(rows, int(float64(settings.Height)*settings.Area)/rowHeight)
	if scrollRows == 0 {
		return track
	}
	busy := make([]time.Duration, rows)
	lastText := make(map[string]time.Duration)
	var keywords []string
	for _, keyword := range settings.Keywords {
		if keyword = strings.ToLower(strings.TrimSpace(keyword)); keyword != "" {
			keywords = append(keywords, keyword)
		}
	}
	for _, comment := range comments {
		if !passes(comment, settings, keywords) {
			continue
		}
		start := comment.At + settings.Offset
		if start < 0 {
			continue
		}
		if settings.Deduplicate {
			text := strings.ToLower(strings.Join(strings.Fields(comment.Text), " "))
			if previous, seen := lastText[text]; seen && start-previous <= 3*time.Second {
				continue
			}
			lastText[text] = start
		}
		active := 0
		for _, end := range busy {
			if end > start {
				active++
			}
		}
		if active >= settings.MaxOnScreen {
			continue
		}
		lanes := busy[:scrollRows]
		if comment.Mode == Bottom {
			lanes = busy
		}
		row := vacantRow(lanes, start, comment.Mode == Bottom)
		if row < 0 {
			continue
		}
		duration := 4 * time.Second
		x0, x1 := settings.Width/2, settings.Width/2
		y := rowHeight * row
		if comment.Mode == Scroll {
			duration = time.Duration(float64(8*time.Second) / settings.Speed)
			x0, x1 = settings.Width, -estimatedWidth(comment.Text, settings.FontSize)
		}
		end := start + duration
		busy[row] = end
		track.longest = max(track.longest, duration)
		track.Events = append(track.Events, Event{Comment: comment, Start: start, End: end, X0: x0, X1: x1, Y: y})
	}
	return track
}

func checkedSettings(s Settings) Settings {
	defaults := DefaultSettings()
	if s.Width <= 0 || s.Height <= 0 {
		s.Width, s.Height = defaults.Width, defaults.Height
	}
	if s.FontSize <= 0 {
		s.FontSize = defaults.FontSize
	}
	if !(s.Opacity > 0 && s.Opacity <= 1) {
		s.Opacity = defaults.Opacity
	}
	if !(s.Area > 0 && s.Area <= .75) {
		s.Area = defaults.Area
	}
	if !(s.Speed >= .25 && s.Speed <= 4) {
		s.Speed = defaults.Speed
	}
	if s.MaxOnScreen <= 0 {
		s.MaxOnScreen = defaults.MaxOnScreen
	}
	return s
}

func passes(c Comment, s Settings, keywords []string) bool {
	if c.Text == "" || (c.Mode != Scroll && c.Mode != Top && c.Mode != Bottom) ||
		(c.Mode == Top && s.HideTop) || (c.Mode == Bottom && s.HideBottom) || (c.Color != White && s.HideColored) {
		return false
	}
	text := strings.ToLower(c.Text)
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return false
		}
	}
	for _, pattern := range s.Patterns {
		if pattern.MatchString(c.Text) {
			return false
		}
	}
	return true
}

func vacantRow(busy []time.Duration, at time.Duration, reverse bool) int {
	for i := range busy {
		row := i
		if reverse {
			row = len(busy) - 1 - i
		}
		if busy[row] <= at {
			return row
		}
	}
	return -1
}

func estimatedWidth(text string, fontsize int) int {
	width := 0
	for _, r := range text {
		if r < 128 {
			width += fontsize * 2 / 3
		} else {
			width += fontsize
		}
	}
	return width + fontsize
}
