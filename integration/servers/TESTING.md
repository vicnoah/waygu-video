# Media server integration tests

The Compose project starts dedicated loopback-only Jellyfin, Emby and Plex servers. Its media directory holds generated H.264/AAC clips and an English SRT file. The bootstrap script creates and scans two libraries on each server. Plex runs as an unclaimed local server for these tests.

```sh
sh integration/servers/fixtures.sh
docker compose -f integration/servers/compose.yaml up -d
python3 integration/servers/bootstrap.py
WAYGU_TEST_SERVERS=1 go tool nsgo test . -count=1 -v
```

The tests log in, read movie and television libraries, search, load details and episodes, resolve a playable MP4, and verify two HTTP byte ranges. Jellyfin also verifies Quick Connect before and after another signed-in client approves the code. A six-minute clip verifies start, pause/progress and stop events and the resume position returned by each server. Set the same `WAYGU_TEST_PASSWORD` for the bootstrap and the tests to change the local fixture account password before the initial setup.

With `libmpv.so.2` installed on Linux, run the video-frame test against the same generated movie. It decodes a frame, seeks to a known position, renders an ASS comment with `osd-overlay` and checks the affected pixels.

```sh
WAYGU_TEST_MPV=1 go tool nsgo test . -run TestRenderWithASS -count=1 -v
```

Flymoo tests run against a local HTTP fixture and exercise the wire formats in `vicnoah/fnmovie` at `de2b3dd817011de1e5f54b65b5bcb8310431b306`. Plex and MediaBrowser endpoint mappings were checked against `edde746/plezy` at `8310991fa14f0cbac9c71cfb494d028d4314c7ff` and the actual containers. Reference checkouts can live under `opensrc/testdata/` so NSGo's app test discovery skips their own test files.

Stop the fixture services with `docker compose -f integration/servers/compose.yaml down`. The named volumes keep the fixture setup for subsequent runs.
