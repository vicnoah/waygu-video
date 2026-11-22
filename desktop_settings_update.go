package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

const wayguReleasePage = "https://github.com/vicnoah/waygu-video/releases"

func latestWayguRelease(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/vicnoah/waygu-video/releases/latest", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if response.StatusCode != http.StatusOK {
		return "", errors.New("release lookup failed")
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&release)
	return strings.TrimSpace(release.Tag), err
}

func (a *application) desktopSettingsUpdate(cx *core.Scope) core.Node {
	request := core.NewSignal(cx, uint64(0))
	latest := core.NewResource(cx, func() (uint64, bool) {
		revision := request.Get()
		return revision, revision > 0
	}, func(ctx context.Context, _ uint64, _ core.ResourceInfo) (string, error) {
		return latestWayguRelease(ctx)
	})
	summary := func() string {
		if latest.Loading() {
			return locales.Loading(cx)
		}
		if latest.Err() != nil {
			return locales.DesktopSettingsUpdateError(cx)
		}
		if version, ok := latest.Get(); ok {
			if version == "" {
				return locales.DesktopSettingsNoRelease(cx)
			}
			if strings.TrimPrefix(version, "v") == strings.TrimPrefix(core.App().Version, "v") {
				return locales.DesktopSettingsCurrentRelease(cx)
			}
			return locales.DesktopSettingsPublishedRelease(cx, version)
		}
		return ""
	}
	return desktopFormGroup(locales.DesktopSettingsUpdate(cx),
		desktopFormRowHeight(locales.DesktopSettingsAutoUpdate(cx), "h-12", desktopFormToggle(cx, "desktop-auto-update", locales.DesktopSettingsAutoUpdate(cx), nil, nil)),
		core.View{Class: "min-h-14 " + desktopSettingsRow, Children: []core.Node{
			core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
				core.Text{Content: locales.DesktopSettingsVersion(cx, core.App().Version), Class: "text-sm " + setupTitle},
				core.Show{When: func() bool { return summary() != "" }, Then: func(*core.Scope) core.Node {
					return core.Text{ContentFunc: summary, Class: "text-xs " + setupMuted, TestID: "desktop-update-status"}
				}},
			}},
			core.Show{When: func() bool { release, _ := latest.Get(); return release != "" }, Then: func(cx *core.Scope) core.Node {
				return ui.Button{Text: locales.DesktopSettingsReleasePage(cx), Size: ui.SizeSmall, Variant: ui.ButtonGhost,
					OnPress: func() { cx.OpenURL(wayguReleasePage, nil) }}
			}},
			desktopSettingsAction(cx, "desktop-check-update", locales.DesktopSettingsCheckUpdate(cx), "h-7 items-center justify-center bg-zinc-900/10 px-3 font-medium dark:bg-white/10 "+setupTitle, func() {
				if !latest.Loading() {
					request.Update(func(value uint64) uint64 { return value + 1 })
				}
			}),
		}},
	)
}
