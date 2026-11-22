# Android packaging

Build the APK for both the regular Android launcher and the Android TV launcher.

```sh
sh build/android/package.sh
```

The script layers TV launcher metadata over the pinned NSGo tool in a temporary directory and uses NSGo to compile and sign the app. It writes the APK and checksum to `dist/releases/`. It accepts NSGo build flags, including `-target android_x64` for a TV emulator with an x86-64 Android NDK.

The APK advertises the normal and Leanback launcher categories and declares touchscreen and Leanback hardware optional. CI installs it on an Android TV emulator, launches the activity through the Leanback intent, and runs tests against the native TV interface.
