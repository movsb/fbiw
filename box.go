package main

import (
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"log"
	"os"
	"path/filepath"
	"unicode/utf8"

	_ "image/jpeg"
	_ "image/png"
)

type Box interface {
	Base() *BaseBox
	// 根据可用的宽度和高度计算自己实际的宽度和高度。
	// 自己写：并把宽度和高度写到 calcPos{width, height}。
	// 父亲写：{x, y}。
	Calc(availableWidth, availableHeight int)
	// 根据自身的 calcPos 直接画。
	// calcPos 的 {x,y} 相对于父元素。
	// 所以除根元素外（因为它是0，0），其它在Draw之前都要调整canvas到offset，
	// 即：父元素在遍历子元素Draw的时候记得根据子元素的{x,y}作offset。
	Draw(canvas *Canvas)
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
	ID string

	Dirty bool

	BorderWidth     int
	BorderColor     Color
	BackgroundColor Color
	BackgroundImage string
	Color           Color
	Padding         int

	Width, Height int

	Children []Box

	calcPos Rect
}

type Rect struct {
	X, Y          int
	Width, Height int
}

func (b *BaseBox) Base() *BaseBox {
	return b
}

func (b *BaseBox) SetNoDirty() {
	b.Dirty = false
}

func (b *BaseBox) IsDirty() bool {
	if b.Dirty {
		return true
	}
	for _, c := range b.Children {
		// 文本会设置给父对象。
		if _, ok := c.(*Text); ok {
			continue
		}
		if c.Base().IsDirty() {
			return true
		}
	}
	return false
}

func (b *BaseBox) appendChild(self, child Box) {
	b.Children = append(b.Children, child)
}

