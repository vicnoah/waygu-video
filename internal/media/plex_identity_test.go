package media

import "testing"

func TestPlexLegacyExternalIdentity(t *testing.T) {
	client := &plexClient{}
	for _, fixture := range []struct{ guid, provider, id string }{
		{"com.plexapp.agents.imdb://tt90000002?lang=en", "imdb", "tt90000002"},
		{"com.plexapp.agents.themoviedb://900000991?lang=en", "tmdb", "900000991"},
		{"local://1", "", ""},
		{"plex://movie/5d7768244de0ee001fcc7fed", "", ""},
	} {
		ids := client.item(plexMetadata{Type: "movie", PrimaryGuid: fixture.guid}).ExternalIDs
		if fixture.provider == "" {
			if len(ids) != 0 {
				t.Errorf("local or Plex GUID treated as an external ID: %+v", ids)
			}
		} else if ids[fixture.provider] != fixture.id {
			t.Errorf("legacy GUID %q gave %+v, want %s=%s", fixture.guid, ids, fixture.provider, fixture.id)
		}
	}
}
