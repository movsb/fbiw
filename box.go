package main

import (
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"log"
	"os"
	"unicode/utf8"

	_ "image/jpeg"
	_ "image/png"

	"github.com/anthonynsimon/bild/transform"
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
	Color           Color
	Padding         int

	Width, Height int

	Children []Box
}

func (b *BaseBox) appendChild(box Box) {
	b.Children = append(b.Children, box)
}

func (b *BaseBox) ApplyAttributes(key string, val string) {
	switch key {
	default:
		panic(`不认识的属性`)
	case `border-width`:
		b.BorderWidth = mustParseInt(val)
	case `border-color`:
		b.BorderColor = Color(val)
	case `background-color`:
		b.BackgroundColor = Color(val)
	case `padding`:
		b.Padding = mustParseInt(val)
	case `width`:
		b.Width = mustParseInt(val)
	case `height`:
		b.Height = mustParseInt(val)
	}
}

type Block struct {
	BaseBox
}

func (b *Block) ApplyAttributes(key string, val string) {
	b.BaseBox.ApplyAttributes(key, val)
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

	if b.Width > 0 {
		cx = b.Width
	} else {
		cx = availableWidth
	}

	if b.Height > 0 {
		cy = b.Height
		return cx, cy
	}

	var ccx, ccy int
	for _, c := range b.Children {
		w, h := c.Calc(cx, availableHeight)
		ccx += w
		ccy += h
	}

	ccx += b.Padding*2 + b.BorderWidth*2
	ccy += b.Padding*2 + b.BorderWidth*2

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
		w, h := c.Calc(width-cx, height-cy)
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
}

func (b *Button) Calc(availableWidth, availableHeight int) (int, int) {
	if b.Width > 0 && b.Height > 0 {
		return b.Width, b.Height
	}

	cxAvail, cyAvail := availableWidth, availableHeight
	if b.Width > 0 {
		cxAvail = b.Width - b.BorderWidth*2 - b.Padding*2
	}

	var ccx, ccy int
	for _, c := range b.Children {
		w, h := c.Calc(cxAvail, cyAvail)
		switch c.(type) {
		case *Block:
			if w > ccx {
				ccx = w
			}
			ccy += h
			cyAvail -= h
		default:
			ccx += w
			if h > ccy {
				ccy = h
			}
			cxAvail -= w
		}
	}

	ccx += b.Padding*2 + b.BorderWidth*2
	ccy += b.Padding*2 + b.BorderWidth*2

	return ccx, ccy
}

func (b *Button) Draw(canvas *Canvas, width, height int) {
	drawBorder(canvas, b.BorderColor.NRGBA(), width, height, b.BorderWidth)
	drawBackground(
		canvas.Offset(b.BorderWidth, b.BorderWidth),
		b.BackgroundColor.NRGBA(),
		width-b.BorderWidth*2, height-b.BorderWidth*2,
	)

	cx := b.BorderWidth + b.Padding
	cy := b.BorderWidth + b.Padding

	for _, c := range b.Children {
		cc := canvas.Offset(cx, cy)
		w, h := c.Calc(width-cx, height-cy)
		c.Draw(cc, w, h)
		switch c.(type) {
		case *Block:
			cy += h
		default:
			cx += w
		}
	}
}

func (b *Button) ApplyAttributes(key string, val string) {
	switch key {
	case `color`:
		b.Color = Color(val)
	default:
		b.BaseBox.ApplyAttributes(key, val)
	}
}

// 只用于嵌入。
type Text struct {
	parent Box
	Data   string
}

func (t *Text) Calc(availWidth, availHeight int) (int, int) {
	face := onceLoadFont()
	metrics := face.Metrics()
	textHeight := (metrics.Ascent + metrics.Descent).Ceil()

	segment := func(s string) (int, string) {
		width := 0
		p := 0
		for {
			r, n := utf8.DecodeRuneInString(s[p:])
			if r == utf8.RuneError {
				return 0, ``
			}
			rw := measureString(face, s[p:p+n])
			if width+rw > availWidth {
				return width, s[p:]
			}
			if width+rw < availWidth {
				width += rw
				p += n
				if p == len(s) {
					return width, ``
				}
				continue
			}
			width += rw
			p += n
			return width, s[p:]
		}
	}

	s := t.Data

	maxWidth := 0
	lines := 0

	for {
		w, r := segment(s)
		if w == 0 {
			return 0, 0
		}
		maxWidth = max(maxWidth, w)
		s = r
		lines++
		if s == `` {
			break
		}
	}

	height := textHeight * lines //+ metrics.Height.Ceil()*(lines-1)

	return maxWidth, height
}

func (t *Text) Draw(canvas *Canvas, width, height int) {
	var cr Color
	switch p := t.parent.(type) {
	case *Button:
		cr = p.Color
	}
	drawString(canvas,
		t.Data, cr.NRGBA(),
		width, height,
	)
}

type Image struct {
	BaseBox

	Src string
}

func (b *Image) ApplyAttributes(key string, val string) {
	switch key {
	case `src`:
		b.Src = val
	default:
		b.BaseBox.ApplyAttributes(key, val)
	}
}

func (b *Image) Calc(availWidth, availHeight int) (int, int) {
	if b.Width > 0 && b.Height > 0 {
		return b.Width, b.Height
	}

	fp, err := os.Open(b.Src)
	if err != nil {
		log.Println(err, b.Src)
		return 0, 0
	}
	defer fp.Close()

	img, _, err := image.DecodeConfig(fp)
	if err != nil {
		log.Println(`图片解码错误`, err, b.Src)
	}

	imgWidth, imgHeight := img.Width, img.Height

	if imgWidth > availWidth || imgHeight > availHeight {
		scaleW := imgWidth / availWidth
		scaleH := imgHeight / availHeight
		bigger := max(scaleW, scaleH)
		return imgWidth / bigger, imgHeight / bigger
	}

	return imgWidth, imgHeight
}

func (b *Image) Draw(canvas *Canvas, width, height int) {
	fp, err := os.Open(b.Src)
	if err != nil {
		log.Println(err, b.Src)
		return
	}
	defer fp.Close()

	img, _, err := image.Decode(fp)
	if err != nil {
		log.Println(`图片解码错误`, err, b.Src)
	}

	imgWidth, imgHeight := img.Bounds().Dx(), img.Bounds().Dy()
	if imgWidth > width || imgHeight > height {
		scaleW := imgWidth / width
		scaleH := imgHeight / height
		bigger := max(scaleW, scaleH)
		imgWidth, imgHeight = imgWidth/bigger, imgHeight/bigger
	}
	resized := transform.Resize(img, imgWidth, imgHeight, transform.Lanczos)
	draw.Draw(canvas.ToDrawable(width, height), resized.Rect, resized, resized.Rect.Min, draw.Src)
}
