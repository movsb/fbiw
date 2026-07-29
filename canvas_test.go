package main

import (
	"image"
	"image/color"
	"testing"
)

func TestCanvasFillRect(t *testing.T) {
	c := &Canvas{
		buffer:        make([]byte, 4*4*4),
		bytesPerPixel: 4,
		width:         4,
		height:        4,
	}

	cr := color.NRGBA{R: 10, G: 20, B: 30, A: 40}
	c.FillRect(1, 1, 2, 2, cr)

	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			expected := color.NRGBA{}
			if x >= 1 && x < 3 && y >= 1 && y < 3 {
				expected = cr
			}
			if got := c.getPixel(x, y); got != expected {
				t.Fatalf("pixel(%d,%d) = %#v, want %#v", x, y, got, expected)
			}
		}
	}
}

func TestDrawImagePreservesExistingPixelsForTransparentSource(t *testing.T) {
	c := &Canvas{
		buffer:        make([]byte, 4*4*4),
		bytesPerPixel: 4,
		width:         4,
		height:        4,
	}
	c.FillRect(0, 0, 4, 4, color.NRGBA{R: 255, G: 0, B: 0, A: 255})

	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 0})

	drawImage(c, img, 1, 1)

	if got := c.getPixel(0, 0); got != (color.NRGBA{R: 255, G: 0, B: 0, A: 255}) {
		t.Fatalf("transparent pixel should preserve existing pixel, got %#v", got)
	}
}
