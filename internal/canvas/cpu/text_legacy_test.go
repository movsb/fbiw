package cpu

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"io/fs"
	"testing"
	"testing/fstest"
	"unsafe"

	"github.com/movsb/fbiw/internal/canvas"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type FontFace = canvas.FontFace

func (c Color) NRGBA() color.NRGBA { return color.NRGBA{R: c.R(), G: c.G(), B: c.B(), A: c.A()} }
func ColorFromRGBA(r, g, b, a uint8) Color {
	return Color(uint32(b) | uint32(g)<<8 | uint32(r)<<16 | uint32(a)<<24)
}
func ColorFromString(name string) Color {
	if name != "red" {
		panic("unsupported benchmark color: " + name)
	}
	return ColorFromRGBA(255, 0, 0, 255)
}
func (c *Canvas) getPixel(x, y int) color.NRGBA { return c.renderer.Pixel(image.Pt(c.x+x, c.y+y)) }
func (c *Canvas) SetPixel(x, y int, value color.NRGBA) {
	xx, yy := c.x+x, c.y+y
	if xx < 0 || xx >= c.width || yy < 0 || yy >= c.height {
		return
	}
	c.renderer.SetPixel(image.Pt(xx, yy), value)
}

// 版本 2：先裁剪整个字形、缓存行和颜色通道，并使用精确的快速除法混色。
//
// 字形缓存中保存的是每个像素的覆盖率（Alpha mask）。这里直接把覆盖率
// 与目标颜色、显存中原有的 BGRA 像素混合，避免经过 image/draw 的通用
// Color 接口和颜色模型转换。这个函数处于每帧绘制的热路径，内层循环应当
// 尽量只保留读取 mask、混色和写回三个步骤。
// NOTE 代码已经删除了
func (c *Canvas) drawStringDevice2(text string, faces []*FontFace, fill Color) {
	canvas.DrawText(c.renderer, text, faces, image.Pt(c.x, c.y), c.clipBounds(), canvas.Color(fill))
}

type FontManager struct{ face *FontFace }

func NewFontManager() *FontManager { return &FontManager{} }
func (fm *FontManager) AddFont(fsys fs.FS, path, family string, bold, italic bool) error {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return err
	}
	parsed, err := opentype.Parse(data)
	if err != nil {
		return err
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 30, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return err
	}
	fm.face = canvas.NewFontFace(family, face)
	return nil
}
func (fm *FontManager) GetFace(string, int, bool, bool) (*FontFace, error) { return fm.face, nil }

// 返回包含整个 framebuffer 的 image.Image/draw.Image。
//
// Image 不受 Canvas 当前 Offset 影响；它主要用于导出完整屏幕截图。
func (c *Canvas) framebuffer() draw.Image {
	origin := *c
	origin.x = 0
	origin.y = 0
	return _CanvasImage{
		underlying: &origin,
		bounds:     image.Rect(0, 0, c.width, c.height),
	}
}

// drawable 返回整个 framebuffer 在 Canvas 局部坐标系中
// 的可绘制范围。它与 Image 的公开语义不同：Bounds 可以包含
// 负坐标，使负 bearing 的字形仍能在 framebuffer 边界处正确裁剪。
//
// 如果写(0,0)，仍然写的是 canvas.(x,y)。
func (c *Canvas) drawable() draw.Image {
	bounds := c.clipBounds().Sub(image.Pt(c.x, c.y))
	return _CanvasImage{
		underlying: c,
		bounds:     bounds,
	}
}

// 内部方法：只是简单地调用官方库在当前位置画完字符串。
func (c *Canvas) drawStringStd(text string, faces []*FontFace, color Color) {
	drawer := font.Drawer{
		Dst:  c.drawable(),
		Src:  image.NewUniform(color.NRGBA()),
		Face: faces[0],
		Dot:  fixed.Point26_6{X: 0, Y: faces[0].Metrics().Ascent},
	}
	drawer.DrawString(text)
}

