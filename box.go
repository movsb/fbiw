package main

import (
	_ "embed"
	"image/color"
	"image/draw"
)

type DisplayStyle byte

type Box interface {
	Calc(availableWidth, availableHeight int) (int, int)
	Draw(canvas *Canvas, width, height int)
}

type Color string

var EmptyColor = color.NRGBA{0, 1, 0, 1}

func (c Color) NRGBA() (out color.NRGBA) {
	if len(c) == 0 {
		// 特殊值。
		return EmptyColor
	}
	if c[0] == '#' {
		index := -1
		decode := func() uint8 {
			index++
			switch b := c[1+index]; {
			case '0' <= b && b <= '9':
				return b - '0'
			case 'a' <= b && b <= 'f':
				return b - 'a'
			case 'A' <= b && b <= 'F':
				return b - 'A'
			default:
				// 无效值忽略。
				return 0
			}
		}
		h := c[1:]
		switch len(h) {
		case 3:
			r, g, b := decode(), decode(), decode()
			r |= r << 4
			g |= g << 4
			b |= b << 4
			out = color.NRGBA{r, g, b, 0xFF}
		case 4:
			r, g, b, a := decode(), decode(), decode(), decode()
			r |= r << 4
			g |= g << 4
			b |= b << 4
			a |= a << 4
			out = color.NRGBA{r, g, b, a}
		case 6:
			r := decode()<<4 | decode()
			g := decode()<<4 | decode()
			b := decode()<<4 | decode()
			out = color.NRGBA{r, g, b, 0xFF}
		case 8:
			r := decode()<<4 | decode()
			g := decode()<<4 | decode()
			b := decode()<<4 | decode()
			a := decode()<<4 | decode()
			out = color.NRGBA{r, g, b, a}
		}
		return
	} else {
		if c, ok := presetColors[string(c)]; ok {
			out.R = uint8(c >> 24)
			out.G = uint8(c >> 16)
			out.B = uint8(c >> 8)
			out.A = uint8(c)
		}
		return
	}
}

var presetColors = map[string]uint32{
	`coral`:          0xF08080FF,
	`salmon`:         0xE9967AFF,
	`red`:            0xFF325BFF,
	`hotpink`:        0xFF69B4FF,
	`deeppink`:       0xFF1493FF,
	`palevioletred`:  0xDB7093FF,
	`tomato`:         0xFF6347FF,
	`darkorange`:     0xFF8C00FF,
	`orange`:         0xFFA500FF,
	`yellow`:         0xFFD800FF,
	`darkkhaki`:      0xBDB76BFF,
	`magenta`:        0xDA70D6FF,
	`purple`:         0x9932CCFF,
	`slateblue`:      0x6A5ACDFF,
	`mediumseagreen`: 0x3CB371FF,
	`green`:          0x17A817FF,
	`yellowgreen`:    0x9ACD32FF,
	`olive`:          0x6B8E23FF,
	`darkseagreen`:   0x8FBC8BFF,
	`lightseagreen`:  0x20B2AAFF,
	`teal`:           0x008080FF,
	`cyan`:           0x00CED1FF,
	`aqua`:           0x00CED1FF,
	`cadetblue`:      0x5F9EA0FF,
	`steelblue`:      0x4682B4FF,
	`deepskyblue`:    0x00BFFFFF,
	`blue`:           0x1E90FFFF,
	`burlywood`:      0xDEB887FF,
	`tan`:            0xD2B48CFF,
	`rosybrown`:      0xBC8F8FFF,
	`sandybrown`:     0xF4A460FF,
	`goldenrod`:      0xDAA520FF,
	`darkgoldenrod`:  0xB8860BFF,
	`peru`:           0xCD853FFF,
	`chocolate`:      0xD2691EFF,
	`white`:          0xFFFFFFFF,
	`silver`:         0xC0C0C0FF,
	`darkgray`:       0xA9A9A9FF,
	`gray`:           0x808080FF,
	`slategray`:      0x708090FF,
	`black`:          0x000000FF,
}

type BaseBox struct {
	BorderWidth     int
	BorderColor     Color
	BackgroundColor Color

	Padding int

	Width, Height int

	Children []Box
}

type Block struct {
	BaseBox
}

type Canvas struct {
	buffer        []byte
	bytesPerPixel int

	// 渲染的偏移坐标。
	x, y int

	// buffer 的宽度和高度。
	width, height int
}

func (c *Canvas) Offset(x, y int) *Canvas {
	if x == 0 && y == 0 {
		return c
	}
	return &Canvas{
		buffer:        c.buffer,
		bytesPerPixel: c.bytesPerPixel,
		x:             c.x + x,
		y:             c.y + y,
		width:         c.width,
		height:        c.height,
	}
}

