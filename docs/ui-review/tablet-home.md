# Tablet home review

[Scenra design](../../design/tablet-home.scenra) · 1180 × 820 px per landscape image · Linux GTK at 1× scale · `zh-Hans`. The app reads the Emby, Jellyfin, and Plex Docker libraries and uses their fixture art and resume progress.

The strips show the pre-tablet layout at `e19c0a1`, the Scenra reference, and the revised tablet layout. The baseline comes from the shared desktop and tablet shell at that revision. The GTK harness selects the new tablet shell explicitly. Scenra `88548db091cd` renders both references with the sidebar components. The component files use the supported clock icon for watch history.

## Light

![Tablet home in light appearance, before, design, and after](images/tablet-home-light.png)

## Dark

![Tablet home in dark appearance, before, design, and after](images/tablet-home-dark.png)

## Portrait drawer

The 820 × 1180 px native capture shows the drawer open over the portrait home. The design describes the drawer interaction in a note and supplies landscape drawings for both appearances.

![Tablet home with the portrait drawer open](images/tablet-home-portrait-drawer.png)

## Findings

| Area | Revised capture and remaining differences |
| --- | --- |
| Landscape shell | The sidebar is 224 pt wide and the content starts 24 pt beyond it. The design draws a 24 pt iPad status bar and rounded screen frame. The app uses platform safe-area insets, so the GTK client starts above the equivalent content in the design. |
| Sidebar | The brand, search field, browse shortcuts, source badges, settings, and account footer use the shared sidebar design. The shortcut follows the platform's key labels. Source names, account, and provider badges reflect fixture data. Category totals and the favorites, history, and downloads group remain pending. |
| Header and source filter | The greeting, live catalog summary, and 36 pt source pill follow the header layout. The summary reports loaded items. The reference shows today's additions. |
| Featured title | The 260 pt wide-format carousel uses a left-to-right scrim, 40 pt pill actions, and bottom-right indicators. Title wrapping, source, duration, and art come from the selected item. Stream quality and HDR labels remain pending. |
| Continue watching | The rail uses 256 × 144 pt artwork, 16 pt gaps, remaining-time pills, and bottom progress tracks. Resume rows reflect saved fixture positions and the current source selection. |
| Recently added | The rail uses 112 × 160 pt poster covers and 16 pt gaps. Watching and freshness badges follow the catalog. Quality labels on the poster corners remain pending. |
| Portrait navigation | The drawer spans the client height with a scrim and the same navigation links. It opens from a leading-edge swipe or the panel button. It closes from a leftward swipe, the panel button, the scrim, or a destination. The physical iPadOS and Android tablet gestures, safe areas, and display scaling remain pending. |

## Interaction checks

`TestDockerTabletHomeReview` verifies horizontal swipes and manual carousel selection, expansion and collapse of the recent rail, source selection and restoration of the aggregate catalog, movie and series shortcuts, search and its keyboard shortcut, the featured detail and playback routes, side-panel toggling in landscape, and portrait drawer gestures and navigation.

The GTK lab uses Cairo and preloads its installed libsoup 3 to load the Docker artwork. The [screen inventory](screens.md) tracks the separate tablet detail screen and the remaining device layouts.
