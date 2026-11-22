#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
module=github.com/nsgo-dev/nsgo
go mod download "$module"
source=$(go list -m -f '{{.Dir}}' "$module")
staging=$(mktemp -d "${TMPDIR:-/tmp}/waygu-android.XXXXXXXX")
trap 'rm -rf -- "$staging"' EXIT

cp -R "$source" "$staging/nsgo"
chmod -R u+w "$staging/nsgo"
patch -d "$staging/nsgo" -F 0 -p1 < build/android/nsgo-tv.patch
cp go.mod "$staging/build.mod"
cp go.sum "$staging/build.sum"
go mod edit -modfile="$staging/build.mod" -replace="$module=$staging/nsgo"
go tool -modfile="$staging/build.mod" nsgo build -target android_arm64 "$@"
