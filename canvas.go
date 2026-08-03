package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"io/fs"
	"log"
	"time"
	"unsafe"

	"github.com/phuslu/lru"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
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

func (c *Canvas) DrawImage(img DecodedImage, width, height int) {
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
				p := c.buffer[offset+i : offset+i+4]
				*(*uint32)(unsafe.Pointer(&p[0])) = uint32(color)
			}
		} else {
			copy(c.buffer[offset:], line0)
		}
	}
}

func (c *Canvas) toDrawable(width, height int) draw.Image {
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

// TODO 去掉。换成画矩形。
func (c *Canvas) DrawBorder(cr Color, w, h int, borderWidth int) {
	c.FillRect(0, 0, w, borderWidth, cr)
	c.FillRect(0, h-borderWidth, w, borderWidth, cr)
	c.FillRect(0, borderWidth, borderWidth, h-borderWidth*2, cr)
	c.FillRect(w-borderWidth, borderWidth, borderWidth, h-borderWidth*2, cr)
}

// 内部方法：只是简单地调用官方库在当前位置画完字符串。
func (c *Canvas) drawStringStd(text string, face *FontFace, color Color, width, height int) {
	drawer := font.Drawer{
		Dst:  c.toDrawable(width, height),
		Src:  image.NewUniform(color.NRGBA()),
		Face: face,
		Dot:  fixed.Point26_6{X: 0, Y: face.Metrics().Ascent},
	}
	drawer.DrawString(text)
}

// 按设备要求直接写显存。
//
/* 版本1 无glyph缓存
 go test -bench=. -benchmem
goos: darwin
goarch: arm64
pkg: gofb
cpu: Apple M2 Pro
BenchmarkDrawString/dev-12                 14042             84548 ns/op              22 B/op          2 allocs/op
BenchmarkDrawString/std-12                  5272            222231 ns/op           29424 B/op       7334 allocs/op
*/
/* 版本2 有glyph缓存、手写kerning、bearing、advance计算，可能有bug
goos: darwin
goarch: arm64
pkg: gofb
cpu: Apple M2 Pro
BenchmarkDrawString/dev-12                102585             10063 ns/op               0 B/op          0 allocs/op
BenchmarkDrawString/std-12                  4749            225611 ns/op           29408 B/op       7333 allocs/op
*/
func (c *Canvas) drawStringDevice(text string, face *FontFace, color Color, width, height int) {
	prev := rune(-1)
	dot := fixed.Point26_6{X: 0, Y: face.Metrics().Ascent}
	for _, next := range text {
		if prev >= 0 {
			dot.X += face.Kern(prev, next)
		}

		glyph := face.GlyphCached(next)
		if glyph.Width == 0 || glyph.Height == 0 {
			dot.X += glyph.Advance
			prev = next
			continue
		}

		dstX := dot.X.Round() + int(glyph.OffsetX)
		dstY := dot.Y.Round() + int(glyph.OffsetY)

		for y := 0; y < int(glyph.Height); y++ {
			sy := c.y + dstY + y
			if sy < 0 || sy >= c.height {
				continue
			}

			for x := 0; x < int(glyph.Width); x++ {
				sx := c.x + dstX + x
				if sx < 0 || sx >= c.width {
					continue
				}

				alpha := int(glyph.Masks[y*int(glyph.Width)+x])
				if alpha == 0 {
					continue
				}

				dstOffset := sy*c.width*c.bytesPerPixel + sx*c.bytesPerPixel
				pixel := c.buffer[dstOffset : dstOffset+4]

				if alpha == 255 {
					*(*uint32)(unsafe.Pointer(&pixel[0])) = uint32(color)
					continue
				}

				inverted := 255 - alpha
				pixel[0] = uint8((int(color.B())*alpha + int(pixel[0])*inverted) / 255)
				pixel[1] = uint8((int(color.G())*alpha + int(pixel[1])*inverted) / 255)
				pixel[2] = uint8((int(color.R())*alpha + int(pixel[2])*inverted) / 255)
				pixel[3] = 255
			}
		}

		dot.X += glyph.Advance
		prev = next
	}
}

type _ImageCacheKey struct {
	fsys fs.FS
	path string
}

// 用标准库的 draw.Draw 造成了极多不必要的计算，
// 而目标屏幕的内存格式是确定的（B、G、R、A），都不是 [image.RGBA] 或
// [image.NRGBA] 的格式（它们是 R、G、B、A），每次渲染的时候都转换实在没有意义。
// 所以这里直接在内存中保存目标格式，加快渲染效率。
type DecodedImage struct {
	Pixels        []byte // 内存格式：B G R A，长度：width*height*4
	Width, Height int
}

type ImageManager struct {
	cache *lru.TTLCache[_ImageCacheKey, DecodedImage]
}

func NewImageManager() *ImageManager {
	return &ImageManager{
		// https://github.com/phuslu/lru/issues/32
		cache: lru.NewTTLCache(1024, lru.WithShards[_ImageCacheKey, DecodedImage](1)),
	}
}

func (m *ImageManager) Close() {
	m.cache = nil
}

func (m *ImageManager) decodeImage(fsys fs.FS, path string) (DecodedImage, error) {
	log.Println(`重新解码：`, path)

	fp, err := fsys.Open(path)
	if err != nil {
		log.Println(err, path)
		return DecodedImage{}, err
	}
	defer fp.Close()

	img, _, err := image.Decode(fp)
	if err != nil {
		log.Println(`图片解码错误`, err, path)
		return DecodedImage{}, err
	}

	width, height := img.Bounds().Dx(), img.Bounds().Dy()

	decoded := DecodedImage{
		Width:  width,
		Height: height,
		Pixels: make([]byte, width*height*4),
	}

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
		return DecodedImage{}, fmt.Errorf(`不支持的图片格式`)
	}

	for y := range decoded.Height {
		p := pixels[y*stride:]
		for x := range decoded.Width {
			offset := (y*decoded.Width + x) * 4
			d := decoded.Pixels[offset : offset+4]
			d[0] = p[2+x*4]
			d[1] = p[1+x*4]
			d[2] = p[0+x*4]
			d[3] = p[3+x*4]
		}
	}

	return decoded, nil
}

func (m *ImageManager) GetImageCached(fsys fs.FS, path string) (DecodedImage, error) {
	img, err, _ := m.cache.GetOrLoad(
		context.Background(),
		_ImageCacheKey{
			fsys: fsys,
			path: path,
		},
		func(ctx context.Context, _ _ImageCacheKey) (DecodedImage, time.Duration, error) {
			decoded, err := m.decodeImage(fsys, path)
			return decoded, time.Minute * 30, err
		},
	)
	return img, err
}

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
