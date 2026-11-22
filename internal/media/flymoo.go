package media

import (
	"context"
	"crypto/md5"
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
)

type flymooClient struct {
	api
	session Session
}

func newFlymoo(a api, session Session) *flymooClient {
	a.client.CheckRedirect = func(next *http.Request, previous []*http.Request) error {
		if len(previous) >= 5 || !sameOrigin(next.URL, a.base) {
			return http.ErrUseLastResponse
		}
		var payload []byte
		if next.GetBody != nil {
			reader, err := next.GetBody()
			if err != nil {
				return err
			}
			payload, err = io.ReadAll(reader)
			reader.Close()
			if err != nil {
				return err
			}
		}
		next.Header.Set("authx", flymooSignature(next.URL, next.Method, payload, time.Now()))
		return nil
	}
	return &flymooClient{api: a, session: session}
}

// flymooSignature implements the fnOS media web client's authx wire format.
func flymooSignature(endpoint *url.URL, method string, body []byte, now time.Time) string {
	const seed = "NDzZTVxnRKP8Z0jXg1VAMonaG8akvh"
	const key = "16CCEB3D-AB42-077D-36A1-F355324E4237"
	nonce := strconv.Itoa(100000 + now.Nanosecond()%900000)
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	if method == http.MethodGet {
		canonical, _ := url.QueryUnescape(endpoint.Query().Encode())
		body = []byte(canonical)
	}
	input := strings.Join([]string{seed, endpoint.EscapedPath(), nonce, timestamp, md5String(body), key}, "_")
	return "nonce=" + nonce + "&timestamp=" + timestamp + "&sign=" + md5String([]byte(input))
}

func md5String(input []byte) string {
	sum := md5.Sum(input)
	return hex.EncodeToString(sum[:])
}

func (f *flymooClient) auth(token string) http.Header {
	headers := make(http.Header)
	headers.Set("X-Trim-Client", "web")
	headers.Set("X-Trim-Client-Version", "631")
	if token != "" {
		headers.Set("Authorization", token)
	}
	return headers
}

type flymooError int

func (e flymooError) Error() string { return fmt.Sprintf("Flymoo returned code %d", e) }
func (e flymooError) Is(err error) bool {
	return err == ErrAuthentication && (e == 401 || e == 403)
}

func (f *flymooClient) call(ctx context.Context, method, route string, query url.Values, body any, token string) (any, error) {
	endpoint, err := f.resolve("api/v1/"+route, query)
	if err != nil {
		return nil, err
	}
	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	headers := f.auth(token)
	headers.Set("Content-Type", "application/json")
	headers.Set("authx", flymooSignature(endpoint, method, payload, time.Now()))
	var result any
	if err := f.send(ctx, method, endpoint, headers, payload, &result); err != nil {
		return nil, err
	}
	if envelope, ok := result.(map[string]any); ok {
		code := int(flymooNumber(envelope, "code"))
		if code != 0 && code != 200 && code != 2000 {
			return nil, flymooError(code)
		}
		if data, exists := envelope["data"]; exists {
			return data, nil
		}
	}
	return result, nil
}

func (f *flymooClient) Probe(ctx context.Context) (ServerInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	data, err := f.call(ctx, http.MethodGet, "sys/config", nil, nil, "")
	if err != nil {
		return ServerInfo{}, err
	}
	config := flymooObject(data)
	info := ServerInfo{ID: flymooText(config, "server_guid", "guid"), Name: flymooText(config, "server_name", "name")}
	if info.ID == "" && info.Name == "" {
		return ServerInfo{}, ErrResponse
	}
	if data, err := f.call(ctx, http.MethodGet, "sys/version", nil, nil, ""); err == nil {
		info.Version = flymooText(flymooObject(data), "version")
	}
	return info, nil
}

func (f *flymooClient) SignIn(ctx context.Context, credentials Credentials) (Session, error) {
	body := struct {
		Username string `json:"username"`
		Password string `json:"password"`
		AppName  string `json:"app_name"`
	}{credentials.Username, credentials.Password, "fnmovie"}
	data, err := f.call(ctx, http.MethodPost, "login", nil, body, "")
	if err != nil {
		return Session{}, err
	}
	account := flymooObject(data)
	token := flymooText(account, "token", "access_token", "accessToken", "auth_token", "authToken")
	if token == "" {
		return Session{}, ErrResponse
	}
	return Session{Token: token, UserID: flymooText(account, "user_guid", "user_id", "uid")}, nil
}

