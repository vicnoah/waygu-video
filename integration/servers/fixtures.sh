#!/bin/sh
set -eu

cd "$(dirname "$0")"
movie='media/Movies/Waygu Test Movie (2025)'
season='media/Shows/Waygu Test Series (2025)/Season 01'
resume='media/Movies/Waygu Resume Test (2025)'
resume_pair='media/Movies/Waygu Resume Across Sources (2025)'
tracks='media/Movies/Waygu Track Test (2025)'
versions='media/Movies/Waygu Version Test (2025)'
mkdir -p "$movie" "$season" "$resume" "$resume_pair" "$tracks" "$versions" "$tracks/trailers" "$tracks/featurettes"

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
make_clip testsrc 660 "$tracks/trailers/Waygu Teaser.mp4"
make_clip smptebars 720 "$tracks/featurettes/Waygu Behind the Scenes.mp4"

cat > "$movie/Waygu Test Movie (2025).nfo" <<'EOF'
<?xml version="1.0" encoding="utf-8"?>
<movie>
  <title>Waygu Test Movie</title>
  <year>2025</year>
  <genre>Test Clip</genre>
  <uniqueid type="imdb" default="true">tt90000002</uniqueid>
  <uniqueid type="tmdb">900000991</uniqueid>
</movie>
EOF

if [ ! -s "$resume/Waygu Resume Test (2025).mp4" ]; then
    ffmpeg -nostdin -hide_banner -loglevel error -y \
        -f lavfi -i 'color=c=purple:size=160x90:rate=12' \
        -f lavfi -i 'sine=frequency=340:sample_rate=24000' \
        -t 360 -c:v libx264 -preset ultrafast -pix_fmt yuv420p \
        -c:a aac -b:a 24k -movflags +faststart "$resume/Waygu Resume Test (2025).mp4"
fi

if [ ! -s "$resume_pair/Waygu Resume Across Sources (2025).mp4" ]; then
    cp "$resume/Waygu Resume Test (2025).mp4" "$resume_pair/Waygu Resume Across Sources (2025).mp4"
fi
cat > "$resume_pair/Waygu Resume Across Sources (2025).nfo" <<'EOF'
<?xml version="1.0" encoding="utf-8"?>
<movie>
  <title>Waygu Resume Across Sources</title>
  <year>2025</year>
  <uniqueid type="imdb" default="true">tt90000003</uniqueid>
  <uniqueid type="tmdb">900000992</uniqueid>
</movie>
EOF

cat > "$movie/Waygu Test Movie (2025).en.srt" <<'EOF'
1
00:00:01,000 --> 00:00:04,000
Waygu Video playback fixture

2
00:00:06,000 --> 00:00:11,000
Subtitle and seek verification
EOF

track_base="$tracks/Waygu Track Test (2025)"
cat > "$track_base.nfo" <<'EOF'
<?xml version="1.0" encoding="utf-8"?>
<movie>
  <title>Waygu Track Test</title>
  <originaltitle>Waygu Audio and Subtitle Check</originaltitle>
  <year>2025</year>
  <mpaa>G</mpaa>
  <rating>8.3</rating>
  <genre>Test Clip</genre>
  <plot>This synthetic clip contains two audio tones and matching embedded and external captions for playback verification.</plot>
  <director>Lin Chen</director>
  <actor><name>Mei Zhou</name><role>First voice</role><order>0</order></actor>
  <actor><name>Wei Ren</name><role>Second voice</role><order>1</order></actor>
  <actor><name>Jun Wu</name><role>Narrator</role><order>2</order></actor>
  <actor><name>Lan Xu</name><role>Caption reader</role><order>3</order></actor>
</movie>
EOF

cat > "$track_base.en.srt" <<'EOF'
1
00:00:01,000 --> 00:00:04,000
Waygu track selection fixture
EOF

if [ ! -s "$track_base.mp4" ]; then
    ffmpeg -nostdin -hide_banner -loglevel error -y \
        -f lavfi -i 'testsrc2=size=320x180:rate=24' \
        -f lavfi -i 'sine=frequency=440:sample_rate=48000' \
        -f lavfi -i 'sine=frequency=880:sample_rate=48000' \
        -i "$track_base.en.srt" -map 0:v -map 1:a -map 2:a -map 3:s \
        -metadata:s:a:0 language=eng -metadata:s:a:1 language=jpn -metadata:s:s:0 language=eng \
        -disposition:a:0 default -disposition:a:1 0 \
        -t 24 -c:v libx264 -preset veryfast -pix_fmt yuv420p \
        -c:a aac -c:s mov_text -movflags +faststart "$track_base.mp4"
fi

if [ ! -s "$tracks/fanart.jpg" ]; then
    ffmpeg -nostdin -hide_banner -loglevel error -y -ss 3 -i "$track_base.mp4" \
        -frames:v 1 -update 1 "$tracks/fanart.jpg"
fi

for height in 180 360; do
    version="$versions/Waygu Version Test (2025) - ${height}p.mp4"
    if [ ! -s "$version" ]; then
        width=$((height * 16 / 9))
        ffmpeg -nostdin -hide_banner -loglevel error -y \
            -f lavfi -i "testsrc2=size=${width}x${height}:rate=24" \
            -f lavfi -i 'sine=frequency=620:sample_rate=48000' \
            -t 14 -c:v libx264 -preset veryfast -pix_fmt yuv420p \
            -c:a aac -movflags +faststart "$version"
    fi
done
