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

func (a *application) onlineDanmaku(cx *core.Scope, current route, stream media.Stream,
	apply func([]danmaku.Comment, float64), closePanel func()) core.Node {
	key, seasonKey := danmakuBindingKey(current), danmakuSeasonKey(current)
	var saved danmaku.DandanEpisode
	var season danmakuSeason
	cx.Untrack(func() {
		if value, found := a.storage.GetItem(key); found {
			_ = json.Unmarshal([]byte(value), &saved)
		}
		if seasonKey != "" {
			if value, found := a.storage.GetItem(seasonKey); found {
				_ = json.Unmarshal([]byte(value), &season)
			}
		}
	})
	cache := cx.CacheDir()
	title, episode := current.item.Title, ""
	if current.item.Kind == media.Episode {
		if current.item.SeriesTitle != "" {
			title = current.item.SeriesTitle
		}
		if current.item.Episode > 0 {
			episode = strconv.Itoa(current.item.Episode)
		}
	}
	draft := core.NewSignal(cx, title)
	query := core.NewSignal(cx, danmakuRequest{action: danmakuIdentify})
	picked := core.NewSignal(cx, saved)
	matches := core.NewSignalFunc(cx, danmaku.EpisodeMatches{}, nil)
	saveError := core.NewSignal(cx, false)
	credentials := core.NewResource(cx, nil, func(context.Context, struct{}, core.ResourceInfo) (danmaku.DandanCredentials, error) {
		return a.dandanCredentials()
	})
	canSearch := func() bool {
		value, ok := credentials.Get()
		return ok && value.AppID != "" && value.Secret != ""
	}
	autoMatch := core.NewMemo(cx, func() bool { return a.danmakuPrefs.Get().AutoMatch })
	type danmakuQuery struct {
		danmakuRequest
		credentials danmaku.DandanCredentials
	}
	resource := core.NewResource(cx, func() (danmakuQuery, bool) {
		request := query.Get()
		value, _ := credentials.Get()
		active := request.action != danmakuIdle && value.AppID != "" && value.Secret != ""
		if request.action == danmakuIdentify && saved.ID <= 0 {
			active = active && autoMatch.Get()
		}
		return danmakuQuery{request, value}, active
	}, func(ctx context.Context, request danmakuQuery, _ core.ResourceInfo) (danmakuResult, error) {
		client := a.newDandan(request.credentials)
		var result danmakuResult
		var err error
		selected := request.pick
		switch request.action {
		case danmakuSearch:
			result.matches, err = client.Search(ctx, request.title, "")
			return result, err
		case danmakuIdentify:
			selected = saved
			if selected.ID <= 0 && season.Number > 0 && current.item.Episode != season.Number {
				selected, err = client.RelatedEpisode(ctx, season.Episode, current.item.Episode-season.Number)
				if errors.Is(err, danmaku.ErrDandanCredentials) || ctx.Err() != nil {
					return result, err
				}
				result.inferred = selected.ID > 0
			}
			if selected.ID <= 0 {
				selected, result.matches, err = identifyDanmaku(ctx, client, stream, title, episode)
				if err != nil || selected.ID <= 0 {
					return result, err
				}
			}
		}
		result.bound = selected
		result.comments, err = client.CachedComments(ctx, cache, selected.ID)
		return result, err
	})
	cx.Effect(func() {
		result, ready := resource.Get()
		if !ready {
			return
		}
		matches.Set(result.matches)
		if result.bound.ID <= 0 {
			return
		}
		picked.Set(result.bound)
		apply(result.comments, result.bound.Shift)
		encoded, _ := json.Marshal(result.bound)
		err := a.storage.SetItem(key, string(encoded))
		if err == nil && seasonKey != "" && result.bound.AnimeID > 0 && !result.inferred &&
			(query.GetUntracked().action == danmakuPick || saved.ID <= 0) {
			note, _ := json.Marshal(danmakuSeason{Episode: result.bound, Number: current.item.Episode})
			err = a.storage.SetItem(seasonKey, string(note))
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
	state := core.Dynamic(func(cx *core.Scope) core.Node {
		var rows []core.Node
		if selected := picked.Get(); selected.ID > 0 {
			rows = append(rows, hint(locales.DanmakuBoundEpisode(cx, selected.Label())),
				ui.Button{Text: locales.DanmakuUnbind(cx), Variant: ui.ButtonGhost, TestID: "danmaku-unbind", OnPress: func() {
					query.Set(danmakuRequest{})
					a.storage.RemoveItem(key)
					if seasonKey != "" {
						a.storage.RemoveItem(seasonKey)
					}
					picked.Set(danmaku.DandanEpisode{})
					matches.Set(danmaku.EpisodeMatches{})
					apply(nil, 0)
				}})
		}
		if credentials.Loading() || resource.Loading() {
			return core.View{Class: "gap-3", Children: append(rows, hint(locales.Loading(cx)))}
		}
		if credentials.Err() != nil {
			rows = append(rows, notice(locales.SecretUnavailable(cx)))
		} else if err := resource.Err(); err != nil && query.Get().action != danmakuIdle {
			message := locales.DanmakuOnlineError(cx)
			if errors.Is(err, danmaku.ErrDandanCredentials) {
				message = locales.DanmakuCredentialsError(cx)
			}
			rows = append(rows, notice(message), ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline,
				OnPress: resource.Refetch})
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
				TestID:  "danmaku-episode-" + strconv.FormatInt(candidate.ID, 10),
				OnPress: func() { submit(danmakuRequest{action: danmakuPick, pick: candidate}) }})
		}
		return core.View{Class: "gap-3", Children: rows}
	})
	return core.View{Class: "gap-3",
		Children: []core.Node{
			ui.Input{Label: locales.DanmakuSearchTitle(cx), Placeholder: locales.DanmakuSearchTitle(cx), Value: draft.Get,
				OnChange: draft.Set, TestID: "danmaku-search-title"},
			ui.Button{Text: locales.DanmakuSearch(cx), Icon: lucide.Search, TestID: "danmaku-search",
				Disabled: func() bool {
					return !canSearch() || utf8.RuneCountInString(strings.TrimSpace(draft.Get())) < 2
				},
				OnPress: edit},
			state,
		},
	}
}

func identifyDanmaku(ctx context.Context, client *danmaku.DandanClient, stream media.Stream, title, episode string) (danmaku.DandanEpisode, danmaku.EpisodeMatches, error) {
	file := danmaku.FileMatch{Name: strings.TrimSuffix(stream.File.Name, path.Ext(stream.File.Name)),
		Size: stream.File.Size, Duration: int64(stream.Duration / time.Second)}
	if stream.File.URL != "" {
		if hash, size, err := danmaku.Fingerprint(ctx, stream.File.URL, stream.File.Headers); err == nil {
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
