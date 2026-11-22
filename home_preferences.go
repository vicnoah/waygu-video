package main

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

func homeModuleOrder(settings appearancePreferences, shelves map[string]core.Node) []core.Node {
	children := make([]core.Node, 0, len(shelves))
	for _, module := range settings.Home {
		if module.Enabled && shelves[module.ID] != nil {
			children = append(children, shelves[module.ID])
		}
	}
	return children
}

func (a *application) homeFavorites(cx *core.Scope) *core.Resource[catalogResult] {
	enabled := core.NewMemo(cx, func() bool { return a.appearancePrefs.Get().enabled(homeFavorite) })
	return core.NewResource(cx, func() (catalogRequest, bool) {
		sources, parental := a.sources.Get(), a.parentalRules()
		a.libraryChanges.Get()
		return catalogRequest{sources: sources, filter: a.sourceFilter.Get(), limit: 18, parental: parental,
				merge: a.mergeTitles.Get(), personal: screenFavorites},
			len(sources) > 0 && enabled.Get() && !(parental.Enabled && parental.Kids)
	}, func(ctx context.Context, request catalogRequest, _ core.ResourceInfo) (catalogResult, error) {
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		return a.loadCatalog(ctx, request), nil
	})
}

func homeSuggestions(entries []catalogItem, seed uint64) []catalogItem {
	suggestions := slices.Clone(entries)
	rng := rand.New(rand.NewPCG(seed, 423))
	rng.Shuffle(len(suggestions), func(i, j int) { suggestions[i], suggestions[j] = suggestions[j], suggestions[i] })
	return suggestions[:min(len(suggestions), 10)]
}

type homeBackdrop struct{ address, image string }

func homePosterGlow(cx *core.Scope, background func() (homeBackdrop, bool)) core.Node {
	resource := core.NewResource(cx, background, func(ctx context.Context, backdrop homeBackdrop, _ core.ResourceInfo) (core.Color, error) {
		return sampleHomePoster(ctx, backdrop), nil
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if _, enabled := background(); !enabled || resource.Loading() {
			return nil
		}
		color, ready := resource.Get()
		if !ready || color == core.Transparent {
			return nil
		}
		return core.View{TestID: "home-poster-ambient", Class: "absolute inset-x-0 top-0 h-[520px]", PointerEvents: core.PointerEventsNone,
			StyleFunc: func() core.Style {
				opacity := float32(.22)
				if cx.Dark() {
					opacity = .32
				}
				return core.Style{Gradients: []core.Gradient{core.LinearGradient(180, color.Faded(opacity), core.Transparent)}}
			}}
	})
}

func sampleHomePoster(ctx context.Context, backdrop homeBackdrop) core.Color {
	base, err := media.NormalizeAddress(backdrop.address)
	if err != nil {
		return core.Transparent
	}
	target, err := url.Parse(backdrop.image)
	if err != nil || target.User != nil || target.Scheme != base.Scheme || !strings.EqualFold(target.Host, base.Host) {
		return core.Transparent
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return core.Transparent
	}
	client := http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return core.Transparent
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return core.Transparent
	}
	const sizeLimit = 2 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, sizeLimit+1))
	if err != nil || len(data) > sizeLimit {
		return core.Transparent
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width > 4096 || config.Height > 4096 {
		return core.Transparent
	}
	poster, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return core.Transparent
	}
	return dominantPosterColor(poster)
}

func dominantPosterColor(poster image.Image) core.Color {
	type bucket struct{ weight, red, green, blue int }
	var palette [64]bucket
	bounds := poster.Bounds()
	for y := bounds.Min.Y + bounds.Dy()/24; y < bounds.Max.Y; y += max(1, bounds.Dy()/12) {
		for x := bounds.Min.X + bounds.Dx()/24; x < bounds.Max.X; x += max(1, bounds.Dx()/12) {
			r, g, b, alpha := poster.At(x, y).RGBA()
			r, g, b = r>>8, g>>8, b>>8
			saturation, value := int(max(r, g, b)-min(r, g, b)), max(r, g, b)
			if alpha < 0xc000 || value < 40 || saturation < 12 {
				continue
			}
			tone := &palette[int(r/64)*16+int(g/64)*4+int(b/64)]
			weight := 20 + saturation
			tone.weight += weight
			tone.red += int(r) * weight
			tone.green += int(g) * weight
			tone.blue += int(b) * weight
		}
	}
	var best bucket
	for _, choice := range palette {
		if choice.weight > best.weight {
			best = choice
		}
	}
	if best.weight == 0 {
		return core.Transparent
	}
	rgb := uint32(best.red/best.weight)<<16 | uint32(best.green/best.weight)<<8 | uint32(best.blue/best.weight)
	return core.RGB(rgb)
}
