# Desktop player review

Scenra is on the left and the Linux GTK app is on the right at 1440 × 810 pt. The application plays an Emby fixture episode. The review uses a repeatable DandanPlay response and a still from the local Emby library for the seek preview. The app also accepts actual thumbnails through the server preview API when a source supplies them. [Scenra design](../../design/desktop-player.scenra)

## Episodes and timeline

![Desktop player and episode sidebar, Scenra and the GTK app](images/desktop-player-episodes.png)

## Danmaku

![Desktop player danmaku sidebar, Scenra and the GTK app](images/desktop-player-danmaku.png)

## Playback information

The application capture uses libmpv. Its decoder and network rows reflect the values available during the test.

![Desktop playback information sidebar, Scenra and the GTK app with libmpv](images/desktop-player-info.png)

| Area | Comparison |
| --- | --- |
| Frame and controls | The player stays dark and uses the two gradient control bars. The 320 pt sidebar leaves a 1120 pt playback pane at reference size. Collapsing it expands playback to the full window without reopening the stream. The fixture has letterboxing and a different picture. |
| Timeline | The played segment, white thumb, hover time and 176 × 96 pt preview appear at the bottom. The heatmap comes from scheduled comments. Buffered extent is shown when libmpv reports it. The system player reports the buffering state. |
| Episodes | The current episode uses the violet outline and shows its progress. The season menu and episode cards open actual Emby episodes. The sample series has five episodes in its first season and two in its second. |
| Danmaku | The sidebar shows the comment count and source, type and color filters, opacity, area and a per-title time offset. Color and position chosen in the composer are retained for the next message. The composer saves its comments on this device. Shared sending and the additional online sources drawn in Scenra remain pending. |
| Information | The mode, engine, resolution and available video, audio and network metadata appear in four groups. With libmpv the app samples the decoder, frame rate, dropped frames, buffer and read speed about once a second when available. The current libmpv backend uses software decoding. [libmpv playback properties](https://mpv.io/manual/stable/) |
| Window and remote output | Full-screen entry and exit requests are verified in the headless window model. The top-bar cast button opens the DLNA device picker. Desktop Picture in Picture and native macOS and Windows captures remain pending. [Casting comparison](casting.md) · [NSGo video Picture in Picture](https://nsgo.dev/docs/picture-in-picture/) |

`TestDockerDesktopPlayerReview` checks actual desktop playback, pause, preview hover, the panel widths, danmaku filters and saved comments, episode selection and keyboard transport. The headless mode also checks the ten-second seek, volume update and full-screen requests. The same native test runs with the system player and with libmpv. The libmpv package tests verify volume, codec and frame-rate reporting, ASS rendering and embedded track changes.
