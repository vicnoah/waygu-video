#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
case "${1:-}" in
    build|check|test) ;;
    *) printf '%s\n' 'Usage: sh build/apple/package.sh build|check|test [nsgo options]' >&2; exit 2 ;;
esac

module=github.com/nsgo-dev/nsgo
go mod download "$module"
source=$(go list -m -f '{{.Dir}}' "$module")
mkdir -p .nsgo
staging=$(mktemp -d "$PWD/.nsgo/waygu-apple.XXXXXXXX")
trap 'rm -rf -- "$staging"' EXIT

cp -R "$source" "$staging/nsgo"
chmod -R u+w "$staging/nsgo"
patch -d "$staging/nsgo" -F 0 -p1 < build/apple/nsgo-tv-focus.patch
patch -d "$staging/nsgo" -F 0 -p1 < build/apple/nsgo-tv-typography.patch
cp go.mod "$staging/build.mod"
cp go.sum "$staging/build.sum"
go mod edit -modfile="$staging/build.mod" -replace="$module=$staging/nsgo"
GOFLAGS="${GOFLAGS:+$GOFLAGS }'-modfile=$staging/build.mod'" go tool nsgo "$@"
