# Android packaging

Build the APK for both the regular Android launcher and the Android TV launcher.

```sh
sh build/android/package.sh
```

The script stages the pinned NSGo tool with the launcher and media service entries and a prebuilt Android runtime. It uses NSGo to compile and sign the app and writes the APK and checksum to `dist/releases/`. The ARM64 package uses the bundled runtime. Pass `-target android_x64` for an x86-64 Android emulator with the matching NDK.

The APK advertises the normal and Leanback launcher categories. It declares touchscreen, Leanback, camera, and autofocus optional so the QR scanner permission leaves camera-free TV devices eligible. [Android TV hardware requirements](https://developer.android.com/training/tv/get-started/hardware)

The bundled launcher queries Android's current default network for the playback profile and cellular warning. The Go player calls the launcher through NSGo's host channel when opening an online title. [Android network state](https://developer.android.com/reference/android/net/ConnectivityManager)

On an Android phone emulator, select Wi-Fi as the default network and run `sh build/android/test-network.sh default <serial>`. Select cellular data and run `sh build/android/test-network.sh cellular <serial>`. The test verifies the launcher response, the two negotiated profiles, and both warning actions. The cellular capture is written to `.nsgo/android-network-cellular/`. [Playback settings evidence](../../docs/ui-review/phone-player-settings.md#playback)

The background audio tests use an Android phone emulator and Python 3.10 or later. They play an H.264/AAC clip, send Home, and restore the app after it pauses. The enabled case checks that Android's foreground media service stays active, that the player's position advances in the background, and that Android's media pause control stops it. The other case checks that the player pauses on Home without a foreground service. Both cases check the same video node and a nonzero playback position upon return. [Settings evidence](../../docs/ui-review/phone-player-settings.md#background-audio)

```sh
sh build/android/test-background-audio.sh off <serial>
sh build/android/test-background-audio.sh on <serial>
```

The [runtime patch](nsgo-tv-focus.patch) applies while Android reports TV mode. A directional key moves focus into the first control when the window itself has focus. Focus and blur on pressable controls reach NSGo so it can draw the app's focus indicator.

After changing the patch or the pinned NSGo version, point `ANDROID_HOME` to a development SDK with API 36 and D8, then run the rebuild script with JDK 17 or later. The archive filename identifies the NSGo version it was built against. Keep the upstream [license](LICENSE.nsgo) and [notices](NOTICE.nsgo) with the archive.

```sh
sh build/android/rebuild-tv-runtime.sh
```

The installed-APK smoke test launches through the Leanback intent and checks onboarding, server setup, and settings using directional keys, Select, and Back. See the [Android TV focus comparison](../../docs/ui-review/welcome.md#android-tv-remote-focus).

Use the `test` subcommand of `build/android/package.sh` with an Android TV device or emulator. It applies the same pinned Android runtime and manifest as packaging, then passes the arguments to `nsgo test -target android`. [Docker media fixture and replay](../../integration/servers/TESTING.md#tv-next-episode)