// 版本 1：逐像素计算屏幕坐标、判断边界并使用整数除法混色。
//
// 这是优化前的基线实现。不要随新版同步优化，否则基准会失去参照意义。
func (c *Canvas) drawStringDevice1(text string, faces []*FontFace, color Color) {
	prev := rune(-1)
	dot := fixed.Point26_6{X: 0, Y: faces[0].Metrics().Ascent}
	pixels := c.softwarePixels()
	for _, next := range text {
		if prev >= 0 {
			dot.X += faces[0].Kern(prev, next)
		}

		glyph := faces[0].GlyphCached(next)
		for _, fa := range faces {
			if fa.HasGlyph(next) {
				glyph = fa.GlyphCached(next)
				break
			}
		}

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

				dstOffset := sy*c.width*4 + sx*4
				pixel := pixels[dstOffset : dstOffset+4]
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

type _CanvasImage struct {
	underlying *Canvas
	bounds     image.Rectangle
}

func (c _CanvasImage) Bounds() image.Rectangle {
	return c.bounds
}

func (c _CanvasImage) ColorModel() color.Model {
	return color.NRGBAModel
}

func (c _CanvasImage) At(x, y int) color.Color {
	if !image.Pt(x, y).In(c.bounds) ||
		c.underlying.x+x < 0 || c.underlying.x+x >= c.underlying.width ||
		c.underlying.y+y < 0 || c.underlying.y+y >= c.underlying.height {
		return color.NRGBA{}
	}
	return c.underlying.getPixel(x, y)
}

func (c _CanvasImage) Set(x, y int, clr color.Color) {
	cc := c.ColorModel().Convert(clr).(color.NRGBA)
	c.underlying.SetPixel(x, y, cc)
}

func TestDrawStringDeviceVersions(t *testing.T) {
	// 两个版本必须产生完全相同的最终 Canvas BGRA 数据。除了正常位置，
	// 也覆盖字形被屏幕四周裁剪以及整段文字位于屏幕外的情况，重点验证
	// 版本 2 的字形级裁剪没有少画、多画或算错显存偏移。
	manager := NewFontManager()
	fonts := fstest.MapFS{
		"regular.ttf": &fstest.MapFile{Data: goregular.TTF},
	}
	if err := manager.AddFont(fonts, "regular.ttf", "system", false, false); err != nil {
		t.Fatal(err)
	}
	face, err := manager.GetFace("system", 30, false, false)
	if err != nil {
		t.Fatal(err)
	}

	const width, height = 160, 48
	offsets := []struct{ x, y int }{
		{0, 0},
		{-20, -10},
		{0, -height},
		{width - 10, height - 10},
		{-width, 0},
		{width, 0},
	}
	texts := []string{"Canvas text", "AVATAR To", " space ", ""}
	colors := []Color{
		ColorFromRGBA(0x33, 0x66, 0x99, 0xff),
		ColorFromRGBA(0xff, 0xff, 0xff, 0xff),
		ColorFromRGBA(0x00, 0x00, 0x00, 0xff),
	}

	for _, offset := range offsets {
		for _, text := range texts {
			for _, color := range colors {
				// 使用非零、有变化的背景，同时覆盖前景色和背景色共同参与的
				// 半透明抗锯齿混色。两个 buffer 从完全相同的初始内容开始。
				buffer1 := make([]byte, width*height*4)
				for i := 0; i < len(buffer1); i += 4 {
					buffer1[i+0] = uint8(i * 17) // B
					buffer1[i+1] = uint8(i * 31) // G
					buffer1[i+2] = uint8(i * 47) // R
					buffer1[i+3] = 255           // A
				}
				buffer2 := bytes.Clone(buffer1)
				buffer3 := bytes.Clone(buffer1)
				canvas1 := testSoftwareCanvas(width, height, buffer1, offset.x, offset.y)
				canvas2 := testSoftwareCanvas(width, height, buffer2, offset.x, offset.y)
				canvas3 := testSoftwareCanvas(width, height, buffer3, offset.x, offset.y)

				canvas1.drawStringDevice1(text, []*FontFace{face}, color)
				canvas2.drawStringDevice2(text, []*FontFace{face}, color)
				// 标准库基线路径也必须能处理负坐标和整段文字
				// 位于屏幕外的情况，不得访问 framebuffer 之外的像素。
				canvas3.drawStringStd(text, []*FontFace{face}, color)
				if bytes.Equal(buffer1, buffer2) {
					continue
				}

				// 报出第一个不同的字节，失败时可以直接定位到像素和 BGRA 通道。
				for i := range buffer1 {
					if buffer1[i] != buffer2[i] {
						t.Fatalf(
							"text=%q color=%08x offset=(%d,%d): 第一个差异位于 pixel=%d channel=%d: dev1=%d dev2=%d",
							text, uint32(color), offset.x, offset.y, i/4, i%4, buffer1[i], buffer2[i],
						)
					}
				}
			}
		}
	}
}

/*
版本1 无glyph缓存
go test -bench=. -benchmem

goos: darwin
goarch: arm64
pkg: gofb
cpu: Apple M2 Pro
BenchmarkDrawString/dev-12                 14042             84548 ns/op              22 B/op          2 allocs/op
BenchmarkDrawString/std-12                  5272            222231 ns/op           29424 B/op       7334 allocs/op

版本2 有glyph缓存、手写kerning、bearing、advance计算，可能有bug
goos: darwin
goarch: arm64
pkg: gofb
cpu: Apple M2 Pro
BenchmarkDrawString/dev-12                102585             10063 ns/op               0 B/op          0 allocs/op
BenchmarkDrawString/std-12                  4749            225611 ns/op           29408 B/op       7333 allocs/op

换成了 goregular 字体方便资源测试

goos: darwin
goarch: arm64
pkg: github.com/movsb/fbiw
cpu: Apple M2 Pro
BenchmarkDrawString
BenchmarkDrawString/dev
BenchmarkDrawString/dev-12         	   86136	     12170 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawString/std
BenchmarkDrawString/std-12         	    5582	    195041 ns/op	   33409 B/op	    8315 allocs/op

  - 将逐像素边界判断改为字形级裁剪。
  - 缓存颜色通道和行切片，减少热循环计算。
  - 用精确快速算法替代三次 /255。
  - 避免重复获取首字体字形。
  - 修复字形完全位于屏幕外时的裁剪边界。

goos: darwin
goarch: arm64
pkg: github.com/movsb/fbiw
cpu: Apple M2 Pro
BenchmarkDrawString
BenchmarkDrawString/dev1
BenchmarkDrawString/dev1-12         	   91054	     12149 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawString/dev2
BenchmarkDrawString/dev2-12         	  128133	      9260 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawString/std
BenchmarkDrawString/std-12          	    5499	    194548 ns/op	   33408 B/op	    8315 allocs/op

root@TinaLinux:~# /tmp/fbiw.test  -test.run=^$ -test.bench=^BenchmarkDrawString$ -test.benchmem
goos: linux
goarch: arm64
pkg: github.com/movsb/fbiw
BenchmarkDrawString/std-4         	     254	   4660639 ns/op	   33432 B/op	    8315 allocs/op
BenchmarkDrawString/dev1-4        	    2725	    394309 ns/op	       2 B/op	       0 allocs/op
BenchmarkDrawString/dev2-4        	    6464	    183326 ns/op	       0 B/op	       0 allocs/op
*/
func BenchmarkDrawString(b *testing.B) {
	fm := NewFontManager()
	fs := fstest.MapFS{
		`regular.ttf`: &fstest.MapFile{Data: goregular.TTF},
	}
	if err := fm.AddFont(fs, `regular.ttf`, `system`, false, false); err != nil {
		b.Fatal(err)
	}
	face, err := fm.GetFace(`system`, 30, false, false)
	if err != nil {
		b.Fatal(err)
	}
	b.Run(`std`, func(b *testing.B) {
		canvas := *NewCanvas(1024, 768)
		for b.Loop() {
			canvas.drawStringStd(`Canvas text rendering benchmark`, []*FontFace{face}, ColorFromString(`red`))
		}
	})
	b.Run(`dev1`, func(b *testing.B) {
		canvas := *NewCanvas(1024, 768)
		for b.Loop() {
			canvas.drawStringDevice1(`Canvas text rendering benchmark`, []*FontFace{face}, ColorFromString(`red`))
		}
	})
	b.Run(`dev2`, func(b *testing.B) {
		canvas := *NewCanvas(1024, 768)
		for b.Loop() {
			canvas.drawStringDevice2(`Canvas text rendering benchmark`, []*FontFace{face}, ColorFromString(`red`))
		}
	})
}
