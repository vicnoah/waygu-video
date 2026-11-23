package danmaku

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"waygu-video/internal/mediahttp"
	"waygu-video/internal/mediatls"
)

const (
	dandanOrigin = "https://api.dandanplay.net"
	matchBytes   = 16 << 20
)

var ErrDandanCredentials = errors.New("DandanPlay credentials were rejected")

type DandanCredentials struct {
	AppID, Secret string
}

type DandanClient struct {
	credentials DandanCredentials
	http        http.Client
	logvar      *logvarEndpoint
}

func NewDandanClient(credentials DandanCredentials, transport http.RoundTripper) *DandanClient {
	return &DandanClient{credentials: credentials, http: http.Client{
		Timeout: 15 * time.Second, Transport: transport,
		CheckRedirect: func(next *http.Request, previous []*http.Request) error {
			if len(previous) >= 4 || next.URL.Scheme != "https" || next.URL.User != nil ||
				!strings.HasPrefix(previous[0].URL.Path, "/api/v2/comment/") {
				return http.ErrUseLastResponse
			}
			// CDN URLs already contain their own access parameters. The
			// signature belongs to the original API request only.
			next.Header.Del("X-AppId")
			next.Header.Del("X-Timestamp")
			next.Header.Del("X-Signature")
			return nil
		},
	}}
}

type DandanEpisode struct {
	ID           int64   `json:"episodeId"`
	AnimeID      int64   `json:"animeId"`
	AnimeTitle   string  `json:"animeTitle"`
	EpisodeTitle string  `json:"episodeTitle"`
	Shift        float64 `json:"shift,omitempty"`
}

func (e DandanEpisode) Label() string {
	if e.AnimeTitle == "" {
		return e.EpisodeTitle
	}
	if e.EpisodeTitle == "" || e.AnimeTitle == e.EpisodeTitle {
		return e.AnimeTitle
	}
	return e.AnimeTitle + " · " + e.EpisodeTitle
}

type FileMatch struct {
	Name     string `json:"fileName"`
	Hash     string `json:"fileHash,omitempty"`
	Size     int64  `json:"fileSize"`
	Duration int64  `json:"videoDuration,omitempty"`
	Mode     string `json:"matchMode"`
}

type EpisodeMatches struct {
	Episodes []DandanEpisode
	Exact    bool
	More     bool
}

type dandanStatus struct {
	Success   *bool
	ErrorCode int
}

func (c *DandanClient) Match(ctx context.Context, file FileMatch) (EpisodeMatches, error) {
	file.Mode = "hashAndFileName"
	if file.Hash == "" {
		file.Mode = "fileNameOnly"
	}
	var response struct {
		dandanStatus
		IsMatched bool
		Matches   []DandanEpisode
	}
	err := c.request(ctx, http.MethodPost, "/api/v2/match", nil, file, &response, &response.dandanStatus)
	episodes := appendEpisodes(nil, response.Matches)
	return EpisodeMatches{Episodes: episodes, Exact: response.IsMatched && len(episodes) == 1}, err
}

func (c *DandanClient) Search(ctx context.Context, title string, episode string) (EpisodeMatches, error) {
	query := url.Values{"anime": {title}}
	if episode != "" {
		query.Set("episode", episode)
	}
	var response struct {
		dandanStatus
		HasMore bool
		Animes  []struct {
			AnimeID    int64
			AnimeTitle string
			Episodes   []DandanEpisode
		}
	}
	err := c.request(ctx, http.MethodGet, "/api/v2/search/episodes", query, nil, &response, &response.dandanStatus)
	var episodes []DandanEpisode
	for _, anime := range response.Animes {
		for i := range anime.Episodes {
			anime.Episodes[i].AnimeTitle = anime.AnimeTitle
			anime.Episodes[i].AnimeID = anime.AnimeID
		}
		episodes = appendEpisodes(episodes, anime.Episodes)
	}
	return EpisodeMatches{Episodes: episodes, More: response.HasMore}, err
}

// RelatedEpisode uses the numbering supplied by DandanPlay. Special episode
// labels and multiple results for the same number require manual selection.
func (c *DandanClient) RelatedEpisode(ctx context.Context, anchor DandanEpisode, difference int) (DandanEpisode, error) {
	type numberedEpisode struct {
		DandanEpisode
		EpisodeNumber string
		SeasonID      string
	}
	var response struct {
		dandanStatus
		Bangumi struct{ Episodes []numberedEpisode }
	}
	if anchor.AnimeID <= 0 || difference == 0 {
		return DandanEpisode{}, nil
	}
	path := "/api/v2/bangumi/" + strconv.FormatInt(anchor.AnimeID, 10)
	if err := c.request(ctx, http.MethodGet, path, nil, nil, &response, &response.dandanStatus); err != nil {
		return DandanEpisode{}, err
	}
	for _, reference := range response.Bangumi.Episodes {
		if reference.ID != anchor.ID {
			continue
		}
		number, err := strconv.Atoi(strings.TrimSpace(reference.EpisodeNumber))
		if err != nil || number < 1 || difference < 1-number {
			break
		}
		var found DandanEpisode
		for _, candidate := range response.Bangumi.Episodes {
			next, err := strconv.Atoi(strings.TrimSpace(candidate.EpisodeNumber))
			if err != nil || next < 1 || candidate.ID <= 0 || candidate.SeasonID != reference.SeasonID || next-number != difference {
				continue
			}
			if found.ID != 0 {
				return DandanEpisode{}, nil
			}
			found = candidate.DandanEpisode
			found.AnimeID, found.AnimeTitle = anchor.AnimeID, anchor.AnimeTitle
		}
		return found, nil
	}
	return DandanEpisode{}, nil
}

