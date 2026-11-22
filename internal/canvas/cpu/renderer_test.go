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

// A scaled source rectangle must fill its geometric bounds without blending
// transparent samples or adjacent pixels from outside the source rectangle.
func TestDrawImageScaledEdges(t *testing.T) {
	for _, opacity := range []float64{1, .5} {
		r := New(10, 8)
		background := color.NRGBA{B: 200, A: 255}
		for y := range 8 {
			for x := range 10 {
				r.SetPixel(image.Pt(x, y), background)
			}
		}
		img := canvas.Image{Width: 4, Height: 4, Pixels: make([]byte, 4*4*4), Opaque: true}
		for i := 0; i < len(img.Pixels); i += 4 {
			img.Pixels[i], img.Pixels[i+3] = 255, 255
		}
		for y := 1; y < 3; y++ {
			for x := 1; x < 3; x++ {
				i := (y*4 + x) * 4
				img.Pixels[i], img.Pixels[i+2] = 0, 200
			}
		}
		r.DrawImage(img, image.Rect(1, 1, 3, 3), 0, 3, 2, 5, 4, opacity, image.Rect(0, 0, 10, 8), image.Rectangle{}, 0)
		want := color.NRGBA{R: uint8(200 * opacity), B: uint8(200 * (1 - opacity)), A: 255}
		for y := range 8 {
			for x := range 10 {
				expected := background
				if x >= 2 && x < 8 && y >= 2 && y < 6 {
					expected = want
				}
				if got := r.Pixel(image.Pt(x, y)); got != expected {
					t.Fatalf("opacity=%v pixel(%d,%d)=%v, want %v", opacity, x, y, got, expected)
				}
			}
		}
	}
}
