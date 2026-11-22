#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
module=github.com/nsgo-dev/nsgo
go mod download "$module"
source=$(go list -m -f '{{.Dir}}' "$module")
version=$(go list -m -f '{{.Version}}' "$module")
staging=$(mktemp -d build/android/.runtime.XXXXXXXX)
trap 'rm -rf -- "$staging"' EXIT

android=internal/platform/android
mkdir -p "$staging/$android"
cp -R "$source/$android/java" "$source/$android/runtime" "$staging/$android/"
chmod -R u+w "$staging/$android"
patch -d "$staging" -F 0 -p1 < build/android/nsgo-tv-focus.patch
patch -d "$staging" -F 0 -p1 < build/android/nsgo-network.patch
sh "$staging/$android/runtime/build.sh"
runtime="classes-$version.dex.gz"
gzip -nc "$staging/$android/runtime/classes.dex" > "$staging/$runtime"
mv "$staging/$runtime" "build/android/$runtime"
