package main

import (
	"image/color"
	"image/draw"
	"unsafe"
)

// 绘图层。
//
// 提供基础绘制工具。
type Canvas struct {
	buffer        []byte
	bytesPerPixel int

	// 渲染的偏移坐标。
	x, y int

	// buffer 的宽度和高度。
	width, height int
}

func NewCanvas(display *Display) *Canvas {
	return &Canvas{
		buffer:        display.Data,
		bytesPerPixel: 4,
		x:             0,
		y:             0,
		width:         display.Width,
		height:        display.Height,
	}
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
		// len := width * 4
		// copy(dst, src[0:len])
		for x := range width {
			// 参考：image/draw/draw.go
			// “Small cap improves performance”
			// 从每帧2.6ms降到1.7ms。
			s := src[x*4 : x*4+4]
			d := dst[x*4 : x*4+4]
			a := s[3]
			switch {
			case a == 255:
				// copy(d, s[:4])
				*(*uint32)(unsafe.Pointer(&d[0])) = *(*uint32)(unsafe.Pointer(&s[0]))
			case a != 0:
				i := 255 - a
				d[0] = uint8((int(s[0])*int(a) + int(d[0])*int(i)) / 255)
				d[1] = uint8((int(s[1])*int(a) + int(d[1])*int(i)) / 255)
				d[2] = uint8((int(s[2])*int(a) + int(d[2])*int(i)) / 255)
				d[3] = 255
			}
		}
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

func (c *Canvas) FillRect(x, y, width, height int, color Color) {
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
				p[0] = color.B() // TODO 确定被内联
				p[1] = color.G()
				p[2] = color.R()
				p[3] = color.A()
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

func (c *Canvas) DrawBorder(cr Color, w, h int, borderWidth int) {
	c.FillRect(0, 0, w, borderWidth, cr)
	c.FillRect(0, h-borderWidth, w, borderWidth, cr)
	c.FillRect(0, borderWidth, borderWidth, h-borderWidth*2, cr)
	c.FillRect(w-borderWidth, borderWidth, borderWidth, h-borderWidth*2, cr)
}

func (c *Canvas) DrawBackgroundColor(cr Color, w, h int) {
	c.FillRect(0, 0, w, h, cr)
}

func (c *Canvas) DrawBackgroundImage(path string, width, height int) {
	if path == `` {
		return
	}
	img, err := loadImageCached(path)
	if err != nil {
		return
	}
	c.DrawImage(img, width, height)
}
