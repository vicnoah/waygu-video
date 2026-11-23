#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
mode=build
if [ "${1:-}" = test ]; then
    mode=test
    shift
fi
module=github.com/nsgo-dev/nsgo
go mod download "$module"
source=$(go list -m -f '{{.Dir}}' "$module")
version=$(go list -m -f '{{.Version}}' "$module")
runtime="build/android/classes-$version.dex.gz"
if [ ! -s "$runtime" ]; then
    printf '%s\n' "Rebuild the TV runtime for NSGo $version with build/android/rebuild-tv-runtime.sh." >&2
    exit 1
fi
mkdir -p .nsgo
staging=$(mktemp -d "$PWD/.nsgo/waygu-android.XXXXXXXX")
trap 'rm -rf -- "$staging"' EXIT

cp -R "$source" "$staging/nsgo"
chmod -R u+w "$staging/nsgo"
patch -d "$staging/nsgo" -F 0 -p1 < build/android/nsgo-tv.patch
patch -d "$staging/nsgo" -F 0 -p1 < build/android/nsgo-tv-banner.patch
cp assets/tv-banner.png assets/tv-banner-zh-Hans.png "$staging/nsgo/cmd/nsgo/"
patch -d "$staging/nsgo" -F 0 -p1 < build/android/nsgo-playback-manifest.patch
gzip -dc "$runtime" > "$staging/nsgo/internal/platform/android/runtime/classes.dex"
cp go.mod "$staging/build.mod"
cp go.sum "$staging/build.sum"
go mod edit -modfile="$staging/build.mod" -replace="$module=$staging/nsgo"
if [ "$mode" = test ]; then
    patch -d "$staging/nsgo" -F 0 -p1 < build/android/nsgo-test-host.patch
    GOFLAGS="${GOFLAGS:+$GOFLAGS }'-modfile=$staging/build.mod'" \
        go tool nsgo test -target android "$@"
else
    go tool -modfile="$staging/build.mod" nsgo build -target android_arm64 "$@"
fi
