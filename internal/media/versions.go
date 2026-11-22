package media

import (
	"cmp"
	"context"
	"slices"
)

// Version describes a distinct playable file exposed by one media server.
type Version struct {
	ID, Name, VideoCodec   string
	Width, Height, Bitrate int
	Size                   int64
}

type VersionClient interface {
	Versions(context.Context, Item) ([]Version, error)
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
		if resolution := cmp.Compare(int64(b.Width)*int64(b.Height), int64(a.Width)*int64(a.Height)); resolution != 0 {
			return resolution
		}
		return cmp.Compare(b.Bitrate, a.Bitrate)
	})
	return indices
}
