# Android packaging

Build the APK for both the regular Android launcher and the Android TV launcher.

```sh
sh build/android/package.sh
```

The script stages the pinned NSGo tool with the TV manifest and a prebuilt Android runtime. It uses NSGo to compile and sign the app and writes the APK and checksum to `dist/releases/`. The ARM64 package uses the bundled runtime. Pass `-target android_x64` for a TV emulator with an x86-64 Android NDK.

The APK advertises the normal and Leanback launcher categories. It declares touchscreen, Leanback, camera, and autofocus optional so the QR scanner permission leaves camera-free TV devices eligible. [Android TV hardware requirements](https://developer.android.com/training/tv/get-started/hardware)

The [runtime patch](nsgo-tv-focus.patch) applies while Android reports TV mode. A directional key moves focus into the first control when the window itself has focus. Focus and blur on pressable controls reach NSGo so it can draw the app's focus indicator.

After changing the patch or the pinned NSGo version, point `ANDROID_HOME` to a development SDK with API 36 and D8, then run the rebuild script with JDK 17 or later. The archive filename identifies the NSGo version it was built against. Keep the upstream [license](LICENSE.nsgo) and [notices](NOTICE.nsgo) with the archive.

```sh
sh build/android/rebuild-tv-runtime.sh
```

The installed-APK smoke test launches through the Leanback intent and checks onboarding, server setup, and settings using directional keys, Select, and Back. See the [Android TV focus comparison](../../docs/ui-review/welcome.md#android-tv-remote-focus).
