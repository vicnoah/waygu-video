# TV player review

Scenra is on the left and the Linux GTK app is on the right at 1920 × 1080 pt in Simplified Chinese. The app runs in the light appearance with the player's dark treatment. The app frames use the Emby Docker clip, a test danmaku feed, and a seek thumbnail supplied by the test adapter. The movie title, tracks, time, frame and comments differ from the values shown in Scenra. [Scenra design](../../design/tv-player.scenra)

![Scenra and the app with the seek thumbnail and quick settings open](images/tv-player.png)

| Area | Comparison |
| --- | --- |
| Video and overlays | The video fills the client area. The top and bottom gradients are 288 and 420 pt high. The title begins 48 pt from the top and 96 pt from the left. The header shows the active provider, play method, format when available, danmaku count and clock. The Emby sample is a short SDR movie. |
| Timeline and seek preview | With quick settings open, the 10 pt timeline spans 1224 pt with 96 pt on the left and 600 pt on the right. The thumbnail is 284 × 160 pt with a white border and moves with the seek target. Left and Right seek in ten-second steps. The timeline shows the current playhead while the preview shows the target time, including when the server has no preview images. The grey buffered segment remains pending because the video backend exposes buffering state without buffered positions. |
| Remote hints | The four hints show tracks, quick settings, ten-second seeking and pause. The app displays them during the first three playback opens. During playback the on-screen display closes after three seconds without input. |
| Quick settings | The panel takes 560 pt. Rows are 80 pt tall with 12 pt gaps and 40 pt insets. The focused row turns white with a violet ring. Left and Right change opacity in 10% steps and save the setting. The remaining rows open the current track, quality and speed controls. The last row opens playback settings and Back returns to playback at the saved position. |
| Danmaku | The TV overlay begins below the header and is clipped before the sidebar while quick settings are open. Comment positions reflect the test feed and playback time. |
| Remaining review | The track picker uses the shared track dialog and the last settings row uses the existing settings screen. Those destinations need their own TV design review. Physical Android TV and the tvOS Siri Remote remain unverified. |

![App playback controls, seek thumbnail and track picker](images/tv-player-states.png)

`TestDockerTVPlayerReview` checks overlay geometry, remote actions, saved opacity, seeking, playback speed, auto-hide, and the route back from settings. The `seek-preview` subtests in `TestDockerServers` retrieve and decode server-generated images at 0 and 12 seconds from all three providers. `TestSeekPreviewProviders` checks file selection and frame indexing with local HTTP fixtures. [Docker test setup](../../integration/servers/TESTING.md)

## Android TV

The Android x64 Leanback APK played the Emby Docker clip on the 1920 × 1080 Android TV emulator. The remote smoke test verifies that Select changes the play state, Menu opens quick settings, the directional buttons adjust opacity and seek, Up opens tracks, and Back returns to the same detail screen.

![On-screen playback controls, quick settings and the track picker on Android TV](images/tv-player-android.png)

With an Emby fixture that has a featured playable movie, launch the app on TV and run `python3 build/android/tv-smoke.py --with-player` with the emulator or device selected through `ANDROID_SERIAL`.
