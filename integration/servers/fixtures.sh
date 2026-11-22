#!/bin/sh
set -eu

cd "$(dirname "$0")"
movie='media/Movies/Waygu Test Movie (2025)'
season='media/Shows/Waygu Test Series (2025)/Season 01'
resume='media/Movies/Waygu Resume Test (2025)'
mkdir -p "$movie" "$season" "$resume"

make_clip() {
    picture=$1
    frequency=$2
    output=$3
    if [ -s "$output" ]; then
        return
    fi
    ffmpeg -nostdin -hide_banner -loglevel error -y \
        -f lavfi -i "$picture=size=640x360:rate=24" \
        -f lavfi -i "sine=frequency=$frequency:sample_rate=48000" \
        -t 18 -c:v libx264 -preset veryfast -pix_fmt yuv420p \
        -c:a aac -movflags +faststart "$output"
}

make_clip testsrc2 440 "$movie/Waygu Test Movie (2025).mp4"
make_clip smptebars 520 "$season/Waygu Test Series - S01E01.mp4"

if [ ! -s "$resume/Waygu Resume Test (2025).mp4" ]; then
    ffmpeg -nostdin -hide_banner -loglevel error -y \
        -f lavfi -i 'color=c=purple:size=160x90:rate=12' \
        -f lavfi -i 'sine=frequency=340:sample_rate=24000' \
        -t 360 -c:v libx264 -preset ultrafast -pix_fmt yuv420p \
        -c:a aac -b:a 24k -movflags +faststart "$resume/Waygu Resume Test (2025).mp4"
fi

cat > "$movie/Waygu Test Movie (2025).en.srt" <<'EOF'
1
00:00:01,000 --> 00:00:04,000
Waygu Video playback fixture

2
00:00:06,000 --> 00:00:11,000
Subtitle and seek verification
EOF
