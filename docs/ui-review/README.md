# UI review

The [screen inventory](screens.md) tracks each Scenra design variant and its panel states. A screen is ready for sign-off when its layout, spacing, typography, color, components, visible states, and interactions match the design on the intended device. Record approved content differences and any unresolved differences in the screen's review entry.

| Screen | Evidence | Status |
| --- | --- | --- |
| Welcome · light and dark | [Screenshots and interaction checks](welcome.md) | Compared on the Linux native backend at phone width. Phone system UI remains pending. |
| Add source · light and dark | [Screenshots and discovery checks](add-source.md) | Compared on Linux GTK with Emby and Jellyfin Docker responders. Several design rows and mobile verification remain pending. |
| Connect to server · both appearances and all provider tabs | [Screenshots and sign-in checks](source-login.md) | Compared on Linux GTK. The Emby advanced group and the real device checks remain pending. The Plex form lacks a Scenra variant. |

## Capture a comparison

Use the same client dimensions, appearance, locale, and display scale for the app and the Scenra reference. Capture empty, loading, error, selected, and focused states when the design includes them. Confirm navigation and input with the input methods the target supports, including touch, keyboard, and TV remote where applicable. Put the design and the native capture side by side and record each difference with its location and verification status.

From the repository root, run the native app check. On a Linux machine without a graphical session, run the commands under `xvfb-run` with a display large enough for the requested viewport.

```sh
go tool nsgo test -target linux -run TestWelcomeReview -screenshots .nsgo/ui-review/app
```

With the [Docker test servers](../../integration/servers/TESTING.md) running locally, capture source discovery, the four connection tabs, and the authenticated Emby form. The discovery check requires a Linux Docker network reachable from the test host over UDP.

```sh
go tool nsgo test -target linux -env WAYGU_TEST_SERVERS=1 \
  -run '^(TestSourceReview|TestDockerSourceReview)$' \
  -screenshots .nsgo/ui-review/sources
```

The reference uses the Scenra version pinned in this project and renders the original design files and components. Run it in its own module.

```sh
cd build/ui-review/reference
go tool nsgo test -target linux -run TestDesignReferences -screenshots .nsgo/review
```

Select a file with `-env WAYGU_REVIEW_DESIGN=source-login.scenra` and a state with `-env WAYGU_REVIEW_PANEL=fnos`. For a hosted desktop design, pass a path such as `views/desktop-movie.scenra`. With `WAYGU_REVIEW_DESIGN=all`, the exporter captures each design variant with its default panel. The actual app capture still needs to navigate to the same state.

Regenerate the inventory with `python3 build/ui-review/inventory.py` after changing design files. `python3 build/ui-review/inventory.py --check` verifies that the version in the repository is current.
