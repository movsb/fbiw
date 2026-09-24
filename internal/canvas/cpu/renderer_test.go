package cpu

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/movsb/fbiw/internal/canvas"
)

func TestFillRectUsesDiv255AlphaBlend(t *testing.T) {
	r := New(1, 1)
	r.FillRect(image.Rect(0, 0, 1, 1), image.Rect(0, 0, 1, 1), canvas.Color(0x80ffffff))
	if got, want := r.Pixels, []byte{128, 128, 128, 128}; !bytes.Equal(got, want) {
		t.Fatalf("pixel = %v, want %v", got, want)
	}
}

func TestDrawImageTransformsOnlySourceRegion(t *testing.T) {
	r := New(6, 3)
	img := canvas.Image{
		Pixels: []byte{
			0, 0, 255, 255,
			0, 255, 0, 255,
			255, 0, 0, 255,
		},
		Width: 3, Height: 1, Opaque: true,
	}
	r.DrawImage(img, image.Rect(1, 0, 2, 1), 0, 2, 2, 3, 1.5, 1, image.Rect(0, 0, 6, 3), image.Rectangle{}, 0)

	seenGreen := false
	for i := 0; i < len(r.Pixels); i += 4 {
		if r.Pixels[i] != 0 || r.Pixels[i+2] != 0 {
			t.Fatalf("transformed source region sampled an adjacent pixel: %v", r.Pixels[i:i+4])
		}
		seenGreen = seenGreen || r.Pixels[i+1] != 0
	}
	if !seenGreen {
		t.Fatal("transformed source region was not drawn")
	}
}

func TestDrawImageOpacity(t *testing.T) {
	r := New(1, 1)
	r.SetPixel(image.Pt(0, 0), color.NRGBA{B: 200, A: 255})
	img := canvas.Image{Pixels: []byte{0, 0, 200, 255}, Width: 1, Height: 1, Opaque: true}
	r.DrawImage(img, image.Rect(0, 0, 1, 1), 0, 1, 1, .5, .5, .5, image.Rect(0, 0, 1, 1), image.Rectangle{}, 0)
	got := r.Pixel(image.Pt(0, 0))
	if got.R < 99 || got.R > 101 || got.B < 99 || got.B > 101 || got.A != 255 {
		t.Fatalf("opacity blend = %#v", got)
	}
	r.DrawImage(img, image.Rect(0, 0, 1, 1), 0, 1, 1, .5, .5, 0, image.Rect(0, 0, 1, 1), image.Rectangle{}, 0)
	if after := r.Pixel(image.Pt(0, 0)); after != got {
		t.Fatal("zero opacity changed pixel")
	}
}
