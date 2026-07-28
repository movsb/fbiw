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

type DisplayStyle string

const (
	DisplayBlock  DisplayStyle = `block`
	DisplayInline DisplayStyle = `inline`
)

type Box interface {
	Base() *BaseBox
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
	Display DisplayStyle

	BorderWidth     int
	BorderColor     Color
	BackgroundColor Color
	Color           Color
	Padding         int

	Width, Height int

	Children []Box
}

func (b *BaseBox) Base() *BaseBox {
	return b
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

func (b *BaseBox) Calc(availWidth, availHeight int) (int, int) {
	// 如果指定了大小，则直接使用。
	if b.Width > 0 && b.Height > 0 {
		return b.Width, b.Height
	}

	// 根据自身大小及可用空间大小取最佳值。
	boxWidth := iif(b.Width > 0, b.Width, availWidth)
	boxHeight := iif(b.Height > 0, b.Height, availHeight)

	// 自身不可用区域。
	ncWidth := b.BorderWidth*2 + b.Padding*2

	// 内容区域可用的大小。
	contentAvailWidth := boxWidth - ncWidth
	contentAvailHeight := boxHeight - ncWidth

	// 每次横向或纵向绘制都会调整可用空间。
	// x, y := 0, 0
	// 调整后的剩余空间
	xRemain, yRemain := contentAvailWidth, contentAvailHeight
	// 最高、最宽占用空间。
	// xh := 0
	// 所有行中占用宽是多少。
	maxWidth := 0
	// 当前行最高是多少。
	maxLineHeight := 0

	// 计算子元素占用。
	for _, child := range b.Children {
		base := child.Base()
		// 如果是块级元素，需要独占一行。
		if base.Display == DisplayBlock {
			xRemain = contentAvailWidth
			yRemain -= maxLineHeight
			maxLineHeight = 0
		}
		cw, ch := child.Calc(xRemain, yRemain)
		xRemain -= cw
		maxLineHeight = max(maxLineHeight, ch)
		maxWidth = max(maxWidth, contentAvailWidth-xRemain)
	}

	// 最后一个元素布置完后需要调整剩余高度
	yRemain -= maxLineHeight

	contentHeight := contentAvailHeight - yRemain + ncWidth
	contentWidth := maxWidth + ncWidth

	return contentWidth, contentHeight
}

func (b *BaseBox) Draw(canvas *Canvas, actualWidth, actualHeight int) {
	// 默认都是 border-box，所以以实际的宽和高为准。
	drawBorder(canvas,
		b.BorderColor.NRGBA(),
		actualWidth, actualHeight, b.BorderWidth,
	)

	// 背景位于边框内，要减掉。
	drawBackground(
		canvas.Offset(b.BorderWidth, b.BorderWidth),
		b.BackgroundColor.NRGBA(),
		actualWidth-b.BorderWidth*2,
		actualHeight-b.BorderWidth*2,
	)

	// 内容起点均在边框和内边距以内。
	initialOffsetX := 0 + b.BorderWidth + b.Padding
	initialOffsetY := 0 + b.BorderWidth + b.Padding

	canvas = canvas.Offset(initialOffsetX, initialOffsetY)
	contentWidth := actualWidth - initialOffsetX*2
	contentHeight := actualHeight - initialOffsetY*2

	// 横向剩余量。
	xRemain := contentWidth
	yRemain := contentHeight
	maxLineHeight := 0

	for _, child := range b.Children {
		base := child.Base()
		if base.Display == DisplayBlock {
			xRemain = contentWidth
			yRemain -= maxLineHeight
			maxLineHeight = 0
		}

		canvas := canvas.Offset(contentWidth-xRemain, contentHeight-yRemain)
		cw, ch := child.Calc(xRemain, yRemain)
		child.Draw(canvas, cw, ch)

		xRemain -= cw
		maxLineHeight = max(maxLineHeight, ch)
	}
}

type Block struct {
	BaseBox
}

func NewBlock() *Block {
	return &Block{
		BaseBox: BaseBox{
			Display: DisplayBlock,
		},
	}
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

func NewButton() *Button {
	return &Button{
		BaseBox: BaseBox{
			Display: DisplayInline,
		},
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

func NewText() *Text {
	return &Text{}
}

func (t *Text) Base() *BaseBox {
	return t.parent.Base()
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

func NewImage() *Image {
	return &Image{
		BaseBox: BaseBox{
			Display: DisplayInline,
		},
	}
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
