package main

import (
	_ "embed"
	"fmt"
	"gofb/style"
	"gofb/utils"
	"image"
	"image/color"
	"image/draw"
	"log"
	"os"
	"path/filepath"
	"strings"
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
	ID    string
	Tag   string
	Class style.Class

	Dirty bool

	BorderWidth     int
	BorderColor     Color
	BackgroundImage string
	Padding         int

	Width, Height int
	// 如果Width是用百分数表示的，会把整数部分记录到这里。
	// Calc时，会预填充Width。
	widthPercentage int

	// 子元素的水平/垂直对齐方式。
	// 默认("")居左，“center”居中。
	// 默认("")居顶，“middle”居中。
	Align string

	Parent   Box
	Children []Box

	calcPos Rect

	inlineStyles   style.Styles
	computedStyles style.Styles
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
	child.Base().Parent = b
}

func (b *BaseBox) ApplyAttributes(key string, val string) {
	switch key {
	default:
		panic(`不认识的属性：` + key)
	case `id`:
		b.ID = val
	case `class`:
		b.Class.Set(val)
	case `border-width`:
		b.BorderWidth = utils.MustParseInt(val)
	case `border-color`:
		b.BorderColor = Color(val)
	case `color`:
		b.inlineStyles.Color = style.StringValue(val)
	case `background-image`:
		b.BackgroundImage = val
	case `padding`:
		b.Padding = utils.MustParseInt(val)
	case `width`:
		if before, ok := strings.CutSuffix(val, `%`); ok {
			b.widthPercentage = utils.MustParseInt(before)
		} else {
			b.Width = utils.MustParseInt(val)
		}
	case `height`:
		b.Height = utils.MustParseInt(val)
	case `align`:
		b.Align = val
	}
}

// 如果宽度指定了百分比，其百分比是相对于父元素的，不能等到其它元素占用（并减去）后再计算。
func (b *BaseBox) presetWidth(parentTotalAvailWidth int) {
	if b.widthPercentage > 0 {
		// 百分比暂时优先级更高，所以如果窗口大小变了。b.Width会怎样？
		// 因为百分比才是真实的初始化，Width原本是没有的。
		b.Width = int(float32(b.widthPercentage) / 100 * float32(parentTotalAvailWidth))
	}
}

type Block struct {
	BaseBox
}

func NewBlock() *Block {
	return &Block{
		BaseBox: BaseBox{
			Tag: `block`,
		},
	}
}

func (b *Block) Calc(availWidth, availHeight int) {
	// 自身不可用区域。
	ncWidth := b.BorderWidth + b.Padding

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := utils.Iif(b.Width > 0, b.Width, availWidth)
	boxMaxHeight := utils.Iif(b.Height > 0, b.Height, availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - ncWidth*2
	contentAvailHeight := boxMaxHeight - ncWidth*2

	// 当前实际占用高度
	contentHeight := 0

	for _, child := range b.Children {
		child.Base().presetWidth(contentAvailWidth)
		child.Calc(contentAvailWidth, contentAvailHeight-contentHeight)
		child.Base().calcPos.X = ncWidth
		contentHeight += child.Base().calcPos.Height

		// 启用对齐。
		// 纵向排列的时候需要对每个元素进行设置。
		if b.Align == `center` {
			child.Base().calcPos.X += (contentAvailWidth - child.Base().calcPos.Width) / 2
		}
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
			spacer.calcPos = Rect{ncWidth, 0, 0, avgHeight}
		}
	}

	// 最后再重新调整 Y
	offsetY := ncWidth
	for _, child := range b.Children {
		p := &child.Base().calcPos
		p.Y = offsetY
		offsetY += p.Height
	}

	b.calcPos.Width = utils.Iif(b.Width > 0, b.Width, boxMaxWidth)
	b.calcPos.Height = utils.Iif(b.Height > 0, b.Height, contentHeight+ncWidth*2)
}

