package main

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/metadata"
)

const libraryPreferencesKey = "library-metadata-v1"
const tmdbSecretKey = "metadata-tmdb-token-v1"

type libraryPreferences struct {
	PreferExternal  bool
	TMDB            bool
	Language        string
	RefreshMinutes  int
	DisableProgress bool
}

func readLibraryPreferences(store core.Storage) libraryPreferences {
	var prefs libraryPreferences
	if raw, has := store.GetItem(libraryPreferencesKey); has {
		_ = json.Unmarshal([]byte(raw), &prefs)
	}
	if !slices.Contains([]string{"", "zh-CN", "en-US", "ja-JP", "ko-KR"}, prefs.Language) {
		prefs.Language = ""
	}
	if !slices.Contains([]int{0, 15, 30, 60}, prefs.RefreshMinutes) {
		prefs.RefreshMinutes = 0
	}
	return prefs
}

func (a *application) saveLibraryPreferences(edit func(*libraryPreferences)) error {
	prefs := a.libraryPrefs.GetUntracked()
	edit(&prefs)
	if err := storeSettings(a.storage, libraryPreferencesKey, a.libraryPrefs, prefs); err != nil {
		return err
	}
	a.setProgressSending(!prefs.DisableProgress)
	return nil
}

func (a *application) startLibraryPreferences(cx *core.Scope) {
	prefs := readLibraryPreferences(a.storage)
	a.libraryPrefs = core.NewSignal(cx, prefs)
	a.metadata = metadata.NewCache(nil)
	a.progress.allowServer.Store(!prefs.DisableProgress)
	interval := core.NewMemo(cx, func() int { return a.libraryPrefs.Get().RefreshMinutes })
	cx.Effect(func() {
		delay := time.Duration(interval.Get()) * time.Minute
		if delay == 0 {
			return
		}
		var stop func() bool
		var refresh func()
		refresh = func() {
			for _, source := range a.sources.GetUntracked() {
				a.checkSource(source, true)
			}
			stop = cx.AfterFunc(delay, refresh)
		}
		stop = cx.AfterFunc(delay, refresh)
		cx.Cleanup(func() { stop() })
	})
}
