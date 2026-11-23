package main

import (
	"image"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAndroidScreenshotClipsArtwork(t *testing.T) {
	if nsgotest.Target() != "android" {
		t.Skip("requires Android's rendered window")
	}
	app := nsgotest.Start(t, func(*core.Scope) core.Node {
		return core.View{Class: "flex-1 items-center justify-center bg-black", Children: []core.Node{
			core.View{Class: "relative size-24 overflow-hidden rounded-full", Children: []core.Node{
				core.View{Class: "absolute inset-0", Style: core.Style{Background: core.RGB(0xff0000)}},
			}},
		}}
	})

	capture := app.Capture()
	bounds := capture.Bounds()
	var artwork image.Rectangle
	pixels := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := capture.At(x, y).RGBA()
			if r > 0xe000 && g < 0x2000 && b < 0x2000 {
				artwork = artwork.Union(image.Rect(x, y, x+1, y+1))
				pixels++
			}
		}
	}
	if artwork.Empty() {
		t.Fatal("capture did not contain the artwork")
	}
	coverage := float64(pixels) / float64(artwork.Dx()*artwork.Dy())
	if coverage < 0.75 || coverage > 0.82 {
		t.Fatalf("artwork covers %.1f%% of %v, want the area of a circle", 100*coverage, artwork)
	}
}
