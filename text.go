package main

import (
	"image"
	"unicode/utf8"

	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type FontCanvas struct {
	underlying    *Canvas
	width, height int
}

func (c FontCanvas) Bounds() image.Rectangle {
	return image.Rect(0, 0, c.width, c.height)
}

func (c FontCanvas) ColorModel() color.Model {
	return color.NRGBAModel
}

func (c FontCanvas) At(x, y int) color.Color {
	return c.underlying.getPixel(x, y)
}

func (c FontCanvas) Set(x, y int, clr color.Color) {
	cc := c.ColorModel().Convert(clr).(color.NRGBA)
	c.underlying.SetPixel(x, y, cc)
}

func drawString(canvas *Canvas, text string, face FontFace, color Color, width, height int) {
	metrics := face.Metrics()
	accent := metrics.Ascent.Ceil()
	decent := metrics.Descent.Ceil()
	textHeight := accent + decent

	segment := func(s string) (int, int) {
		w := 0
		p := 0
		for {
			r, n := utf8.DecodeRuneInString(s[p:])
			if r == utf8.RuneError {
				return 0, 0
			}
			rw := face.MeasureString(s[p : p+n])
			if w+rw > width {
				return width, p
			}
			if w+rw < width {
				w += rw
				p += n
				if p == len(s) {
					return w, p
				}
				continue
			}
			w += rw
			p += n
			return w, p
		}
	}

	lines := []string{}
	maxWidth := 0

	for text != `` {
		w, p := segment(text)
		if w == 0 {
			return
		}
		maxWidth = max(maxWidth, w)
		lines = append(lines, text[:p])
		text = text[p:]
	}

	allHeight := textHeight * len(lines) //+ (len(lines)-1)*metrics.Height.Ceil()
	y := accent + (height-allHeight)/2
	x := (width - maxWidth) / 2

	for _, line := range lines {
		drawer := font.Drawer{
			Dst:  canvas.ToDrawable(width, height),
			Src:  image.NewUniform(color.NRGBA()),
			Face: face,
			Dot: fixed.Point26_6{
				X: 0,
				Y: 0,
			},
		}

		drawer.Dot = fixed.P(x, y)
		drawer.DrawString(line)

		y += textHeight //+ metrics.Height.Ceil()
	}
}
