package main

import "github.com/nsgo-dev/nsgo/core"

func (a *application) resetPreferences(cx *core.Scope) {
	cx.Batch(func() {
		for _, key := range []string{
			appearanceKey, appearancePreferencesKey, playbackPreferencesKey, subtitlePreferencesKey,
			danmakuPreferencesKey, diagnosticPreferencesKey, desktopPreferencesKey, desktopLastPageKey,
			mpvKey, bitrateKey, versionPolicyKey, autoNextKey, mergeTitlesKey, sourceTimeoutKey,
			downloadSlotsKey, downloadQuotaKey, downloadQualityKey, downloadWiFiKey, downloadTracksKey, libraryPreferencesKey,
		} {
			a.storage.RemoveItem(key)
		}
		appearance := readAppearancePreferences(a.storage)
		a.appearance.Set("system")
		a.appearancePrefs.Set(appearance)
		a.playbackPrefs.Set(readPlaybackPreferences(a.storage))
		a.subtitlePrefs.Set(readSubtitlePreferences(a.storage))
		a.danmakuPrefs.Set(readDanmakuPreferences(a.storage))
		a.diagnosticPrefs.Set(diagnosticPreferences{})
		a.diagnostics.enabled.Store(false)
		a.desktopPrefs.Set(readDesktopPreferences(a.storage))
		a.scope.SetLocale("")
		a.desktopLibraryState.Set(desktopLibraryState{mode: preferredDesktopLibraryMode(appearance)})
		a.phoneLibrary.Set(phoneLibraryState{})
		a.mpvPreferred.Set(false)
		a.bitrateLimit.Set(0)
		a.versionPolicy.Set("")
		a.autoNext.Set(true)
		a.mergeTitles.Set(true)
		a.sourceFilter.Set("")
		a.sourceTimeout.Set(readSourceTimeout(""))
		a.libraryPrefs.Set(readLibraryPreferences(a.storage))
		a.setProgressSending(true)
		if a.downloads != nil {
			a.downloads.slots.Set(2)
			a.downloads.quota.Set(0)
			a.downloads.height.Set(0)
			a.downloads.wifiOnly.Set(true)
			a.downloads.setDeleteWatched(false)
			a.downloads.tracks.Set(true)
			a.downloads.networkChanged()
		}
	})
}
