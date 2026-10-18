package canvas

import (
	"bytes"
	"golang.org/x/image/font/basicfont"
	"image"
	"reflect"
	"testing"
)

func TestOutlineMaskCoverageAndCache(t *testing.T) {
	got := dilateMask([]byte{80, 200}, 2, 1, 1)
	want := []byte{80, 200, 200, 200, 80, 200, 200, 200, 80, 200, 200, 200}
	if !bytes.Equal(got, want) {
		t.Fatalf("mask = %v, want %v", got, want)
	}
	face := NewFontFace("basic", basicfont.Face7x13)
	glyph := face.GlyphCached('A')
	outline := face.OutlineCached('A', 1)
	if outline.Width != glyph.Width+2 || outline.Height != glyph.Height+2 || outline.OffsetX != glyph.OffsetX-1 || outline.OffsetY != glyph.OffsetY-1 || outline.Advance != glyph.Advance {
		t.Fatal("outline changed layout or has incorrect bounds")
	}
	cached := face.OutlineCached('A', 1)
	if &cached.Masks[0] != &outline.Masks[0] {
		t.Fatal("outline mask was not cached")
	}
}

// 只覆写本测试使用的绘制接口。
type textMaskRecorder struct {
	Renderer
	colors []Color
	widths []int
}

func (r *textMaskRecorder) DrawMask(_ []byte, w, _ int, _ image.Point, _ image.Rectangle, color Color) {
	r.colors = append(r.colors, color)
	r.widths = append(r.widths, w)
}

func TestDrawTextEffects(t *testing.T) {
	face := NewFontFace("basic", basicfont.Face7x13)
	faces := []*FontFace{face}
	const foreground Color = 0xffffffff
	const outline Color = 0xa0000000
	draw := func(options TextOptions) *textMaskRecorder {
		r := &textMaskRecorder{}
		DrawText(r, "AB", faces, image.Point{}, image.Rect(0, 0, 100, 100), foreground, options)
		return r
	}
	plain := draw(TextOptions{})
	if !reflect.DeepEqual(plain.colors, []Color{foreground, foreground}) {
		t.Fatal("zero options should draw only foreground glyphs")
	}
	outlined := draw(TextOptions{OutlineWidth: 1, OutlineColor: outline})
	if !reflect.DeepEqual(outlined.colors, []Color{outline, outline, foreground, foreground}) {
		t.Fatalf("incorrect pass order: %v", outlined.colors)
	}
	for i := range 2 {
		if outlined.widths[i] != plain.widths[i]+2 || outlined.widths[i+2] != plain.widths[i] {
			t.Fatal("incorrect glyph dimensions")
		}
	}
}
