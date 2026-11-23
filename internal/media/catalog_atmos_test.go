package media

import (
	"encoding/json"
	"testing"
)

func TestCatalogAtmosMetadata(t *testing.T) {
	t.Run("Emby and Jellyfin", func(t *testing.T) {
		browser := &mediaBrowser{}
		var item browserItem
		err := json.Unmarshal([]byte(`{"Type":"Movie","MediaStreams":[{"Type":"Video","DisplayTitle":"Atmos","Width":3840}],
		  "MediaSources":[{"MediaStreams":[{"Type":"Audio","Codec":"truehd","Title":"Atmosphere"}]}]}`), &item)
		if err != nil {
			t.Fatal(err)
		}
		if browser.item(item).DolbyAtmos {
			t.Fatal("labels outside an Atmos audio track must leave the badge unset")
		}
		item.MediaSources[0].MediaStreams[0].Profile = "TrueHD + Atmos"
		if !browser.item(item).DolbyAtmos {
			t.Fatal("missing the Atmos profile from the catalog's media source")
		}
	})
	t.Run("Plex", func(t *testing.T) {
		client := &plexClient{}
		entry := plexMetadata{Type: "movie", Media: []plexMedia{
			{AudioCodec: "eac3", Part: []plexPart{{Stream: []plexTrack{{StreamType: 2, DisplayTitle: "Dolby Digital Plus"}}}}},
		}}
		if client.item(entry).DolbyAtmos {
			t.Fatal("E-AC-3 alone does not identify Dolby Atmos")
		}
		entry.Media[0].Part[0].Stream[0].DisplayTitle = "English (EAC3 Atmos 7.1)"
		if !client.item(entry).DolbyAtmos {
			t.Fatal("missing the Atmos label from the Plex audio stream")
		}
	})
}