func (b *BaseBox) ApplyAttributes(key string, val string) {
	switch key {
	default:
		panic(`不认识的属性`)
	case `id`:
		b.ID = val
	case `border-width`:
		b.BorderWidth = mustParseInt(val)
	case `border-color`:
		b.BorderColor = Color(val)
	case `background-color`:
		b.BackgroundColor = Color(val)
	case `background-image`:
		b.BackgroundImage = val
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

func NewBlock() *Block {
	return &Block{}
}

func (b *Block) Calc(availWidth, availHeight int) {
	// 自身不可用区域。
	ncWidth := b.BorderWidth + b.Padding

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := iif(b.Width > 0, b.Width, availWidth)
	boxMaxHeight := iif(b.Height > 0, b.Height, availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - ncWidth*2
	contentAvailHeight := boxMaxHeight - ncWidth*2

	// 当前实际占用高度
	contentHeight := 0

	for _, child := range b.Children {
		child.Calc(contentAvailWidth, contentAvailHeight-contentHeight)
		child.Base().calcPos.X = ncWidth
		contentHeight += child.Base().calcPos.Height
	}

	// 如果有 Spacer（未设定大小的），则均匀地铺满。
	zeroSpacers := []*Spacer{}
	for _, child := range b.Children {
		if spacer, ok := child.(*Spacer); ok && spacer.Height == 0 {
			zeroSpacers = append(zeroSpacers, spacer)
		}
	}
	if len(zeroSpacers) > 0 {
		// 铺满，然后均匀分布
		avgHeight := (contentAvailHeight - contentHeight) / len(zeroSpacers)
		contentHeight = contentAvailHeight
		for _, spacer := range zeroSpacers {
			spacer.calcPos = Rect{ncWidth, 0, avgHeight, 0}
		}
	}

	// 最后再重新调整 Y
	offsetY := ncWidth
	for _, child := range b.Children {
		p := &child.Base().calcPos
		p.Y = offsetY
		offsetY += p.Height
	}

	b.calcPos.Width = boxMaxWidth
	b.calcPos.Height = contentHeight + ncWidth*2
}

func (b *BaseBox) Calc(availWidth, availHeight int) {
	b.calcPos = Rect{0, 0, b.Width, b.Height}
}

func (b *BaseBox) Draw(canvas *Canvas) {
	defer b.SetNoDirty()

	// 默认都是 border-box，所以以实际的宽和高为准。
	drawBorder(canvas, b.BorderColor.NRGBA(), b.calcPos.Width, b.calcPos.Height, b.BorderWidth)

	// 背景位于边框内，要减掉。
	if b.BackgroundImage != `` {
		drawBackgroundImage(
			canvas.Offset(b.BorderWidth, b.BorderWidth),
			b.BackgroundImage,
			b.calcPos.Width-b.BorderWidth*2,
			b.calcPos.Height-b.BorderWidth*2,
		)
	} else {
		drawBackgroundColor(
			canvas.Offset(b.BorderWidth, b.BorderWidth),
			b.BackgroundColor.NRGBA(),
			b.calcPos.Width-b.BorderWidth*2,
			b.calcPos.Height-b.BorderWidth*2,
		)
	}

	for _, child := range b.Children {
		p := child.Base().calcPos
		child.Draw(canvas.Offset(p.X, p.Y))
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
	_ = p[3]
	p[0] = color.B
	p[1] = color.G
	p[2] = color.R
	p[3] = color.A
}

func (c *Canvas) FillRect(x, y, width, height int, color color.NRGBA) {
	if width <= 0 || height <= 0 {
		return
	}

	x0 := c.x + x
	y0 := c.y + y
	x1 := x0 + width
	y1 := y0 + height

	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > c.width {
		x1 = c.width
	}
	if y1 > c.height {
		y1 = c.height
	}

	var line0 []byte
	for yy := y0; yy < y1; yy++ {
		offset := c.width*c.bytesPerPixel*yy + x0*c.bytesPerPixel
		if yy == y0 {
			line0 = c.buffer[offset : offset+(x1-x0)*c.bytesPerPixel]
			for i := 0; i < (x1-x0)*c.bytesPerPixel; i += c.bytesPerPixel {
				p := c.buffer[offset+i:]
				p[3] = color.A
				p[0] = color.B
				p[1] = color.G
				p[2] = color.R
			}
		} else {
			copy(c.buffer[offset:], line0)
		}
	}
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
	fc := &FontCanvas{
		underlying: c,
		width:      width,
		height:     height,
	}
	if c.x+width > c.width {
		fc.width = c.width - c.x
	}
	if c.y+height > c.height {
		fc.height = c.height - c.y
	}
	return fc
}

func drawBorder(c *Canvas, cr color.NRGBA, w, h int, borderWidth int) {
	if cr == EmptyColor {
		return
	}
	c.FillRect(0, 0, w, borderWidth, cr)
	c.FillRect(0, h-borderWidth, w, borderWidth, cr)
	c.FillRect(0, borderWidth, borderWidth, h-borderWidth*2, cr)
	c.FillRect(w-borderWidth, borderWidth, borderWidth, h-borderWidth*2, cr)
}

func drawBackgroundColor(c *Canvas, cr color.NRGBA, w, h int) {
	if cr == EmptyColor {
		return
	}
	c.FillRect(0, 0, w, h, cr)
}

func drawImage(c *Canvas, img image.Image, width, height int) {
	draw.Draw(
		c.ToDrawable(width, height),
		image.Rect(0, 0, width, height),
		img, image.Pt(0, 0), draw.Over,
	)
}

func drawBackgroundImage(c *Canvas, path string, width, height int) {
	img, err := loadImageCached(path)
	if err != nil {
		return
	}
	drawImage(c, img, width, height)
}

type Button struct {
	BaseBox
}

func NewButton() *Button {
	return &Button{}
}

func (b *Button) ApplyAttributes(key string, val string) {
	switch key {
	case `color`:
		b.Color = Color(val)
	default:
		b.BaseBox.ApplyAttributes(key, val)
	}
}

type Inline struct {
	BaseBox
}

func NewInline() *Inline {
	return &Inline{}
}

func (b *Inline) Calc(availWidth, availHeight int) {
	// 自身不可用区域。
	ncWidth := b.BorderWidth + b.Padding

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := iif(b.Width > 0, b.Width, availWidth)
	boxMaxHeight := iif(b.Height > 0, b.Height, availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - ncWidth*2
	contentAvailHeight := boxMaxHeight - ncWidth*2

	// 当前实际占用宽度
	contentWidth := 0

	// 实际最高占用。
	contentMaxHeight := 0

	for _, child := range b.Children {
		child.Calc(contentAvailHeight, contentAvailWidth-contentWidth)
		child.Base().calcPos.Y = ncWidth
		contentWidth += child.Base().calcPos.Width
		contentMaxHeight = max(contentMaxHeight, child.Base().calcPos.Height)
	}

	// 如果有 Spacer（未设定大小的），则均匀地铺满。
	zeroSpacers := []*Spacer{}
	for _, child := range b.Children {
		if spacer, ok := child.(*Spacer); ok && spacer.Width == 0 {
			zeroSpacers = append(zeroSpacers, spacer)
		}
	}
	if len(zeroSpacers) > 0 {
		// 铺满，然后均匀分布
		avgWidth := (contentAvailWidth - contentWidth) / len(zeroSpacers)
		contentWidth = contentAvailWidth
		for _, spacer := range zeroSpacers {
			spacer.calcPos = Rect{0, ncWidth, avgWidth, 0}
		}
	}

	// 最后再重新调整 X
	offsetX := ncWidth
	for _, child := range b.Children {
		p := &child.Base().calcPos
		p.X = offsetX
		offsetX += p.Width
	}

	b.calcPos.Width = contentWidth + ncWidth*2
	b.calcPos.Height = contentMaxHeight + ncWidth*2
}

func (b *Inline) ApplyAttributes(key string, val string) {
	switch key {
	case `color`:
		b.Color = Color(val)
	default:
		b.BaseBox.ApplyAttributes(key, val)
	}
}

// 用来代替 margin 的使用。
//
// <spacer>是旧html标签，如果使用会被标红。。。
type Spacer struct {
	BaseBox
}

func NewSpacer() *Spacer {
	return &Spacer{}
}

// 只用于嵌入。
type Text struct {
	BaseBox

	Color Color
	Data  string
}

func NewText() *Text {
	return &Text{}
}

func (b *Text) ApplyAttributes(key string, val string) {
	switch key {
	case `color`:
		b.Color = Color(val)
	default:
		b.BaseBox.ApplyAttributes(key, val)
	}
}

func (t *Text) Calc(availWidth, availHeight int) {
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
			return // 出错了
		}
		maxWidth = max(maxWidth, w)
		s = r
		lines++
		if s == `` {
			break
		}
	}

	t.calcPos.Width = maxWidth
	t.calcPos.Height = textHeight * lines
}

func (t *Text) Draw(canvas *Canvas) {
	t.Base().SetNoDirty()
	if t.Color.NRGBA() == EmptyColor {
		t.Color = Color(`black`)
	}
	drawString(canvas,
		t.Data, t.Color.NRGBA(),
		t.calcPos.Width, t.calcPos.Height,
	)
}

type Image struct {
	BaseBox

	Src string
}

func NewImage() *Image {
	return &Image{}
}

func (b *Image) ApplyAttributes(key string, val string) {
	switch key {
	case `src`:
		b.Src = val
	default:
		b.BaseBox.ApplyAttributes(key, val)
	}
}

var imgCache = map[string]image.Image{}

func loadImageCached(path string) (image.Image, error) {
	if img, ok := imgCache[path]; ok {
		return img, nil
	}

	log.Println(`重新解码：`, path)
	fp, err := os.Open(filepath.Join(skinDir, path))
	if err != nil {
		log.Println(err, path)
		return nil, err
	}
	defer fp.Close()
	img, _, err := image.Decode(fp)
	if err != nil {
		log.Println(`图片解码错误`, err, path)
		return nil, err
	}

	imgCache[path] = img

	return img, nil
}

func (b *Image) Calc(availWidth, availHeight int) {
	if b.Width > 0 && b.Height > 0 {
		b.calcPos.Width = b.Width
		b.calcPos.Height = b.Height
		return
	}

	img, err := loadImageCached(b.Src)
	if err != nil {
		b.calcPos = Rect{}
		return
	}

	imgWidth, imgHeight := img.Bounds().Dx(), img.Bounds().Dy()

	// if imgWidth > availWidth || imgHeight > availHeight {
	// 	scaleW := imgWidth / availWidth
	// 	scaleH := imgHeight / availHeight
	// 	bigger := max(scaleW, scaleH)
	// 	if bigger <= 0 {
	// 		return 0, 0
	// 	}
	// 	return imgWidth / bigger, imgHeight / bigger
	// }

	b.calcPos.Width = imgWidth
	b.calcPos.Height = imgHeight
}

func (b *Image) Draw(canvas *Canvas) {
	defer b.SetNoDirty()

	img, err := loadImageCached(b.Src)
	if err != nil {
		return
	}

	// imgWidth, imgHeight := img.Bounds().Dx(), img.Bounds().Dy()
	// if imgWidth > width || imgHeight > height {
	// 	scaleW := imgWidth / width
	// 	scaleH := imgHeight / height
	// 	bigger := max(scaleW, scaleH)
	// 	imgWidth, imgHeight = imgWidth/bigger, imgHeight/bigger
	// }
	// // resized := transform.Resize(img, imgWidth, imgHeight, transform.Lanczos)
	resized := img
	drawImage(canvas, resized, b.calcPos.Width, b.calcPos.Height)
}
