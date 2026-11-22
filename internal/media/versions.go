package media

import (
	"cmp"
	"context"
	"net/url"
	"path"
	"slices"
	"strings"
)

// Version describes a playable file or rendition exposed by one media server.
type Version struct {
	ID, Name, VideoCodec   string
	Container, VideoRange  string
	FileName               string
	Tracks                 []Track
	Width, Height, Bitrate int
	Size                   int64
	Original               bool
	Method                 PlayMethod
}

type VersionClient interface {
	Versions(context.Context, Item) ([]Version, error)
}

func (item *Item) addVideoProfile(profile VideoProfile) {
	if !slices.Contains(item.VideoProfiles, profile) {
		item.VideoProfiles = append(item.VideoProfiles, profile)
	}
}

func videoFileName(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return ""
		}
		value = parsed.Path
	}
	if name := path.Base(value); name != "." && name != "/" {
		return name
	}
	return ""
}

type VersionPolicy string

const (
	VersionServerOrder VersionPolicy = ""
	VersionHighest     VersionPolicy = "highest"
	VersionFitBitrate  VersionPolicy = "fit-bitrate"
	VersionRemember    VersionPolicy = "remember"
)

// versionOrder ranks candidates while retaining their original indices,
// which Plex requires when opening a particular version.
func versionOrder(versions []Version, options PlaybackOptions) []int {
	indices := make([]int, len(versions))
	for i := range indices {
		indices[i] = i
	}
	if options.SourceID != "" || (options.VersionPolicy != VersionHighest && options.VersionPolicy != VersionFitBitrate) {
		return indices
	}
	slices.SortStableFunc(indices, func(i, j int) int {
		a, b := versions[i], versions[j]
		if options.VersionPolicy == VersionFitBitrate && options.MaxBitrate > 0 {
			// Prefer known versions that fit. If none fit, prefer the lowest
			// known bitrate to limit the amount of server conversion needed.
			rank := func(rate int) int {
				switch {
				case rate > 0 && rate <= options.MaxBitrate:
					return 0
				case rate > options.MaxBitrate:
					return 1
				default:
					return 2
				}
			}
			ar, br := rank(a.Bitrate), rank(b.Bitrate)
			if ar != br {
				return cmp.Compare(ar, br)
			}
			if ar == 1 && a.Bitrate != b.Bitrate {
				return cmp.Compare(a.Bitrate, b.Bitrate)
			}
		}
		if resolution := cmp.Compare(versionPixels(b), versionPixels(a)); resolution != 0 {
			return resolution
		}
		return cmp.Compare(b.Bitrate, a.Bitrate)
	})
	return indices
}

// PreferredVersion describes the automatic candidate before a playback
// session is negotiated. Playback may skip candidates the client cannot use.
func PreferredVersion(versions []Version, options PlaybackOptions) (Version, bool) {
	if options.SourceID != "" {
		for _, version := range versions {
			if version.ID == options.SourceID {
				return version, true
			}
		}
		return Version{}, false
	}
	if len(versions) == 0 {
		return Version{}, false
	}
	return versions[versionOrder(versions, options)[0]], true
}

func versionPixels(v Version) int64 {
	width, height := int64(max(v.Width, 0)), int64(max(v.Height, 0))
	if width == 0 {
		width = height * 16 / 9
	}
	return width * height
}
