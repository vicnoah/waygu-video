package danmaku

import (
	"bufio"
	"errors"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type assStyle struct {
	mode  Mode
	color uint32
}

// ReadASS imports timed ASS dialogue into the regular danmaku scheduler.
// The scheduler supplies the lanes and styling for the current player.
func ReadASS(reader io.Reader) ([]Comment, error) {
	limited := &io.LimitedReader{R: reader, N: maxCommentFileSize + 1}
	scanner := bufio.NewScanner(transform.NewReader(limited, unicode.BOMOverride(unicode.UTF8.NewDecoder())))
	scanner.Buffer(make([]byte, 4096), maxCommentFileSize+1)

	var section string
	var columns []string
	styles := map[string]assStyle{}
	var comments []Comment
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(line[1 : len(line)-1])
			columns = nil
			switch section {
			case "v4+ styles", "v4++ styles":
				columns = strings.Split("name,fontname,fontsize,primarycolour,secondarycolour,outlinecolour,backcolour,bold,italic,underline,strikeout,scalex,scaley,spacing,angle,borderstyle,outline,shadow,alignment,marginl,marginr,marginv,encoding", ",")
			case "events":
				columns = strings.Split("layer,start,end,style,name,marginl,marginr,marginv,effect,text", ",")
			}
			continue
		}
		if len(columns) == 0 {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "format" {
			columns = strings.SplitN(strings.ToLower(value), ",", 64)
			for i := range columns {
				columns[i] = strings.TrimSpace(columns[i])
			}
			continue
		}
		fields := strings.SplitN(value, ",", len(columns))
		if len(fields) != len(columns) {
			continue
		}
		field := func(name string) string {
			if index := slices.Index(columns, name); index >= 0 {
				return strings.TrimSpace(fields[index])
			}
			return ""
		}
		if key == "style" && section != "events" {
			if name := field("name"); name != "" {
				style := assStyle{mode: assAlignment(field("alignment")), color: White}
				if color, ok := assColor(field("primarycolour")); ok {
					style.color = color
				}
				styles[strings.ToLower(name)] = style
			}
		}
		if key != "dialogue" || section != "events" || columns[len(columns)-1] != "text" {
			continue
		}
		start, validStart := assTime(field("start"))
		end, validEnd := assTime(field("end"))
		if !validStart || !validEnd || end <= start {
			continue
		}
		style, exists := styles[strings.ToLower(field("style"))]
		if !exists {
			style, exists = styles["default"]
			if !exists {
				style = assStyle{mode: Bottom, color: White}
			}
		}
		comment := assDialogue(field("text"), style)
		if comment.Text == "" {
			continue
		}
		comment.At, comment.User = start, field("name")
		comments = append(comments, comment)
		if len(comments) > maxEntries {
			return nil, errors.New("comment file contains too many entries")
		}
	}
	if limited.N <= 0 {
		return nil, errors.New("comment file exceeds the size limit")
	}
	return comments, scanner.Err()
}

func assTime(raw string) (time.Duration, bool) {
	hour, rest, ok := strings.Cut(raw, ":")
	minute, second, split := strings.Cut(rest, ":")
	if !ok || !split {
		return 0, false
	}
	h, hourErr := strconv.ParseUint(hour, 10, 32)
	m, minuteErr := strconv.ParseUint(minute, 10, 8)
	sec, secErr := time.ParseDuration(second + "s")
	invalidSecond := strings.IndexFunc(second, func(c rune) bool { return (c < '0' || c > '9') && c != '.' }) >= 0
	if hourErr != nil || minuteErr != nil || secErr != nil || h > uint64(math.MaxInt64/int64(time.Hour)) ||
		m >= 60 || sec >= time.Minute || invalidSecond {
		return 0, false
	}
	wholeHours := time.Duration(h) * time.Hour
	remainder := time.Duration(m)*time.Minute + sec
	if wholeHours > math.MaxInt64-remainder {
		return 0, false
	}
	return wholeHours + remainder, true
}

