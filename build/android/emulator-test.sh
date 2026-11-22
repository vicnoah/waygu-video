#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
if [ "$1" = tv ]; then
    timeout 90s adb -s "$ANDROID_SERIAL" install -r dist/android-tv/releases/*-android-x64.apk
    app_id=$(sed -n 's/^id: *//p' nsgo.yaml)
    activity=$(timeout 20s adb -s "$ANDROID_SERIAL" shell cmd package resolve-activity --brief \
        -a android.intent.action.MAIN -c android.intent.category.LEANBACK_LAUNCHER "$app_id" |
        tr -d '\r' | tail -n 1)
    printf 'Resolved TV launcher activity: %s\n' "$activity"
    test "$activity" = "$app_id/dev.oneo.nsgo.NsgoActivity"
    timeout 45s adb -s "$ANDROID_SERIAL" shell am start -W \
        -a android.intent.action.MAIN -c android.intent.category.LEANBACK_LAUNCHER -n "$activity"
    exec python3 build/android/tv-smoke.py
fi
if ! go tool nsgo test -target android -device "$ANDROID_SERIAL" \
    -run '^(TestChineseAppName|TestSkipOnboarding|TestNativeVideoDecodes)$' -timeout 2m .; then
    timeout 8s adb -s "$ANDROID_SERIAL" logcat -d -v brief -s AndroidRuntime:E MediaPlayer:V \
        MediaPlayerNative:V NuPlayer:V NuPlayerDriver:V CCodec:V | tail -120
    exit 1
fi
