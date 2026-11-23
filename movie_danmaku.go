package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/i18n"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
	"waygu-video/locales"
)

type movieDanmakuInfo struct {
	dandan, logvar danmakuBinding
	saved          *int
}

func (info movieDanmakuInfo) total() (int, bool) {
	if info.saved != nil {
		return *info.saved, true
	}
	var total int
	known := false
	for _, binding := range []danmakuBinding{info.dandan, info.logvar} {
		if binding.ID > 0 && binding.Count != nil {
			total += *binding.Count
			known = true
		}
	}
	return total, known
}

type movieDanmakuSource struct {
	name string
	danmakuBinding
}

func (info movieDanmakuInfo) sources(cx *core.Scope) []movieDanmakuSource {
	var sources []movieDanmakuSource
	if info.dandan.ID > 0 {
		sources = append(sources, movieDanmakuSource{locales.PhonePlayerDandan(cx), info.dandan})
	}
	if info.logvar.ID > 0 {
		sources = append(sources, movieDanmakuSource{locales.LogvarName(cx), info.logvar})
	}
	return sources
}

func (a *application) movieDanmaku(cx *core.Scope, current route) func() movieDanmakuInfo {
	if current.downloadID != "" {
		saved := core.NewResource(cx, nil, func(context.Context, struct{}, core.ResourceInfo) (*int, error) {
			tracks, err := a.downloads.store.ReadTracks(current.downloadID)
			if err != nil || tracks.Danmaku == nil {
				return nil, err
			}
			count := len(tracks.Danmaku.Comments)
			return &count, nil
		})
		return func() movieDanmakuInfo {
			count, _ := saved.Get()
			return movieDanmakuInfo{saved: count}
		}
	}
	config := core.NewResource(cx, func() (struct{}, bool) {
		return struct{}{}, !a.danmakuPrefs.Get().DisableLogvar
	}, func(context.Context, struct{}, core.ResourceInfo) (danmaku.LogvarConfig, error) {
		return a.logvarConfig()
	})
	dandan := a.movieDanmakuBinding(cx, func() (string, danmaku.LogvarConfig) {
		if a.danmakuPrefs.Get().DisableDandan {
			return "", danmaku.LogvarConfig{}
		}
		return danmakuBindingKey(current), danmaku.LogvarConfig{}
	})
	logvar := a.movieDanmakuBinding(cx, func() (string, danmaku.LogvarConfig) {
		if a.danmakuPrefs.Get().DisableLogvar {
			return "", danmaku.LogvarConfig{}
		}
		if value, ok := config.Get(); ok && value.Address != "" {
			return logvarBindingKey(current, value), value
		}
		return "", danmaku.LogvarConfig{}
	})
	return func() movieDanmakuInfo { return movieDanmakuInfo{dandan: dandan(), logvar: logvar()} }
}

func (a *application) movieDanmakuBinding(cx *core.Scope, source func() (string, danmaku.LogvarConfig)) func() danmakuBinding {
	type lookup struct {
		key, raw string
		config   danmaku.LogvarConfig
		binding  danmakuBinding
	}
	bound := core.NewMemo(cx, func() lookup {
		key, config := source()
		if key == "" {
			return lookup{}
		}
		raw, _ := a.storage.GetItem(key)
		return lookup{key: key, raw: raw, config: config, binding: decodeDanmakuBinding(raw)}
	})
	type response struct {
		request lookup
		count   *int
	}
	cacheDir := cx.CacheDir()
	loaded := core.NewResource(cx, func() (lookup, bool) {
		request := bound.Get()
		return request, request.binding.ID > 0 && request.binding.Count == nil
	}, func(ctx context.Context, request lookup, _ core.ResourceInfo) (response, error) {
		var client *danmaku.DandanClient
		if request.config.Address != "" {
			var err error
			client, err = danmaku.NewLogvarClient(request.config)
			if err != nil {
				return response{}, err
			}
		} else {
			credentials, err := a.dandanCredentials()
			if err != nil || credentials.AppID == "" || credentials.Secret == "" {
				return response{}, err
			}
			client = a.newDandan(credentials)
		}
		comments, err := client.CachedComments(ctx, cacheDir, request.binding.ID)
		if err != nil {
			return response{}, err
		}
		count := len(comments)
		return response{request: request, count: &count}, nil
	})
	counted := func() (lookup, bool) {
		request := bound.Get()
		result, ready := loaded.Get()
		if ready && !loaded.Loading() && result.count != nil && result.request == request {
			request.binding.Count = result.count
			return request, true
		}
		return request, false
	}
	cx.Effect(func() {
		if request, updated := counted(); updated {
			encoded, _ := json.Marshal(request.binding)
			_ = a.storage.SetItem(request.key, string(encoded))
		}
	})
	return func() danmakuBinding {
		request, _ := counted()
		return request.binding
	}
}

func (a *application) movieDanmakuMatch(cx *core.Scope, current route, close func()) core.Node {
	dandan := a.onlineDanmaku(cx, current, media.Stream{}, nil, close, nil)
	logvar, _ := a.remoteDanmaku(cx, current, media.Stream{}, true, nil, close, nil)
	return core.Scroll{Class: "max-h-[350px]", TestID: "movie-danmaku-match-scroll", Children: []core.Node{
		core.View{Class: "gap-4", Children: []core.Node{dandan, logvar}},
	}}
}

func movieDanmakuCountLabel(cx *core.Scope, count int, compact bool) string {
	lang := cx.Env().Locale()
	if compact && count >= 10_000 && strings.HasPrefix(lang, "zh") {
		return locales.MovieDanmakuTenThousands(cx, i18n.FormatNumberSkeleton(lang, float64(count)/10_000, ".# group-off"))
	}
	return i18n.FormatInteger(lang, float64(count))
}
