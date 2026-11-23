package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
	"waygu-video/locales"
)

const dandanCredentialKey = "dandanplay-credentials-v1"

type danmakuFactory func(danmaku.DandanCredentials) *danmaku.DandanClient

func (a *application) dandanCredentials() (danmaku.DandanCredentials, error) {
	var result danmaku.DandanCredentials
	value, found, err := a.secrets.Get(dandanCredentialKey)
	if errors.Is(err, core.ErrSecretsUnavailable) {
		return result, nil
	}
	if err != nil || !found {
		return result, err
	}
	err = json.Unmarshal([]byte(value), &result)
	return result, err
}

func danmakuBindingKey(current route) string {
	return "danmaku-binding-v1:" + danmakuItemKey(current)
}

func danmakuItemKey(current route) string {
	return fmt.Sprintf("%s:%x", mediaAccountKey(current.source), sha256.Sum256([]byte(current.item.ID)))
}

func danmakuSeasonKey(current route) string {
	item := current.item
	if item.Kind != media.Episode || item.SeriesID == "" || item.Season <= 0 || item.Episode <= 0 {
		return ""
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s:%d", item.SeriesID, item.Season))
	return fmt.Sprintf("danmaku-season-v1:%s:%x", mediaAccountKey(current.source), sum)
}

type danmakuSeason struct {
	Episode danmaku.DandanEpisode
	Number  int
}

type danmakuAction uint8

const (
	danmakuIdle danmakuAction = iota
	danmakuIdentify
	danmakuSearch
	danmakuPick
)

type danmakuRequest struct {
	action danmakuAction
	title  string
	pick   danmaku.DandanEpisode
}

type danmakuResult struct {
	bound    danmaku.DandanEpisode
	comments []danmaku.Comment
	matches  danmaku.EpisodeMatches
	inferred bool
}

type danmakuBinding struct {
	danmaku.DandanEpisode
	Count *int `json:"commentCount,omitempty"`
}

func (a *application) onlineDanmaku(cx *core.Scope, current route, stream media.Stream,
	apply func([]danmaku.Comment, float64), closePanel, showOnMiss func()) core.Node {
	form, _ := a.remoteDanmaku(cx, current, stream, false, apply, closePanel, showOnMiss)
	return form
}

func (a *application) remoteDanmaku(cx *core.Scope, current route, stream media.Stream, logvar bool,
	apply func([]danmaku.Comment, float64), closePanel, showOnMiss func()) (core.Node, func() bool) {
	type credentials struct {
		dandan danmaku.DandanCredentials
		logvar danmaku.LogvarConfig
	}
	config := core.NewResource(cx, nil, func(context.Context, struct{}, core.ResourceInfo) (credentials, error) {
		if logvar {
			value, err := a.logvarConfig()
			return credentials{logvar: value}, err
		}
		value, err := a.dandanCredentials()
		return credentials{dandan: value}, err
	})
	type keys struct{ item, season string }
	bindingKeys := core.NewMemo(cx, func() keys {
		if !logvar {
			return keys{danmakuBindingKey(current), danmakuSeasonKey(current)}
		}
		value, ready := config.Get()
		if ready && value.logvar.Address != "" {
			return keys{item: logvarBindingKey(current, value.logvar)}
		}
		return keys{}
	})
	bindings := core.NewMemoFunc(cx, func() danmakuMatchBindings {
		key := bindingKeys.Get()
		return core.Untracked(cx, func() danmakuMatchBindings { return readDanmakuBindings(a.storage, key.item, key.season) })
	}, nil)
	cache := cx.CacheDir()
	title, _ := danmakuSearchTitle(current.item)
	draft := core.NewSignal(cx, title)
	var initial danmakuRequest
	if apply != nil {
		initial.action = danmakuIdentify
	}
	query := core.NewSignal(cx, initial)
	picked := core.NewSignal(cx, danmaku.DandanEpisode{})
	cx.Effect(func() { picked.Set(bindings.Get().item) })
	matches := core.NewSignalFunc(cx, danmaku.EpisodeMatches{}, nil)
	saveError := core.NewSignal(cx, false)
	sourceEnabled := core.NewMemo(cx, func() bool {
		prefs := a.danmakuPrefs.Get()
		if logvar {
			return !prefs.DisableLogvar
		}
		return !prefs.DisableDandan
	})
	configured := func() bool {
		value, ok := config.Get()
		return ok && (value.logvar.Address != "" || (value.dandan.AppID != "" && value.dandan.Secret != ""))
	}
	canSearch := func() bool {
		return sourceEnabled.Get() && configured()
	}
	autoMatch := core.NewMemo(cx, func() bool { return a.danmakuPrefs.Get().AutoMatch })
	type danmakuQuery struct {
		danmakuRequest
		credentials
		bindings danmakuMatchBindings
	}
	resource := core.NewResource(cx, func() (danmakuQuery, bool) {
		request := query.Get()
		value, _ := config.Get()
		saved := bindings.Get()
		active := request.action != danmakuIdle && canSearch()
		if request.action == danmakuIdentify && saved.item.ID <= 0 {
			active = active && autoMatch.Get()
		}
		return danmakuQuery{request, value, saved}, active
	}, func(ctx context.Context, request danmakuQuery, _ core.ResourceInfo) (danmakuResult, error) {
		result := danmakuResult{bound: request.pick}
		var client *danmaku.DandanClient
		var err error
		if logvar {
			client, err = danmaku.NewLogvarClient(request.credentials.logvar)
			if err != nil {
				return result, err
			}
		} else {
			client = a.newDandan(request.credentials.dandan)
		}
		switch request.action {
		case danmakuSearch:
			result.matches, err = client.Search(ctx, request.title, "")
			return result, err
		case danmakuIdentify:
			if logvar {
				result, err = findLogvarBinding(ctx, client, current.item, request.bindings.item)
			} else {
				result, err = findDanmakuBinding(ctx, client, stream, current.item, request.bindings)
			}
			if err != nil || result.bound.ID <= 0 {
				return result, err
			}
		}
		if apply != nil {
			result.comments, err = client.CachedComments(ctx, cache, result.bound.ID)
		}
		return result, err
	})
	prompted := false
	cx.Effect(func() {
		result, ready := resource.Get()
		if !ready || resource.Loading() || resource.Err() != nil {
			return
		}
		matches.Set(result.matches)
		if result.bound.ID <= 0 {
			prefs := a.danmakuPrefs.GetUntracked()
			if showOnMiss != nil && !prompted && prefs.Enabled && prefs.AutoMatch && sourceEnabled.GetUntracked() &&
				prefs.PromptOnMiss && query.GetUntracked().action == danmakuIdentify {
				prompted = true
				showOnMiss()
			}
			return
		}
		picked.Set(result.bound)
		if apply != nil {
			apply(result.comments, result.bound.Shift)
		}
		binding := danmakuBinding{DandanEpisode: result.bound}
		if apply != nil {
			count := len(result.comments)
			binding.Count = &count
		}
		encoded, _ := json.Marshal(binding)
		key := bindingKeys.GetUntracked()
		err := a.storage.SetItem(key.item, string(encoded))
		if err == nil && key.season != "" && result.bound.AnimeID > 0 && !result.inferred &&
			(query.GetUntracked().action == danmakuPick || bindings.GetUntracked().item.ID <= 0) {
			note, _ := json.Marshal(danmakuSeason{Episode: result.bound, Number: current.item.Episode})
			err = a.storage.SetItem(key.season, string(note))
		}
		saveError.Set(err != nil)
		if err == nil && query.GetUntracked().action == danmakuPick {
			closePanel()
		}
	})
	submit := func(request danmakuRequest) {
		saveError.Set(false)
		if request == query.GetUntracked() {
			resource.Refetch()
		} else {
			query.Set(request)
		}
	}
	edit := func() {
		if utf8.RuneCountInString(strings.TrimSpace(draft.GetUntracked())) >= 2 {
			submit(danmakuRequest{action: danmakuSearch, title: strings.TrimSpace(draft.GetUntracked())})
		}
	}
	id := "danmaku"
	searchLabel := locales.DanmakuSearch(cx)
	if logvar {
		id, searchLabel = "logvar", locales.LogvarSearch(cx)
	}
	state := core.Dynamic(func(cx *core.Scope) core.Node {
		var rows []core.Node
		if selected := picked.Get(); selected.ID > 0 {
			label := locales.DanmakuBoundEpisode(cx, selected.Label())
			if logvar {
				label = locales.LogvarBound(cx, selected.Label())
			}
			rows = append(rows, hint(label),
				ui.Button{Text: locales.DanmakuUnbind(cx), Variant: ui.ButtonGhost, TestID: id + "-unbind", OnPress: func() {
					query.Set(danmakuRequest{})
					key := bindingKeys.GetUntracked()
					a.storage.RemoveItem(key.item)
					if key.season != "" {
						a.storage.RemoveItem(key.season)
					}
					picked.Set(danmaku.DandanEpisode{})
					matches.Set(danmaku.EpisodeMatches{})
					if apply != nil {
						apply(nil, 0)
					}
				}})
		}
		if config.Loading() || resource.Loading() {
			return core.View{Class: "gap-3", Children: append(rows, hint(locales.Loading(cx)))}
		}
		if config.Err() != nil {
			rows = append(rows, notice(locales.SecretUnavailable(cx)))
		} else if err := resource.Err(); err != nil && query.Get().action != danmakuIdle {
			message := locales.DanmakuOnlineError(cx)
			if errors.Is(err, danmaku.ErrDandanCredentials) {
				message = locales.DanmakuCredentialsError(cx)
			}
			if logvar {
				message = locales.LogvarFailed(cx)
				if errors.Is(err, danmaku.ErrLogvarAuthentication) {
					message = locales.LogvarRejected(cx)
				}
			}
			rows = append(rows, notice(message), ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline,
				OnPress: resource.Refetch})
		} else if !sourceEnabled.Get() {
			rows = append(rows, hint(locales.SettingsDisabled(cx)))
		} else if !canSearch() {
			rows = append(rows, hint(locales.DanmakuConfigure(cx)))
		}
		if saveError.Get() {
			rows = append(rows, notice(locales.DanmakuBindingError(cx)))
		}
		results := matches.Get()
		if _, ready := resource.Get(); ready && canSearch() && picked.Get().ID <= 0 && len(results.Episodes) == 0 &&
			query.Get().action != danmakuIdle && resource.Err() == nil {
			rows = append(rows, hint(locales.DanmakuNoResults(cx)))
		}
		if results.More || len(results.Episodes) > 40 {
			rows = append(rows, hint(locales.DanmakuRefineSearch(cx)))
		}
		for _, candidate := range results.Episodes[:min(40, len(results.Episodes))] {
			rows = append(rows, ui.Button{Text: candidate.Label(), Variant: ui.ButtonOutline,
				TestID:  id + "-episode-" + strconv.FormatInt(candidate.ID, 10),
				OnPress: func() { submit(danmakuRequest{action: danmakuPick, pick: candidate}) }})
		}
		return core.View{Class: "gap-3", Children: rows}
	})
	form := core.View{Class: "gap-3",
		Children: []core.Node{
			ui.Input{Label: locales.DanmakuSearchTitle(cx), Placeholder: locales.DanmakuSearchTitle(cx), Value: draft.Get,
				OnChange: draft.Set, TestID: id + "-search-title"},
			ui.Button{Text: searchLabel, Icon: lucide.Search, TestID: id + "-search",
				Disabled: func() bool {
					return !canSearch() || utf8.RuneCountInString(strings.TrimSpace(draft.Get())) < 2
				},
				OnPress: edit},
			state,
		},
	}
	if logvar {
		return core.Show{When: configured, Then: func(cx *core.Scope) core.Node { return form }}, configured
	}
	return form, configured
}

