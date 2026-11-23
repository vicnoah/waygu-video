package playback

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"waygu-video/internal/mediatls"
)

const (
	captionLimit      = 4 << 20
	maxCaptionSamples = 24_000
	maxCaptionTime    = 30 * 24 * time.Hour
)

var (
	errCaptions = errors.New("could not read subtitles")
	captionTags = regexp.MustCompile(`<[^>]*>`)
)

type cue struct {
	start, end time.Duration
	text       string
}

type Captions struct {
	cues      []cue
	latestEnd []time.Duration
}

func ReadCaptions(reader io.Reader) (*Captions, error) {
	data, err := io.ReadAll(io.LimitReader(reader, captionLimit+1))
	if err != nil || len(data) > captionLimit {
		return nil, errCaptions
	}
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(string(data), "\uFEFF")))
	scanner.Buffer(make([]byte, 4096), captionLimit)
	var lines []string
	var cues []cue
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			cues = appendCue(cues, lines)
			lines = lines[:0]
		} else {
			lines = append(lines, line)
		}
	}
	cues = appendCue(cues, lines)
	if scanner.Err() != nil {
		return nil, errCaptions
	}
	return newCaptions(cues)
}

func newCaptions(cues []cue) (*Captions, error) {
	if len(cues) == 0 {
		return nil, errCaptions
	}
	slices.SortStableFunc(cues, func(a, b cue) int { return cmp.Compare(a.start, b.start) })
	ends := make([]time.Duration, len(cues))
	var latest time.Duration
	for i, cue := range cues {
		latest = max(latest, cue.end)
		ends[i] = latest
	}
	return &Captions{cues: cues, latestEnd: ends}, nil
}

func appendCue(cues []cue, lines []string) []cue {
	if len(lines) < 2 || strings.HasPrefix(lines[0], "NOTE") || lines[0] == "STYLE" || lines[0] == "REGION" {
		return cues
	}
	if !strings.Contains(lines[0], "-->") {
		lines = lines[1:]
	}
	if len(lines) < 2 {
		return cues
	}
	left, right, ok := strings.Cut(lines[0], "-->")
	if !ok {
		return cues
	}
	fields := strings.Fields(right)
	if len(fields) == 0 {
		return cues
	}
	start, startOK := cueTime(strings.TrimSpace(left))
	end, endOK := cueTime(fields[0])
	if !startOK || !endOK || start >= end {
		return cues
	}
	text := plainCaptionText(strings.Join(lines[1:], "\n"))
	if text != "" {
		cues = append(cues, cue{start, end, text})
	}
	return cues
}

func plainCaptionText(text string) string {
	return strings.TrimSpace(html.UnescapeString(captionTags.ReplaceAllString(text, "")))
}

func cueTime(value string) (time.Duration, bool) {
	parts := strings.Split(strings.Replace(value, ",", ".", 1), ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, false
	}
	last := strings.Split(parts[len(parts)-1], ".")
	if len(last) != 2 || len(last[1]) != 3 {
		return 0, false
	}
	minutes, minErr := strconv.ParseUint(parts[len(parts)-2], 10, 6)
	seconds, secErr := strconv.ParseUint(last[0], 10, 6)
	_, msErr := strconv.ParseUint(last[1], 10, 10)
	if minErr != nil || secErr != nil || msErr != nil || minutes >= 60 || seconds >= 60 {
		return 0, false
	}
	hours := "0"
	if len(parts) == 3 {
		hours = parts[0]
		if _, err := strconv.ParseUint(hours, 10, 32); err != nil {
			return 0, false
		}
	}
	t, err := time.ParseDuration(hours + "h" + parts[len(parts)-2] + "m" + parts[len(parts)-1] + "s")
	return t, err == nil
}

// At returns all cues active at position, including overlapping cues.
func (c *Captions) At(position time.Duration) string {
	start := sort.Search(len(c.latestEnd), func(i int) bool { return c.latestEnd[i] > position })
	var text strings.Builder
	for i := start; i < len(c.cues) && c.cues[i].start <= position; i++ {
		if c.cues[i].end > position {
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(c.cues[i].text)
		}
	}
	return text.String()
}

func FetchCaptions(ctx context.Context, address string, header http.Header, pins ...mediatls.Trust) (*Captions, error) {
	client, err := newCaptionClient(address, pins...)
	if err != nil {
		return nil, err
	}
	if client.Transport != nil {
		defer client.CloseIdleConnections()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errCaptions
	}
	req.Header = header.Clone()
	res, err := client.Do(req)
	if err != nil {
		return nil, errCaptions
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errCaptions
	}
	return ReadCaptions(res.Body)
}

func newCaptionClient(address string, pins ...mediatls.Trust) (*http.Client, error) {
	origin, err := url.Parse(address)
	if err != nil || origin.User != nil || origin.Hostname() == "" || (origin.Scheme != "http" && origin.Scheme != "https") {
		return nil, errCaptions
	}
	transport, err := mediatls.Transport(address, pins...)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.User != nil || req.URL.Scheme != origin.Scheme || !strings.EqualFold(req.URL.Host, origin.Host) {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	if transport != nil {
		client.Transport = transport
	}
	return client, nil
}
