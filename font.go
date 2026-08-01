package main

import (
	"fmt"
	"os"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

type FontManager struct {
	fonts map[_FontKey]*_FontValue
	faces map[_FontFaceKey]font.Face
}

func NewFontManager() *FontManager {
	return &FontManager{
		fonts: map[_FontKey]*_FontValue{},
		faces: map[_FontFaceKey]font.Face{},
	}
}

func (fm *FontManager) Close() {
	for _, f := range fm.fonts {
		f.File.Close()
	}
	clear(fm.fonts)
}

type FontFace struct {
	font.Face
}

// 测试文本 text 使用此字体时所占据的宽度。
func (ff FontFace) MeasureString(text string) int {
	return font.MeasureString(ff, text).Ceil()
}

func (ff FontFace) TextHeight() int {
	return (ff.Metrics().Ascent + ff.Metrics().Descent).Ceil()
}

// 把文本 text 按最大宽度切割成子串。
// 返回子串结束点索引（不含此位置），子串宽度。
func (ff FontFace) Segment(text string, maxWidth int) (int, int, error) {
	var width, index int
	for {
		if index == len(text) {
			return index, width, nil
		}
		char, size := utf8.DecodeRuneInString(text[index:])
		if char == utf8.RuneError {
			return 0, 0, fmt.Errorf(`无效字符`)
		}
		// NOTE 此处的 MeasureString 方法返回的不是精确整数值（ceil过），
		// 每次只算一个字符然后再在一起作为总宽度可能会导致误差越来越大。
		nextCharWidth := ff.MeasureString(text[index : index+size])
		if width+nextCharWidth > maxWidth {
			return index, width, nil
		}
		width += nextCharWidth
		index += size
	}
}

type _FontKey struct {
	Family string
	Bold   bool
	Italic bool
}
type _FontValue struct {
	Font *opentype.Font
	File *os.File
}

type FontKey = _FontKey

type _FontFaceKey struct {
	Family string
	Size   int
	Bold   bool
	Italic bool
}

// 添加字体族。
//
// 为了降低内存使用，不会完整读取字体文件，使用过程中按需读取。
// 所以字体文件在加载后会被一直引用。
//
// family 可以重复，只要其它样式不一样就行。
func (fm *FontManager) AddFont(path string, family string, bold, italic bool) error {
	key := _FontKey{
		Family: family,
		Bold:   bold,
		Italic: italic,
	}
	if _, ok := fm.fonts[key]; ok {
		return nil
	}

	fp, err := os.Open(path)
	if err != nil {
		return err
	}

	// 先完整读内存，如果占用高，可以考虑转 ParseReader，
	// 但是那样可以会每个字符读文件？不知道速度怎样。
	parsedFont, err := opentype.ParseReaderAt(fp)
	if err != nil {
		return err
	}

	fm.fonts[key] = &_FontValue{
		File: fp,
		Font: parsedFont,
	}

	return nil
}

// 尝试找字体，找不到返回系统字体。
// 如果系统字体也找不到，直接崩溃。
func (fm *FontManager) GetFaceWithFallback(family string, size int, bold bool, italic bool) FontFace {
	face, err := fm.GetFace(family, size, bold, italic)
	if err == nil {
		return face
	}

	if !(family == `system` && !bold && !italic) {
		system, err := fm.GetFace(`system`, size, bold, italic)
		if err == nil {
			return system
		}
		// 怎么连对应形状的系统字体也找不到？
		system, err = fm.GetFace(`system`, size, false, false)
		if err == nil {
			return system
		}
	}

	panic(`没有任何可用的系统字体，没救了。`)
}

func (fm *FontManager) GetFace(family string, size int, bold bool, italic bool) (FontFace, error) {
	out := FontFace{}

	faceKey := _FontFaceKey{
		Family: family,
		Size:   size,
		Bold:   bold,
		Italic: italic,
	}
	if face, ok := fm.faces[faceKey]; ok {
		out.Face = face
		return out, nil
	}

	fontKey := _FontKey{
		Family: family,
		Bold:   bold,
		Italic: italic,
	}
	fontValue, ok := fm.fonts[fontKey]
	if !ok {
		return out, fmt.Errorf(`字体家族未找到：%v`, fontKey)
	}

	theFace, err := opentype.NewFace(fontValue.Font, &opentype.FaceOptions{
		Size:    float64(size),
		DPI:     72, // 为72时1点=1像素
		Hinting: font.HintingFull,
	})
	if err != nil {
		return out, fmt.Errorf(`无法创建字体样式：%w`, err)
	}

	fm.faces[faceKey] = theFace
	out.Face = theFace

	return out, nil
}