func (b *BaseBox) Calc(availWidth, availHeight int) {
	b.calcPos = Rect{
		0, 0,
		b.Width,
		b.Height,
	}
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
			Color(b.computedStyles.BackgroundColor.String).NRGBA(),
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

func (c *Canvas) DrawImage(img *DecodedImage, width, height int) {
	// 右下角限制在屏幕内。
	if c.x+width > c.width {
		width = c.width - c.x
	}
	if c.y+height > c.height {
		height = c.height - c.y
	}
	// 也不要超出图片外。
	// 后期缩放图片的时候需要考虑。
	width = min(width, img.Width)
	height = min(height, img.Height)

	for y := range height {
		offset := (c.y + y) * c.width * c.bytesPerPixel
		offset += c.x * c.bytesPerPixel
		dst := c.buffer[offset:]
		src := img.Pixels[y*img.Width*4:]
		copy(dst, src[0:width*4])
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
				_ = p[3]
				p[0] = color.B
				p[1] = color.G
				p[2] = color.R
				p[3] = color.A
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
	fc := FontCanvas{
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

func drawBackgroundImage(c *Canvas, path string, width, height int) {
	if path == `` {
		return
	}
	img, err := loadImageCached(path)
	if err != nil {
		return
	}
	c.DrawImage(img, width, height)
}

type Button struct {
	BaseBox
}

func NewButton() *Button {
	return &Button{
		BaseBox: BaseBox{
			Tag: `button`,
		},
	}
}

func (b *Button) ApplyAttributes(key string, val string) {
	switch key {
	default:
		b.BaseBox.ApplyAttributes(key, val)
	}
}

type Inline struct {
	BaseBox
}

func NewInline() *Inline {
	return &Inline{
		BaseBox: BaseBox{
			Tag: `inline`,
		},
	}
}

func (b *Inline) Calc(availWidth, availHeight int) {
	// 自身不可用区域。
	ncWidth := b.BorderWidth + b.Padding

	// 根据自身大小及可用空间大小取最佳值。
	boxMaxWidth := utils.Iif(b.Width > 0, b.Width, availWidth)
	boxMaxHeight := utils.Iif(b.Height > 0, b.Height, availHeight)

	// 内容区域可用的大小。
	contentAvailWidth := boxMaxWidth - ncWidth*2
	contentAvailHeight := boxMaxHeight - ncWidth*2

	// 当前实际占用宽度
	contentWidth := 0

	// 实际最高占用。
	contentMaxHeight := 0

	for _, child := range b.Children {
		child.Base().presetWidth(contentAvailWidth)
		child.Calc(contentAvailWidth-contentWidth, contentAvailHeight)
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

	b.calcPos.Width = utils.Iif(b.Width > 0, b.Width, contentWidth+ncWidth*2)
	b.calcPos.Height = utils.Iif(b.Height > 0, b.Height, contentMaxHeight+ncWidth*2)

	// 如果是垂直居中，则重新调整Y
	if b.Align == `middle` {
		contentHeight := utils.Iif(b.Height > 0, b.Height-ncWidth*2, contentMaxHeight)
		for _, child := range b.Children {
			child.Base().calcPos.Y += (contentHeight - child.Base().calcPos.Height) / 2
		}
	}
}

func (b *Inline) ApplyAttributes(key string, val string) {
	switch key {
	default:
		b.BaseBox.ApplyAttributes(key, val)
	}
}

// 用来代替 margin 的使用。
//
// <spacer>是旧html标签，如果使用会被标红，且 html.Parse 错乱。
type Spacer struct {
	BaseBox
}

func NewSpacer() *Spacer {
	return &Spacer{
		BaseBox: BaseBox{
			Tag: `space`,
		},
	}
}

// 只用于嵌入。
type Text struct {
	BaseBox
	Data string
}

func NewText() *Text {
	return &Text{
		BaseBox: BaseBox{
			Tag: `text`,
		},
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
	drawString(canvas,
		t.Data, Color(t.computedStyles.Color.String).NRGBA(),
		t.calcPos.Width, t.calcPos.Height,
	)
}

type Image struct {
	BaseBox

	Src string
}

func NewImage() *Image {
	return &Image{
		BaseBox: BaseBox{
			Tag: `image`,
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

var imgCache = map[string]*DecodedImage{}

// 用标准库的 draw.Draw 造成了极多不必要的计算，
// 而目标屏幕的内存格式是确定的（B、G、R、A），都不是 [image.RGBA] 或
// [image.NRGBA] 的格式（它们是 R、G、B、A），每次渲染的时候都转换实在没有意义。
// 所以这里直接在内存中保存目标格式，加快渲染效率。
type DecodedImage struct {
	Pixels        []byte // 内存格式：B G R A，长度：width*height*4
	Width, Height int
}

func loadImageCached(path string) (*DecodedImage, error) {
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

	decoded := &DecodedImage{
		Width:  img.Bounds().Dx(),
		Height: img.Bounds().Dy(),
	}
	decoded.Pixels = make([]byte, decoded.Width*decoded.Height*4)

	var pixels []byte
	var stride int

	switch m := img.(type) {
	case *image.RGBA:
		pixels = m.Pix
		stride = m.Stride
	case *image.NRGBA:
		pixels = m.Pix
		stride = m.Stride
	default:
		log.Printf(`暂不支持的图片解码格式：%T`, img)
		return nil, fmt.Errorf(`不支持的图片格式`)
	}

	for y := range decoded.Height {
		p := pixels[y*stride:]
		for x := range decoded.Width {
			d := decoded.Pixels[y*decoded.Width*4+x*4:]
			d[0] = p[2+x*4]
			d[1] = p[1+x*4]
			d[2] = p[0+x*4]
			d[3] = p[3+x*4]
		}
	}

	imgCache[path] = decoded

	return decoded, nil
}

func (b *Image) Calc(availWidth, availHeight int) {
	if b.Width > 0 && b.Height > 0 {
		b.calcPos.Width = b.Width
		b.calcPos.Height = b.Height
		return
	}

	if b.Src == `` {
		b.calcPos = Rect{}
		return
	}

	img, err := loadImageCached(b.Src)
	if err != nil {
		b.calcPos = Rect{}
		return
	}

	b.calcPos.Width = img.Width
	b.calcPos.Height = img.Height
}

func (b *Image) Draw(canvas *Canvas) {
	defer b.SetNoDirty()

	if b.Src == `` {
		return
	}

	img, err := loadImageCached(b.Src)
	if err != nil {
		return
	}

	canvas.DrawImage(img, b.calcPos.Width, b.calcPos.Height)
}
