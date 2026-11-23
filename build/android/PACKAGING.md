# Android packaging

Build the APK for both the regular Android launcher and the Android TV launcher.

```sh
sh build/android/package.sh
```

The script stages the pinned NSGo tool with the launcher and media service entries and a prebuilt Android runtime. It uses NSGo to compile and sign the app and writes the APK and checksum to `dist/releases/`. The ARM64 package uses the bundled runtime. Pass `-target android_x64` for an x86-64 Android emulator with the matching NDK.

The APK advertises the normal and Leanback launcher categories. It declares touchscreen, Leanback, camera, and autofocus optional so the QR scanner permission leaves camera-free TV devices eligible. [Android TV hardware requirements](https://developer.android.com/training/tv/get-started/hardware)

The APK and the App Bundle (`-channel store`) use the 320 × 180 launch banners in `assets/tv-banner.png` and `assets/tv-banner-zh-Hans.png`. Android selects the Simplified Chinese banner from the device language and falls back to the English banner. The regular launcher still uses the app icon. [Android TV launcher evidence](../../docs/ui-review/welcome.md#android-tv-launcher) · [Android TV banner requirements](https://developer.android.com/training/tv/get-started/create)

The bundled launcher queries Android's current default network for the playback profile and cellular warning. Its default-network callback reports Wi-Fi, wired, cellular and connection loss to the download queue. [Android network state](https://developer.android.com/reference/android/net/ConnectivityManager)

On an Android phone emulator, select Wi-Fi as the default network and run `sh build/android/test-network.sh default <serial>`. Select cellular data and run `sh build/android/test-network.sh cellular <serial>`. The test verifies the launcher response, the two negotiated profiles, and both warning actions. The cellular capture is written to `.nsgo/android-network-cellular/`. [Playback settings evidence](../../docs/ui-review/phone-player-settings.md#playback)

The background audio tests use an Android phone emulator and Python 3.10 or later. They play an H.264/AAC clip, send Home, and restore the app after it pauses. The enabled case checks that Android's foreground media service stays active, that the player's position advances in the background, and that Android's media pause control stops it. The other case checks that the player pauses on Home without a foreground service. Both cases check the same video node and a nonzero playback position upon return. [Settings evidence](../../docs/ui-review/phone-player-settings.md#background-audio)

```sh
sh build/android/test-background-audio.sh off <serial>
sh build/android/test-background-audio.sh on <serial>
```

The [runtime patch](nsgo-tv-focus.patch) preserves TV remote key names and moves focus into the first control when the window itself has focus. NSGo reports focus and blur on pressable controls so the app can draw the focus indicator.

After changing a runtime patch or the pinned NSGo version, point `ANDROID_HOME` to a development SDK with API 36 and D8, then run the rebuild script with JDK 17 or later. The archive filename identifies the NSGo version it was built against. Keep the upstream [license](LICENSE.nsgo) and [notices](NOTICE.nsgo) with the archive.

```sh
sh build/android/rebuild-tv-runtime.sh
```

The installed-APK smoke test launches through the Leanback intent and checks onboarding, server setup, and settings using directional keys, Select, and Back. See the [Android TV focus comparison](../../docs/ui-review/welcome.md#android-tv-remote-focus).

Use the `test` subcommand of `build/android/package.sh` with an Android TV device or emulator. It applies the same pinned Android runtime and manifest as packaging, then passes the arguments to `nsgo test -target android`. [Docker media fixture and replay](../../integration/servers/TESTING.md#tv-next-episode)

The NSGo 0.3 test agent captures the rendered Android window with PixelCopy. Run `TestAndroidScreenshotClipsArtwork` on an emulator to check the rounded artwork in the captured pixels. [Android PixelCopy API](https://developer.android.com/reference/android/view/PixelCopy)

Run `python3 build/android/test-download-network.py <serial>` on an Android phone emulator with the Docker fixtures running. The driver switches the emulator between AndroidWifi, mobile data and offline while the app transfers an actual movie. It restores the Wi-Fi and mobile-data settings on exit. [Download network proof](../../docs/ui-review/storage-downloads.md#wi-fi-downloads)
