# UI review

The [screen inventory](screens.md) tracks each Scenra design variant and its panel states. A screen is ready for sign-off when its layout, spacing, typography, color, components, visible states, and interactions match the design on the intended device. Record approved content differences and any unresolved differences in the screen's review entry.

| Screen | Evidence | Status |
| --- | --- | --- |
| Welcome · light and dark | [Screenshots and interaction checks](welcome.md) | Compared on native Linux at phone width and Android with system navigation. iPhone comparison remains pending. |
| Add source · light and dark | [Screenshots and discovery checks](add-source.md) | Compared on Linux GTK with Emby and Jellyfin Docker responders and a Flymoo mDNS fixture. Several design rows and mobile verification remain pending. |
| Connect to server · both appearances and all provider tabs | [Screenshots and sign-in checks](source-login.md) | Compared on Linux GTK. The Flymoo FN ID dialog, Advanced settings, connection status, and compact library selection have light and dark captures. Real device checks remain pending. The Plex form lacks a Scenra variant. |
| Phone home · light and dark | [Screenshots and media server checks](home.md) | The compact layout was compared on Linux GTK with three Docker media servers. Phone device captures and some metadata details remain pending. |
| Phone media library · light and dark | [Category, poster and list comparisons](phone-library.md) | Compared on GTK with categorized Docker libraries. Mobile device captures remain pending. |
| Phone search · light and dark | [Title and credit search comparisons](phone-search.md) | Compared on GTK with real search results and saved history. Mobile device captures remain pending. |
| Phone movie detail · light and dark | [Top and bottom comparisons and playback checks](phone-movie.md) | Compared on GTK with Docker movies, a trailer, and a featurette. Device captures and several design actions remain pending. |
| Phone series detail · light and dark | [Season and specials comparisons](phone-series.md) | Compared on GTK with two seasons and a special. The season download picker is captured on GTK and Android TV. Phone device captures remain pending. |
| Phone danmaku settings · light and dark | [Preview, matching and filter comparisons](phone-danmaku-settings.md) | Compared on GTK. DandanPlay and Logvar exercise matching, playback and cache reuse. Standalone Bilibili and Bahamut, public posting and mobile captures remain pending. |
| Phone About and help · light and dark | [Help, diagnostics and reset comparisons](phone-about.md) | Compared on GTK. Device save behavior and the App Store rating action remain pending. |
| Phone library and metadata · light and dark | [Library controls and metadata comparison](phone-library-metadata.md) | Compared on GTK. Scans were accepted by Emby, Jellyfin and Plex. Additional metadata providers and target-device captures remain pending. |
| Phone Sync and accounts · light and dark | [Account, device and progress comparison](phone-sync-accounts.md) | Compared on GTK. Docker verifies isolated updates for two real servers. iCloud, store products and remote account integrations remain pending. |
| Phone appearance · light and dark | [Layout and saved preference checks](phone-appearance.md) | Compared on GTK at phone width. The local Docker fixtures verify home and library effects. Mobile device captures remain pending. |
| Phone parental controls · light and dark | [PIN, rating and viewing-time comparisons](phone-parental.md) | Compared on GTK with Docker fixtures and verified with a simulated playback clock. Mobile and TV device checks remain pending. |
| Tablet home · light and dark | [Screenshots and tablet interaction checks](tablet-home.md) | The tablet layout was compared on Linux GTK with three Docker media servers. Real-device captures remain pending. |
| Tablet movie detail · light and dark | [Screenshots and playback checks](tablet-detail.md) | Compared on GTK with Docker playback and on an Android landscape viewport with native artwork clipping and rating and format fixtures. Physical tablet checks remain pending. |
| TV home · light and dark | [Design, shelf and remote checks](tv-home.md) | Compared at 1920 × 1760 pt on GTK with three Docker media servers. Android TV D-pad navigation passed at 1920 × 1080. Apple TV simulator checks cover preview playback, moving focus to Home and artwork restoration. |
| TV detail · light and dark | [Movie details, resume and remote checks](tv-detail.md) | Compared at 1920 × 1640 pt on GTK. Android TV and the Apple TV simulator verify attributed ratings and the 4K HDR to 1080p SDR change at 1920 × 1080. |
| TV player | [Playback, seek preview and quick settings comparisons](tv-player.md) | Compared at 1920 × 1080 pt on GTK. Android TV remote playback passed against Emby. The TV next-episode flow has captures of the preview, focus, countdown and season transition. Buffered progress and tvOS checks remain pending. |
| TV settings · light and dark | [Danmaku, focus and pairing comparisons](tv-settings.md) | Compared at 1920 × 1080 pt on GTK. Pairing was tested with a Docker media source and the phone form. Android and Apple TV captures compare the focused danmaku area. Apple TV simulator checks also cover Home layout row order and visibility. |
| Desktop home · light and dark | [Sidebar, carousel and shelf comparisons](desktop-home.md) | Compared on GTK in both appearances and on native Windows and macOS in dark appearance. The Apple silicon runner clips the outer edges. |
| Desktop movie library · light and dark | [Grid, filter and view comparisons](desktop-library.md) | Compared on GTK, macOS and Windows with an HDR fixture. Validated source filtering and playback with three Docker media servers. |
| Desktop movie detail · light and dark | [Hero, metadata and playback comparisons](desktop-movie.md) | Compared on GTK. Trailer playback, watch state and selecting a different server copy were checked against Docker media servers. |
| Desktop player · all three sidebar tabs | [Playback and sidebar comparisons](desktop-player.md) | Compared at 1440 × 810 pt on GTK with Emby using the system player and libmpv. |
| Phone settings and source management · light and dark | [Comparison and server checks](phone-source-settings.md) | Compared on GTK with three Docker media servers. Source reordering, sync and removal are tested. Phone device captures remain pending. |
| Phone landscape player · all seven panel tabs | [Playback and panel comparisons](phone-player.md) | Compared at 844 × 390 pt on GTK with an Emby video and subtitle fixture. Android emulator coverage includes rotation, saved swipe settings and timeline dragging. |
| DLNA device picker · empty, list and connected | [Flow comparison and receiver checks](casting.md) | Captured on GTK. Linux GStreamer verifies multicast discovery and remote playback. Mobile and physical TV checks remain pending. |
| Phone playback and subtitle settings · light and dark | [Settings comparison and playback checks](phone-player-settings.md) | Compared on GTK. Audio and caption preferences were verified against Emby and Jellyfin. Device captures and several controls remain pending. |
| Storage and downloads · light and dark | [Design comparison and offline playback checks](storage-downloads.md) | GTK verifies original and transcoded copies with saved subtitles and danmaku from Emby, Jellyfin and Plex. Android verifies the Emby copy and network transitions. Both verify deletion after playback ends. |
| Personal library | [Favorite and watch-history captures](personal-library.md) | Compared the tablet sidebar and detail actions, and recorded both catalog destinations on GTK. |

