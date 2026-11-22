# Media server integration tests

The Compose project starts dedicated loopback-only Jellyfin, Emby and Plex servers. Its generated H.264/AAC clips include a movie with two different audio tones and text subtitles and a second movie with 180p and 360p versions. The tests decode the selected audio and check the resolution of the selected version. The bootstrap script creates and scans two libraries on each server. Plex runs as an unclaimed local server, and the bootstrap merges the two version fixtures if Plex indexes them separately.

```sh
sh integration/servers/fixtures.sh
docker compose -f integration/servers/compose.yaml up -d --wait --wait-timeout 150
python3 integration/servers/bootstrap.py
WAYGU_TEST_SERVERS=1 go tool nsgo test . -count=1 -v
```

The tests log in, read movie and television libraries, search, load details and episodes, resolve a playable MP4, and verify two HTTP byte ranges. With a 300 kbps limit, they follow the HLS playlist through the playback proxy and verify the first MPEG-TS segment served by each transcoder. Jellyfin also verifies Quick Connect before and after another signed-in client approves the code. A six-minute clip verifies start, pause/progress and stop events and the resume position returned by each server. Set the same `WAYGU_TEST_PASSWORD` for the bootstrap and the tests to change the local fixture account password before the initial setup.

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
