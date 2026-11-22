#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
case "${1:-}" in
    on|off) mode=$1 ;;
    *) printf '%s\n' 'Usage: sh build/android/test-background-audio.sh on|off [adb serial]' >&2; exit 2 ;;
esac
device=${2:-${ANDROID_SERIAL:?Set ANDROID_SERIAL or pass the Android emulator serial}}
module=github.com/nsgo-dev/nsgo
go mod download "$module"
source=$(go list -m -f '{{.Dir}}' "$module")
version=$(go list -m -f '{{.Version}}' "$module")
mkdir -p .nsgo
stage=$(mktemp -d "$PWD/.nsgo/android-background-audio.XXXXXXXX")
trap 'rm -rf -- "$stage"' EXIT

cp -R "$source" "$stage/nsgo"
chmod -R u+w "$stage/nsgo"
gzip -dc "build/android/classes-$version.dex.gz" > "$stage/nsgo/internal/platform/android/runtime/classes.dex"
patch -d "$stage/nsgo" -F 0 -p1 < build/android/nsgo-playback-manifest.patch
# Preserve the real launcher channel when the native test has no simulated host.
patch -d "$stage/nsgo" -F 0 -p1 <<'PATCH'
--- a/internal/engine/testhost.go
+++ b/internal/engine/testhost.go
@@ -57 +57,3 @@
-	o.Host.Channel = t.Host
+	if t.Host != nil {
+		o.Host.Channel = t.Host
+	}
PATCH
cp go.mod "$stage/test.mod"
cp go.sum "$stage/test.sum"
go mod edit -modfile="$stage/test.mod" -replace="$module=$stage/nsgo"
GOFLAGS="${GOFLAGS:+$GOFLAGS }'-modfile=$stage/test.mod'" python3 build/android/drive-background-audio.py "$mode" "$device" \
    go tool nsgo test -target android -device "$device" \
    -env "WAYGU_TEST_ANDROID_BACKGROUND_AUDIO=$mode" -run '^TestAndroidBackgroundAudioNative$' -timeout 90s \
    -screenshots ".nsgo/android-background-audio-$mode" .
