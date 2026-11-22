package main

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

const playbackPreferencesKey = "playback-controls-v1"

var playbackRates = []float32{0.5, 0.75, 1, 1.25, 1.5, 1.75, 2}
var seekIntervals = []int{5, 10, 15, 30, 60}

type playbackPreferences struct {
	BackSeconds      int
	NextSeconds      int
	Rate             float32
	Picture          string
	DisableAutoPiP   bool
	KeepIntroCredits bool
}

func readPlaybackPreferences(storage core.Storage) playbackPreferences {
	prefs := playbackPreferences{BackSeconds: 10, NextSeconds: 30, Rate: 1}
	if raw, ok := storage.GetItem(playbackPreferencesKey); ok {
		var saved playbackPreferences
		if json.Unmarshal([]byte(raw), &saved) == nil {
			prefs.DisableAutoPiP = saved.DisableAutoPiP
			prefs.KeepIntroCredits = saved.KeepIntroCredits
			if slices.Contains(seekIntervals, saved.BackSeconds) {
				prefs.BackSeconds = saved.BackSeconds
			}
			if slices.Contains(seekIntervals, saved.NextSeconds) {
				prefs.NextSeconds = saved.NextSeconds
			}
			if slices.Contains(playbackRates, saved.Rate) {
				prefs.Rate = saved.Rate
			}
			if saved.Picture == "crop" || saved.Picture == "stretch" {
				prefs.Picture = saved.Picture
			}
		}
	}
	return prefs
}

func (p playbackPreferences) backStep() time.Duration {
	return time.Duration(p.BackSeconds) * time.Second
}

func (p playbackPreferences) forwardStep() time.Duration {
	return time.Duration(p.NextSeconds) * time.Second
}

func storeSettings[T any](storage core.Storage, key string, signal *core.Signal[T], value T) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return storeSettingValue(storage, key, signal, value, string(raw))
}

func storeSettingValue[T any](storage core.Storage, key string, signal *core.Signal[T], value T, encoded string) error {
	if err := storage.SetItem(key, encoded); err != nil {
		return err
	}
	signal.Set(value)
	return nil
}