func (c *Canvas) getPixel(x, y int) color.NRGBA {
	xx, yy := c.x+x, c.y+y
	offset := c.width*c.bytesPerPixel*yy + xx*c.bytesPerPixel

	p := c.buffer[offset:]
	return color.NRGBA{p[2], p[1], p[0], p[3]}
}

func (c *Canvas) SetPixel(x, y int, color color.NRGBA) {
	if yy := c.y + y; yy < 0 || yy >= c.height {
		return
	}
	if xx := c.x + x; xx < 0 || xx >= c.width {
		return
	}

	xx, yy := c.x+x, c.y+y
	offset := c.width*c.bytesPerPixel*yy + xx*c.bytesPerPixel

	p := c.buffer[offset:]
	p[0] = color.B
	p[1] = color.G
	p[2] = color.R
	p[3] = color.A
}

/*
// 批量写，自动折行。
func (c *Canvas) Writer(x, y int) io.Writer {
	return _BatchWriter{c: c, x: x, y: y}
}

type _BatchWriter struct {
	c    *Canvas
	x, y int

	offsetX, offsetY int
}

func (w _BatchWriter) Write(p []byte) (int, error) {
	if len(p)&3 > 0 {
		panic(`应该为4字节颜色数据`)
	}
}
*/

func (c *Canvas) ToDrawable(width, height int) draw.Image {
	return &FontCanvas{
		underlying: c,
		width:      width,
		height:     height,
	}
}

// 要先画背景才能画子元素，所以必须先知道大小。
func (b *Block) Calc(
	availableWidth, availableHeight int,
) (int, int) {
	cx, cy := 0, 0

	if b.Width >= 0 {
		cx = b.Width
	} else {
		cx = availableWidth
	}

	if b.Height >= 0 {
		cy = b.Height
		return cx, cy
	}

	var ccx, ccy int
	for _, c := range b.Children {
		w, h := c.Calc(cx, availableHeight)
		ccx += w
		ccy += h
	}

	ccx += b.Padding * 2
	ccy += b.Padding * 2

	return ccx, ccy
}

func (b *Block) Draw(canvas *Canvas, width, height int) {
	drawBorder(canvas, b.BorderColor.NRGBA(), width, height, b.BorderWidth)
	drawBackground(
		canvas.Offset(b.BorderWidth, b.BorderWidth),
		b.BackgroundColor.NRGBA(),
		width-b.BorderWidth, height-b.BorderWidth,
	)

	cx := b.BorderWidth + b.Padding
	cy := b.BorderWidth + b.Padding

	for _, c := range b.Children {
		cc := canvas.Offset(cx, cy)
		w, h := c.Calc(width, height-cy)
		c.Draw(cc, w, h)
		cy += h
	}
}

func drawBorder(c *Canvas, cr color.NRGBA, w, h int, borderWidth int) {
	if borderWidth <= 0 {
		return
	}
	for b := range borderWidth {
		for x := range w {
			c.SetPixel(x, 0+b, cr)
			c.SetPixel(x, h-1-b, cr)
		}
		for y := range h {
			c.SetPixel(0+b, y, cr)
			c.SetPixel(w-1-b, y, cr)
		}
	}
}

func drawBackground(c *Canvas, cr color.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c.SetPixel(x, y, cr)
		}
	}
}

//go:embed logo.png
var logo []byte

// func (b *Block) drawText(c *Canvas, w, h int) {
// 	drawString(c, `测试文字 A 1334 ，。，！*（#¥&`)

// 	// img, _ := png.Decode(bytes.NewReader(logo))
// 	// draw.Draw(c.ToDrawable(), image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()), img, image.Pt(0, 0), draw.Src)
// }

type Button struct {
	BaseBox

	Text  string
	Color Color
}

func (b *Button) Calc(availableWidth, availableHeight int) (int, int) {
	face := onceLoadFont()

	var width, height int

	if b.Width > 0 {
		width = b.Width
	} else {
		width = measureString(onceLoadFont(), b.Text)
		width += b.BorderWidth * 2
		width += b.Padding * 2
	}
	if b.Height > 0 {
		height = b.Height
	} else {
		height = (face.Metrics().Ascent + face.Metrics().Descent).Ceil()
		height += b.BorderWidth * 2
		height += b.Padding * 2
	}

	return width, height
}

func (b *Button) Draw(canvas *Canvas, width, height int) {
	drawBorder(canvas, b.BorderColor.NRGBA(), width, height, b.BorderWidth)
	drawBackground(
		canvas.Offset(b.BorderWidth, b.BorderWidth),
		b.BackgroundColor.NRGBA(),
		width-b.BorderWidth*2, height-b.BorderWidth*2,
	)
	drawString(
		canvas.Offset(
			b.BorderWidth+b.Padding,
			b.BorderWidth+b.Padding,
		),
		b.Text, b.Color.NRGBA(),
		width-b.BorderWidth*2-b.Padding*2,
		height-b.BorderWidth*2-b.Padding*2,
	)
}
