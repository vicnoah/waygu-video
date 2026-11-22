# Delivery roadmap

The Scenra files in `design/` define the product. The current app connects to Emby, Jellyfin, Plex and Flymoo, imports selected video libraries, lists and searches titles, shows movie and series details, and plays and reports progress. Jellyfin supports Quick Connect. Plex supports account authorization by browser or QR code and lets users choose a discovered server. The app has a native video path, an optional desktop libmpv path, synchronized local XML comments, and a dismissible next-episode countdown. Emby, Jellyfin and Plex support a configurable streaming bitrate limit.

## Media sources

- Add LAN discovery and Flymoo FN Connect.
- Imported libraries support category, visibility, home visibility and per-source ordering. Refresh the server's library list from source settings.
- Add multi-address failover, account recovery, scheduled library refresh and cross-server title matching.
- Verify Plex account authorization with a real account and a claimed server, including remote and relay connections. Extend Flymoo fixture coverage for artwork, authentication redirects and version-specific payloads.

## Playback and library

- Movie and episode details offer the versions grouped by an Emby, Jellyfin or Plex server. Playback settings can prefer the highest resolution, fit the configured streaming bitrate or remember the last version played. Add Flymoo quality selection and version selection across servers.
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
