# Delivery roadmap

The Scenra files in `design/` define the product. The current app connects to Emby, Jellyfin, Plex and Flymoo, imports selected video libraries, lists and searches titles, shows movie and series details, and plays and reports progress. Jellyfin supports Quick Connect. Plex supports account authorization by browser or QR code and lets users choose a discovered server. The app has a native video path, an optional desktop libmpv path, synchronized local XML comments, and a dismissible next-episode countdown. Emby, Jellyfin and Plex support a configurable streaming bitrate limit.

## Media sources

- Add LAN discovery and Flymoo FN Connect.
- Imported libraries support category, visibility, home visibility and per-source ordering. Refresh the server's library list from source settings.
- All Sources combines movies and series with matching IMDb or TMDB IDs. Source order supplies the default copy. The media source settings let users reorder sources and turn matching off. Add multi-address failover, account recovery and scheduled library refresh.
- Verify Plex account authorization with a real account and a claimed server, including remote and relay connections. Extend Flymoo fixture coverage for artwork, authentication redirects and version-specific payloads.

## Playback and library

- Movie and series details offer the matched copies in visible imported libraries. Each copy offers the versions or qualities its server exposes. Playback reports progress to the selected server. The player switches versions at the current position on that server. Emby, Jellyfin and Plex also allow a bitrate override for the current playback. Playback settings can prefer the highest quality, fit the configured streaming bitrate or remember the last version played. Add cross-source progress aggregation and choosing other servers inside the player.
- The player switches Emby, Jellyfin and Plex audio at the current position, supports local SRT and WebVTT files, text subtitles from Emby and Jellyfin, external SRT and WebVTT from Plex, subtitle delay and playback speed on capable backends.
- Desktop direct play with libmpv switches embedded audio and subtitles in place, including Flymoo streams and embedded Plex subtitles. It supports subtitle delay alongside the independent danmaku overlay.
- Verify codec-driven direct stream and transcode decisions. Add network-specific bitrate preferences, Flymoo track selection and embedded Plex subtitles on native backends, subtitle image format coverage, subtitle language preferences, online search, caption styles, audio controls and offline progress replay. Verify next-episode playback across seasons on TV devices and add intro and credits markers.
- Add DandanPlay matching, comment binding and cache, additional comment import formats, and the player settings from `design/`.
- Implement offline downloads, casting, settings, parental controls and iCloud sync. Complete the phone, tablet, desktop and TV views from `design/`.

## Platform verification

- Test signed macOS packages and iOS and tvOS builds on a Mac with Xcode. Verify controller, keyboard and focus behavior on TV devices.
- Complete Android TV remote-control and store-banner verification, and verify signed release builds on TVs, phones and tablets.
- Verify HarmonyOS signing, authenticated LAN sessions and video playback on HarmonyOS phones and tablets, including secure credential persistence and authenticated streams.
- Verify Linux and Windows runtime behavior across both architectures, including media codecs, hardware decoding and libmpv distribution.
