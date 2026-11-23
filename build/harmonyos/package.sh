#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
mode=build
case "${1:-}" in
    test|test-store|rebuild-host)
        mode=$1
        shift
        ;;
esac
module=github.com/nsgo-dev/nsgo
go mod download "$module"
source=$(go list -m -f '{{.Dir}}' "$module")
version=$(go list -m -f '{{.Version}}' "$module")
host="$PWD/build/harmonyos/host-$version.tar.gz"
mkdir -p .nsgo
staging=$(mktemp -d "$PWD/.nsgo/waygu-harmony.XXXXXXXX")
trap 'rm -rf -- "$staging"' EXIT

cp -R "$source" "$staging/nsgo"
chmod -R u+w "$staging/nsgo"
patch -d "$staging/nsgo" -F 0 -p1 < build/harmonyos/nsgo-secrets.patch
patch -d "$staging/nsgo" -F 0 -p1 < build/harmonyos/nsgo-network.patch
cp build/harmonyos/MediaSockets.ets "$staging/nsgo/cmd/nsgo/harmony/templates/entry/src/main/ets/pages/"
case "$mode" in
    rebuild-host)
        compiler="$staging/nsgo/cmd/nsgo/harmony"
        TMPDIR="$staging" sh "$compiler/host/build.sh"
        tar -C "$compiler" -czf "$staging/host.tar.gz" \
            host/ets/modules.abc host/ets/widgets.abc host/module.json host/rpcid.sc host-templates.sha256
        mv "$staging/host.tar.gz" "$host"
        exit
        ;;
    test-store)
        cd "$staging/nsgo"
        go tool nsgo test -run '^TestEncryptedSecrets' ./internal/platform/harmonyos
        exit
        ;;
esac

if [ ! -s "$host" ]; then
    printf '%s\n' "Rebuild the HarmonyOS host for NSGo $version with build/harmonyos/package.sh rebuild-host." >&2
    exit 1
fi
tar -C "$staging/nsgo/cmd/nsgo/harmony" -xzf "$host"
cp go.mod "$staging/build.mod"
cp go.sum "$staging/build.sum"
go mod edit -modfile="$staging/build.mod" -replace="$module=$staging/nsgo"
export GOFLAGS="${GOFLAGS:+$GOFLAGS }'-modfile=$staging/build.mod'"
if [ "$mode" = test ]; then
    go tool nsgo test -target harmonyos "$@"
else
    go tool nsgo build -target harmonyos "$@"
fi
