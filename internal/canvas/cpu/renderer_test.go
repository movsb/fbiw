package cpu

import (
	"bytes"
	"image"
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
	r.DrawImage(img, image.Rect(1, 0, 2, 1), 0, 2, 2, 3, 1.5, image.Rect(0, 0, 6, 3))

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
