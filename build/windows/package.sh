#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
target=${1:-}
output=${2:-dist/windows}
case "$target" in
    windows_x64)
        arch=x86_64
        expected=8e5f77430efc53ef175804ba6618527e3af0448a0dcabf04d3cc72659d1a6fce
        ;;
    windows_arm64)
        arch=aarch64
        expected=e5092a8ec0878127fcac2c33d15c566c569bd9dcf13cee4829ae91a2770abbcb
        ;;
    *)
        printf '%s\n' 'Usage: sh build/windows/package.sh windows_x64|windows_arm64 [output-directory]' >&2
        exit 1
        ;;
esac

version=0.42.0
repository=https://github.com/mpv-player/mpv
asset="libmpv-v$version-$arch-w64-mingw32-lgpl.zip"
url="$repository/releases/download/v$version/$asset"
cache=${WAYGU_LIBMPV_CACHE:-.nsgo/libmpv}
archive="$cache/$asset"
mkdir -p "$output" "$cache"
staging=$(mktemp -d "$output/.waygu-windows.XXXXXXXX")
download=
trap 'rm -rf -- "$staging"; if [ -n "$download" ]; then rm -f -- "$download"; fi' EXIT

check_archive() {
    if command -v sha256sum >/dev/null 2>&1; then
        printf '%s  %s\n' "$expected" "$1" | sha256sum --check --status
    else
        printf '%s  %s\n' "$expected" "$1" | shasum -a 256 --check --status
    fi
}

if ! { [ -f "$archive" ] && check_archive "$archive"; }; then
    download=$(mktemp "$cache/.$asset.XXXXXXXX")
    curl --fail --silent --show-error --location --retry 3 --output "$download" "$url"
    if ! check_archive "$download"; then
        printf 'SHA-256 mismatch for %s\n' "$asset" >&2
        exit 1
    fi
    mv -f "$download" "$archive"
    download=
fi

go tool nsgo build -target "$target" -installers=false -output "$staging/app"
set -- "$staging/app"/waygu-video-*-"$target"
if [ "$#" -ne 1 ] || [ ! -s "$1/waygu-video.exe" ]; then
    printf 'Could not locate the NSGo package for %s\n' "$target" >&2
    exit 1
fi
package=$1
name=${package##*/}
unzip -p "$archive" libmpv-2.dll > "$package/libmpv-2.dll"
unzip -p "$archive" LICENSE.LGPL > "$package/LICENSE.libmpv.LGPL.txt"
cat > "$package/LIBMPV-SOURCE.txt" <<EOF
Official libmpv $version Windows $arch LGPL build
Binary archive: $url
Archive SHA-256: $expected
Source: $repository/tree/v$version
Build and dependencies: $repository/actions/runs/41936479105
Library license: LICENSE.libmpv.LGPL.txt
EOF

if [ "${OS:-}" = Windows_NT ] && command -v cygpath >/dev/null 2>&1; then
    windows_tar="$(cygpath -u "$WINDIR")/System32/tar.exe"
    (cd "$staging/app" && "$windows_tar" -a -cf ../portable.zip "$name")
else
    (cd "$staging/app" && zip -q -r ../portable.zip "$name")
fi
mv -f "$staging/portable.zip" "$output/$name.zip"
printf '%s\n' "$output/$name.zip"