func (f *flymooClient) Libraries(ctx context.Context) ([]Library, error) {
	data, err := f.call(ctx, http.MethodGet, "mdb/list", nil, nil, f.session.Token)
	if err != nil {
		return nil, err
	}
	var libraries []Library
	for _, entry := range flymooRows(data, "list", "items", "data", "results", "rows") {
		if id := flymooText(entry, "guid", "id", "mdb_guid"); id != "" {
			libraries = append(libraries, Library{ID: id, Name: flymooText(entry, "name", "title"),
				Kind: flymooText(entry, "category", "type", "view_type")})
		}
	}
	return libraries, nil
}

func (f *flymooClient) Catalog(ctx context.Context, q Query) (Page, error) {
	offset, limit := q.bounds()
	if term := strings.TrimSpace(q.Search); term != "" {
		params := url.Values{"q": {term}}
		if q.LibraryID != "" {
			params.Set("ancestor_guid", q.LibraryID)
		}
		data, err := f.call(ctx, http.MethodGet, "search/list", params, nil, f.session.Token)
		if err != nil {
			return Page{}, err
		}
		items, err := f.catalogItems(data, q.LibraryID, true)
		if err != nil {
			return Page{}, err
		}
		start := min(offset, len(items))
		return Page{Items: items[start:min(start+limit, len(items))], Total: len(items)}, nil
	}
	return f.itemPage(ctx, q, "item/list", nil)
}

func (f *flymooClient) itemPage(ctx context.Context, q Query, route string, tags map[string]string) (Page, error) {
	offset, limit := q.bounds()
	page := offset/limit + 1
	inPage := offset % limit
	items := make([]Item, 0, limit)
	total := 0
	for len(items) < limit {
		body := map[string]any{"page": page, "page_size": limit}
		if q.LibraryID != "" {
			body["ancestor_guid"] = q.LibraryID
		}
		if len(tags) != 0 {
			body["tags"] = tags
		}
		data, err := f.call(ctx, http.MethodPost, route, nil, body, f.session.Token)
		if err != nil {
			return Page{}, err
		}
		batch, err := f.catalogItems(data, q.LibraryID, false)
		if err != nil {
			return Page{}, err
		}
		total = int(flymooNumber(flymooObject(data), "total"))
		if total <= 0 {
			total = (page-1)*limit + len(batch)
			if len(batch) == limit {
				total++
			}
		}
		items = append(items, batch[min(inPage, len(batch)):]...)
		if inPage == 0 || page*limit >= total || len(batch) < limit {
			break
		}
		inPage = 0
		page++
	}
	return Page{Items: items[:min(len(items), limit)], Total: total}, nil
}

func (f *flymooClient) catalogItems(data any, library string, search bool) ([]Item, error) {
	rows := flymooRows(data, "list", "items", "data", "results", "medias", "movies", "tv", "rows")
	items := make([]Item, 0, len(rows))
	matched := make([]Item, 0, len(rows))
	scoped := false
	for _, row := range rows {
		item := f.item(row)
		if item.ID == "" {
			continue
		}
		items = append(items, item)
		if library != "" {
			id, hasID := flymooLibraryID(row)
			if search && id == "" {
				return nil, ErrResponse
			}
			scoped = scoped || hasID
			if id == library {
				matched = append(matched, item)
			}
		}
	}
	if scoped {
		return matched, nil
	}
	return items, nil
}

func (f *flymooClient) Detail(ctx context.Context, item Item) (Item, error) {
	resource := "movie"
	if item.Kind == Series {
		resource = "tv"
	}
	data, err := f.call(ctx, http.MethodGet, resource+"/"+url.PathEscape(item.ID), nil, nil, f.session.Token)
	if errors.Is(err, StatusError(http.StatusNotFound)) || errors.Is(err, StatusError(http.StatusNotImplemented)) {
		return item, nil
	}
	if err != nil {
		return Item{}, err
	}
	object := flymooObject(data)
	for _, key := range []string{"item", "info", "detail", "movie", "tv"} {
		if nested, ok := object[key].(map[string]any); ok {
			object = nested
			break
		}
	}
	detail := f.item(object)
	mergeFlymooDetail(&item, detail)
	return item, nil
}

func (f *flymooClient) Children(ctx context.Context, parent Item) ([]Item, error) {
	var route string
	switch parent.Kind {
	case Series:
		route = "season/list/"
	case Season:
		route = "episode/list/"
	default:
		return nil, nil
	}
	data, err := f.call(ctx, http.MethodGet, route+url.PathEscape(parent.ID), nil, nil, f.session.Token)
	if err != nil {
		return nil, err
	}
	return f.childItems(data, parent), nil
}
