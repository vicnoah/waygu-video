#!/bin/sh
set -eu

tools="$ANDROID_HOME/cmdline-tools/latest/bin"
export PATH="$tools:$ANDROID_HOME/platform-tools:$PATH"
export ANDROID_USER_HOME="$RUNNER_TEMP/waygu-tv"
export ANDROID_EMULATOR_HOME="$ANDROID_USER_HOME"
export ANDROID_AVD_HOME="$ANDROID_USER_HOME/avd"
export ANDROID_SERIAL="${ANDROID_SERIAL:-emulator-5554}"
mkdir -p "$ANDROID_AVD_HOME"

sdkmanager --install emulator platform-tools "$ANDROID_IMAGE" >/dev/null
printf 'no\n' | avdmanager create avd -n waygu_tv -k "$ANDROID_IMAGE" -d "$ANDROID_PROFILE"
df -h "$ANDROID_AVD_HOME"
adb start-server

report="$ANDROID_USER_HOME/emulator.log"
"$ANDROID_HOME/emulator/emulator" -avd waygu_tv -port "${ANDROID_SERIAL#emulator-}" -accel on -memory 2048 \
    -no-window -no-snapshot -noaudio -no-boot-anim -no-metrics -gpu swiftshader -feature -Vulkan >"$report" 2>&1 &
emulator_pid=$!
cleanup() {
    status=$?
    if [ "$status" -ne 0 ]; then
        tail -90 "$report"
    fi
    timeout 8s adb -s "$ANDROID_SERIAL" emu kill || kill "$emulator_pid" 2>/dev/null || true
}
trap cleanup EXIT

wait_for_tv() {
    deadline=$(( $(date +%s) + 240 ))
    while kill -0 "$emulator_pid" 2>/dev/null && [ "$(date +%s)" -lt "$deadline" ]; do
        boot=$(timeout 5s adb -s "$ANDROID_SERIAL" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')
        if [ "$boot" = 1 ] && timeout 8s adb -s "$ANDROID_SERIAL" shell input keyevent 82; then
            return 0
        fi
        sleep 2
    done
    return 1
}

if ! wait_for_tv; then
    printf '%s\n' 'Android TV emulator did not finish starting.' >&2
    exit 1
fi
printf '%s\n' 'Android TV emulator ready.'
sh build/android/emulator-test.sh tv
