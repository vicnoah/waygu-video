package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"waygu-video/internal/media"
)

const tmdbOrigin = "https://api.themoviedb.org/3"
const imageOrigin = "https://image.tmdb.org/t/p/"

var imdbID = regexp.MustCompile(`^tt[0-9]{5,14}$`)
var imagePath = regexp.MustCompile(`^/[a-zA-Z0-9_-]+\.(jpg|jpeg|png|webp)$`)

type titleInfo struct {
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalTitle string  `json:"original_title"`
	OriginalName  string  `json:"original_name"`
	Overview      string  `json:"overview"`
	Poster        string  `json:"poster_path"`
	Backdrop      string  `json:"backdrop_path"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int     `json:"vote_count"`
}

type cacheKey struct{ kind, id, language string }

type cacheEntry struct {
	title titleInfo
	until time.Time
}

type Cache struct {
	http  *http.Client
	mu    sync.Mutex
	items map[cacheKey]cacheEntry
	epoch uint64
}

func NewCache(transport http.RoundTripper) *Cache {
	return &Cache{http: &http.Client{Transport: transport, Timeout: 4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, items: make(map[cacheKey]cacheEntry)}
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.items)
	c.epoch++
}

func (c *Cache) Enrich(ctx context.Context, item media.Item, token, language string, preferExternal bool) (media.Item, error) {
	needsScore := item.Kind == media.Movie && item.Ratings.TMDB == 0
	if token == "" || (!preferExternal && !needsScore && item.Overview != "" && item.Poster != "" && item.Backdrop != "") {
		return item, nil
	}
	key, ok := tmdbKey(item, language)
	if !ok {
		return item, nil
	}
	now := time.Now()
	c.mu.Lock()
	cached, exists := c.items[key]
	epoch := c.epoch
	c.mu.Unlock()
	if exists && cached.until.After(now) {
		return cached.title.apply(item, preferExternal), nil
	}
	info, err := c.lookup(ctx, key, token)
	if err != nil {
		return item, err
	}
	c.mu.Lock()
	if c.epoch == epoch && ctx.Err() == nil {
		if len(c.items) >= 192 {
			c.evictOldest()
		}
		c.items[key] = cacheEntry{info, time.Now().Add(12 * time.Hour)}
	}
	c.mu.Unlock()
	return info.apply(item, preferExternal), nil
}

func (c *Cache) evictOldest() {
	var oldest cacheKey
	var until time.Time
	for candidate, entry := range c.items {
		if until.IsZero() || entry.until.Before(until) {
			oldest, until = candidate, entry.until
		}
	}
	delete(c.items, oldest)
}

func tmdbKey(item media.Item, language string) (cacheKey, bool) {
	kind := ""
	switch item.Kind {
	case media.Movie:
		kind = "movie"
	case media.Series:
		kind = "tv"
	default:
		return cacheKey{}, false
	}
	if id, err := strconv.ParseUint(item.ExternalIDs["tmdb"], 10, 31); err == nil && id > 0 {
		return cacheKey{kind, strconv.FormatUint(id, 10), language}, true
	}
	id := strings.TrimSpace(strings.ToLower(item.ExternalIDs["imdb"]))
	return cacheKey{kind, id, language}, imdbID.MatchString(id)
}

func (c *Cache) lookup(ctx context.Context, key cacheKey, token string) (titleInfo, error) {
	query := url.Values{"language": {key.language}}
	if !imdbID.MatchString(key.id) {
		var result titleInfo
		err := c.get(ctx, "/"+key.kind+"/"+key.id, query, token, &result)
		return result, err
	}
	query.Set("external_source", "imdb_id")
	var found struct {
		Movies []titleInfo `json:"movie_results"`
		Series []titleInfo `json:"tv_results"`
	}
	if err := c.get(ctx, "/find/"+key.id, query, token, &found); err != nil {
		return titleInfo{}, err
	}
	rows := found.Movies
	if key.kind == "tv" {
		rows = found.Series
	}
	if len(rows) == 1 {
		return rows[0], nil
	}
	return titleInfo{}, nil
}

func (c *Cache) get(ctx context.Context, path string, query url.Values, token string, output any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tmdbOrigin+path+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode != http.StatusOK {
		return errors.New("TMDB lookup failed")
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(output)
}

func (info titleInfo) apply(item media.Item, preferExternal bool) media.Item {
	if info.VoteCount > 0 && info.VoteAverage > 0 && info.VoteAverage <= 10 && (item.Ratings.TMDB == 0 || preferExternal) {
		item.Ratings.TMDB = info.VoteAverage
	}
	set := func(target *string, value string) {
		if value = strings.TrimSpace(value); value != "" && (*target == "" || preferExternal) {
			*target = value
		}
	}
	title, original := info.Title, info.OriginalTitle
	if item.Kind == media.Series {
		title, original = info.Name, info.OriginalName
	}
	set(&item.Title, title)
	set(&item.OriginalTitle, original)
	set(&item.Overview, info.Overview)
	if imagePath.MatchString(info.Poster) {
		set(&item.Poster, imageOrigin+"w500"+info.Poster)
	}
	if imagePath.MatchString(info.Backdrop) {
		set(&item.Backdrop, imageOrigin+"w1280"+info.Backdrop)
	}
	return item
}
