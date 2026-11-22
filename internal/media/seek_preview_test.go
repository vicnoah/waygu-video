package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSeekPreviewProviders(t *testing.T) {
	red, green := color.RGBA{R: 240, A: 255}, color.RGBA{G: 210, A: 255}
	encode := func(first, second color.Color) []byte {
		t.Helper()
		width := 24
		if second != nil {
			width *= 2
		}
		picture := image.NewRGBA(image.Rect(0, 0, width, 16))
		draw.Draw(picture, image.Rect(0, 0, 24, 16), image.NewUniform(first), image.Point{}, draw.Src)
		if second != nil {
			draw.Draw(picture, image.Rect(24, 0, 48, 16), image.NewUniform(second), image.Point{}, draw.Src)
		}
		var output bytes.Buffer
		if err := jpeg.Encode(&output, picture, nil); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}
	first, second := encode(red, nil), encode(green, nil)
	index := make([]byte, 88)
	copy(index, "\x89BIF\r\n\x1a\n")
	for position, value := range map[int]uint32{12: 2, 16: 1000, 68: 88, 72: 10, 76: uint32(88 + len(first)),
		80: ^uint32(0), 84: uint32(88 + len(first) + len(second))} {
		binary.LittleEndian.PutUint32(index[position:], value)
	}
	archive := append(append(index, first...), second...)
	tile := encode(red, green)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/clip/PlaybackInfo":
			fmt.Fprint(w, `{"MediaSources":[{"Id":"primary","ItemId":"clip"},{"Id":"source","ItemId":"second"}]}`)
		case "/Videos/second/index.bif":
			if r.URL.Query().Get("Width") != "320" || r.Header.Get("X-Emby-Token") != "fixture-token" {
				t.Errorf("Emby did not receive the requested width and account")
			}
			w.Write(archive)
		case "/Items/clip":
			if r.URL.Query().Get("Fields") != "Trickplay" {
				t.Error("Jellyfin did not receive the Trickplay field request")
			}
			fmt.Fprint(w, `{"Trickplay":{"source":{"24":{"Width":24,"Height":16,"TileWidth":2,"TileHeight":1,"ThumbnailCount":2,"Interval":10000}}}}`)
		case "/Videos/clip/Trickplay/24/0.jpg":
			if r.URL.Query().Get("MediaSourceId") != "source" {
				t.Error("Jellyfin did not receive the selected source ID")
			}
			w.Write(tile)
		case "/library/metadata/clip":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"RatingKey":"clip","Media":[{"ID":12,"Part":[{"ID":44}]}]}]}}`)
		case "/library/parts/44/indexes/sd/12500":
			if r.Header.Get("X-Plex-Token") != "fixture-token" {
				t.Error("Plex did not receive the account")
			}
			w.Write(second)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	for _, provider := range []Provider{Emby, Jellyfin, Plex} {
		t.Run(string(provider), func(t *testing.T) {
			client, err := NewClient(provider, server.URL, NewDevice("Waygu preview test"), Session{UserID: "viewer", Token: "fixture-token"})
			if err != nil {
				t.Fatal(err)
			}
			source := "source"
			if provider == Plex {
				source = "12"
			}
			preview, err := client.(SeekPreviewClient).SeekPreviews(t.Context(), Item{ID: "clip"}, source)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := preview(t.Context(), 12500*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := jpeg.Decode(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			r, g, _, _ := frame.At(12, 8).RGBA()
			if frame.Bounds().Size() != image.Pt(24, 16) || g <= r {
				t.Fatalf("preview did not return the second frame at 12.5 seconds")
			}
		})
	}
	if _, err := bifPreview(archive[:len(archive)-1]); err == nil {
		t.Fatal("truncated BIF archive was accepted")
	}
}
