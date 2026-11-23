# Phone series detail review

[Scenra design](../../design/series-detail.scenra) · 390 × 1500 pt · Linux GTK at 1× scale · `zh-Hans` · Scenra `88548db091cd`. The first-season strips show the design, the previous app, and the current app. The other strips show the matching design panel and the current app.

## First season

![Light series detail with watched and resumable episodes](images/phone-series-light.png)

![Dark series detail with watched and resumable episodes](images/phone-series-dark.png)

## Second season

![Light second-season panel](images/phone-series-light-s2.png)

![Dark second-season panel](images/phone-series-dark-s2.png)

## Specials

![Light specials panel](images/phone-series-light-extras.png)

![Dark specials panel](images/phone-series-dark-extras.png)

## Findings

| Area | Result |
| --- | --- |
| Hero | The 420 pt edge-to-edge art, lower scrim, source badge, favorite and overflow controls follow the design. The active episode's file supplies resolution and HDR badges when available. GTK uses its own window chrome and has no 44 pt mobile status bar. The source's aggregate score appears with a star. |
| Resume | The 52 pt action starts the series bookmark. The body shows watched time and the remaining episode count from the active season. Long-press offers restart and episode details. The Emby fixture resumes at 2:24 with three later episodes remaining. |
| Season bar | The violet underline follows the selected tab. The sort menu changes order and can hide watched episodes. Returning from the player retains the season, sort and filter, including when the page explicitly shows watched episodes with the global hide preference enabled. The More menu opens the DLNA picker for the selected episode. [Casting comparison](casting.md) |
| Episode rows | The fixture exercises the 128 × 72 pt thumbnail, watched check, active scene-progress overlay, cached danmaku count and actual download state. The count snapshots for the reference capture are synthetic. Real bindings record their count after the player loads comments. |
| Second season and specials | Scenra uses a placeholder box for each panel. The app uses the first season's row treatment for populated seasons and specials. Empty panels use the design's icon and text treatment. Art and episode names come from each server. Returning to Specials waits for the server's season list before loading season zero and extras. |

The review checks the first-season resume at 2:24, and opens a downloaded episode through the row's restart action. Headless coverage verifies all three media adapters' season indexing. [Fixture setup](../../integration/servers/TESTING.md) · [Capture commands](README.md)

## Android phone

The Pixel 7 emulator ran Android 15 at 390 × 844 dp. Each pair shows Scenra on the left and a capture of the emulator display on the right, scaled from 1170 × 2532 px. The network, battery and navigation indicators come from Android. The native status bar uses light icons over the backdrop in both appearances, matching the reference. After scrolling past the backdrop, the icons follow the page appearance. [Android system bars](https://developer.android.com/design/ui/mobile/guides/foundations/system-bars)

![Scenra and Android in light appearance, showing the series hero, resume action and season selector](images/phone-series-android-light.png)

![Scenra and Android in dark appearance, showing the series hero, resume action and season selector](images/phone-series-android-dark.png)

![Android status bar over the backdrop and the scrolled page, light above dark](images/phone-series-android-status.png)

The app measures the 420 dp hero, 52 dp resume button, and 128 × 72 dp thumbnails. In the scrolled first-season list, Android shows the watched check, the active episode with progress, comment counts and the downloaded row. Longer episode names from the fixture wrap onto two lines.

![Scenra and the scrolled first-season list on Android](images/phone-series-android-episodes.png)

The second-season and specials crops show the selected tabs and the populated Emby cards. Light is above dark. The Android sort menu is set to descending, and the bottom of the second-season list is below the captured viewport.

![Scenra and Android with the second-season and specials tabs selected](images/phone-series-android-panels.png)

`TestDockerPhoneSeriesReview` passed in both appearances on Android. It checks the selected episode and native playback position for season two, the local file and playback position for the downloaded episode, and the selected Specials tab and episode after returning from the player. iOS, HarmonyOS and physical phone captures remain pending. [Android test command](../../integration/servers/TESTING.md#catalog-and-player-fixtures)

## Season downloads

The series More menu opens the picker on the active season. The picker follows the offline flow's season selection, quality estimate, space check and queue steps. Scenra specifies those steps in the flow. The dialog reuses the existing individual download controls and app theme. [Series design](../../design/series-detail.scenra) · [Offline download flow](../../design/flows/offline-download.scenra)

![Light GTK picker with two episodes checked](images/season-download-light.png)

![Dark GTK picker with two episodes checked](images/season-download-dark.png)

![Android TV picker with two episodes checked at 1920 × 1080](images/season-download-tv.png)

| Flow step | Verified behavior |
| --- | --- |
| Choose episodes | The picker opens on the active season. Switching seasons refreshes the list. Users can select individual episodes or use Select all. Existing queue entries show their state and cannot be selected a second time. |
| Choose quality and check space | The chosen quality applies to all selected episodes. Available metadata supplies a total estimate. Original sizes can be absent, as in these captures. The space step compares known sizes with both available storage and the app's download limit. |
| Queue and play | The app adds the selected episodes together and opens Storage and Downloads. Returning to the phone detail restores the previously active season. With the gateway disconnected, a completed episode opens from its local file. |

The GTK captures use the phone layout in both themes. Headless testing verifies the same queue with Emby, Jellyfin and Plex and opens the picker on the desktop and tablet layouts. On the Android TV emulator, the test queues two Emby episodes and advances playback on a saved copy with the gateway disconnected. Device layout and remote checks beyond this Android TV run remain open. [Fixture tests](../../integration/servers/TESTING.md#season-downloads)