type danmakuMatchBindings struct {
	item   danmaku.DandanEpisode
	season danmakuSeason
}

func readDanmakuBindings(storage core.Storage, key, seasonKey string) danmakuMatchBindings {
	var bindings danmakuMatchBindings
	if key != "" {
		if value, found := storage.GetItem(key); found {
			_ = json.Unmarshal([]byte(value), &bindings.item)
		}
	}
	if seasonKey != "" {
		if value, found := storage.GetItem(seasonKey); found {
			_ = json.Unmarshal([]byte(value), &bindings.season)
		}
	}
	return bindings
}

func danmakuSearchTitle(item media.Item) (string, string) {
	title, episode := item.Title, ""
	if item.Kind == media.Episode {
		if item.SeriesTitle != "" {
			title = item.SeriesTitle
		}
		if item.Episode > 0 {
			episode = strconv.Itoa(item.Episode)
		}
	}
	return title, episode
}

func findDanmakuBinding(ctx context.Context, client *danmaku.DandanClient, stream media.Stream, item media.Item, bindings danmakuMatchBindings) (danmakuResult, error) {
	if bindings.item.ID > 0 {
		return danmakuResult{bound: bindings.item}, nil
	}
	season := bindings.season
	if season.Number > 0 && item.Episode != season.Number {
		selected, err := client.RelatedEpisode(ctx, season.Episode, item.Episode-season.Number)
		if selected.ID > 0 || errors.Is(err, danmaku.ErrDandanCredentials) || ctx.Err() != nil {
			return danmakuResult{bound: selected, inferred: selected.ID > 0}, err
		}
	}
	title, episode := danmakuSearchTitle(item)
	selected, matches, err := identifyDanmaku(ctx, client, stream, title, episode)
	return danmakuResult{bound: selected, matches: matches}, err
}

