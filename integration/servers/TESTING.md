# Media server integration tests

The Compose project starts dedicated loopback-only Jellyfin, Emby and Plex servers. Its generated H.264/AAC clips include a movie with two different audio tones, text subtitles and four chapters, and a second movie with 180p and 360p versions. The tests decode the selected audio and check the resolution of the selected version. The bootstrap script creates and scans two libraries on each server. Plex runs as an unclaimed local server, and the bootstrap merges the two version fixtures if Plex indexes them separately.

```sh
sh integration/servers/fixtures.sh
docker compose -f integration/servers/compose.yaml up -d --wait --wait-timeout 150
python3 integration/servers/bootstrap.py
GOFLAGS=-p=1 WAYGU_TEST_SERVERS=1 go tool nsgo test ./... -count=1 -v
```

The app and media adapter tests share the fixture accounts. Run the packages serially so one package can check a bookmark or favorite before another changes it.

The tests log in, read movie and television libraries, search, load details and episodes, resolve a playable MP4, and verify two HTTP byte ranges. With a 300 kbps limit, they follow the HLS playlist through the playback proxy and verify the first MPEG-TS segment served by each transcoder. Jellyfin also verifies Quick Connect before and after another signed-in client approves the code. A six-minute clip verifies start, pause/progress and stop events and the resume position returned by each server. Set the same `WAYGU_TEST_PASSWORD` for the bootstrap and the tests to change the local fixture account password before the initial setup.

