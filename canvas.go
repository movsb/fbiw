package fbiw

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"io/fs"
	"log"
	"os"
	"simd/archsimd"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/anthonynsimon/bild/transform"
	"github.com/phuslu/lru"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/webp"
)

// 绘图层。
//
// 提供基础绘制工具。
type Canvas struct {
	buffer []byte

	// 渲染的偏移坐标。
	x, y int

	// buffer 的宽度和高度。
	width, height int
}

func NewCanvas(width, height int) *Canvas {
	if width <= 0 || height <= 0 {
		panic(`无效Canvas大小`)
	}
	return &Canvas{
		width:  width,
		height: height,
		x:      0,
		y:      0,
		buffer: make([]byte, width*height*4),
	}
}

func (c *Canvas) SaveToFile(path string) {
	fp, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer fp.Close()
	if err := png.Encode(fp, c.framebuffer()); err != nil {
		panic(err)
	}
}

// 提供局部平移 ——— 仅仅是把内部的起点 (x,y) 平移 (x,y) 个单位，并不承担任何裁剪功能。
// 非常类似于常见的 translate 方法。
func (c *Canvas) Offset(x, y int) *Canvas {
	if x == 0 && y == 0 {
		return c
	}
	return &Canvas{
		buffer: c.buffer,
		x:      c.x + x,
		y:      c.y + y,
		width:  c.width,
		height: c.height,
	}
}

func (c *Canvas) DrawImage(img DecodedImage) {
	c.drawImage5Region(img, 0, 0, img.Width, img.Height)
}

// DrawImageRegion 把图片的指定区域绘制到 Canvas 当前原点。
func (c *Canvas) DrawImageRegion(img DecodedImage, srcX, srcY, width, height int) {
	c.drawImage5Region(img, srcX, srcY, width, height)
}

type imageDrawRegion struct {
	dstX, dstY    int
	srcX, srcY    int
	width, height int
}

// clipImageRegion 同时裁剪源图和 framebuffer；目标左上越界时，
// 必须同步跳过源图左上的像素，否则不仅会切片 panic，图像也会错位。
func (c *Canvas) clipImageRegion(img DecodedImage, srcX, srcY, width, height int) (imageDrawRegion, bool) {
	r := imageDrawRegion{
		dstX:   c.x,
		dstY:   c.y,
		srcX:   srcX,
		srcY:   srcY,
		width:  width,
		height: height,
	}
	if r.srcX < 0 {
		r.dstX -= r.srcX
		r.width += r.srcX
		r.srcX = 0
	}
	if r.srcY < 0 {
		r.dstY -= r.srcY
		r.height += r.srcY
		r.srcY = 0
	}
	r.width = min(r.width, img.Width-r.srcX)
	r.height = min(r.height, img.Height-r.srcY)
	if r.dstX < 0 {
		delta := -r.dstX
		r.dstX = 0
		r.srcX += delta
		r.width -= delta
	}
	if r.dstY < 0 {
		delta := -r.dstY
		r.dstY = 0
		r.srcY += delta
		r.height -= delta
	}
	r.width = min(r.width, c.width-r.dstX)
	r.height = min(r.height, c.height-r.dstY)
	return r, r.width > 0 && r.height > 0
}

