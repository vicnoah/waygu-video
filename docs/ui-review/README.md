# UI review

The [screen inventory](screens.md) tracks each Scenra design variant and its panel states. A screen is ready for sign-off when its layout, spacing, typography, color, components, visible states, and interactions match the design on the intended device. Record approved content differences and any unresolved differences in the screen's review entry.

| Screen | Evidence | Status |
| --- | --- | --- |
| Welcome · light and dark | [Screenshots and interaction checks](welcome.md) | Compared on the Linux native backend at phone width. Phone system UI remains pending. |
| Add source · light and dark | [Screenshots and discovery checks](add-source.md) | Compared on Linux GTK with Emby and Jellyfin Docker responders. Several design rows and mobile verification remain pending. |
| Connect to server · both appearances and all provider tabs | [Screenshots and sign-in checks](source-login.md) | Compared on Linux GTK. The Emby advanced group and the real device checks remain pending. The Plex form lacks a Scenra variant. |
| Phone home · light and dark | [Screenshots and media server checks](home.md) | The compact layout was compared on Linux GTK with three Docker media servers. Phone device captures and some metadata details remain pending. |
| Phone media library · light and dark | [Category, poster and list comparisons](phone-library.md) | Compared on GTK with categorized Docker libraries. Mobile device captures remain pending. |
| Phone search · light and dark | [Title and credit search comparisons](phone-search.md) | Compared on GTK with real search results and saved history. Mobile device captures remain pending. |
| Phone movie detail · light and dark | [Top and bottom comparisons and playback checks](phone-movie.md) | Compared on GTK with Docker movies, a trailer, and a featurette. Device captures and several design actions remain pending. |
| Phone series detail · light and dark | [Season and specials comparisons](phone-series.md) | Compared on GTK with two seasons and a special. Phone device captures remain pending. |
| Tablet home · light and dark | [Screenshots and tablet interaction checks](tablet-home.md) | The tablet layout was compared on Linux GTK with three Docker media servers. Real-device captures remain pending. |
| Tablet movie detail · light and dark | [Screenshots and playback checks](tablet-detail.md) | Compared on GTK with Docker movie metadata and tracks. Tablet device checks remain pending. |
| Storage and downloads · light and dark | [Design comparison and offline playback checks](storage-downloads.md) | Original-file downloads compared and tested on GTK. Transcoded downloads and platform background handling remain pending. |
| Personal library | [Favorite and watch-history captures](personal-library.md) | Compared the tablet sidebar and detail actions, and recorded both catalog destinations on GTK. |

## Capture a comparison

Use the same client dimensions, appearance, locale, and display scale for the app and the Scenra reference. Capture empty, loading, error, selected, and focused states when the design includes them. Confirm navigation and input with the input methods the target supports, including touch, keyboard, and TV remote where applicable. Put the design and the native capture side by side and record each difference with its location and verification status.

From the repository root, run the native app check. On a Linux machine without a graphical session, run the commands under `xvfb-run` with a display large enough for the requested viewport.

```sh
go tool nsgo test -target linux -run TestWelcomeReview -screenshots .nsgo/ui-review/app
```

With the [Docker test servers](../../integration/servers/TESTING.md) running locally, capture source discovery, the four connection tabs, and the authenticated Emby form. The discovery check requires a Linux Docker network reachable from the test host over UDP.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^(TestSourceReview|TestDockerSourceReview)$' \
  -screenshots .nsgo/ui-review/sources
```

With the same Docker fixtures, capture the phone home layout and its source cards. The Linux test host uses the compact layout at 390 pt.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerHomeReview$' -screenshots .nsgo/ui-review/home
```

Capture the compact media library across four categories, both appearances, and the list and empty states.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerPhoneLibraryReview$' -screenshots .nsgo/ui-review/library
```

Use `WAYGU_REVIEW_DESIGN=library.scenra` with `WAYGU_REVIEW_PANEL=movies`, `shows`, `anime` or `docs` to render each Scenra reference.

Capture compact search with a title query and a scoped director query. The test also captures the idle and empty states.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerPhoneSearchReview$' -screenshots .nsgo/ui-review/search
```

Use `WAYGU_REVIEW_DESIGN=search.scenra` to export the Scenra reference.

Capture the compact movie detail at 390 pt wide with the Emby clip, cast, and extras indexed by the fixtures.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerPhoneMovieReview$' -screenshots .nsgo/ui-review/movie
```

Capture the compact series detail with the multi-season fixture. The test saves both appearances for the first season, the second season, and specials.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerPhoneSeriesReview$' -screenshots .nsgo/ui-review/series
```

Render the matching Scenra frame with `WAYGU_REVIEW_DESIGN=series-detail.scenra`. Render the other two sets with `WAYGU_REVIEW_PANEL=s2` and `WAYGU_REVIEW_PANEL=extras`.

The tablet home test selects the tablet layout in the GTK harness at 1180 × 820 pt in landscape and 820 × 1180 pt in portrait.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerTabletHomeReview$' -screenshots .nsgo/ui-review/tablet
```

Capture tablet movie details with the same fixture servers. The track test movie's NFO contains the synopsis and cast. Refresh the item metadata on media servers that had already scanned the file before the NFO was added.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerTabletDetailReview$' -screenshots .nsgo/ui-review/tablet-detail
```

Capture the personal state transitions with the same servers.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerPersonalLibraryReview$' -screenshots .nsgo/ui-review/personal
```

With the same Docker servers, capture the storage page while an original file is downloading and another is complete. The test also denies all traffic to each media source while opening the saved file.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerOfflineDownloadReview$' -screenshots .nsgo/ui-review/downloads
```

The reference uses the Scenra version pinned in this project and renders the original design files and components. Run it in its own module.

```sh
cd build/ui-review/reference
go tool nsgo test -target linux -run TestDesignReferences -screenshots .nsgo/review
```

Select a file with `-env WAYGU_REVIEW_DESIGN=source-login.scenra` and a state with `-env WAYGU_REVIEW_PANEL=fnos`. For a hosted desktop design, pass a path such as `views/desktop-movie.scenra`. With `WAYGU_REVIEW_DESIGN=all`, the exporter captures each design variant with its default panel. The actual app capture still needs to navigate to the same state.

The reference exporter captures a second image at the end of a tall design when the native window is shorter than its frame.

Regenerate the inventory with `python3 build/ui-review/inventory.py` after changing design files. `python3 build/ui-review/inventory.py --check` verifies that the version in the repository is current.
