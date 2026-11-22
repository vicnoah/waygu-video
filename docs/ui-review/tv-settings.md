# TV settings review

Scenra is on the left and the Linux GTK app is on the right. Both use the Simplified Chinese TV layout at 1920 × 1080 pt. The app shows the Docker library count, its build version, a generated five-minute pairing code, and the current danmaku providers and filter count. [Scenra design](../../design/tv-settings.scenra)

## Light

![TV settings, light Scenra reference and GTK app](images/tv-settings-light.png)

## Dark

![TV settings, dark Scenra reference and GTK app](images/tv-settings-dark.png)

## Playback offset

The crop compares the timeline offset row in Scenra and in the settings opened from a playing title. The app shows the saved half-second adjustment with the focus ring.

![Timeline offset row in Scenra and the GTK app](images/tv-settings-offset.png)

| Area | Comparison and interaction |
| --- | --- |
| Sidebar | The sidebar takes 640 pt with 64 pt horizontal insets. The rows are 56 pt high. The active category keeps its violet marker while the option pane has focus. Up and Down change category and Right enters its options. |
| Danmaku controls | The content uses 80 pt horizontal insets and 68 pt standard rows. The opacity slider and four screen coverage choices match the Scenra layout. The focused option inverts with a violet outline. Left and Right change values and update the same preferences the player reads. |
| Timeline offset | Enter from the player to adjust the current movie or episode in half-second steps. The app displays the row as unavailable when opened from Home without a current title. Back resumes the previous playback position when opened from the player. |
| Pairing | The card renders a real code. In the Waygu phone app, open Settings and Sync and account, then scan the code or enter the pairing address. The phone sends the details directly to the TV over a connection pinned to the scanned certificate. The code expires after five minutes and a successful setup consumes it. The published captures use an isolated loopback endpoint. |
| Design coverage | Scenra supplies the Danmaku category in both appearances. The other categories use existing settings and media-source actions. Bilibili, account synchronization and parental controls remain pending. |

`TestDockerTVSettingsReview` checks focus transitions, the sidebar and options geometry, persisted danmaku changes, and receipt of both Emby media credentials and DandanPlay credentials. The Emby test signs in, reads its libraries and checks the saved session. `TestPhoneSendsTVSetup` runs the actual phone form against a local pairing endpoint. The transport test covers incorrect certificates, incorrect tokens, retry after a rejected request and reuse of a consumed code. `TestDockerTVPlayerReview` exercises the route from playback and the per-title timeline offset.

The Android TV emulator passed the remote smoke test using a connected Emby fixture. The test opens Settings from Home, moves from the category list into the danmaku options, changes opacity, verifies that the pairing QR is present and returns Home. Run it with `python3 build/android/tv-smoke.py --with-settings`.

Physical phone camera pairing and tvOS remote verification remain pending.