// 版本 1：逐像素切出四字节切片，并分别计算 B、G、R 三个通道。
// 这是优化前的基线实现，保留下来用于性能和最终显存数据对照。
func (c *Canvas) drawImage1(img DecodedImage, width, height int) {
	r, ok := c.clipImageRegion(img, 0, 0, width, height)
	if !ok {
		return
	}
	width, height = r.width, r.height

	for y := range r.height {
		offset := (r.dstY+y)*c.width*4 + r.dstX*4
		dst := c.buffer[offset:]
		src := img.Pixels[((r.srcY+y)*img.Width+r.srcX)*4:]
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

// 版本 2：按 uint32 BGRA 像素读写，并用 SWAR 同时混合 B/R 两个通道。
// 混色仍然精确除以 255，因此最终结果应当与版本 1 逐字节完全相同。
func (c *Canvas) drawImage2(img DecodedImage, width, height int) {
	r, ok := c.clipImageRegion(img, 0, 0, width, height)
	if !ok {
		return
	}
	width, height = r.width, r.height

	const maskBR = uint32(0x00ff00ff)
	for y := range r.height {
		dstOffset := ((r.dstY+y)*c.width + r.dstX) * 4
		srcOffset := ((r.srcY+y)*img.Width + r.srcX) * 4
		dstBytes := c.buffer[dstOffset : dstOffset+width*4]
		srcBytes := img.Pixels[srcOffset : srcOffset+width*4]
		dst := unsafe.Slice((*uint32)(unsafe.Pointer(&dstBytes[0])), width)
		src := unsafe.Slice((*uint32)(unsafe.Pointer(&srcBytes[0])), width)

		for x, source := range src {
			a := source >> 24
			switch a {
			case 255:
				dst[x] = source
			case 0:
				continue
			default:
				ia := uint32(255) - a
				destination := dst[x]

				// B/R 分别位于两个互不干扰的 16-bit lane 中。混色分子
				// 最大为 255²，不会产生跨 lane 进位。
				brSum := (source&maskBR)*a + (destination&maskBR)*ia
				br := brSum + 0x00010001 + ((brSum >> 8) & maskBR)
				br = (br >> 8) & maskBR

				gSum := ((source>>8)&0xff)*a + ((destination>>8)&0xff)*ia
				g := uint32(div255(gSum))
				dst[x] = 0xff000000 | br | g<<8
			}
		}
	}
}

// 版本 3：在版本 2 的精确 SWAR 混色基础上，使用 SIMD 一次处理四个像素。
// 每个 uint32 lane 对应一个 BGRA 像素；每个像素可以拥有不同的 Alpha。
func (c *Canvas) drawImage3(img DecodedImage, width, height int) {
	c.drawImageSIMD(img, width, height, false)
}

// 版本 4：在版本 3 之前增加“四个像素全部不透明”的块级快速路径。
// UI 图片经常整块不透明，此时直接复制比执行完整 SIMD 混色快得多。
func (c *Canvas) drawImage4(img DecodedImage, width, height int) {
	c.drawImageSIMD(img, width, height, true)
}

// 版本 5：利用解码阶段缓存的整图不透明信息选择最快路径。
// 不透明图片直接逐行复制；含透明像素的图片直接使用版本 3，避免版本 4
// 在每四个像素上重复判断。外部手工构造的 DecodedImage 默认 Opaque=false，
// 会安全地走通用混色路径。
/*
对 []byte 的 copy，Go 编译器通常会降低为 runtime.memmove。Go 1.27 的 ARM64 memmove 是专门写的汇编：
- 小块复制使用 MOVD、LDP/STP。
- 大块复制每轮处理 64 字节。
- 使用软件流水线。
- 自动处理 16 字节对齐和内存重叠。
- 主要使用成对的 64-bit 整数加载/存储，而不是 NEON 向量寄存器。
因此它虽然不一定是“SIMD 指令”，但已经能充分利用 ARM64 的宽加载、宽存储和内存带宽。我们 drawImage5 每行约复制 4096 字节，会进入高度优化的大块 memmove 路径。
手写 archsimd.LoadUint32x4/Store 每次只复制 16 字节，通常很难超过 runtime 每轮 64 字节的软件流水线；还会增加 Go 循环、边界和分支开销。所以不透明图片继续使用内置 copy 是合理的，TinaLinux 的结果也证明它明显更快。
*/
func (c *Canvas) drawImage5(img DecodedImage, width, height int) {
	c.drawImage5Region(img, 0, 0, width, height)
}

func (c *Canvas) drawImage5Region(img DecodedImage, srcX, srcY, width, height int) {
	if !img.Opaque {
		c.drawImageSIMDRegion(img, srcX, srcY, width, height, false)
		return
	}

	r, ok := c.clipImageRegion(img, srcX, srcY, width, height)
	if !ok {
		return
	}
	for y := range r.height {
		dstOffset := ((r.dstY+y)*c.width + r.dstX) * 4
		srcOffset := ((r.srcY+y)*img.Width + r.srcX) * 4
		copy(c.buffer[dstOffset:dstOffset+r.width*4], img.Pixels[srcOffset:srcOffset+r.width*4])
	}
}

func (c *Canvas) drawImageSIMD(img DecodedImage, width, height int, copyOpaque bool) {
	c.drawImageSIMDRegion(img, 0, 0, width, height, copyOpaque)
}

func (c *Canvas) drawImageSIMDRegion(img DecodedImage, srcX, srcY, width, height int, copyOpaque bool) {
	r, ok := c.clipImageRegion(img, srcX, srcY, width, height)
	if !ok {
		return
	}
	width, height = r.width, r.height

	vMaskBR := archsimd.BroadcastUint32x4(0x00ff00ff)
	vMaskG := archsimd.BroadcastUint32x4(0x000000ff)
	vOneBR := archsimd.BroadcastUint32x4(0x00010001)
	vOneG := archsimd.BroadcastUint32x4(1)
	v255 := archsimd.BroadcastUint32x4(255)
	vZero := archsimd.BroadcastUint32x4(0)
	vOpaque := archsimd.BroadcastUint32x4(0xff000000)
	vectorWidth := width &^ 3

	for y := range height {
		dstOffset := ((r.dstY+y)*c.width + r.dstX) * 4
		srcOffset := ((r.srcY+y)*img.Width + r.srcX) * 4
		dstBytes := c.buffer[dstOffset : dstOffset+width*4]
		srcBytes := img.Pixels[srcOffset : srcOffset+width*4]
		dst := unsafe.Slice((*uint32)(unsafe.Pointer(&dstBytes[0])), width)
		src := unsafe.Slice((*uint32)(unsafe.Pointer(&srcBytes[0])), width)

		x := 0
		for ; x < vectorWidth; x += 4 {
			// 四个 Alpha 的按位与仍为 0xff，说明四个源像素都完全不透明。
			// 只在版本 4 启用，使版本 3 保持纯 SIMD 基线便于对比。
			if copyOpaque &&
				(src[x]&src[x+1]&src[x+2]&src[x+3]&0xff000000) == 0xff000000 {
				archsimd.LoadUint32x4(src[x : x+4]).Store(dst[x : x+4])
				continue
			}
			source := archsimd.LoadUint32x4(src[x : x+4])
			destination := archsimd.LoadUint32x4(dst[x : x+4])
			a := source.ShiftAllRight(24)
			ia := v255.Sub(a)

			// B/R 分别放在每个 uint32 的两个 16-bit lane 内并行混色。
			brSum := source.And(vMaskBR).Mul(a).
				Add(destination.And(vMaskBR).Mul(ia))
			br := brSum.Add(vOneBR).
				Add(brSum.ShiftAllRight(8).And(vMaskBR)).
				ShiftAllRight(8).And(vMaskBR)

			gSum := source.ShiftAllRight(8).And(vMaskG).Mul(a).
				Add(destination.ShiftAllRight(8).And(vMaskG).Mul(ia))
			g := gSum.Add(vOneG).
				Add(gSum.ShiftAllRight(8).And(vMaskG)).
				ShiftAllRight(8).And(vMaskG).ShiftAllLeft(8)

			out := vOpaque.Or(br).Or(g)
			// 保持版本 1 的两个快速路径语义：全透明时目标像素一字节不动；
			// 全不透明时连同源像素的 Alpha 原样复制。
			out = source.IfElse(a.Equal(v255), out)
			out = destination.IfElse(a.Equal(vZero), out)
			out.Store(dst[x : x+4])
		}

		// 行尾不足四个像素时沿用版本 2 的精确标量算法。
		for ; x < width; x++ {
			source := src[x]
			a := source >> 24
			if a == 255 {
				dst[x] = source
				continue
			}
			if a == 0 {
				continue
			}

			ia := uint32(255) - a
			destination := dst[x]
			brSum := (source&0x00ff00ff)*a + (destination&0x00ff00ff)*ia
			br := brSum + 0x00010001 + ((brSum >> 8) & 0x00ff00ff)
			br = (br >> 8) & 0x00ff00ff
			gSum := ((source>>8)&0xff)*a + ((destination>>8)&0xff)*ia
			dst[x] = 0xff000000 | br | uint32(div255(gSum))<<8
		}
	}
}

func (c *Canvas) getPixel(x, y int) color.NRGBA {
	xx, yy := c.x+x, c.y+y
	offset := c.width*4*yy + xx*4

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
	offset := c.width*yy*4 + xx*4

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
	if x0 > c.width {
		return
	}
	if y0 < 0 {
		y0 = 0
	}
	if y0 > c.height {
		return
	}
	if x1 > c.width {
		x1 = c.width
	}
	if y1 > c.height {
		y1 = c.height
	}

	// 如果是完全不透明色，则直接覆盖。
	// 或者是需要“打洞”的颜色。
	if color.A() == 255 || color.IsClear() {
		if color.IsClear() {
			color = 0
		}
		var line0 []byte
		for yy := y0; yy < y1; yy++ {
			offset := c.width*4*yy + x0*4
			if yy == y0 {
				line0 = c.buffer[offset : offset+(x1-x0)*4]
				for i := 0; i < (x1-x0)*4; i += 4 {
					p := c.buffer[offset+i : offset+i+4]
					*(*uint32)(unsafe.Pointer(&p[0])) = color.Value()
				}
			} else {
				copy(c.buffer[offset:], line0)
			}
		}
		return
	}

	// 带透明通道的颜色需要和背景混合。
	// 提出来方便做性能测试。
	fillAlphaBlend5(c, color, x0, x1, y0, y1)
}

// 基线标准
func fillAlphaBlend1(c *Canvas, color Color, x0, x1, y0, y1 int) {
	a, ia := color.A(), 255-color.A()
	for yy := y0; yy < y1; yy++ {
		offset := c.width*4*yy + x0*4
		for i := 0; i < (x1-x0)*4; i += 4 {
			p := c.buffer[offset+i : offset+i+4]
			p[0] = uint8((int(color.B())*int(a) + int(p[0])*int(ia)) / 255)
			p[1] = uint8((int(color.G())*int(a) + int(p[1])*int(ia)) / 255)
			p[2] = uint8((int(color.R())*int(a) + int(p[2])*int(ia)) / 255)
			p[3] = 255
		}
	}
}

// 查表法
func fillAlphaBlend2(c *Canvas, color Color, x0, x1, y0, y1 int) {
	a := int(color.A())
	ia := 255 - a

	b := int(color.B()) * a
	g := int(color.G()) * a
	r := int(color.R()) * a

	var blendB [256]uint8
	var blendG [256]uint8
	var blendR [256]uint8

	for i := range 256 {
		blendB[i] = uint8((b + i*ia) / 255)
		blendG[i] = uint8((g + i*ia) / 255)
		blendR[i] = uint8((r + i*ia) / 255)
	}

	rowBytes := (x1 - x0) * 4

	for yy := y0; yy < y1; yy++ {
		offset := (yy*c.width + x0) * 4
		p := c.buffer[offset : offset+rowBytes]

		for i := 0; i < rowBytes; i += 4 {
			p[i+0] = blendB[p[i+0]]
			p[i+1] = blendG[p[i+1]]
			p[i+2] = blendR[p[i+2]]
			p[i+3] = 255
		}
	}
}

// 不精确：/255 ---> >>8
func fillAlphaBlend3(c *Canvas, color Color, x0, x1, y0, y1 int) {
	a := int(color.A())

	// 注意这里是256，数学上更正确？
	ia := 256 - a

	b := int(color.B()) * a
	g := int(color.G()) * a
	r := int(color.R()) * a

	var blendB [256]uint8
	var blendG [256]uint8
	var blendR [256]uint8

	for i := range 256 {
		blendB[i] = uint8((b + i*ia) >> 8)
		blendG[i] = uint8((g + i*ia) >> 8)
		blendR[i] = uint8((r + i*ia) >> 8)
	}

	rowBytes := (x1 - x0) * 4

	for yy := y0; yy < y1; yy++ {
		offset := (yy*c.width + x0) * 4
		p := c.buffer[offset : offset+rowBytes]

		for i := 0; i < rowBytes; i += 4 {
			p[i+0] = blendB[p[i+0]]
			p[i+1] = blendG[p[i+1]]
			p[i+2] = blendR[p[i+2]]
			p[i+3] = 255
		}
	}
}

// uint32 + SWAR + >>8
func fillAlphaBlend4(c *Canvas, color Color, x0, x1, y0, y1 int) {
	a := uint32(color.A())
	ia := uint32(256) - a

	// B、R 分别占两个 16-bit lane。
	srcBR := uint32(color.B()) | uint32(color.R())<<16
	srcBR *= a

	srcG := uint32(color.G()) * a

	width := x1 - x0

	for yy := y0; yy < y1; yy++ {
		offset := (yy*c.width + x0) * 4

		for x := range width {
			p := (*uint32)(unsafe.Pointer(&c.buffer[offset+x*4]))
			dst := *p

			// B 和 R 一次计算。
			br := ((srcBR + (dst&0x00ff00ff)*ia) >> 8) & 0x00ff00ff
			// G 单独计算。
			g := ((srcG + ((dst>>8)&0xff)*ia) >> 8) & 0xff

			*p = 0xff000000 | br | g<<8
		}
	}
}

// archsimd / ARM64 NEON
//
// 思路：
//
//	一个 Uint32x4 = 4 个 BGRA8888 像素。
//	每个 uint32 lane 内继续使用 fillAlphaBlend4 的 SWAR 技巧：
//	B/R 两个 16-bit lane 一起算，G 单独算。
func fillAlphaBlend5(c *Canvas, color Color, x0, x1, y0, y1 int) {
	a := uint32(color.A())
	ia := uint32(256) - a

	srcBR := (uint32(color.B()) | uint32(color.R())<<16) * a
	srcG := uint32(color.G()) * a

	vIA := archsimd.BroadcastUint32x4(ia)
	vSrcBR := archsimd.BroadcastUint32x4(srcBR)
	vSrcG := archsimd.BroadcastUint32x4(srcG)

	vMaskBR := archsimd.BroadcastUint32x4(0x00ff00ff)
	vMaskG := archsimd.BroadcastUint32x4(0x000000ff)
	vAlpha := archsimd.BroadcastUint32x4(0xff000000)

	width := x1 - x0
	vectorWidth := width &^ 3

	for yy := y0; yy < y1; yy++ {
		offset := (yy*c.width + x0) * 4

		row := c.buffer[offset : offset+width*4]

		// BGRA8888 => 每 4 字节一个 uint32。
		pixels := unsafe.Slice(
			(*uint32)(unsafe.Pointer(&row[0])),
			width,
		)

		x := 0

		for ; x < vectorWidth; x += 4 {
			dst := archsimd.LoadUint32x4(pixels[x : x+4])

			// B + R:
			//
			// ((dst & 0x00ff00ff) * ia + srcBR) >> 8
			br := dst.
				And(vMaskBR).
				Mul(vIA).
				Add(vSrcBR).
				ShiftAllRight(8).
				And(vMaskBR)

			// G:
			//
			// ((((dst >> 8) & 0xff) * ia + srcG) >> 8) << 8
			g := dst.
				ShiftAllRight(8).
				And(vMaskG).
				Mul(vIA).
				Add(vSrcG).
				ShiftAllRight(8).
				And(vMaskG).
				ShiftAllLeft(8)

			out := vAlpha.Or(br).Or(g)

			out.Store(pixels[x : x+4])
		}

		// 最后的 0~3 个像素。
		for ; x < width; x++ {
			dst := pixels[x]

			br := ((srcBR + (dst&0x00ff00ff)*ia) >> 8) & 0x00ff00ff
			g := ((srcG + ((dst>>8)&0xff)*ia) >> 8) & 0xff

			pixels[x] = 0xff000000 | br | g<<8
		}
	}
}

// 清屏。
// 暂时是简单用黑色清。
func (c *Canvas) Clear() {
	clear(c.buffer)
}

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
	return _CanvasImage{
		underlying: c,
		bounds:     image.Rect(-c.x, -c.y, c.width-c.x, c.height-c.y),
	}
}

// TODO 去掉。换成画矩形。
func (c *Canvas) DrawBorder(cr Color, w, h int, borderWidth int) {
	c.FillRect(0, 0, w, borderWidth, cr)
	c.FillRect(0, h-borderWidth, w, borderWidth, cr)
	c.FillRect(0, borderWidth, borderWidth, h-borderWidth*2, cr)
	c.FillRect(w-borderWidth, borderWidth, borderWidth, h-borderWidth*2, cr)
}

// 画字符串，以指定的字体、指定的颜色、于当前位置。
//
// 超出 framebuffer 的像素会被裁剪。
func (c *Canvas) DrawString(text string, faces []*FontFace, color Color) {
	if color == ColorNone {
		return
	}
	c.drawStringDevice(text, faces, color)
}

// 精确计算 value / 255。
//
// 混色时 value 最大为 255*255，利用 255 == 256-1 可以把耗时较高的
// 整数除法换成加法和移位。这个公式在 [0, 255²] 范围内与向下取整的
// value/255 完全相同，并不是 fillAlphaBlend3 使用的近似除以 256。
func div255(value uint32) uint8 {
	return uint8((value + 1 + (value >> 8)) >> 8)
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

// 按设备要求直接写显存。
//
// 和 fillAlphaBlend 系列一样保留各个版本，方便在实际设备上持续比较。
// 正常绘制始终调用当前最快的版本。
func (c *Canvas) drawStringDevice(text string, faces []*FontFace, color Color) {
	log.Println(`画文本:`, text)
	c.drawStringDevice2(text, faces, color)
}

// 版本 1：逐像素计算屏幕坐标、判断边界并使用整数除法混色。
//
// 这是优化前的基线实现。不要随新版同步优化，否则基准会失去参照意义。
func (c *Canvas) drawStringDevice1(text string, faces []*FontFace, color Color) {
	prev := rune(-1)
	dot := fixed.Point26_6{X: 0, Y: faces[0].Metrics().Ascent}
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

// 版本 2：先裁剪整个字形、缓存行和颜色通道，并使用精确的快速除法混色。
//
// 字形缓存中保存的是每个像素的覆盖率（Alpha mask）。这里直接把覆盖率
// 与目标颜色、显存中原有的 BGRA 像素混合，避免经过 image/draw 的通用
// Color 接口和颜色模型转换。这个函数处于每帧绘制的热路径，内层循环应当
// 尽量只保留读取 mask、混色和写回三个步骤。
func (c *Canvas) drawStringDevice2(text string, faces []*FontFace, color Color) {
	prev := rune(-1)
	dot := fixed.Point26_6{X: 0, Y: faces[0].Metrics().Ascent}

	// Color 的通道提取包含移位和类型转换。颜色在整段文本中不会改变，
	// 提前计算一次，避免在每个半透明像素上重复执行。
	colorB := uint32(color.B())
	colorG := uint32(color.G())
	colorR := uint32(color.R())
	for _, next := range text {
		// 字偶距始终按主字体计算，以保持与原来的排版行为一致。
		if prev >= 0 {
			dot.X += faces[0].Kern(prev, next)
		}

		// 按字体列表顺序查找第一个包含当前字符的字体。全部不包含时仍用
		// 主字体取得缺字方框及其 Advance，保证缺字也会正常推进光标。
		// 先确定字体再读取缓存，可以省掉原实现对主字体的一次多余缓存查询。
		var face *FontFace
		for _, fa := range faces {
			if fa.HasGlyph(next) {
				face = fa
				break
			}
		}
		if face == nil {
			face = faces[0]
		}
		glyph := face.GlyphCached(next)

		if glyph.Width == 0 || glyph.Height == 0 {
			dot.X += glyph.Advance
			prev = next
			continue
		}

		dstX := dot.X.Round() + int(glyph.OffsetX)
		dstY := dot.Y.Round() + int(glyph.OffsetY)

		// 先在“字形坐标系”中计算字形与屏幕的交集。原实现对每个像素分别
		// 判断 sx/sy 是否越界；绝大多数字形完全在屏幕内，这些重复判断会
		// 占据内层循环的可观开销。
		//
		// sx0/sy0 是字形左上角在屏幕中的绝对坐标；x0..x1、y0..y1 则是
		// 真正需要绘制的 mask 范围。字形完全位于屏幕外时区间为空。
		glyphWidth := int(glyph.Width)
		glyphHeight := int(glyph.Height)
		sx0 := c.x + dstX
		sy0 := c.y + dstY
		x0, y0 := max(0, -sx0), max(0, -sy0)
		x1, y1 := min(glyphWidth, c.width-sx0), min(glyphHeight, c.height-sy0)

		if x0 < x1 && y0 < y1 {
			for y := y0; y < y1; y++ {
				// 每行只计算一次 mask 和显存切片。这样内层循环使用相对下标，
				// 不必反复计算 y*width、屏幕偏移以及切出整个 buffer 的尾部。
				maskRow := glyph.Masks[y*glyphWidth : y*glyphWidth+glyphWidth]
				dstOffset := ((sy0+y)*c.width + sx0 + x0) * 4
				dstRow := c.buffer[dstOffset : dstOffset+(x1-x0)*4]

				for x := x0; x < x1; x++ {
					alpha := uint32(maskRow[x])
					if alpha == 0 {
						continue
					}

					pixel := dstRow[(x-x0)*4 : (x-x0)*4+4]
					// 完全覆盖时直接写入一个 BGRA 像素，不需要混色。
					if alpha == 255 {
						*(*uint32)(unsafe.Pointer(&pixel[0])) = uint32(color)
						continue
					}

					inverted := uint32(255) - alpha
					// mask 的 alpha 表示前景覆盖率：
					// out = (foreground*alpha + background*(255-alpha)) / 255。
					// 三个通道分别混合，最后通过 div255 精确完成除法。
					b := colorB*alpha + uint32(pixel[0])*inverted
					g := colorG*alpha + uint32(pixel[1])*inverted
					r := colorR*alpha + uint32(pixel[2])*inverted
					pixel[0] = div255(b)
					pixel[1] = div255(g)
					pixel[2] = div255(r)
					pixel[3] = 255
				}
			}
		}

		dot.X += glyph.Advance
		prev = next
	}
}

type _ImageCacheKey struct {
	fsys          fs.FS
	path          string
	width, height int
}
type _ImageConfigCacheKey struct {
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
	Opaque        bool // 整张图片的 Alpha 是否全部为 255；用于选择直接复制路径。
}

type ImageManager struct {
	contentCache *lru.TTLCache[_ImageCacheKey, DecodedImage]
	configCache  *lru.TTLCache[_ImageConfigCacheKey, DecodedImage]

	// 图片加载可能被异步调用。
	closed atomic.Bool
}

func NewImageManager() *ImageManager {
	return &ImageManager{
		// https://github.com/phuslu/lru/issues/32
		contentCache: lru.NewTTLCache(1024, lru.WithShards[_ImageCacheKey, DecodedImage](1)),
		configCache:  lru.NewTTLCache(1024, lru.WithShards[_ImageConfigCacheKey, DecodedImage](1)),
	}
}

// 不需要清空两个cache，因为刚刚启动的 goroutine 可能仍然在访问。
// 原子锁没有阻止此行为。
func (m *ImageManager) Close() {
	m.closed.Store(true)
}

func (m *ImageManager) decodeImageConfigCached(fsys fs.FS, path string, checking bool) (DecodedImage, error) {
	if m.closed.Load() {
		return DecodedImage{}, fs.ErrClosed
	}

	key := _ImageConfigCacheKey{
		fsys: fsys,
		path: path,
	}
	if checking {
		img, found := m.configCache.Get(key)
		if found {
			return img, nil
		}
		return img, os.ErrNotExist
	}
	img, err, _ := m.configCache.GetOrLoad(context.Background(), key,
		func(ctx context.Context, _ _ImageConfigCacheKey) (DecodedImage, time.Duration, error) {
			width, height, err := m.decodeImageConfig(fsys, path)
			return DecodedImage{Width: width, Height: height}, time.Minute * 30, err
		},
	)
	return img, err
}

func (m *ImageManager) decodeImageConfig(fsys fs.FS, path string) (int, int, error) {
	if m.closed.Load() {
		return 0, 0, fs.ErrClosed
	}

	fp, err := fsys.Open(path)
	if err != nil {
		log.Println(err, path)
		return 0, 0, err
	}
	defer fp.Close()
	img, _, err := image.DecodeConfig(fp)
	if err != nil {
		log.Println(err)
		return 0, 0, err
	}
	return img.Width, img.Height, nil
}

// 如果 width和height均为0，返回原图大小。
// 否则表示指定缩放到此大小。
func (m *ImageManager) decodeImage(fsys fs.FS, path string, wantWidth, wantHeight int) (DecodedImage, error) {
	if m.closed.Load() {
		return DecodedImage{}, fs.ErrClosed
	}

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
	if wantWidth != 0 && wantHeight != 0 && (wantWidth != width || wantHeight != height) {
		now := time.Now()
		img = transform.Resize(img, wantWidth, wantHeight, transform.Lanczos)
		log.Printf(`缩放图片: %s (%dx%d)->(%dx%d) %v`,
			path, width, height, wantWidth, wantHeight, time.Since(now).Round(time.Millisecond*100))
		width = wantWidth
		height = wantHeight
	}

	decoded := DecodedImage{
		Width:  width,
		Height: height,
		Pixels: make([]byte, width*height*4),
		Opaque: true,
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
	}

	// fast-path
	if len(pixels) > 0 {
		for y := range decoded.Height {
			s := pixels[y*stride:]
			for x := range decoded.Width {
				offset := (y*decoded.Width + x) * 4
				d := decoded.Pixels[offset : offset+4]
				d[0] = s[2+x*4]
				d[1] = s[1+x*4]
				d[2] = s[0+x*4]
				d[3] = s[3+x*4]
				if d[3] != 255 {
					decoded.Opaque = false
				}
			}
		}
		return decoded, nil
	}

	// better-slow than never 🥵
	for y0, y1 := img.Bounds().Min.Y, img.Bounds().Max.Y; y0 < y1; y0++ {
		for x0, x1 := img.Bounds().Min.X, img.Bounds().Max.X; x0 < x1; x0++ {
			converted := color.NRGBAModel.Convert(img.At(x0, y0)).(color.NRGBA)
			offset := (y0*decoded.Width + x0) * 4
			d := decoded.Pixels[offset : offset+4]
			d[0] = converted.B
			d[1] = converted.G
			d[2] = converted.R
			d[3] = converted.A
			if converted.A != 255 {
				decoded.Opaque = false
			}
		}
	}

	return decoded, nil
}

// 多线程安全。
func (m *ImageManager) GetImageCached(fsys fs.FS, path string) (DecodedImage, error) {
	return m.getImageCached(fsys, path, 0, 0, false)
}

// 多线程安全。
func (m *ImageManager) GetImageScaledCached(fsys fs.FS, path string, width, height int, checking bool) (DecodedImage, error) {
	return m.getImageCached(fsys, path, width, height, checking)
}

func (m *ImageManager) getImageCached(fsys fs.FS, path string, width, height int, checking bool) (DecodedImage, error) {
	if m.closed.Load() {
		return DecodedImage{}, fs.ErrClosed
	}
	key := _ImageCacheKey{
		fsys:   fsys,
		path:   path,
		width:  width,
		height: height,
	}
	if checking {
		img, found := m.contentCache.Get(key)
		if found {
			return img, nil
		}
		return img, os.ErrNotExist
	}
	img, err, _ := m.contentCache.GetOrLoad(context.Background(), key,
		func(ctx context.Context, _ _ImageCacheKey) (DecodedImage, time.Duration, error) {
			decoded, err := m.decodeImage(fsys, path, width, height)
			return decoded, time.Minute * 10, err
		},
	)
	return img, err
}

type FontManager struct {
	fonts map[_FontKey]*_FontValue
	faces map[_FontFaceKey]*FontFace
}

func NewFontManager() *FontManager {
	return &FontManager{
		fonts: map[_FontKey]*_FontValue{},
		faces: map[_FontFaceKey]*FontFace{},
	}
}

func (fm *FontManager) Close() {
	for _, f := range fm.fonts {
		f.File.Close()
	}
	clear(fm.fonts)
}

type _FontKey struct {
	Family string
	Bold   bool
	Italic bool
}
type _FontValue struct {
	Font *opentype.Font
	File io.ReadCloser
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
//
// fsys.Open的文件必须支持 io.ReaderAt。os.DirFS和embed.FS 均支持。
func (fm *FontManager) AddFont(fsys fs.FS, path string, family string, bold, italic bool) error {
	key := _FontKey{
		Family: family,
		Bold:   bold,
		Italic: italic,
	}
	if _, ok := fm.fonts[key]; ok {
		return nil
	}

	fp, err := fsys.Open(path)
	if err != nil {
		return err
	}

	// 先完整读内存，如果占用高，可以考虑转 ParseReader，
	// 但是那样可以会每个字符读文件？不知道速度怎样。
	parsedFont, err := opentype.ParseReaderAt(fp.(io.ReaderAt))
	if err != nil {
		return err
	}

	fm.fonts[key] = &_FontValue{
		File: fp,
		Font: parsedFont,
	}

	return nil
}

// 返回系统字体。
//
// 如果有样式的系统字体找不到，会返回非粗体、非斜体版本。
// 如果还是找不到，就直接崩溃。
func (fm *FontManager) GetSystemFace(size int, bold bool, italic bool) *FontFace {
	system, err := fm.GetFace(`system`, size, bold, italic)
	if err == nil {
		return system
	}
	// 怎么连对应形状的系统字体也找不到？
	system, err = fm.GetFace(`system`, size, false, false)
	if err == nil {
		return system
	}
	panic(`没有任何可用的系统字体，没救了。`)
}

// 返回指定名字的字体。
func (fm *FontManager) GetFace(family string, size int, bold bool, italic bool) (*FontFace, error) {
	faceKey := _FontFaceKey{
		Family: family,
		Size:   size,
		Bold:   bold,
		Italic: italic,
	}
	if face, ok := fm.faces[faceKey]; ok {
		return face, nil
	}

	fontKey := _FontKey{
		Family: family,
		Bold:   bold,
		Italic: italic,
	}
	fontValue, ok := fm.fonts[fontKey]
	if !ok {
		return nil, fmt.Errorf(`字体家族未找到：%v`, fontKey)
	}

	theFace, err := opentype.NewFace(fontValue.Font, &opentype.FaceOptions{
		Size:    float64(size),
		DPI:     72, // 为72时1点=1像素
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf(`无法创建字体样式：%w`, err)
	}

	fontFace := &FontFace{
		Face:  theFace,
		name:  family,
		cache: map[rune]GlyphValue{},
	}

	fm.faces[faceKey] = fontFace

	return fontFace, nil
}

type FontFace struct {
	font.Face

	name string

	// 文字渲染过程的光栅化非常消耗，所以缓存一下。
	cache map[rune]GlyphValue
}

// 测试文本 text 使用此字体时所占据的宽度。
func (ff FontFace) MeasureString(text string) fixed.Int26_6 {
	return font.MeasureString(ff, text)
}

func (ff FontFace) TextHeight() int {
	return (ff.Metrics().Ascent + ff.Metrics().Descent).Ceil()
}

// golang.org/x/image/font/opentype/opentype.go
/*
	nPixels := width * height
	if cap(f.mask.Pix) < nPixels {
		f.mask.Pix = make([]uint8, 2*nPixels)
	}
	f.mask.Pix = f.mask.Pix[:nPixels]
	f.mask.Stride = width
	f.mask.Rect.Min.X = 0
	f.mask.Rect.Min.Y = 0
	f.mask.Rect.Max.X = width
	f.mask.Rect.Max.Y = height
*/
type GlyphValue struct {
	Masks []byte

	Width  uint16
	Height uint16

	OffsetX int16
	OffsetY int16

	Advance fixed.Int26_6
}

func (ff *FontFace) GlyphCached(r rune) GlyphValue {
	if mask, ok := ff.cache[r]; ok {
		return mask
	}

	dot := fixed.Point26_6{X: 0, Y: ff.Metrics().Ascent}
	rect, mask, _, advance, _ := ff.Glyph(dot, r)
	alpha := mask.(*image.Alpha)

	value := GlyphValue{
		Width:   uint16(rect.Dx()),
		Height:  uint16(rect.Dy()),
		OffsetX: int16(rect.Min.X - dot.X.Round()),
		OffsetY: int16(rect.Min.Y - dot.Y.Round()),
		Advance: advance,
	}

	value.Masks = make([]byte, int(value.Width)*int(value.Height))
	for y := 0; y < rect.Dy(); y++ {
		copy(
			value.Masks[y*rect.Dx():(y+1)*rect.Dx()],
			alpha.Pix[y*alpha.Stride:y*alpha.Stride+rect.Dx()],
		)
	}

	ff.cache[r] = value
	return value
}

func (ff FontFace) HasGlyph(r rune) bool {
	_, ok := ff.GlyphAdvance(r)
	return ok
}

// 把文本 text 按最大宽度切割成子串。
// 返回子串结束点索引（不含此位置），子串宽度。
//
// 注意：这个方法并不在某单一 FontFace 上，原因是字体需要 fallback（回退）。
// 如果一种字体提供不了某一个glyph，则需要用后续字体继续搜索。
func SegmentText(text string, maxWidth int, faces []*FontFace) (int, int, error) {
	var width fixed.Int26_6
	var index int
	for {
		if index == len(text) {
			return index, width.Ceil(), nil
		}
		char, size := utf8.DecodeRuneInString(text[index:])
		if char == utf8.RuneError {
			return 0, 0, fmt.Errorf(`无效字符`)
		}
		// 找哪个字体库提供了此glyph。
		face := faces[0]
		for _, f := range faces {
			if f.HasGlyph(char) {
				face = f
				break
			}
		}
		// NOTE 此处的 MeasureString 方法返回的不是精确整数值（ceil过），
		// 每次只算一个字符然后再在一起作为总宽度可能会导致误差越来越大。
		// TODO 换成 GlyphAdvance
		nextCharWidth := face.MeasureString(text[index : index+size])
		if width+nextCharWidth > fixed.I(maxWidth) {
			return index, width.Ceil(), nil
		}
		width += nextCharWidth
		index += size
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
