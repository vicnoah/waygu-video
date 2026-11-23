package playback

import (
	"bytes"
	"cmp"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"

	"waygu-video/internal/mediahttp"
)

const (
	OpenSubtitles    = "OpenSubtitles"
	Shooter          = "Shooter"
	openSubtitlesAPI = "https://api.opensubtitles.com/api/v1"
	shooterAPI       = "https://www.shooter.cn/api/subapi.php"
)

var (
	ErrSubtitleService     = errors.New("online subtitle service unavailable")
	ErrSubtitleCredentials = errors.New("online subtitle credentials rejected")
	ErrSubtitleQuota       = errors.New("online subtitle service limit reached")
)

type OpenSubtitleCredentials struct {
	Key, Username, Password string
}

type OnlineSubtitleQuery struct {
	ExactOnly               bool
	Title, IMDb, SeriesIMDb string
	Year, Season, Episode   int
	Languages               []string
	Original                mediahttp.Resource
	FileName                string
	FileSize                int64
}

type OnlineSubtitle struct {
	Provider, Name, Language string
	Exact                    bool
	Delay                    time.Duration
	id                       int64
	link                     string
	downloads                int
}

type SubtitleServices struct {
	Open    OpenSubtitleCredentials
	Shooter bool
	HTTP    *http.Client
}

// Search tries the original file signature and searches OpenSubtitles by
// title or metadata when the signature has no match.
func (services SubtitleServices) Search(ctx context.Context, query OnlineSubtitleQuery) ([]OnlineSubtitle, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var matches []OnlineSubtitle
	var openErr, shooterErr error
	var attempted, succeeded bool
	if services.Open.Key != "" {
		attempted = true
		var found []OnlineSubtitle
		found, openErr = services.searchOpen(ctx, query)
		succeeded = openErr == nil
		matches = append(matches, found...)
	}
	if services.Shooter {
		if selections := shooterLanguages(query.Languages); len(selections) != 0 && query.FileSize >= 12_288 && query.Original.URL != "" {
			attempted = true
			var found []OnlineSubtitle
			found, shooterErr = services.searchShooter(ctx, query, selections)
			succeeded = succeeded || shooterErr == nil
			matches = append(matches, found...)
		}
	}
	if !succeeded && attempted {
		return nil, cmp.Or(openErr, shooterErr, ErrSubtitleService)
	}
	if len(matches) == 0 && openErr != nil {
		return nil, openErr
	}
	slices.SortStableFunc(matches, func(a, b OnlineSubtitle) int {
		if a.Exact != b.Exact {
			if a.Exact {
				return -1
			}
			return 1
		}
		rank := func(language string) int {
			i := slices.Index(query.Languages, language)
			if i < 0 {
				return len(query.Languages)
			}
			return i
		}
		return cmp.Or(cmp.Compare(rank(a.Language), rank(b.Language)), cmp.Compare(b.downloads, a.downloads))
	})
	return matches[:min(len(matches), 40)], nil
}

func (services SubtitleServices) searchOpen(ctx context.Context, query OnlineSubtitleQuery) ([]OnlineSubtitle, error) {
	base := url.Values{}
	if len(query.Languages) > 0 {
		codes := make([]string, 0, len(query.Languages))
		for _, code := range query.Languages {
			switch code {
			case "zh-Hans":
				code = "zh-cn"
			case "zh-Hant":
				code = "zh-tw"
			}
			codes = append(codes, strings.ToLower(code))
		}
		slices.Sort(codes)
		base.Set("languages", strings.Join(slices.Compact(codes), ","))
	}
	hashContext, cancel := context.WithTimeout(ctx, 4*time.Second)
	hash, _ := openMovieHash(hashContext, query.Original, query.FileSize)
	cancel()
	if hash != "" {
		base.Set("moviehash", hash)
		found, err := services.openResults(ctx, base, true)
		if err != nil || len(found) != 0 {
			return found, err
		}
		base.Del("moviehash")
	}
	if query.ExactOnly {
		return nil, nil
	}
	if query.Episode > 0 {
		base.Set("type", "episode")
		base.Set("season_number", strconv.Itoa(query.Season))
		base.Set("episode_number", strconv.Itoa(query.Episode))
		if id := imdbNumber(query.SeriesIMDb); id != "" {
			base.Set("parent_imdb_id", id)
		}
	} else {
		base.Set("type", "movie")
		if query.Year > 0 {
			base.Set("year", strconv.Itoa(query.Year))
		}
	}
	if id := imdbNumber(query.IMDb); id != "" {
		base.Set("imdb_id", id)
	} else if !base.Has("parent_imdb_id") {
		title := strings.ToLower(strings.TrimSpace(query.Title))
		if title == "" {
			return nil, nil
		}
		base.Set("query", title)
	}
	return services.openResults(ctx, base, false)
}

