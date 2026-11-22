#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
navigation=TestSkipOnboarding
if [ "$1" = tv ]; then
    sh build/android/package.sh -target android_x64 -output dist/android-tv
    adb -s "$ANDROID_SERIAL" install -r dist/android-tv/releases/*-android-x64.apk
    app_id=$(sed -n 's/^id: *//p' nsgo.yaml)
    adb -s "$ANDROID_SERIAL" shell cmd package resolve-activity --brief \
        -a android.intent.action.MAIN -c android.intent.category.LEANBACK_LAUNCHER "$app_id" |
        grep -F "$app_id/dev.oneo.nsgo.NsgoActivity"
    adb -s "$ANDROID_SERIAL" shell am start -W \
        -a android.intent.action.MAIN -c android.intent.category.LEANBACK_LAUNCHER -p "$app_id"
    adb -s "$ANDROID_SERIAL" shell am force-stop "$app_id"
    navigation=TestTVNavigation
fi
if ! go tool nsgo test -target android -device "$ANDROID_SERIAL" \
    -run "^(TestChineseAppName|$navigation|TestNativeVideoDecodes)$" -timeout 2m .; then
    adb -s "$ANDROID_SERIAL" logcat -d -v brief -s AndroidRuntime:E MediaPlayer:V \
        MediaPlayerNative:V NuPlayer:V NuPlayerDriver:V CCodec:V | tail -120
    exit 1
fi
