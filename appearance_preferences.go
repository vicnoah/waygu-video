package main

import (
	"encoding/json"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

const appearanceKey = "appearance-v1"
const appearancePreferencesKey = "appearance-options-v1"

var appearanceChoices = []string{"system", "light", "dark"}

type homeModule struct {
	ID      string
	Enabled bool
}

const (
	homeContinue = "continue"
	homeRecent   = "recent"
	homeSeries   = "series"
	homeFavorite = "favorites"
	homeGrouped  = "sources"
	homeRandom   = "random"
)

func defaultHomeModules() []homeModule {
	return []homeModule{
		{homeContinue, true}, {homeRecent, true}, {homeSeries, true},
		{homeFavorite, true}, {homeGrouped, false}, {homeRandom, false},
	}
}

type solarLocation struct{ Latitude, Longitude float64 }

type appearancePreferences struct {
	Accent          string
	Poster          string
	Night, OLED     bool
	ShowWatched     bool
	ShowQuality     bool
	HideFinished    bool
	PosterAmbient   bool
	GroupingSet     bool
	HomeOrdered     bool
	SolarLocation   *solarLocation `json:",omitempty"`
	Home            []homeModule
	TVSourceShelves []homeModule `json:",omitempty"`
}

func readAppearancePreferences(storage core.Storage) appearancePreferences {
	prefs := appearancePreferences{Accent: "violet", Poster: "portrait", ShowWatched: true, ShowQuality: true}
	if raw, found := storage.GetItem(appearancePreferencesKey); found {
		next := prefs
		if json.Unmarshal([]byte(raw), &next) == nil {
			prefs = next
		}
	}
	if !slices.ContainsFunc(appearanceAccents, func(choice appearanceAccent) bool { return choice.id == prefs.Accent }) {
		prefs.Accent = "violet"
	}
	switch prefs.Poster {
	case "portrait", "landscape", "list":
	default:
		prefs.Poster = "portrait"
	}
	prefs.Home = normalizeHomeModules(prefs.Home, defaultHomeModules())
	return prefs
}

func normalizeHomeModules(saved, defaults []homeModule) []homeModule {
	var modules []homeModule
	for _, item := range saved {
		known := slices.ContainsFunc(defaults, func(option homeModule) bool { return option.ID == item.ID })
		if known && !slices.ContainsFunc(modules, func(saved homeModule) bool { return saved.ID == item.ID }) {
			modules = append(modules, item)
		}
	}
	for _, item := range defaults {
		if !slices.ContainsFunc(modules, func(saved homeModule) bool { return saved.ID == item.ID }) {
			modules = append(modules, item)
		}
	}
	return modules
}

func (p appearancePreferences) tvSourceShelves(sources []source) []homeModule {
	defaults := make([]homeModule, 0, len(sources))
	for _, entry := range sources {
		defaults = append(defaults, homeModule{ID: entry.ID, Enabled: true})
	}
	return normalizeHomeModules(p.TVSourceShelves, defaults)
}

func (p appearancePreferences) enabled(id string) bool {
	return slices.ContainsFunc(p.Home, func(module homeModule) bool { return module.ID == id && module.Enabled })
}

func (p appearancePreferences) forHome(groupedByDefault bool) appearancePreferences {
	if groupedByDefault && !p.GroupingSet {
		p.Home = slices.Clone(p.Home)
		for i := range p.Home {
			if p.Home[i].ID == homeGrouped {
				p.Home[i].Enabled = true
				break
			}
		}
	}
	return p
}

func (a *application) applyAppearance(cx *core.Scope) {
	stored := core.Untracked(cx, func() string { value, _ := a.storage.GetItem(appearanceKey); return value })
	if !slices.Contains(appearanceChoices, stored) {
		stored = "system"
	}
	a.appearance = core.NewSignal(cx, stored)
	a.appearancePrefs = core.NewSignalFunc(cx, core.Untracked(cx, func() appearancePreferences { return readAppearancePreferences(a.storage) }), nil)
	night := core.NewSignal(cx, false)
	cx.Effect(func() {
		prefs, mode := a.appearancePrefs.Get(), a.appearance.Get()
		if !prefs.Night || mode != "light" {
			night.Set(false)
			return
		}
		var next func() bool
		var refresh func()
		refresh = func() {
			now := cx.WallTime()
			night.Set(isAppearanceNight(now, prefs.SolarLocation))
			next = cx.AfterFunc(time.Minute-time.Duration(now.Second())*time.Second-time.Duration(now.Nanosecond()), refresh)
		}
		refresh()
		cx.Cleanup(func() { next() })
	})
	a.effectiveAppearance = core.NewMemo(cx, func() string {
		if a.appearance.Get() == "light" && night.Get() {
			return "dark"
		}
		return a.appearance.Get()
	})
	a.mountAppearance(cx)
}

func (a *application) mountAppearance(cx *core.Scope) {
	prefs := a.appearancePrefs.GetUntracked()
	cx.SetTheme(appearanceTheme(prefs.Accent, prefs.OLED))
	type themeChoice struct {
		accent string
		oled   bool
	}
	choice := core.NewMemo(cx, func() themeChoice {
		value := a.appearancePrefs.Get()
		return themeChoice{value.Accent, value.OLED}
	})
	cx.Effect(func() {
		selected := choice.Get()
		cx.SetTheme(appearanceTheme(selected.accent, selected.oled))
	})
	observeAppearance(cx, a.effectiveAppearance.Get)
}

func observeAppearance(cx *core.Scope, appearance func() string) {
	apply := func() {
		switch appearance() {
		case "light":
			cx.SetSubtreeAppearance(core.AppearanceLight)
		case "dark":
			cx.SetSubtreeAppearance(core.AppearanceDark)
		default:
			cx.SetSubtreeAppearance(core.AppearanceSystem)
		}
	}
	cx.Untrack(apply)
	cx.Effect(apply)
}

func (a *application) saveAppearance(edit func(*appearancePreferences)) error {
	value := a.appearancePrefs.GetUntracked()
	edit(&value)
	return storeSettings(a.storage, appearancePreferencesKey, a.appearancePrefs, value)
}

func (a *application) saveHomeModule(id string, enabled bool) error {
	return a.saveAppearance(func(p *appearancePreferences) {
		p.Home = slices.Clone(p.Home)
		for i := range p.Home {
			if p.Home[i].ID == id {
				p.Home[i].Enabled = enabled
				if id == homeGrouped {
					p.GroupingSet = true
				}
				break
			}
		}
	})
}

func (a *application) moveHomeModule(id string, offset int) error {
	modules, moved := reorderHomeModules(a.appearancePrefs.GetUntracked().Home, id, offset)
	if !moved {
		return nil
	}
	return a.saveAppearance(func(p *appearancePreferences) {
		p.Home = modules
		p.HomeOrdered = true
	})
}

func reorderHomeModules(modules []homeModule, id string, offset int) ([]homeModule, bool) {
	from := slices.IndexFunc(modules, func(item homeModule) bool { return item.ID == id })
	to := from + offset
	if from < 0 || to < 0 || to >= len(modules) || from == to {
		return modules, false
	}
	next := slices.Clone(modules)
	item := next[from]
	next = slices.Delete(next, from, from+1)
	return slices.Insert(next, to, item), true
}

func isAppearanceNight(now time.Time, location *solarLocation) bool {
	if location == nil || math.Abs(location.Latitude) > 90 || math.Abs(location.Longitude) > 180 {
		return now.Hour() >= 18 || now.Hour() < 6
	}
	// NOAA's solar position approximation. The 90.833° horizon includes refraction.
	utc := now.UTC()
	hour := float64(utc.Hour()) + float64(utc.Minute())/60
	days := float64(time.Date(utc.Year(), 12, 31, 0, 0, 0, 0, time.UTC).YearDay())
	gamma := 2 * math.Pi / days * (float64(utc.YearDay()-1) + (hour-12)/24)
	sin1, cos1 := math.Sincos(gamma)
	sin2, cos2 := math.Sincos(2 * gamma)
	sin3, cos3 := math.Sincos(3 * gamma)
	equation := 229.18 * (0.000075 + 0.001868*cos1 - 0.032077*sin1 - 0.014615*cos2 - 0.040849*sin2)
	declination := 0.006918 - 0.399912*cos1 + 0.070257*sin1 - 0.006758*cos2 + 0.000907*sin2 - 0.002697*cos3 + 0.00148*sin3
	radians := math.Pi / 180
	latitude := location.Latitude * radians
	angle := (hour*15 + equation/4 + location.Longitude - 180) * radians
	elevation := math.Sin(latitude)*math.Sin(declination) + math.Cos(latitude)*math.Cos(declination)*math.Cos(angle)
	return elevation < math.Cos(90.833*radians)
}

func (a *application) findAppearanceSunset(cx *core.Scope, done func(error)) {
	cx.RequestPermission(core.PermissionLocation, func(status core.PermissionStatus) {
		if !status.Allowed() {
			return
		}
		_, _ = cx.Locate(core.LocationOptions{Accuracy: 20_000, MaxAge: time.Hour, Timeout: 10 * time.Second},
			func(fix core.LocationFix, err error) {
				if err == nil && a.appearancePrefs.GetUntracked().Night {
					done(a.saveAppearance(func(p *appearancePreferences) {
						// A one-degree fix is enough for the appearance schedule.
						p.SolarLocation = &solarLocation{math.Round(fix.Latitude), math.Round(fix.Longitude)}
					}))
				}
			})
	})
}

func preferredDesktopLibraryMode(prefs appearancePreferences) desktopLibraryMode {
	if prefs.Poster == "list" {
		return desktopList
	}
	return desktopGrid
}