func (services SubtitleServices) openResults(ctx context.Context, query url.Values, exact bool) ([]OnlineSubtitle, error) {
	var result struct {
		Data []struct {
			Attributes struct {
				Language  string
				Release   string
				Downloads int `json:"download_count"`
				Files     []struct {
					ID   int64  `json:"file_id"`
					Name string `json:"file_name"`
				}
			}
		}
	}
	address := openSubtitlesAPI + "/subtitles?" + query.Encode()
	if err := services.openRequest(ctx, http.MethodGet, address, "", nil, &result); err != nil {
		return nil, err
	}
	var matches []OnlineSubtitle
	seen := make(map[int64]bool)
	for _, entry := range result.Data {
		for _, file := range entry.Attributes.Files {
			if file.ID <= 0 || seen[file.ID] {
				continue
			}
			seen[file.ID] = true
			name := cmp.Or(file.Name, entry.Attributes.Release)
			if name == "" {
				continue
			}
			language := entry.Attributes.Language
			switch strings.ToLower(language) {
			case "zh-cn":
				language = "zh-Hans"
			case "zh-tw":
				language = "zh-Hant"
			}
			matches = append(matches, OnlineSubtitle{Provider: OpenSubtitles, Name: name, Language: language,
				Exact: exact, id: file.ID, downloads: entry.Attributes.Downloads})
		}
	}
	return matches, nil
}

func imdbNumber(id string) string {
	id = strings.TrimLeft(strings.TrimPrefix(id, "tt"), "0")
	if len(id) < 1 || len(id) > 16 || strings.ContainsFunc(id, func(c rune) bool { return c < '0' || c > '9' }) {
		return ""
	}
	return id
}

func openMovieHash(ctx context.Context, original mediahttp.Resource, size int64) (string, error) {
	const chunk = 64 << 10
	if size < 2*chunk || original.URL == "" {
		return "", ErrSubtitleService
	}
	file, closeIdle, err := openCaptionFile(original, size)
	if err != nil {
		return "", err
	}
	defer closeIdle()
	hash := uint64(size)
	for _, offset := range []int64{0, size - chunk} {
		block, err := file.read(ctx, offset, chunk)
		if err != nil {
			return "", err
		}
		for len(block) >= 8 {
			hash += binary.LittleEndian.Uint64(block)
			block = block[8:]
		}
	}
	return fmt.Sprintf("%016x", hash), nil
}

type shooterLanguage struct{ service, tag string }

func shooterLanguages(languages []string) []shooterLanguage {
	var selected []shooterLanguage
	if len(languages) == 0 || slices.Contains(languages, "zh-Hans") || slices.Contains(languages, "zh-Hant") {
		selected = append(selected, shooterLanguage{"Chn", "zh-Hans"})
	}
	if len(languages) == 0 || slices.Contains(languages, "en") {
		selected = append(selected, shooterLanguage{"Eng", "en"})
	}
	return selected
}

func (services SubtitleServices) searchShooter(ctx context.Context, query OnlineSubtitleQuery, languages []shooterLanguage) ([]OnlineSubtitle, error) {
	hashContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	hash, err := shooterHash(hashContext, query.Original, query.FileSize)
	cancel()
	if err != nil {
		return nil, err
	}
	var matches []OnlineSubtitle
	seen := make(map[string]bool)
	for _, language := range languages {
		name := path.Base(strings.ReplaceAll(query.FileName, "\\", "/"))
		form := url.Values{"filehash": {hash}, "pathinfo": {name}, "format": {"json"}, "lang": {language.service}}
		contents, err := services.read(ctx, http.MethodPost, shooterAPI, "shooter.cn", "application/x-www-form-urlencoded", "", "", strings.NewReader(form.Encode()), 1<<20)
		if err != nil {
			return matches, err
		}
		contents = bytes.TrimSpace(contents)
		if len(contents) == 0 || bytes.Equal(contents, []byte{0xff}) {
			continue
		}
		var results []struct {
			Desc  string
			Delay int64
			Files []struct{ Ext, Link string }
		}
		if err := json.Unmarshal(contents, &results); err != nil {
			return matches, ErrSubtitleService
		}
		for _, result := range results {
			for _, file := range result.Files {
				ext := strings.ToLower(strings.TrimPrefix(file.Ext, "."))
				if (ext != "srt" && ext != "vtt") || seen[file.Link] {
					continue
				}
				address := strings.Replace(file.Link, "http://", "https://", 1)
				if !subtitleProviderURL(address, "shooter.cn") {
					continue
				}
				seen[file.Link] = true
				title := strings.TrimSpace(result.Desc)
				if title == "" {
					title = strings.TrimSuffix(name, path.Ext(name)) + "." + ext
				}
				delay := time.Duration(min(max(result.Delay, -10_000), 10_000)) * time.Millisecond
				matches = append(matches, OnlineSubtitle{Provider: Shooter, Name: title, Language: language.tag,
					Exact: true, Delay: delay, link: address})
			}
		}
	}
	return matches, nil
}

