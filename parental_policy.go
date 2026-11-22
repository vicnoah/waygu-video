package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

const parentalSettingsKey = "parental-controls-v1"
const parentalPINKey = "parental-pin-v1"

type parentalRating uint8

const (
	ratingUnknown parentalRating = iota
	ratingG
	ratingPG
	ratingPG13
	ratingR
	ratingAdult
	ratingUnlimited
)

type parentalLibrary struct{ Source, ID string }

type parentalPolicy struct {
	Enabled, PINSet, Kids, TimeLimit bool
	Rating                           parentalRating
	RestrictSources                  bool
	Sources                          []string
	Hidden                           []parentalLibrary
	Weekday, Weekend                 int
	Quiet                            bool
	QuietFrom, QuietUntil            int
}

func readParentalPolicy(storage core.Storage) parentalPolicy {
	prefs := parentalPolicy{Rating: ratingPG13, Weekday: 60, Weekend: 120, QuietFrom: 21*60 + 30, QuietUntil: 7 * 60}
	if raw, found := storage.GetItem(parentalSettingsKey); found {
		saved := prefs
		if json.Unmarshal([]byte(raw), &saved) == nil {
			prefs = saved
		}
	}
	if prefs.Rating < ratingG || prefs.Rating == ratingAdult || prefs.Rating > ratingUnlimited {
		prefs.Rating = ratingPG13
	}
	prefs.Weekday = max(1, min(prefs.Weekday, 24*60))
	prefs.Weekend = max(1, min(prefs.Weekend, 24*60))
	prefs.QuietFrom = max(0, min(prefs.QuietFrom, 24*60-1))
	prefs.QuietUntil = max(0, min(prefs.QuietUntil, 24*60-1))
	return prefs
}

func (a *application) parentalRules() parentalPolicy {
	if a.parental == nil {
		return parentalPolicy{}
	}
	return a.parental.Get()
}

func (p parentalPolicy) sourceAllowed(id string) bool {
	return !p.Enabled || !p.RestrictSources || slices.Contains(p.Sources, id)
}

func (p parentalPolicy) libraryAllowed(source, id string) bool {
	if !p.Enabled {
		return true
	}
	return !slices.ContainsFunc(p.Hidden, func(library parentalLibrary) bool {
		return library.Source == source && (id == "" || id == library.ID)
	})
}

func contentAgeRating(raw string) parentalRating {
	value := strings.ToUpper(strings.TrimSpace(raw))
	value = strings.TrimPrefix(value, "US/")
	switch value {
	case "G", "TV-G", "TV-Y":
		return ratingG
	case "PG", "TV-PG", "TV-Y7", "TV-Y7-FV":
		return ratingPG
	case "PG-13", "TV-14":
		return ratingPG13
	case "R", "TV-MA":
		return ratingR
	case "NC-17", "X", "XXX":
		return ratingAdult
	default:
		return ratingUnknown
	}
}

type parentalDecision uint8

const (
	parentalAllowed parentalDecision = iota
	parentalUnrated
	parentalBlocked
)

func (p parentalPolicy) assess(source string, item media.Item) parentalDecision {
	if !p.Enabled {
		return parentalAllowed
	}
	if !p.sourceAllowed(source) || !p.libraryAllowed(source, item.LibraryID) {
		return parentalBlocked
	}
	if p.Rating == ratingUnlimited && !p.Kids {
		return parentalAllowed
	}
	rating := contentAgeRating(item.ContentRating)
	if rating == ratingUnknown {
		return parentalUnrated
	}
	if rating > p.Rating {
		return parentalBlocked
	}
	if p.Kids && rating > ratingG {
		family := slices.ContainsFunc(item.Genres, func(genre string) bool {
			switch strings.ToLower(strings.TrimSpace(genre)) {
			case "family", "animation", "kids", "家庭", "动画", "儿童":
				return true
			}
			return false
		})
		if !family || rating > ratingPG {
			return parentalBlocked
		}
	}
	return parentalAllowed
}

