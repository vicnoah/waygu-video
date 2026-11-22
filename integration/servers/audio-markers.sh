#!/bin/sh
set -eu
cd "$(dirname "$0")"

series='media/Shows/Waygu Audio Marker Series (2025)'
mkdir -p "$series/Season 01"
cat > "$series/tvshow.nfo" <<'NFO'
<?xml version="1.0" encoding="utf-8"?>
<tvshow><title>Waygu Audio Marker Series</title><year>2025</year><genre>Test Clip</genre></tvshow>
NFO

make_episode() (
    number=$1
    opening=$2
    length=$3
    scene_tone=$4
    episode="$series/Season 01/Waygu Audio Marker Series - S01E$number.mp4"
    if [ -s "$episode" ]; then
        exit 0
    fi
    story=$((length - opening - 18))
    ffmpeg -nostdin -hide_banner -loglevel error -y \
        -f lavfi -i "color=c=navy:s=320x180:r=12:d=$length" \
        -f lavfi -i "aevalsrc=0.30*sin(2*PI*($scene_tone*t+6*t*t)):s=16000:d=$opening" \
        -f lavfi -i 'aevalsrc=0.26*sin(2*PI*(167*t+62*t*t))+0.12*sin(2*PI*(294*t+17*t*t)):s=16000:d=9' \
        -f lavfi -i "aevalsrc=0.27*sin(2*PI*($scene_tone*t+3*t*t)):s=16000:d=$story" \
        -f lavfi -i 'aevalsrc=0.26*sin(2*PI*(1940*t-88*t*t))+0.13*sin(2*PI*(910*t-30*t*t)):s=16000:d=9' \
        -filter_complex '[1:a][2:a][3:a][4:a]concat=n=4:v=0:a=1[audio]' \
        -map 0:v -map '[audio]' -c:v libx264 -preset veryfast -pix_fmt yuv420p -c:a aac \
        -movflags +faststart "$episode"
)

make_episode 01 4 48 151
make_episode 02 6 52 2690
make_episode 03 2 56 3635
