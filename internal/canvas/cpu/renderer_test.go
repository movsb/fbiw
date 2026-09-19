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
	if got, want := r.Pixels, []byte{128, 128, 128, 255}; !bytes.Equal(got, want) {
		t.Fatalf("pixel = %v, want %v", got, want)
	}
}
