# Android packaging

Build the APK for both the regular Android launcher and the Android TV launcher.

```sh
sh build/android/package.sh
```

The script layers TV launcher metadata over the pinned NSGo tool in a temporary directory and uses NSGo to compile and sign the app. It writes the APK and checksum to `dist/releases/`. It accepts NSGo build flags, including `-target android_x64` for a TV emulator with an x86-64 Android NDK.

The APK advertises the normal and Leanback launcher categories. It declares touchscreen, Leanback, camera, and autofocus optional so the QR scanner permission leaves camera-free TV devices eligible. [Android TV hardware requirements](https://developer.android.com/training/tv/get-started/hardware)

The installed-APK smoke test launches through the Leanback intent and checks onboarding, settings, and source selection. The current Android TV cold-start focus result is recorded with the [welcome screen captures](../../docs/ui-review/welcome.md#android-tv-viewport).