func identifyDanmaku(ctx context.Context, client *danmaku.DandanClient, stream media.Stream, title, episode string) (danmaku.DandanEpisode, danmaku.EpisodeMatches, error) {
	file := danmaku.FileMatch{Name: strings.TrimSuffix(stream.File.Name, path.Ext(stream.File.Name)),
		Size: stream.File.Size, Duration: int64(stream.Duration / time.Second)}
	if stream.File.URL != "" {
		if hash, size, err := danmaku.Fingerprint(ctx, stream.Original()); err == nil {
			file.Hash, file.Size = hash, size
			if file.Name == "" {
				file.Name = strings.TrimSpace(title + " " + episode)
			}
		}
	}
	if ctx.Err() != nil {
		return danmaku.DandanEpisode{}, danmaku.EpisodeMatches{}, ctx.Err()
	}
	if file.Name != "" {
		matches, err := client.Match(ctx, file)
		if err == nil && matches.Exact && file.Hash != "" {
			return matches.Episodes[0], danmaku.EpisodeMatches{}, nil
		}
		if err != nil || len(matches.Episodes) > 0 {
			return danmaku.DandanEpisode{}, matches, err
		}
	}
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) < 2 {
		return danmaku.DandanEpisode{}, danmaku.EpisodeMatches{}, nil
	}
	matches, err := client.Search(ctx, title, episode)
	return danmaku.DandanEpisode{}, matches, err
}
