# Delivery roadmap

The Scenra files in `design/` define the product. The current app connects to Emby, Jellyfin, Plex and Flymoo, imports selected video libraries, lists and searches titles, shows movie and series details, and plays and reports progress. Jellyfin can also sign in with Quick Connect. It has a native video path, an optional desktop libmpv path, and synchronized local XML comments.

## Media sources

- Add LAN discovery, Plex account linking, and Flymoo FN Connect.
- Add multi-address failover, account recovery, media library management, cross-server title matching and version selection.
- Verify the claimed Plex account flow. Extend Flymoo fixture coverage for artwork, authentication redirects and version-specific payloads.

## Playback and library

- Verify direct stream and transcode decisions with codec, bitrate and network fixtures. Validate track selection, external and online subtitles, audio controls, next-episode playback and offline progress replay.
- Add DandanPlay matching, comment binding and cache, additional comment import formats, and the player settings from `design/`.
- Implement offline downloads, casting, settings, parental controls and iCloud sync. Complete the phone, tablet, desktop and TV views from `design/`.

## Platform verification

- Test signed macOS packages and iOS and tvOS builds on a Mac with Xcode. Verify controller, keyboard and focus behavior on TV devices.
- Complete Android TV remote-control and store-banner verification, and verify signed release builds on TVs, phones and tablets.
- Verify HarmonyOS signing, authenticated LAN sessions and video playback on HarmonyOS phones and tablets, including secure credential persistence and authenticated streams.
- Verify Linux and Windows runtime behavior across both architectures, including media codecs, hardware decoding and libmpv distribution.
