#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
case "${1:-}" in
    default|cellular) network=$1 ;;
    *) printf '%s\n' 'Usage: sh build/android/test-network.sh default|cellular [adb serial]' >&2; exit 2 ;;
esac
device=${2:-${ANDROID_SERIAL:-}}

module=github.com/nsgo-dev/nsgo
go mod download "$module"
source=$(go list -m -f '{{.Dir}}' "$module")
version=$(go list -m -f '{{.Version}}' "$module")
mkdir -p .nsgo
stage=$(mktemp -d "$PWD/.nsgo/android-network.XXXXXXXX")
trap 'rm -rf -- "$stage"' EXIT

cp -R "$source" "$stage/nsgo"
chmod -R u+w "$stage/nsgo"
gzip -dc "build/android/classes-$version.dex.gz" > "$stage/nsgo/internal/platform/android/runtime/classes.dex"
# Preserve the device's launcher channel during a native test that supplies no simulated host.
patch -d "$stage/nsgo" -F 0 -p1 < build/android/nsgo-test-host.patch
cp go.mod "$stage/test.mod"
cp go.sum "$stage/test.sum"
go mod edit -modfile="$stage/test.mod" -replace="$module=$stage/nsgo"
# GOFLAGS also passes the replacement to the child Go build started by NSGo.
GOFLAGS="${GOFLAGS:+$GOFLAGS }'-modfile=$stage/test.mod'" go tool nsgo test -target android -device "$device" \
    -env "WAYGU_TEST_ANDROID_NETWORK=$network" \
    -run '^(TestAndroidDefaultPlaybackNetwork|TestPlaybackUsesNetworkQuality)$' -timeout 90s \
    -screenshots ".nsgo/android-network-$network" .