## Capture a comparison

Use the same client dimensions, appearance, locale, and display scale for the app and the Scenra reference. Capture empty, loading, error, selected, and focused states when the design includes them. Confirm navigation and input with the input methods the target supports, including touch, keyboard, and TV remote where applicable. Put the design and the native capture side by side and record each difference with its location and verification status.

From the repository root, run the native app check. On a Linux machine without a graphical session, run the commands under `xvfb-run` with a display large enough for the requested viewport.

For Android comparisons that depend on clipped outlines or video surfaces, capture the emulator or device display with `adb exec-out screencap -p`. The test framework's Android PNG draws the view tree into a software bitmap.

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

Capture the TV home at the design's 1920 × 1760 pt. The native test also scrolls to the source shelves.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerTVHomeReview$' -screenshots .nsgo/ui-review/tv-home
```

Use `WAYGU_REVIEW_DESIGN=tv-home.scenra` to export the reference.

Capture the TV detail at 1920 × 1640 pt. The native test also captures the scrolled shelves and the resume state.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerTVDetailReview$' -screenshots .nsgo/ui-review/tv-detail
```

Use `WAYGU_REVIEW_DESIGN=tv-detail.scenra` to export the reference.

Capture the tablet and TV rating and format rows using the simulated 4K and 1080p versions. Export the matching `tablet-detail.scenra` and `tv-detail.scenra` references.

```sh
go tool nsgo test -target linux -run '^TestDetailBadges$' -screenshots .nsgo/ui-review/detail-badges
```

Capture the TV player's full-screen controls, quick settings, seek preview and track picker at 1920 × 1080 pt.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerTVPlayerReview$' -screenshots .nsgo/ui-review/tv-player
```

Use `WAYGU_REVIEW_DESIGN=tv-player.scenra` to export the reference.

Capture the TV settings in both appearances. The playback review also captures the episode-specific time offset.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^(TestDockerTVSettingsReview|TestDockerTVPlayerReview)$' \
  -screenshots .nsgo/ui-review/tv-settings
```

