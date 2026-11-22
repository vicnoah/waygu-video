# Delivery roadmap

The Scenra files in `design/` define the product. The current app connects to Emby, Jellyfin, Plex and Flymoo, imports selected video libraries, lists and searches titles, shows movie and series details, and plays and reports progress. Jellyfin supports Quick Connect. Plex supports account authorization by browser or QR code and lets users choose a discovered server. The app has a native video path, an optional desktop libmpv path, synchronized local and DandanPlay comments, and a dismissible next-episode countdown. Emby, Jellyfin and Plex support a configurable streaming bitrate limit.

## Media sources

- Add LAN discovery and Flymoo FN Connect.
- Imported libraries support category, visibility, home visibility and per-source ordering. Refresh the server's library list from source settings.
- All Sources combines movies and series with matching IMDb or TMDB IDs. Source order supplies the default copy. The media source settings let users reorder sources and turn matching off. Add multi-address failover, account recovery and scheduled library refresh.
- Verify Plex account authorization with a real account and a claimed server, including remote and relay connections. Extend Flymoo fixture coverage for artwork, authentication redirects and version-specific payloads.

## Playback and library

- Movie and series details offer the matched copies in visible imported libraries. Each copy offers the versions or qualities its server exposes. Playback reports progress to the selected server. The player switches versions at the current position on that server. Emby, Jellyfin and Plex also allow a bitrate override for the current playback. Playback settings can prefer the highest quality, fit the configured streaming bitrate or remember the last version played. Add choosing other servers inside the player.
- Continue Watching shows one record per matched movie or episode, ordered by the server's last-played time. Records without a timestamp retain their source order. The selected card resumes its own server's copy and position. The app caches the last successful resume list for each account, applying the current library visibility when a server is unavailable.
- Playback checkpoints that fail to reach the server survive app restarts and appear in Continue Watching. The app retries after leaving the player or restarting and checks the server's current progress before sending a checkpoint. Flymoo compares positions when the server supplies no timestamp.
- The player switches Emby, Jellyfin and Plex audio at the current position, supports local SRT and WebVTT files, text subtitles from Emby and Jellyfin, external SRT and WebVTT from Plex, subtitle delay and playback speed on capable backends.
- Desktop direct play with libmpv switches embedded audio and subtitles in place, including Flymoo streams and embedded Plex subtitles. It supports subtitle delay alongside the independent danmaku overlay.
- Verify codec-driven direct stream and transcode decisions. Add network-specific bitrate preferences, Flymoo track selection and embedded Plex subtitles on native backends, subtitle image format coverage, subtitle language preferences, online search, caption styles and audio controls. Verify next-episode playback across seasons on TV devices and add intro and credits markers.
- With DandanPlay developer credentials, the player matches original files by fingerprint, offers episode search, remembers bindings per media source, and reuses their numbering within a season. It caches downloaded comments for 24 hours on native platforms and combines them with local XML or JSON comments. Display and filter preferences are shared by the native and libmpv renderers. Playback applies keyword, regular expression and comment type filters, marks repeat counts, and saves time offsets per item. Add ASS import, other online sources, comment sending and user blocks.
- Implement offline downloads, casting, settings, parental controls and iCloud sync. Complete the phone, tablet, desktop and TV views from `design/`.

## Platform verification

Run checks locally during development. Start the GitHub Actions build and test matrix manually when a milestone is ready for platform verification.

- Test signed macOS packages and iOS and tvOS builds on a Mac with Xcode. Verify controller, keyboard and focus behavior on TV devices.
- Complete Android TV remote-control and store-banner verification, and verify signed release builds on TVs, phones and tablets.
- Verify HarmonyOS signing, authenticated LAN sessions and video playback on HarmonyOS phones and tablets, including secure credential persistence and authenticated streams.
- Verify Linux and Windows runtime behavior across both architectures, including media codecs, hardware decoding and libmpv distribution.
