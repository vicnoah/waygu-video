# Phone landscape player

844 × 390 pt · Linux GTK at 1× scale · `zh-Hans` · Scenra `88548db091cd`. Each main comparison shows the design above the running app. The app plays the Emby track test clip. Its 16:9 source video has side margins in the wider player, and the reference uses placeholder footage. The test uses a dark player palette inside a light app session. The main GTK capture freezes a decoded frame with playback paused, so its transport shows the play glyph. [Player design](../../design/player.scenra) · [Tracks design](../../design/player-tracks.scenra) · [Danmaku design](../../design/player-danmaku.scenra)

## Player

![Landscape transport, lock and timeline](images/phone-player-main.png)

![Locked player with its unlock control](images/phone-player-locked.png)

The main comparison shows the chapter ticks at 18%, 55%, and 80% of the timeline in both the design and the app. The app draws these from the three divisions between four chapters in the fixture video. The design specifies the gestures, preview and chapter action in notes. The captures below record those states in the running app.

![Jellyfin thumbnail and time above the dragged phone timeline](images/phone-player-seek-thumbnail.png)

![Temporary 2× speed during a held press](images/phone-player-hold-speed.png)

![Four Emby chapters in the playback information panel](images/phone-player-chapters.png)

## Subtitles and audio

![Subtitles tab comparison](images/phone-player-tracks-subs.png)

![Audio tab comparison](images/phone-player-tracks-audio.png)

![Quality tab comparison](images/phone-player-tracks-quality.png)

![Speed tab comparison](images/phone-player-tracks-speed.png)

## Danmaku

![Danmaku display tab comparison](images/phone-player-danmaku-show.png)

![Danmaku sources tab comparison](images/phone-player-danmaku-source.png)

![Danmaku filters tab comparison](images/phone-player-danmaku-block.png)

## Findings

| Area | Result |
| --- | --- |
| Player chrome | Top and bottom gradients, 68 pt central play control, seek buttons, timed progress and shortcut groups are positioned from the landscape frame. The lock leaves the unlock control available and disables the playback surface. The player uses actual resolution and source data. The cast button is disabled, and the next button is available when a subsequent episode exists. |
| Playback gestures | A single tap toggles the controls and a double tap plays or pauses. A horizontal drag previews and seeks on release. Left and right vertical drags adjust picture dimming and playback volume within the app. Holding while a supported stream plays temporarily switches to 2× and restores the previous rate on release or cancellation. Lock disables the gestures. Native rate verification uses GstPlay and scaletempo. |
| Timeline and actions | The slider draws divisions from Emby, Jellyfin and Plex chapter metadata and the overflow panel can jump to each chapter. On sources with a seek index, both the progress slider and horizontal gesture show a real preview image. The timer remains available when an image is unavailable. Native buffering currently reports whether it is waiting and does not expose buffered time ranges for the lighter rail segment. The additional aspect ratios, casting and the rest of the overflow actions remain pending. |
| Tracks | Each tab uses the 360 pt right-side panel. The subtitle list uses actual server and decoder tracks. Delay and style changes apply in the current session, and the local subtitle picker occupies the action row. Online subtitle search, audio delay, audio dynamic range controls and complete channel metadata remain pending. |
| Danmaku display | The sliders and choices update saved rendering preferences. The overlay reads the playback clock and the picture fit mode. The capture has four generated comments from a local DandanPlay response. The headless test imports XML and exercises the same rendering and source controls. |
| Danmaku sources and filters | The panel exposes the supported DandanPlay and local sources and their counts, with per-session switches. Keyword and regular expression rules use removable chips and the shared settings editor. Deduplication and colored-comment filtering are available. Other online providers, sender blocking, comment sending and a quality-level filter remain pending. |
| Platform | GTK verifies rendering and input at the landscape design size. iOS and Android receive a landscape request scoped to the player. Rotation, native Picture in Picture, safe areas and gesture input still need verification on phone hardware. |

The native review also exercises the unlock path, changing picture fit, selecting a subtitle, opening the current style editor, switching all seven tabs, checking a source switch, adding and removing a filter, and closing the session. The headless test checks the position sent to the decoder, cancellation of a preview and restoration of speed. [Capture commands](README.md) · [Docker media fixtures](../../integration/servers/TESTING.md)
