package canvas

import (
	"fmt"
	"image"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type FontFace struct {
	font.Face

	name string

	// 文字渲染过程的光栅化非常消耗，所以缓存一下。
	cache map[rune]GlyphValue
}

func NewFontFace(name string, face font.Face) *FontFace {
	return &FontFace{Face: face, name: name, cache: map[rune]GlyphValue{}}
}

// 测试文本 text 使用此字体时所占据的宽度。
func (ff FontFace) MeasureString(text string) fixed.Int26_6 {
	return font.MeasureString(ff, text)
}

func (ff FontFace) TextHeight() int {
	return (ff.Metrics().Ascent + ff.Metrics().Descent).Ceil()
}

// golang.org/x/image/font/opentype/opentype.go
/*
	nPixels := width * height
	if cap(f.mask.Pix) < nPixels {
		f.mask.Pix = make([]uint8, 2*nPixels)
	}
	f.mask.Pix = f.mask.Pix[:nPixels]
	f.mask.Stride = width
	f.mask.Rect.Min.X = 0
	f.mask.Rect.Min.Y = 0
	f.mask.Rect.Max.X = width
	f.mask.Rect.Max.Y = height
*/
type GlyphValue struct {
	Masks []byte

	Width  uint16
	Height uint16

	OffsetX int16
	OffsetY int16

	Advance fixed.Int26_6
}

func (ff *FontFace) GlyphCached(r rune) GlyphValue {
	if mask, ok := ff.cache[r]; ok {
		return mask
	}

	dot := fixed.Point26_6{X: 0, Y: ff.Metrics().Ascent}
	rect, mask, _, advance, _ := ff.Glyph(dot, r)
	alpha := mask.(*image.Alpha)

	value := GlyphValue{
		Width:   uint16(rect.Dx()),
		Height:  uint16(rect.Dy()),
		OffsetX: int16(rect.Min.X - dot.X.Round()),
		OffsetY: int16(rect.Min.Y - dot.Y.Round()),
		Advance: advance,
	}

	value.Masks = make([]byte, int(value.Width)*int(value.Height))
	for y := 0; y < rect.Dy(); y++ {
		copy(
			value.Masks[y*rect.Dx():(y+1)*rect.Dx()],
			alpha.Pix[y*alpha.Stride:y*alpha.Stride+rect.Dx()],
		)
	}

	if ff.cache == nil {
		ff.cache = map[rune]GlyphValue{}
	}
	ff.cache[r] = value
	return value
}

func (ff FontFace) HasGlyph(r rune) bool {
	_, ok := ff.GlyphAdvance(r)
	return ok
}

// DrawText 完成字体 fallback、kerning 和字符推进，并把规范化后的字形
// Alpha mask 提交给 renderer。
func DrawText(renderer Renderer, text string, faces []*FontFace, origin image.Point, clip image.Rectangle, color Color) {
	prev := rune(-1)
	dot := fixed.Point26_6{X: 0, Y: faces[0].Metrics().Ascent}

	for _, next := range text {
		// 字偶距始终按主字体计算，以保持与原来的排版行为一致。
		if prev >= 0 {
			dot.X += faces[0].Kern(prev, next)
		}

		// 按字体列表顺序查找第一个包含当前字符的字体。全部不包含时仍用
		// 主字体取得缺字方框及其 Advance，保证缺字也会正常推进光标。
		// 先确定字体再读取缓存，可以省掉原实现对主字体的一次多余缓存查询。
		var face *FontFace
		for _, candidate := range faces {
			if candidate.HasGlyph(next) {
				face = candidate
				break
			}
		}
		if face == nil {
			face = faces[0]
		}
		glyph := face.GlyphCached(next)

		if glyph.Width == 0 || glyph.Height == 0 {
			dot.X += glyph.Advance
			prev = next
			continue
		}

		dst := image.Pt(
			origin.X+dot.X.Round()+int(glyph.OffsetX),
			origin.Y+dot.Y.Round()+int(glyph.OffsetY),
		)
		renderer.DrawMask(glyph.Masks, int(glyph.Width), int(glyph.Height), dst, clip, color)

		dot.X += glyph.Advance
		prev = next
	}
}

// 把文本 text 按最大宽度切割成子串。
// 返回子串结束点索引（不含此位置），子串宽度。
//
// 注意：这个方法并不在某单一 FontFace 上，原因是字体需要 fallback（回退）。
// 如果一种字体提供不了某一个glyph，则需要用后续字体继续搜索。
func SegmentText(text string, maxWidth int, faces []*FontFace) (int, int, error) {
	var width fixed.Int26_6
	var index int
	for {
		if index == len(text) {
			return index, width.Ceil(), nil
		}
		char, size := utf8.DecodeRuneInString(text[index:])
		if char == utf8.RuneError {
			return 0, 0, fmt.Errorf(`无效字符`)
		}
		// 找哪个字体库提供了此glyph。
		face := faces[0]
		for _, f := range faces {
			if f.HasGlyph(char) {
				face = f
				break
			}
		}
		// NOTE 此处的 MeasureString 方法返回的不是精确整数值（ceil过），
		// 每次只算一个字符然后再在一起作为总宽度可能会导致误差越来越大。
		// TODO 换成 GlyphAdvance
		nextCharWidth := face.MeasureString(text[index : index+size])
		if width+nextCharWidth > fixed.I(maxWidth) {
			return index, width.Ceil(), nil
		}
		width += nextCharWidth
		index += size
	}
}
