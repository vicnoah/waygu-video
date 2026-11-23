package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/internal/playback"
	"waygu-video/locales"
)

const (
	openSubtitleSecret = "opensubtitles-account-v1"
	onlineSubtitleID   = "online-subtitle"
)

var onlineSubtitleTransport = core.NewKey[http.RoundTripper]("waygu.subtitleTransport", nil)

type onlineSubtitlePlayback struct {
	item            media.Item
	sourceID, cache string
	storage         core.Storage
	secrets         core.Secrets
	transport       http.RoundTripper
	allowSearch     bool
}

type cachedOnlineSubtitle struct {
	Key            string `json:"-"`
	Name, Language string
	Text           []byte
	Delay          time.Duration
	cues           *playback.Captions
}

func (s onlineSubtitlePlayback) services(shooter bool) (playback.SubtitleServices, error) {
	creds, err := readOpenSubtitleCredentials(s.secrets)
	return playback.SubtitleServices{Open: creds, Shooter: shooter, HTTP: &http.Client{Transport: s.transport}}, err
}

func readOpenSubtitleCredentials(secrets core.Secrets) (playback.OpenSubtitleCredentials, error) {
	var credentials playback.OpenSubtitleCredentials
	data, exists, err := secrets.Get(openSubtitleSecret)
	if err == nil && exists {
		err = json.Unmarshal([]byte(data), &credentials)
	}
	return credentials, err
}

func (s onlineSubtitlePlayback) title() string {
	if s.item.Kind == media.Episode {
		return cmp.Or(s.item.SeriesTitle, s.item.Title)
	}
	return cmp.Or(s.item.OriginalTitle, s.item.Title)
}

func (s onlineSubtitlePlayback) query(stream media.Stream, title string, languages []string) playback.OnlineSubtitleQuery {
	query := playback.OnlineSubtitleQuery{
		Title: title, Year: s.item.Year, Languages: languages,
	}
	if title == s.title() {
		query.Original, query.FileName, query.FileSize = stream.Original(), stream.File.Name, stream.File.Size
		query.IMDb = s.item.ExternalIDs["imdb"]
		query.SeriesIMDb = s.item.SeriesExternalIDs["imdb"]
	}
	if s.item.Kind == media.Episode {
		query.Season, query.Episode = s.item.Season, s.item.Episode
	}
	return query
}