func assAlignment(value string) Mode {
	switch value {
	case "4", "5", "6", "7", "8", "9":
		return Top
	default:
		return Bottom
	}
}

func assColor(value string) (uint32, bool) {
	var bgr uint32
	if strings.HasPrefix(strings.ToLower(value), "&h") {
		n, err := strconv.ParseUint(strings.TrimSuffix(value[2:], "&"), 16, 32)
		if err != nil {
			return 0, false
		}
		bgr = uint32(n)
	} else {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < math.MinInt32 || n > math.MaxUint32 {
			return 0, false
		}
		bgr = uint32(n)
	}
	return (bgr&0xff)<<16 | bgr&0xff00 | (bgr>>16)&0xff, true
}

var assLineBreaks = strings.NewReplacer(`\N`, " ", `\n`, " ", `\h`, " ", "\x00", "")

func assDialogue(value string, style assStyle) Comment {
	var text strings.Builder
	mode, color := style.mode, style.color
	var drawing, moving, rightward bool
	for value != "" {
		plain, rest, open := strings.Cut(value, "{")
		tags, tail, close := strings.Cut(rest, "}")
		if !open || !close {
			if !drawing {
				text.WriteString(assLineBreaks.Replace(value))
			}
			break
		}
		if !drawing {
			text.WriteString(assLineBreaks.Replace(plain))
		}
		value = tail
		for tag := range assTags(tags) {
			switch {
			case strings.HasPrefix(tag, "an") && len(tag) == 3 && tag[2] >= '1' && tag[2] <= '9':
				mode = assAlignment(tag[2:])
			case strings.HasPrefix(tag, "move(") && !moving:
				moving, rightward = assMovement(tag)
			case strings.HasPrefix(tag, "p") && len(tag) > 1 && tag[1] >= '0' && tag[1] <= '9':
				n, err := strconv.Atoi(tag[1:])
				drawing = err == nil && n > 0
			case tag == "r":
				mode, drawing = style.mode, false
				if text.Len() == 0 {
					color = style.color
				}
			case text.Len() == 0 && (strings.HasPrefix(tag, "1c") || strings.HasPrefix(tag, "c&") || tag == "c"):
				primary := strings.TrimPrefix(strings.TrimPrefix(tag, "1"), "c")
				if primary == "" {
					color = style.color
				} else if rgb, ok := assColor(primary); ok {
					color = rgb
				}
			}
		}
	}
	if moving {
		mode = Scroll
	}
	return Comment{Mode: mode, Color: color, Text: strings.TrimSpace(text.String()), Rightward: rightward}
}

// assTags splits overrides at the top level so an animated style inside
// \t(...) cannot change the imported comment's static color or direction.
func assTags(block string) func(func(string) bool) {
	return func(yield func(string) bool) {
		depth, start := 0, -1
		for i, char := range block {
			switch char {
			case '(':
				depth++
			case ')':
				depth = max(0, depth-1)
			case '\\':
				if depth == 0 {
					if start >= 0 && !yield(strings.TrimSpace(block[start:i])) {
						return
					}
					start = i + 1
				}
			}
		}
		if start >= 0 {
			yield(strings.TrimSpace(block[start:]))
		}
	}
}

func assMovement(tag string) (moving, rightward bool) {
	value, ok := strings.CutSuffix(strings.TrimPrefix(tag, "move("), ")")
	fields := strings.Split(value, ",")
	if !ok || (len(fields) != 4 && len(fields) != 6) {
		return false, false
	}
	var points [4]float64
	for i := range points {
		n, err := strconv.ParseFloat(strings.TrimSpace(fields[i]), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return false, false
		}
		points[i] = n
	}
	dx, dy := points[2]-points[0], points[3]-points[1]
	if math.Abs(dx) <= math.Abs(dy) {
		return false, false
	}
	return true, dx > 0
}