func (p parentalPolicy) visible(source string, item media.Item) bool {
	return p.assess(source, item) == parentalAllowed
}

func (p parentalPolicy) catalog(entries []catalogItem) []catalogItem {
	if !p.Enabled {
		return entries
	}
	visible := make([]catalogItem, 0, len(entries))
	for _, entry := range entries {
		if p.visible(entry.source.ID, entry.item) {
			visible = append(visible, entry)
		}
	}
	return visible
}

var errParentalMedia = errors.New("restricted media")
var errParentalPIN = errors.New("invalid parental PIN")
var errParentalPINMismatch = errors.New("parental PIN mismatch")

func parentalPINValid(pin string) bool {
	return len(pin) == 4 && strings.IndexFunc(pin, func(c rune) bool { return c < '0' || c > '9' }) < 0
}

func newParentalPIN(pin string) (string, error) {
	if !parentalPINValid(pin) {
		return "", errParentalPIN
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash, err := parentalPINHash(pin, salt)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(append(salt, hash...)), nil
}

func parentalPINHash(pin string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, pin, salt, 100_000, sha256.Size)
}

func verifyParentalPIN(value, pin string) bool {
	if !parentalPINValid(pin) {
		return false
	}
	record, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(record) != 16+sha256.Size {
		return false
	}
	hash, err := parentalPINHash(pin, record[:16])
	return err == nil && subtle.ConstantTimeCompare(hash, record[16:]) == 1
}

type parentalLimit uint8

const (
	parentalCanPlay parentalLimit = iota
	parentalTimeUsed
	parentalQuietHours
)

type parentalUsage struct {
	Day   string
	Used  time.Duration
	Bonus time.Duration
}

func readParentalUsage(storage core.Storage) parentalUsage {
	var usage parentalUsage
	if raw, found := storage.GetItem(parentalUsageKey); found {
		_ = json.Unmarshal([]byte(raw), &usage)
	}
	usage.Used = max(0, usage.Used)
	usage.Bonus = max(0, usage.Bonus)
	return usage
}

const parentalUsageKey = "parental-usage-v1"

func (u parentalUsage) onDay(now time.Time) parentalUsage {
	day := now.Format(time.DateOnly)
	if u.Day == day {
		return u
	}
	return parentalUsage{Day: day}
}

func (p parentalPolicy) quietAt(now time.Time) bool {
	minute := now.Hour()*60 + now.Minute()
	switch {
	case !p.Quiet || p.QuietFrom == p.QuietUntil:
		return false
	case p.QuietFrom < p.QuietUntil:
		return minute >= p.QuietFrom && minute < p.QuietUntil
	default:
		return minute >= p.QuietFrom || minute < p.QuietUntil
	}
}

func (p parentalPolicy) viewingLimit(now time.Time, usage parentalUsage) (parentalLimit, time.Duration) {
	if !p.Enabled || !p.TimeLimit {
		return parentalCanPlay, 0
	}
	if p.quietAt(now) {
		return parentalQuietHours, 0
	}
	minutes := p.Weekday
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
		minutes = p.Weekend
	}
	usage = usage.onDay(now)
	remaining := time.Duration(minutes)*time.Minute + usage.Bonus - usage.Used
	if remaining <= 0 {
		return parentalTimeUsed, 0
	}
	return parentalCanPlay, remaining
}

func (a *application) kidsBrowsing() bool {
	p := a.parentalRules()
	return p.Enabled && p.Kids
}

func (a *application) browseSources() []source {
	p := a.parentalRules()
	sources := a.sources.Get()
	if !p.Enabled {
		return sources
	}
	visible := make([]source, 0, len(sources))
	for _, entry := range sources {
		if !p.sourceAllowed(entry.ID) {
			continue
		}
		entry.Libraries = slices.DeleteFunc(slices.Clone(entry.Libraries), func(library sourceLibrary) bool {
			return !p.libraryAllowed(entry.ID, library.ID)
		})
		visible = append(visible, entry)
	}
	return visible
}