The bootstrap enables seek previews for the movie fixtures and waits for each server to generate them. In `TestDockerServers`, the `seek-preview` subtests retrieve and decode frames at 0 and 12 seconds. Emby generates BIF for the six-minute resume clip. Jellyfin and Plex generate two visibly distinct previews for the track-selection clip. Rerun `bootstrap.py` on an existing test setup to generate these preview files. [Emby BIF API](https://dev.emby.media/reference/RestAPI/BifService/getVideosByIdIndexBif.html) · [Jellyfin Trickplay API](https://typescript-sdk.jellyfin.org/functions/generated-client.TrickplayApiFp.html) · [Plex part BIF API](https://developer.plex.tv/pms/#tag/Library/operation/get-part-bif-image)

Emby and Jellyfin import the same synthetic IMDb and TMDB IDs from the test movie's NFO sidecar. The app integration test checks that their two copies share a card, selects Emby, reads the actual video through the playback proxy and verifies that Emby gets the stop report. The unclaimed Plex server supplies a local GUID, so its same-name copy has its own card. The test also changes source priority and turns matching off.

The personal library checks set and clear watched state on all three servers and verify the film is visible in the scoped watch-history listing. Emby and Jellyfin also verify adding and removing the same fixture from favorites. The UI test uses a resumable Emby clip to confirm that marking it watched removes its resume bar and that saved favorites can be reopened.

The cross-source resume fixture is six minutes long so it meets the servers' resume requirements. The app test reports 82 seconds to Jellyfin and later reports 34 seconds to Emby. It checks that Continue Watching displays one card, opens the newer Emby copy at 34 seconds and reports back to Emby. The three server tests recover a queued checkpoint without a playback session and verify that a stale replay leaves newer server progress intact.

With `libmpv.so.2` installed on Linux, run the video-frame test against the same generated movie. It decodes a frame, seeks to a known position, renders an ASS comment with `osd-overlay` and checks the affected pixels.

```sh
WAYGU_TEST_MPV=1 go tool nsgo test . -run TestRenderWithASS -count=1 -v
```

Flymoo tests run against a local HTTP fixture and exercise the wire formats in `vicnoah/fnmovie` at `de2b3dd817011de1e5f54b65b5bcb8310431b306`. Plex and MediaBrowser endpoint mappings were checked against `edde746/plezy` at `8310991fa14f0cbac9c71cfb494d028d4314c7ff` and the actual containers. Reference checkouts can live under `opensrc/testdata/` so NSGo's app test discovery skips their own test files.

Flymoo quality fixtures cover the original NAS URL, NAS-hosted direct renditions, quality enumeration and the server's `play/play` transcode request. The progress fixture verifies that transcoding reports the actual media GUID and the active play link. The transcode request follows [Feiniu TV Lite's NAS client](https://github.com/greatbody/feiniu-tv-lite/blob/7688390e3bb6207d0fb806d9b8648257afecf229/app/src/main/java/ink/sunrui/feiniutv/network/NasApiClient.kt#L473-L526). A Flymoo NAS has yet to be used for transcode verification.

Stop the fixture services with `docker compose -f integration/servers/compose.yaml down`. The named volumes keep the fixture setup for subsequent runs.

With the same three fixtures, `TestDockerOfflineDownloadReview` saves an original video from the detail page, restarts the app with the saved data, disconnects the test gateway, opens the cached movie from the library and probes the local file with `ffprobe`. The headless case reconnects and verifies the offline checkpoint in the server's detail response. GTK also advances the native playback clock while the gateway refuses requests. The download store fixture checks byte ranges after cancellation, verifies the supplied response MD5 and final SHA-256, and checks the space limit and deletion.

`TestDockerMovieFeatures` reads local extras and recommendations from all three servers. Emby and Jellyfin index the generated trailer and featurette and resolve both clips for playback. The unclaimed Plex fixture returns an empty extras list. `TestDockerPhoneMovieReview` opens the Emby trailer in the native player, follows a recommendation from the same library, and tests the cast and playback controls. When adding the extras to an existing fixture library, rescan it with `bootstrap.py` before running the phone review. [Emby library API](https://dev.emby.media/reference/RestAPI/UserLibraryService.html) · [Jellyfin library API](https://typescript-sdk.jellyfin.org/classes/generated-client.LibraryApi.html) · [Plex Media Server API](https://developer.plex.tv/pms/)

`TestDockerSeriesEpisodes` expects `Waygu Review Series` to contain five episodes in season one, two in season two, and one in season zero on all three servers. The test resolves season two and the special for playback. `TestDockerPhoneSeriesReview` seeds an Emby bookmark at 2:24, downloads an episode, opens season two and verifies playback, returns to the selected season, and opens the specials chapter. It also checks sort order, the hidden watched filter, and the resume position.

`TestDockerLibraryFormats` reads the indexed version fixture from Emby, Jellyfin and Plex. The Jellyfin and Plex catalogs expose both 180p and 360p copies. `TestDockerPhoneLibraryReview` tests the compact poster wall, the list, and all four category tabs. The review configures two Jellyfin libraries as anime and documentaries through the app's category overrides. [Emby item API](https://dev.emby.media/reference/RestAPI/ItemsService/getItems.html) · [Jellyfin item fields](https://typescript-sdk.jellyfin.org/variables/generated-client.ItemFields.html) · [Plex Media Server API](https://developer.plex.tv/pms/)

`TestDockerCreditSearchScopesLibraries` looks up the fixture director Lin Chen through the movie and series libraries. Emby and Jellyfin resolve the director's credited movie using the person lookup and scoped item query. The unclaimed Plex fixture responds to the search with an empty credit hub. A wire-format test checks Plex title results from actor and director hubs, including exclusion of hidden and unscoped rows. [Emby person API](https://dev.emby.media/reference/RestAPI/PersonsService/getPersons.html) · [Jellyfin person API](https://typescript-sdk.jellyfin.org/classes/generated-client.PersonApi.html) · [Plex search API](https://developer.plex.tv/pms/)

`TestDockerPhonePlayerUsesSettings` opens the track test movie from Emby and Jellyfin with saved preferences. Both containers return the requested Japanese audio rendition and the English caption fallback. The test verifies the active audio row, caption text and font size, and the playback surface's initial speed, crop mode and seek requests.

`TestDockerPhoneSourceSettingsReview` checks authenticated library reads and refreshes on Emby, Jellyfin and Plex. It also checks the saved source order after button and drag reordering, matching preferences, and local removal after a swipe. `TestPhoneSourceSettingsOfflineAndEmpty` uses a loopback HTTP 503 response for the offline card and verifies that adding a source is available after the last source is removed.

`TestDockerPhonePlayerDesignReview` starts the generated track test movie through its Emby detail page. The native run decodes the video, selects an external Emby subtitle, checks all six aspect choices, and uses chapter times to verify an A/B repeat. It advances the test clock to exercise the sleep timer while the video plays. Its DandanPlay transport supplies generated comments locally. The headless run imports the corresponding XML fixture and checks that stale decoder notifications during a loop do not end playback.

`TestDockerServers` compares the four chapter names and times reported by each service to the embedded chapter metadata. `TestDockerPhonePlayerGesturesAndPreview` plays the Jellyfin copy, jumps to a chapter, and checks slider commits, lock behavior and the thumbnail returned by Jellyfin while the slider is held. The headless case also verifies the temporary speed command and its restoration after cancellation. On an existing Docker setup, run `fixtures.sh` and `bootstrap.py` again to import the chaptered fixture.


## DLNA casting

`TestDockerPhoneDLNACasting` connects to a local renderer fixture from the Emby detail page. It verifies byte ranges, video decode, play and pause, volume, seek, position polling and local resume. `TestDockerDLNAUsesServerTranscode` limits the renderer to HLS and decodes the stream produced by each of the three Docker servers.

For an independent receiver, run GMediaRender with GStreamer H.264 and AAC decoding installed on Linux. Set the interface name to the sender's local network interface. GMediaRender can advertise its existing MP4 decoder under the DLNA `video/mp4` MIME type with `--mime-filter`. [GMediaRender manual](https://manpages.ubuntu.com/manpages/resolute/man1/gmediarender.1.html)

```sh
gmediarender -I eth0 -p 51323 -f 'Waygu DLNA Lab' --mime-filter=+video/mp4 \
  --gstout-audiopipe='fakesink sync=true' --gstout-videopipe='fakesink sync=true'
```

In another shell, discover that receiver, start the Emby fixture, and verify playback time, seek, pause, and volume.

```sh
GOFLAGS=-p=1 WAYGU_TEST_SERVERS=1 WAYGU_TEST_DLNA_RENDERER='Waygu DLNA Lab' \
  go tool nsgo test . -run '^TestDockerDLNAExternalRenderer$' -count=1
```


## Intro and credit markers

`fixtures.sh` embeds named chapters in episode 3 of season 1 of Waygu Review Series. Rescan using `bootstrap.py` on an existing Docker setup. The Docker adapter tests read the opening and both credit ranges from Emby, Jellyfin and Plex. Wire fixtures also verify Emby typed chapters, Jellyfin MediaSegments and Plex marker offsets. The alternate-version fixture checks that Plex uses that version's own chapters. [Emby ChapterInfo](https://dev.emby.media/reference/pluginapi/MediaBrowser.Model.Entities.ChapterInfo.html) · [Jellyfin MediaSegments controller](https://github.com/jellyfin/jellyfin/blob/master/Jellyfin.Api/Controllers/MediaSegmentsController.cs) · [Plex Media Server API](https://developer.plex.tv/pms/)

```sh
GOFLAGS=-p=1 WAYGU_TEST_SERVERS=1 go tool nsgo test . -run '^TestDockerPhonePlaybackMarkers$' -count=1
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerPhonePlaybackMarkers$' -screenshots .nsgo/ui-review/playback-markers
```
