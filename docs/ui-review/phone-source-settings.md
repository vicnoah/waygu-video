# Phone settings and media sources

390 pt wide · Linux GTK at 1× scale · `zh-Hans` · Scenra `88548db091cd`. The paired captures show the design on the left and the running app on the right. The source cards use the Docker fixtures for Emby, Jellyfin and Plex. [Settings design](../../design/settings.scenra) · [Media source design](../../design/settings-sources.scenra)

## Settings home

![Light settings home](images/phone-settings-home-light.png)

![Dark settings home](images/phone-settings-home-dark.png)

## Media sources

![Light media source settings](images/phone-media-sources-light.png)

![Dark media source settings](images/phone-media-sources-dark.png)

![Editing the order and revealing removal actions](images/phone-media-sources-editing.png)

![Offline and empty states](images/phone-media-sources-states.png)

## Findings

| Area | Result |
| --- | --- |
| Settings home | The title, profile card, grouped rows and footer follow the design. The card shows the local profile. It has no paid badge or connected device count. Row summaries show live online counts, playback quality, subtitle preference, danmaku state, used download bytes and the app version. The home page links to source management, adding a source, playback, subtitles, [danmaku](phone-danmaku-settings.md), downloads, [appearance](phone-appearance.md), [library and metadata](phone-library-metadata.md), [parental controls](phone-parental.md) and [About and help](phone-about.md). Account sync still needs its designed screen. |
| Navigation and layout | The media source page uses the shared back row with the Edit action. GTK lacks the phone status bar and device frame. The shell highlights Settings on its floating bottom bar. The setting rows and source card actions can receive pointer and keyboard input. |
| Connection and refresh | Each card shows the latest authenticated connection check and its last library refresh. Connection health and refresh were tested with all three servers. New libraries stay hidden after a refresh until enabled. The timeout applies to saved-source checks, library refresh, and direct sign-in. Automatic LAN and WAN switching remains pending. |
| Source priority and removal | The cards support dragging the grip, edit-mode up and down buttons, and swipe-to-reveal removal with confirmation. Priority changes persist and feed the matched catalog. Reconnecting keeps the source in its existing position. Phone touch verification remains pending. |
| Differences still under review | The app shows a single configured address and online, offline or sign-in-required status. The design shows local and remote reachability. The edit actions appear in an extra row, and the app has a confirmation dialog for removal. The reference bottom bar uses demo tab values. The app retains an accurate selected Settings tab. |

`TestDockerPhoneSourceSettingsReview` drives source sync, the library manager, the edit entry point, timeout selection, matching, drag and swipe on GTK. `TestPhoneSourceSettingsOfflineAndEmpty` verifies the offline and add paths. Real phone captures and the open design differences remain pending. [Capture commands](README.md) · [Media server fixtures](../../integration/servers/TESTING.md)