func (s onlineSubtitlePlayback) bind(stream media.Stream) string {
	if s.cache == "" || s.sourceID == "" || s.item.ID == "" || stream.VersionID() == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{s.sourceID, s.item.ID, stream.VersionID(),
		strconv.FormatInt(stream.File.Size, 10)}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func (s onlineSubtitlePlayback) cached(ctx context.Context, key string) (cachedOnlineSubtitle, error) {
	if key == "" || ctx.Err() != nil {
		return cachedOnlineSubtitle{}, nil
	}
	f, err := os.Open(filepath.Join(s.cache, key+".json"))
	if err != nil {
		return cachedOnlineSubtitle{}, nil
	}
	defer f.Close()
	var record cachedOnlineSubtitle
	if json.NewDecoder(io.LimitReader(f, 6<<20)).Decode(&record) != nil {
		return cachedOnlineSubtitle{}, nil
	}
	record.Key = key
	record.cues, err = playback.ReadCaptions(bytes.NewReader(record.Text))
	record.Text = nil
	if err != nil {
		return cachedOnlineSubtitle{}, nil
	}
	return record, nil
}

func (s onlineSubtitlePlayback) download(ctx context.Context, key string, match playback.OnlineSubtitle) (cachedOnlineSubtitle, error) {
	services, err := s.services(false)
	if err != nil && match.Provider == playback.OpenSubtitles {
		return cachedOnlineSubtitle{}, err
	}
	text, err := services.Download(ctx, match)
	if err != nil {
		return cachedOnlineSubtitle{}, err
	}
	cues, err := playback.ReadCaptions(bytes.NewReader(text))
	if err != nil {
		return cachedOnlineSubtitle{}, err
	}
	record := cachedOnlineSubtitle{Key: key, Name: match.Name, Language: match.Language, Delay: match.Delay, Text: text, cues: cues}
	if key != "" && ctx.Err() == nil {
		// The live selection is available even when the OS cache is full.
		_ = s.save(record)
	}
	record.Text = nil
	return record, nil
}

func (s onlineSubtitlePlayback) save(record cachedOnlineSubtitle) error {
	if err := os.MkdirAll(s.cache, 0o700); err != nil {
		return err
	}
	out, err := os.CreateTemp(s.cache, "subtitle-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	writeErr := json.NewEncoder(out).Encode(record)
	if err := errors.Join(writeErr, out.Close()); err != nil {
		return err
	}
	return os.Rename(out.Name(), filepath.Join(s.cache, record.Key+".json"))
}

func (s onlineSubtitlePlayback) savedDelay(file cachedOnlineSubtitle) time.Duration {
	if file.Key != "" {
		if value, ok := s.storage.GetItem("subtitle-delay-" + file.Key); ok {
			if delay, err := time.ParseDuration(value); err == nil {
				return min(max(delay, -10*time.Second), 10*time.Second)
			}
		}
	}
	return file.Delay
}

func (s onlineSubtitlePlayback) setDelay(key string, delay time.Duration) {
	if key != "" {
		_ = s.storage.SetItem("subtitle-delay-"+key, delay.String())
	}
}

func onlineSubtitleSearchRow(cx *core.Scope, action func()) core.Node {
	label := locales.OnlineSubtitleSearch(cx)
	providers := locales.OnlineSubtitleProviders(cx)
	if !phoneLandscape(cx) {
		return ui.Button{Text: label, Icon: lucide.Search, Variant: ui.ButtonOutline,
			TestID: "online-subtitle-search", OnPress: action}
	}
	return base.Button{Label: label, Hint: providers, TestID: "online-subtitle-search",
		Class: "h-10 flex-row items-center gap-3 bg-zinc-800/70 px-3", Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
		OnPress: func(core.PressEvent) { action() }, Children: []core.Node{
			core.Icon{SVG: lucide.Search(), Class: "size-4 text-violet-400"},
			core.Text{Content: label, Class: "flex-1 text-sm text-violet-300", MaxLines: 1},
			core.Text{Content: providers, Class: "text-[11px] text-zinc-500", MaxLines: 1},
		}}
}

func onlineSubtitleResultView(cx *core.Scope, online onlineSubtitlePlayback, stream media.Stream, prefs subtitlePreferences,
	busy func() bool, downloadError func() error, choose func(playback.OnlineSubtitle)) core.Node {
	configuring := core.NewSignal(cx, false)
	account := openSubtitleAccountStatus(cx, online.secrets)
	draft := core.NewSignal(cx, online.title())
	language := core.NewSignal(cx, "")
	type searchRequest struct{ query playback.OnlineSubtitleQuery }
	request := core.NewSignal(cx, &searchRequest{query: online.query(stream, online.title(), prefs.Languages)})
	matches := core.NewResource(cx, func() (*searchRequest, bool) {
		return request.Get(), true
	}, func(ctx context.Context, req *searchRequest, _ core.ResourceInfo) ([]playback.OnlineSubtitle, error) {
		services, err := online.services(!prefs.DisableShooter)
		if err != nil {
			return nil, err
		}
		if services.Open.Key == "" && !services.Shooter {
			return nil, playback.ErrSubtitleCredentials
		}
		return services.Search(ctx, req.query)
	})
	submit := func() {
		if busy() {
			return
		}
		title := strings.TrimSpace(draft.GetUntracked())
		if title == "" {
			return
		}
		languages := prefs.Languages
		if code := language.GetUntracked(); code != "" {
			languages = []string{code}
		}
		request.Set(&searchRequest{query: online.query(stream, title, languages)})
	}
	languages := []ui.Option[string]{{Label: locales.SettingsPreferredSubtitles(cx)}}
	for _, code := range subtitleLanguages {
		languages = append(languages, ui.Option[string]{Value: code, Label: settingsLanguageName(cx, code)})
	}
	results := core.View{Class: "gap-2.5", Children: []core.Node{
		ui.Input{Label: locales.OnlineSubtitleTitle(cx), Value: draft.Get, OnChange: draft.Set, OnSubmit: func(string) { submit() }, TestID: "online-subtitle-title"},
		core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
			core.View{Class: "min-w-0 flex-1", Children: []core.Node{
				ui.Select[string]{Label: locales.SettingsLanguages(cx), TestID: "online-subtitle-language",
					Options: languages, Value: language.Get, OnChange: language.Set},
			}},
			ui.Button{Text: locales.OnlineSubtitleSubmit(cx), TestID: "online-subtitle-submit", Busy: matches.Loading,
				Disabled: busy, OnPress: submit},
		}},
		core.Dynamic(func(cx *core.Scope) core.Node {
			if err := downloadError(); err != nil {
				return notice(onlineSubtitleError(cx, err))
			}
			return nil
		}),
		core.Dynamic(func(cx *core.Scope) core.Node {
			if matches.Loading() || busy() {
				return hint(locales.Loading(cx))
			}
			if err := matches.Err(); err != nil {
				return notice(onlineSubtitleError(cx, err))
			}
			found, _ := matches.Get()
			if len(found) == 0 {
				return hint(locales.OnlineSubtitleEmpty(cx))
			}
			rows := make([]core.Node, 0, len(found))
			for index, match := range found {
				provider := match.Provider
				if provider == playback.Shooter {
					provider = locales.OnlineSubtitleShooter(cx)
				}
				description := provider
				if code := subtitleLanguage(match.Language); code != "" {
					description += " · " + settingsLanguageName(cx, code)
				}
				if match.Exact {
					description += " · " + locales.OnlineSubtitleExact(cx)
				}
				rows = append(rows, base.Button{TestID: "online-subtitle-result-" + strconv.Itoa(index), Label: match.Name, Hint: description,
					Class: "min-h-12 flex-row items-center gap-3 bg-zinc-800/70 px-3 py-1.5",
					Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, Disabled: busy, OnPress: func(core.PressEvent) { choose(match) },
					Children: []core.Node{
						core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
							core.Text{Content: match.Name, MaxLines: 2, Class: "text-sm text-zinc-100"},
							core.Text{Content: description, MaxLines: 1, Class: "text-[11px] text-zinc-400"},
						}},
						core.Icon{SVG: lucide.Download(), Class: "size-4 text-violet-400"},
					}})
			}
			return core.View{Class: "gap-px overflow-hidden rounded-xl border border-white/10 bg-white/10", Children: rows}
		}),
		core.Show{When: func() bool {
			status, ready := account.Get()
			return ready && !status.Enabled
		}, Then: func(cx *core.Scope) core.Node {
			return ui.Button{Text: locales.OnlineSubtitleConfigure(cx), Variant: ui.ButtonOutline,
				TestID: "configure-opensubtitles-player", Disabled: busy, OnPress: func() { configuring.Set(true) }}
		}},
	}}
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if configuring.Get() {
			return core.View{Class: "gap-3", Children: []core.Node{
				ui.Button{Text: locales.Back(cx), Variant: ui.ButtonGhost, OnPress: func() { configuring.Set(false) }},
				openSubtitleAccountForm(cx, online.secrets, online.transport, func() {
					account.Refetch()
					configuring.Set(false)
					matches.Refetch()
				}),
			}}
		}
		return results
	})
}

func onlineSubtitleError(cx *core.Scope, err error) string {
	switch {
	case errors.Is(err, playback.ErrSubtitleCredentials):
		return locales.OnlineSubtitleCredentialError(cx)
	case errors.Is(err, playback.ErrSubtitleQuota):
		return locales.OnlineSubtitleLimit(cx)
	default:
		return locales.OnlineSubtitleError(cx)
	}
}

func newOnlineSubtitlePlayback(cx *core.Scope, a *application, item media.Item, sourceID string, allowSearch bool) *onlineSubtitlePlayback {
	cache := cx.CacheDir()
	if cache != "" {
		cache = filepath.Join(cache, "online-subtitles")
	}
	return &onlineSubtitlePlayback{item: item, sourceID: sourceID, cache: cache,
		storage: a.storage, secrets: a.secrets, transport: onlineSubtitleTransport.Use(cx), allowSearch: allowSearch}
}

func (file cachedOnlineSubtitle) selection() subtitleSelection {
	return subtitleSelection{id: onlineSubtitleID, name: file.Name, language: file.Language, cues: file.cues}
}
