package media

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerVideoRanges(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider Provider
		wire     string
		want     videoRangeInfo
	}{
		{"Jellyfin enhancement layer", Jellyfin, `"VideoRangeType":"DOVIWithEL","DvProfile":7,"DvBlSignalCompatibilityId":6`,
			videoRangeInfo{name: "DOVI", compatible: "HDR10", dolbyProfile: 7}},
		{"Jellyfin HDR10 Plus", Jellyfin, `"VideoRangeType":"DOVIWithHDR10Plus","DvProfile":8,"DvBlSignalCompatibilityId":1`,
			videoRangeInfo{name: "DOVI", compatible: "HDR10+", dolbyProfile: 8}},
		{"Jellyfin invalid Dolby Vision with readable base", Jellyfin, `"VideoRangeType":"DOVIInvalid","DvProfile":8,"ColorTransfer":"smpte2084","ColorPrimaries":"bt2020"`,
			videoRangeInfo{name: "HDR10"}},
		{"Emby Dolby Vision", Emby, `"VideoRange":"HDR 10","ExtendedVideoType":"DolbyVision","ExtendedVideoSubType":"DoviProfile76"`,
			videoRangeInfo{name: "DOVI", compatible: "HDR10", dolbyProfile: 7}},
		{"Emby HLG compatible base", Emby, `"ExtendedVideoType":"DolbyVision","ExtendedVideoSubType":"DoviProfile84"`,
			videoRangeInfo{name: "DOVI", compatible: "HLG", dolbyProfile: 8}},
		{"Plex Dolby Vision HDR10", Plex, `"displayTitle":"4K DoVi/HDR10","DOVIProfile":"7","DOVIBLCompatID":"6"`,
			videoRangeInfo{name: "DOVI", compatible: "HDR10", dolbyProfile: 7}},
		{"Plex Dolby Vision without HDR10 compatibility", Plex, `"DOVIProfile":5,"DOVIBLCompatID":0,"colorTrc":"smpte2084","colorPrimaries":"bt2020"`,
			videoRangeInfo{name: "DOVI", dolbyProfile: 5}},
		{"Plex HDR with unspecified primaries", Plex, `"colorTrc":"smpte2084"`,
			videoRangeInfo{name: "HDR"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if test.provider == Plex {
					if request.URL.Path != "/library/metadata/video" {
						http.NotFound(w, request)
						return
					}
					fmt.Fprintf(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"video","Media":[{"id":1,"Part":[{"Stream":[{"streamType":1,%s}]}]}]}]}}`, test.wire)
					return
				}
				if request.URL.Path != "/Items/video/PlaybackInfo" {
					http.NotFound(w, request)
					return
				}
				fmt.Fprintf(w, `{"MediaSources":[{"Id":"main","MediaStreams":[{"Type":"Video",%s}]}]}`, test.wire)
			}))
			defer server.Close()
			client, err := NewClient(test.provider, server.URL, NewDevice("range test"), Session{UserID: "viewer", Token: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			versions, err := client.(VersionClient).Versions(t.Context(), Item{ID: "video", Kind: Movie})
			if err != nil || len(versions) != 1 {
				t.Fatalf("versions = %v, error = %v", versions, err)
			}
			got := videoRangeInfo{versions[0].VideoRange, versions[0].CompatibleRange, versions[0].DolbyVisionProfile}
			if got != test.want {
				t.Fatalf("range = %+v, want %+v", got, test.want)
			}
		})
	}
}
