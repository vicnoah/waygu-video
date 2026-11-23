# TV home review

The left frames are Scenra and the right frames are the app on Linux GTK. Both use the Simplified Chinese layout at 1920 × 1760 pt. The app connects to the Emby, Jellyfin, and Plex Docker fixtures. The reference renderer substitutes image placeholders for the design's film stills and posters. [Scenra design](../../design/tv-home.scenra)

## Light

![TV home in light appearance, Scenra and the app](images/tv-home-light.png)

## Dark

![TV home in dark appearance, Scenra and the app](images/tv-home-dark.png)

## Source shelves

The design frame clips the first source shelf at 1760 pt. To compare this section, the reference export extends the frame to 2450 pt and crops the first source shelf. The app capture scrolls to its first shelf. The reference shows Flymoo and the fixture shows Emby.

![TV source shelf in light appearance, Scenra and the app](images/tv-home-light-source-shelf.png)

![TV source shelf in dark appearance, Scenra and the app](images/tv-home-dark-source-shelf.png)

## Findings

| Area | Comparison |
| --- | --- |
| Featured artwork | Both appearances use a 900 pt panel with a left scrim extending 1360 pt, a top scrim and a 96 pt fade into the page background. The high-contrast color bars in the app come from the synthetic movie. |
| Navigation | The wordmark, central pill, clock and profile sit in a row with 96 pt side insets. The app shows the active Home pill. The static Scenra export draws the unselected pills. The app uses the current time and the first source account initial. |
| Featured metadata and actions | Source and freshness pills, title, metadata, synopsis, actions and page indicators use the reference layout. The title and synopsis change line breaks with the catalog text. Ratings use the server's value, and quality badges depend on available stream metadata. The primary focus uses a violet ring and a small scale increase. |
| Continue watching | Cards use 420 × 236 pt artwork, a 32 pt gap, a source badge over the top-left corner and a 6 pt progress line. The fixture shows five saved bookmarks. The horizontal viewport clips the next card at the 96 pt right inset. |
| Recently added and source shelves | The shelves use 220 × 330 pt artwork, 32 pt gaps and section markers. The fixture's available source names, provider badges and media count determine the labels. Content cards link to their media copy. |
| Source shelf settings | **Settings → Appearance → Home layout** controls the visibility and order of each source row. The catalog and playback keep their original source priority. [Settings captures and checks](tv-settings.md#home-layout) |
| Background trailers | The focused featured panel can play a server trailer behind its metadata and actions. The image fills the 900 pt panel and uses the same scrims. [Playback checks](#silent-background-trailers) |

## Interaction checks

`TestDockerTVHomeReview` verifies the featured and card dimensions, all three Docker source shelves, details, Movies, Series, Anime, Favorites, Search and the complete catalog shortcut. Headless it checks the six-second focus-gated rotation and the long-press removal of a resume bookmark. Removing a bookmark from the TV shelf preserves the server's progress. New playback progress makes the title eligible for the shelf again.

After adjusting the Home layout, both appearances show Jellyfin above Emby and omit the Plex row. The covers and titles come from the fixture libraries. Items without artwork use the app's poster placeholder. [Home layout steps and settings captures](tv-settings.md#home-layout)

![Jellyfin above Emby after changing the TV Home layout, light and dark GTK](images/tv-home-custom-source-shelves.png)

The [screen inventory](screens.md) tracks the detail page and the other TV views.

## Silent background trailers

After 750 ms of focus in the featured actions, Waygu looks for a trailer attached to the title. It keeps the artwork until the native player advances, then puts the muted frame under the same title, scrims, indicator and focus ring. The next recommendation still appears after six seconds. Leaving the actions or the foreground restores the artwork and closes the stream. An unavailable trailer retains the artwork. Reduced motion, parental controls, and a reported cellular or offline connection keep the background static. [Scenra background and timing note](../../design/tv-home.scenra#L4)

![TV Home playing a silent trailer, light and dark GTK](images/tv-home-silent-trailer-gtk.png)

![Android TV emulator display playing a silent trailer with Play focused](images/tv-home-silent-trailer-android.png)

The test footage uses a synthetic color chart, and the buttons and scrims occupy the same positions as in the artwork captures. GTK elides the fixture title sooner than Android because of native font metrics. The Android image comes from the emulator display. The native test host's software snapshot omits the video texture.

`TestDockerTVSilentTrailer` uses Emby and Jellyfin trailer fixtures and checks mute, fill mode, focus and artwork restoration. The GTK and Android TV runs decode the Emby trailer. The preview has its own encoding session and creates no viewing history. Plex supplies trailers when present in Extras. Its unclaimed Docker fixture has no trailer. Flymoo displays artwork because its adapter has no trailer endpoint. tvOS preview decoding remains to be verified.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^TestDockerTVSilentTrailer$' -screenshots .nsgo/ui-review/tv-trailer
```

Use `-env WAYGU_REVIEW_APPEARANCE=light` for the light capture. For Android TV, forward the fixture ports 18096, 28096 and 32400 with `adb reverse` and use `sh build/android/package.sh test -device <serial> -env WAYGU_TEST_SERVERS=1 -run '^TestDockerTVSilentTrailer$'`.

## Android TV

The 1920 × 1080 Android TV emulator ran the installed x86-64 Leanback APK against the same Emby fixture. These captures show the initial Play focus, the first resume bookmark after Down scrolled the page, and a recently added poster after moving Right and Down. The native accessibility tree reported each focused control. Select opened the featured details and Back returned to the home page.

![Android TV remote focus on the hero, resume bookmark, and recent poster](images/tv-home-android-remote.png)

With a connected test library and at least two saved bookmarks, start Waygu from Leanback and run `python3 build/android/tv-smoke.py --with-library` on the host. The script uses the connected emulator's Android serial. Physical TV and tvOS validation remain pending.