func shooterHash(ctx context.Context, original mediahttp.Resource, size int64) (string, error) {
	file, closeIdle, err := openCaptionFile(original, size)
	if err != nil {
		return "", err
	}
	defer closeIdle()
	var sums []string
	for _, offset := range []int64{4096, size / 3 * 2, size / 3, size - 8192} {
		block, err := file.read(ctx, offset, 4096)
		if err != nil {
			return "", err
		}
		sums = append(sums, fmt.Sprintf("%x", md5.Sum(block)))
	}
	return strings.Join(sums, ";"), nil
}

func (services SubtitleServices) VerifyOpenAccount(ctx context.Context) error {
	_, _, err := services.openSession(ctx)
	return err
}

func (services SubtitleServices) openSession(ctx context.Context) (string, string, error) {
	if services.Open.Username == "" {
		return openSubtitlesAPI, "", nil
	}
	data := map[string]string{"username": services.Open.Username, "password": services.Open.Password}
	var response struct {
		Token string
		Host  string `json:"base_url"`
	}
	if err := services.openRequest(ctx, http.MethodPost, openSubtitlesAPI+"/login", "", data, &response); err != nil {
		return "", "", err
	}
	base := openSubtitlesAPI
	if response.Host != "" {
		if response.Host != "api.opensubtitles.com" && response.Host != "vip-api.opensubtitles.com" {
			return "", "", ErrSubtitleService
		}
		base = "https://" + response.Host + "/api/v1"
	}
	if response.Token == "" {
		return "", "", ErrSubtitleCredentials
	}
	return base, response.Token, nil
}

func (services SubtitleServices) Download(ctx context.Context, match OnlineSubtitle) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	address, domain := match.link, "shooter.cn"
	if match.Provider == OpenSubtitles {
		base, bearer, err := services.openSession(ctx)
		if err != nil {
			return nil, err
		}
		var result struct{ Link string }
		request := map[string]any{"file_id": match.id, "sub_format": "srt"}
		if err := services.openRequest(ctx, http.MethodPost, base+"/download", bearer, request, &result); err != nil {
			return nil, err
		}
		address, domain = result.Link, "opensubtitles.com"
	}
	data, err := services.read(ctx, http.MethodGet, address, domain, "", "", "", nil, captionLimit)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		return unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM).NewDecoder().Bytes(data)
	}
	if match.Provider == Shooter && !utf8.Valid(data) {
		return simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	}
	return data, nil
}

func (services SubtitleServices) openRequest(ctx context.Context, method, address, bearer string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return ErrSubtitleService
		}
		body = bytes.NewReader(data)
	}
	data, err := services.read(ctx, method, address, "opensubtitles.com", "application/json", services.Open.Key, bearer, body, 2<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, output); err != nil {
		return ErrSubtitleService
	}
	return nil
}

func (services SubtitleServices) read(ctx context.Context, method, address, domain, kind, key, bearer string, body io.Reader, limit int64) ([]byte, error) {
	if !subtitleProviderURL(address, domain) {
		return nil, ErrSubtitleService
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, ErrSubtitleService
	}
	req.Header.Set("User-Agent", "WayguVideo v0.1")
	if kind != "" {
		req.Header.Set("Content-Type", kind)
	}
	if key != "" {
		req.Header.Set("Api-Key", key)
		req.Header.Set("Accept", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := http.Client{Timeout: 15 * time.Second}
	if services.HTTP != nil {
		client.Transport = services.HTTP.Transport
	}
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !subtitleProviderURL(next.URL.String(), domain) ||
			(key != "" && next.URL.Host != req.URL.Host) ||
			(method != http.MethodGet && next.Method != method) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, ErrSubtitleService
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrSubtitleCredentials
	case http.StatusTooManyRequests, http.StatusNotAcceptable:
		return nil, ErrSubtitleQuota
	case http.StatusOK:
	default:
		return nil, ErrSubtitleService
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, ErrSubtitleService
	}
	return data, nil
}

func subtitleProviderURL(address, domain string) bool {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == domain || strings.HasSuffix(host, "."+domain)
}
