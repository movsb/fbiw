package main

import (
	"fmt"
	"os"

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

type FontFace struct {
	font.Face
}

func (ff FontFace) MeasureString(text string) int {
	return font.MeasureString(ff, text).Ceil()
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

func (fm *FontManager) GetFace(
	family string,
	size int,
	bold bool,
	italic bool,
) (FontFace, error) {
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