Use `WAYGU_REVIEW_DESIGN=tv-settings.scenra` to export the references.

Capture the desktop home in both appearances, then check the scroll position at the normal 1440 × 900 pt window size.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerDesktopHomeReview$' -screenshots .nsgo/ui-review/desktop-home
```

Use `WAYGU_REVIEW_DESIGN=desktop-main.scenra` to export the references.

Capture the desktop movie library with the 4K HDR test catalog and the three Docker media servers.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^(TestDesktopLibraryGridReview|TestDockerDesktopLibraryReview)$' \
  -screenshots .nsgo/ui-review/desktop-library
```

Use `WAYGU_REVIEW_DESIGN=views/desktop-library.scenra` to export the two Scenra references.

Capture the desktop movie detail with representative film metadata and all three Docker sources.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^(TestDesktopMovieDesignReview|TestDockerDesktopMovieReview)$' \
  -screenshots .nsgo/ui-review/desktop-movie
```

Use `WAYGU_REVIEW_DESIGN=views/desktop-movie.scenra` to export the two Scenra references.

Capture the desktop player with the Docker Emby episode. The native test captures Episodes, Danmaku and Info at 1440 × 810 pt.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerDesktopPlayerReview$' -screenshots .nsgo/ui-review/desktop-player
```

Repeat with `-env WAYGU_TEST_DESKTOP_MPV=1` and libmpv installed to capture decoder information. Export `desktop-player.scenra` with `WAYGU_REVIEW_PANEL=episodes`, `danmaku` and `info` to generate the three references.

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

Capture the watched-file cleanup switch and test actual local playback on GTK and the Android phone emulator. The headless test uses a fixed wall clock to exercise the 24-hour threshold from saved state.

```sh
go tool nsgo test . -run '^TestWatchedDownload'
go tool nsgo test -target linux \
  -run '^(TestWatchedDownloadCleanup|TestStorageWatchedCleanupReview)$' \
  -screenshots .nsgo/ui-review/watched-downloads
sh build/android/package.sh test -device <serial> -run '^TestWatchedDownloadCleanup$'
```

Use `WAYGU_REVIEW_DESIGN=settings-storage.scenra` with the reference exporter.

