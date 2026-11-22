# Delivery roadmap

The Scenra files in `design/` define the product. The current app connects to Emby, Jellyfin, Plex and Flymoo, imports selected video libraries, lists and searches titles, shows movie and series details, and plays and reports progress. Jellyfin can also sign in with Quick Connect. It has a native video path, an optional desktop libmpv path, synchronized local XML comments, and a dismissible next-episode countdown. Emby, Jellyfin and Plex support a configurable streaming bitrate limit.

## Media sources

- Add LAN discovery, Plex account linking, and Flymoo FN Connect.
- Add multi-address failover, account recovery, media library management, cross-server title matching and version selection.
- Verify the claimed Plex account flow. Extend Flymoo fixture coverage for artwork, authentication redirects and version-specific payloads.

## Playback and library

- Verify codec-driven direct stream and transcode decisions. Add network-specific bitrate preferences. Validate track selection, external and online subtitles, audio controls and offline progress replay. Verify next-episode playback across seasons on TV devices and add intro and credits markers.
- Add DandanPlay matching, comment binding and cache, additional comment import formats, and the player settings from `design/`.
- Implement offline downloads, casting, settings, parental controls and iCloud sync. Complete the phone, tablet, desktop and TV views from `design/`.

## Platform verification

- Test signed macOS packages and iOS and tvOS builds on a Mac with Xcode. Verify controller, keyboard and focus behavior on TV devices.
- Complete Android TV remote-control and store-banner verification, and verify signed release builds on TVs, phones and tablets.
- Verify HarmonyOS signing, authenticated LAN sessions and video playback on HarmonyOS phones and tablets, including secure credential persistence and authenticated streams.
- Verify Linux and Windows runtime behavior across both architectures, including media codecs, hardware decoding and libmpv distribution.
