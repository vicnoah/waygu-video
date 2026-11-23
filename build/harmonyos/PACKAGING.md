# HarmonyOS preview

Build the Waygu Video preview HAP from the repository root.

```sh
sh build/harmonyos/package.sh -installers=false
```

The package includes the ArkUI host and the Wasm app. The HAP declares phone and tablet support. Without an OpenHarmony SDK, the build embeds the checked-in host and uses a padded emulator bundle ID. With API 26 and `hvigorw` configured, the same command builds for the exact `dev.nsgo.wayguvideo` ID. A physical device requires the matching profile and certificate in NSGo's signing configuration. [NSGo HarmonyOS packaging](https://nsgo.dev/docs/building/#harmonyos)

The Waygu host uses the system Asset Store to hold an encryption key. Media tokens, passwords, and custom request headers use authenticated encryption in the app's private files. The host uses the same system key alias when the app restarts. If the system store is unavailable, writing a credential fails before creating its encrypted file. [Asset Store API](https://developer.huawei.com/consumer/en/doc/harmonyos-guides/asset-js-add/)

## Local checks

```sh
sh build/harmonyos/package.sh test-store
go tool nsgo check -target harmonyos
WAYGU_TEST_HAP="$PWD/dist/releases/waygu-video-0.1.0-harmonyos.hap" \
  go tool nsgo test -run '^TestPreviewArchive$' ./build/harmonyos
```

The storage test covers reopening a credential with the same system key, replacing it, detecting modified ciphertext, and deleting it. The key service in this test is a local fixture. The archive test checks that both the ArkTS host and Wasm include their side of the bridge. With `hdc` on the path and an emulator connected, run `sh build/harmonyos/package.sh -run` to install the HAP. Verify an authenticated Emby or Jellyfin source, restart the app, open the same library, and play a synthetic clip. Repeat on a tablet profile and compare both form factors with the matching Scenra screens. The emulator acceptance pass is pending. [HarmonyOS device connection](https://developer.huawei.com/consumer/en/doc/HarmonyOS-Guides/hdc/) · [Screen inventory](../../docs/ui-review/screens.md)

Streams that need the app's authenticated loopback proxy still need HarmonyOS transport support. The Go runtime inside ArkWeb has no listening sockets for that proxy. [NSGo HarmonyOS host](https://github.com/nsgo-dev/nsgo/blob/v0.3.0/docs/architecture/harmonyos.md)

To rebuild the embedded host after changing the patch or SDK version, put `hvigorw` on the path, set `OHOS_BASE_SDK_HOME` to the SDK root containing `26.0.0`, and run `sh build/harmonyos/package.sh rebuild-host`. The archive contains the rebuilt ArkTS bytecode, manifest and host template hash. [NSGo HarmonyOS host](https://github.com/nsgo-dev/nsgo/blob/v0.3.0/docs/architecture/harmonyos.md)