Verify the subtitle and danmaku switch and open the resulting video with the providers disconnected. [Offline tracks test commands](../../integration/servers/TESTING.md#offline-subtitles-and-danmaku)

Capture the settings home and media source pages with the Docker fixtures. The offline fixture also captures the error and empty states.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^(TestDockerPhoneSourceSettingsReview|TestPhoneSourceSettingsOfflineAndEmpty)$' \
  -screenshots .nsgo/ui-review/source-settings
```

Use `WAYGU_REVIEW_DESIGN=settings.scenra` or `settings-sources.scenra` with the reference exporter.

Capture the 844 × 390 pt phone player, the subtitle and audio tabs, and the danmaku panel with generated media in Emby. The GTK host needs GStreamer, GstPlay, and scaletempo for a speed-change capture.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -env WAYGU_REVIEW_X11_POINTER=1 \
  -run '^TestDockerPhonePlayer(DesignReview|GesturesAndPreview)$' -screenshots .nsgo/ui-review/phone-player
```

The held-gesture captures use `xdotool` in an X11 display. The native test also runs without that capture setting. Jellyfin serves the seek thumbnail while the pointer holds the slider. The Emby review records the ratio chooser, actual 4:3 video, playback menu, A/B marks, and sleep timer.

Export `player.scenra`, the four `player-tracks.scenra` panels and the three `player-danmaku.scenra` panels with the reference exporter below.

Capture the phone DLNA picker while it is empty, after adding a receiver, and while connected. Keep other DLNA receivers off on the test network to capture the empty state. [Casting comparison](casting.md)

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerPhoneDLNACasting$' -screenshots .nsgo/ui-review/casting
```

Capture the phone playback and subtitle settings and exercise the language ordering and color controls.

```sh
go tool nsgo test -target linux -run '^TestPhonePlaybackAndSubtitleSettingsReview$' \
  -screenshots .nsgo/ui-review/player-settings
```

Use `WAYGU_REVIEW_DESIGN=settings-playback.scenra` or `settings-subtitles.scenra` with the reference exporter. Run `TestDockerPhonePlayerUsesSettings` with the local Docker fixtures to verify the saved settings through the player.

Capture the progress switch before and after tapping it on an Android phone, then verify the saved state after reopening the page.

```sh
go tool nsgo test -target android -device <serial> -run '^TestAndroidRememberPlaybackPosition$' \
  -screenshots .nsgo/ui-review/android-progress
```

The comparison uses the installed APK on a Pixel 7 emulator. Capture both states from the emulator display with `adb -s <serial> exec-out screencap -p` to include Android's status bar and hardware clipping.

Verify the gesture settings, landscape rotation, right-side volume swipe and timeline dragging on an Android phone emulator.

```sh
go tool nsgo test -target android -device <serial> -run '^TestPhoneGesturePreferencesAffectPlayback$' \
  -screenshots .nsgo/ui-review/android-gestures
```

Capture the desktop Settings window, overview, and the General, Playback, Danmaku and Media Sources pages in both appearances. The media source test uses the local Docker fixtures.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerDesktopSettingsReview$' -screenshots .nsgo/ui-review/desktop-settings
```

Capture `desktop-settings.scenra` and the four `views/desktop-settings-*.scenra` designs with the reference exporter. [Desktop Settings comparisons](desktop-settings.md)

Capture phone parental controls with both appearances and verify the source allowlist against the Docker fixtures.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerPhoneParentalReview$' -screenshots .nsgo/ui-review/parental
```

Use `WAYGU_REVIEW_DESIGN=settings-parental.scenra` with the reference exporter. Run `TestPhoneParentalControlsAndPlaybackLimit` in the headless test suite to verify expiry and the protected extension.

Capture the phone Appearance page in both modes and verify the saved preferences.

```sh
go tool nsgo test -target linux -run '^TestPhoneAppearanceReview$' -screenshots .nsgo/ui-review/appearance
```

Use `WAYGU_REVIEW_DESIGN=settings-appearance.scenra` with the reference exporter. Run `TestDockerHomeAppearance` with the local Docker fixtures to verify home modules and the library cover layout.

Capture the phone danmaku settings at the full reference height. Use a display at least 2200 pt tall for GTK.

```sh
go tool nsgo test -target linux -run '^TestPhoneDanmakuSettingsReview$' -screenshots .nsgo/ui-review/danmaku-settings
```

Use `WAYGU_REVIEW_DESIGN=settings-danmaku.scenra` with the reference exporter. Use the local Docker fixtures with `TestDockerPhonePlayerDesignReview` to verify danmaku during playback.

Capture the phone About and help page in both appearances and verify its help panels and reset. With the local Docker fixtures, also verify the player overlay with a video from Emby.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^(TestPhoneAboutReview|TestDockerAboutPlayerOverlay)$' -screenshots .nsgo/ui-review/about
```

Use `WAYGU_REVIEW_DESIGN=settings-about.scenra` with the reference exporter.

Capture both appearances of the Library and metadata page. The test exercises preferences, restoring a hidden library and the rescan confirmation.

```sh
go tool nsgo test -target linux -run '^TestPhoneLibraryMetadataReview$' -screenshots .nsgo/ui-review/metadata
```

Use `WAYGU_REVIEW_DESIGN=settings-library.scenra` with the reference exporter. For the scan endpoints, run `WAYGU_TEST_SCAN=1 go tool nsgo test . -run '^TestDockerLibraryMetadataScans$'` against the dedicated Docker fixtures.

Capture the phone Sync and accounts page with two appearances. Use the dedicated Docker fixtures to verify that the watched action follows each media source’s setting.

```sh
go tool nsgo test -target linux -run '^TestPhoneSyncAccountsReview$' -screenshots .nsgo/ui-review/sync
WAYGU_TEST_SYNC=1 go tool nsgo test . -run '^TestDockerSyncSourceWatchedIsolation$'
```

Use `WAYGU_REVIEW_DESIGN=settings-sync.scenra` with the reference exporter.

The reference uses the Scenra version pinned in this project and renders the original design files and components. Run it in its own module.

```sh
cd build/ui-review/reference
go tool nsgo test -target linux -run TestDesignReferences -screenshots .nsgo/review
```

Select a file with `-env WAYGU_REVIEW_DESIGN=source-login.scenra` and a state with `-env WAYGU_REVIEW_PANEL=fnos`. For a hosted desktop design, pass a path such as `views/desktop-movie.scenra`. With `WAYGU_REVIEW_DESIGN=all`, the exporter captures each design variant with its default panel. The actual app capture still needs to navigate to the same state.

The reference exporter captures a second image at the end of a tall design when the native window is shorter than its frame.

Regenerate the inventory with `python3 build/ui-review/inventory.py` after changing design files. `python3 build/ui-review/inventory.py --check` verifies that the version in the repository is current.
