#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
module=github.com/nsgo-dev/nsgo
go mod download "$module"
source=$(go list -m -f '{{.Dir}}' "$module")
version=$(go list -m -f '{{.Version}}' "$module")
output="build/android/classes-$version.dex.gz"
system_input_test=false
if [ "${1:-}" = --system-input-test ]; then
    output=${2:?Provide an output path for the test runtime}
    system_input_test=true
fi
staging=$(mktemp -d build/android/.runtime.XXXXXXXX)
trap 'rm -rf -- "$staging"' EXIT

android=internal/platform/android
mkdir -p "$staging/$android"
cp -R "$source/$android/java" "$source/$android/runtime" "$staging/$android/"
chmod -R u+w "$staging/$android"
patch -d "$staging" -F 0 -p1 < build/android/nsgo-tv-focus.patch
patch -d "$staging" -F 0 -p1 < build/android/nsgo-network.patch
patch -d "$staging" -F 0 -p1 < build/android/nsgo-background-audio.patch
if [ "$system_input_test" = true ]; then
    patch -d "$staging" -F 0 -p1 < build/android/nsgo-test-system-input.patch
fi
cp build/android/WayguPlaybackService.java "$staging/$android/java/dev/oneo/nsgo/"
sh "$staging/$android/runtime/build.sh"
runtime="classes-$version.dex.gz"
gzip -nc "$staging/$android/runtime/classes.dex" > "$staging/$runtime"
mv "$staging/$runtime" "$output"