func appendEpisodes(output, entries []DandanEpisode) []DandanEpisode {
	for _, entry := range entries {
		if entry.ID > 0 && entry.Label() != "" {
			output = append(output, entry)
		}
	}
	return output
}

type dandanComments struct {
	dandanStatus
	Comments []struct{ P, M string }
}

func (c *DandanClient) Comments(ctx context.Context, episode int64) ([]Comment, error) {
	var response dandanComments
	route := "/api/v2/comment/" + strconv.FormatInt(episode, 10)
	query := url.Values{"withRelated": {"true"}}
	if c.logvar != nil {
		query = url.Values{"format": {"json"}}
	}
	err := c.request(ctx, http.MethodGet, route, query, nil, &response, &response.dandanStatus)
	if err != nil {
		return nil, err
	}
	return response.parse()
}

// ReadJSON reads a DandanPlay comment response saved to a local file.
func ReadJSON(reader io.Reader) ([]Comment, error) {
	var response dandanComments
	if err := decodeDanmakuJSON(reader, &response); err != nil {
		return nil, err
	}
	return response.parse()
}

func (response dandanComments) parse() ([]Comment, error) {
	if len(response.Comments) > maxEntries {
		return nil, errors.New("comment file contains too many entries")
	}
	var comments []Comment
	for _, entry := range response.Comments {
		if comment, ok := ParseComment(entry.P, entry.M, false); ok {
			comments = append(comments, comment)
		}
	}
	return comments, nil
}

func (c *DandanClient) request(ctx context.Context, method, route string, query url.Values, body, response any, status *dandanStatus) error {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	address := dandanOrigin + route
	if c.logvar != nil {
		address = c.logvar.path + route
	}
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, address, payload)
	if err != nil {
		if c.logvar != nil {
			return ErrLogvarRequest
		}
		return err
	}
	if c.logvar == nil {
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		signature := sha256.Sum256([]byte(c.credentials.AppID + stamp + route + c.credentials.Secret))
		request.Header.Set("X-AppId", c.credentials.AppID)
		request.Header.Set("X-Timestamp", stamp)
		request.Header.Set("X-Signature", base64.StdEncoding.EncodeToString(signature[:]))
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	reply, err := c.http.Do(request)
	if err != nil {
		if c.logvar != nil {
			return ErrLogvarRequest
		}
		return err
	}
	defer reply.Body.Close()
	switch reply.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		if c.logvar != nil {
			return ErrLogvarAuthentication
		}
		return ErrDandanCredentials
	case http.StatusOK:
	default:
		if c.logvar != nil {
			return fmt.Errorf("%w (HTTP %d)", ErrLogvarRequest, reply.StatusCode)
		}
		return fmt.Errorf("DandanPlay HTTP status %d", reply.StatusCode)
	}
	if err := decodeDanmakuJSON(reply.Body, response); err != nil {
		return err
	}
	if status.ErrorCode != 0 || (status.Success != nil && !*status.Success) {
		if c.logvar != nil {
			return fmt.Errorf("%w (API %d)", ErrLogvarRequest, status.ErrorCode)
		}
		return fmt.Errorf("DandanPlay API error %d", status.ErrorCode)
	}
	return nil
}

func decodeDanmakuJSON(reader io.Reader, response any) error {
	limited := &io.LimitedReader{R: reader, N: maxCommentFileSize + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(response); err != nil {
		return err
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after comment response")
	}
	if limited.N == 0 {
		return errors.New("comment response exceeds the size limit")
	}
	return nil
}

// Fingerprint reads only the initial range of an original video file. A
// server ignoring Range is treated as unsupported so it cannot download a
// whole movie in the background.
func Fingerprint(ctx context.Context, source mediahttp.Resource) (string, int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return "", 0, err
	}
	if request.URL.Scheme != "https" && request.URL.Scheme != "http" {
		return "", 0, errors.New("unsupported fingerprint URL")
	}
	for key, values := range source.Headers {
		request.Header[key] = values
	}
	request.Header.Set("Range", "bytes=0-"+strconv.Itoa(matchBytes-1))
	request.Header.Set("Accept-Encoding", "identity")
	client := http.Client{Timeout: 12 * time.Second, CheckRedirect: func(next *http.Request, previous []*http.Request) error {
		if len(previous) >= 3 || next.URL.Host != request.URL.Host || next.URL.Scheme != request.URL.Scheme || next.URL.User != nil {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	if transport, err := mediatls.Transport(source.URL, source.Trust); err != nil {
		return "", 0, err
	} else if transport != nil {
		defer transport.CloseIdleConnections()
		client.Transport = transport
	}
	client.Transport = source.Authorize.Transport(client.Transport)
	response, err := client.Do(request)
	if err != nil {
		return "", 0, err
	}
	defer response.Body.Close()
	var last, size int64
	var extra string
	count, _ := fmt.Sscanf(response.Header.Get("Content-Range"), "bytes 0-%d/%d%s", &last, &size, &extra)
	encoding := response.Header.Get("Content-Encoding")
	if response.StatusCode != http.StatusPartialContent || count != 2 || size <= 0 || min(size, matchBytes)-1 != last ||
		(encoding != "" && !strings.EqualFold(encoding, "identity")) {
		return "", 0, errors.New("original file does not expose the required range")
	}
	hash := md5.New()
	n, err := io.Copy(hash, io.LimitReader(response.Body, last+2))
	if err != nil || n != last+1 {
		return "", 0, errors.New("original file range was incomplete")
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}
