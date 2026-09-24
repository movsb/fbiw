package cpu

import (
	"bytes"
	"image"
	"math"
	"testing"
	"unsafe"

	"simd/archsimd"

	"github.com/movsb/fbiw/internal/canvas"
)

// These aliases and the small Canvas harness keep the original optimization
// implementations and benchmark commentary intact while locating them beside
// the software renderer they measure.
type DecodedImage = canvas.Image

type Color uint32

func (c Color) B() uint8      { return uint8(c) }
func (c Color) G() uint8      { return uint8(c >> 8) }
func (c Color) R() uint8      { return uint8(c >> 16) }
func (c Color) A() uint8      { return uint8(c >> 24) }
func (c Color) IsClear() bool { return c == 1 }

type Canvas struct {
	renderer      *Renderer
	x, y          int
	width, height int
	clip          image.Rectangle
}

func testSoftwareCanvas(width, height int, pixels []byte, x, y int) Canvas {
	return Canvas{
		renderer: &Renderer{Width: width, Height: height, Pixels: pixels},
		x:        x, y: y, width: width, height: height,
		clip: image.Rect(0, 0, width, height),
	}
}

func NewCanvas(width, height int) *Canvas {
	c := testSoftwareCanvas(width, height, make([]byte, width*height*4), 0, 0)
	return &c
}

func (c *Canvas) softwarePixels() []byte { return c.renderer.Pixels }
func (c *Canvas) clipBounds() image.Rectangle {
	if c.clip.Empty() {
		return image.Rect(0, 0, c.width, c.height)
	}
	return c.clip
}
func toCanvasImage(img DecodedImage) canvas.Image { return img }

func (c *Canvas) drawImageTransformedCenterSoftware(img DecodedImage, degrees, scale, cx, cy float64) {
	pixels := c.softwarePixels()
	sin, cos := math.Sincos(degrees * math.Pi / 180)
	// 多留一个采样像素，覆盖双线性插值在透明边界的贡献。
	rx := (math.Abs(cos)*float64(img.Width)+math.Abs(sin)*float64(img.Height))*scale/2 + scale
	ry := (math.Abs(sin)*float64(img.Width)+math.Abs(cos)*float64(img.Height))*scale/2 + scale
	sin, cos = sin/scale, cos/scale
	clip := c.clipBounds().Intersect(image.Rect(0, 0, c.width, c.height))
	minX, maxX := max(clip.Min.X, int(math.Floor(cx-rx))), min(clip.Max.X, int(math.Ceil(cx+rx)))
	minY, maxY := max(clip.Min.Y, int(math.Floor(cy-ry))), min(clip.Max.Y, int(math.Ceil(cy+ry)))
	for y := minY; y < maxY; y++ {
		dx, dy := float64(minX)+0.5-cx, float64(y)+0.5-cy
		sx := cos*dx + sin*dy + float64(img.Width)/2 - 0.5
		sy := -sin*dx + cos*dy + float64(img.Height)/2 - 0.5
		for x := minX; x < maxX; x++ {
			ix, iy := int(math.Floor(sx)), int(math.Floor(sy))
			fx, fy := sx-float64(ix), sy-float64(iy)
			var a, blue, green, red float64
			for oy := 0; oy < 2; oy++ {
				py := iy + oy
				if py < 0 || py >= img.Height {
					continue
				}
				wy := 1 - fy
				if oy == 1 {
					wy = fy
				}
				for ox := 0; ox < 2; ox++ {
					px := ix + ox
					if px < 0 || px >= img.Width {
						continue
					}
					wx := 1 - fx
					if ox == 1 {
						wx = fx
					}
					p := img.Pixels[(py*img.Width+px)*4:][:4]
					weightAlpha := wx * wy * float64(p[3]) / 255
					a += weightAlpha
					blue += weightAlpha * float64(p[0])
					green += weightAlpha * float64(p[1])
					red += weightAlpha * float64(p[2])
				}
			}
			if a > 0 {
				p := pixels[(y*c.width+x)*4:][:4]
				p[0] = uint8(math.Round(min(255, blue+(1-a)*float64(p[0]))))
				p[1] = uint8(math.Round(min(255, green+(1-a)*float64(p[1]))))
				p[2] = uint8(math.Round(min(255, red+(1-a)*float64(p[2]))))
				p[3] = 255 // framebuffer 与现有 DrawImage 一样保存不透明混色结果。
			}
			sx += cos
			sy -= sin
		}
	}
}

type imageDrawRegion struct {
	dstX, dstY    int
	srcX, srcY    int
	width, height int
}

// clipImageRegion 同时裁剪源图和 framebuffer；目标左上越界时，
// 必须同步跳过源图左上的像素，否则不仅会切片 panic，图像也会错位。
func (c *Canvas) clipImageRegion(img DecodedImage, srcX, srcY, width, height int) (imageDrawRegion, bool) {
	clip := c.clipBounds()
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
	if r.dstX < clip.Min.X {
		delta := clip.Min.X - r.dstX
		r.dstX = clip.Min.X
		r.srcX += delta
		r.width -= delta
	}
	if r.dstY < clip.Min.Y {
		delta := clip.Min.Y - r.dstY
		r.dstY = clip.Min.Y
		r.srcY += delta
		r.height -= delta
	}
	r.width = min(r.width, clip.Max.X-r.dstX)
	r.height = min(r.height, clip.Max.Y-r.dstY)
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

	pixels := c.softwarePixels()
	for y := range r.height {
		offset := (r.dstY+y)*c.width*4 + r.dstX*4
		dst := pixels[offset:]
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
	pixels := c.softwarePixels()
	for y := range r.height {
		dstOffset := ((r.dstY+y)*c.width + r.dstX) * 4
		srcOffset := ((r.srcY+y)*img.Width + r.srcX) * 4
		dstBytes := pixels[dstOffset : dstOffset+width*4]
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
	c.renderer.DrawImage(toCanvasImage(img), image.Rect(srcX, srcY, srcX+width, srcY+height),
		0, 1, 1, float64(c.x)+float64(width)/2, float64(c.y)+float64(height)/2, 1,
		c.clipBounds(), image.Rectangle{}, 0)
}

func (c *Canvas) drawImage5RegionSoftware(img DecodedImage, srcX, srcY, width, height int) {
	if !img.Opaque {
		c.drawImageSIMDRegion(img, srcX, srcY, width, height, false)
		return
	}

	r, ok := c.clipImageRegion(img, srcX, srcY, width, height)
	if !ok {
		return
	}
	pixels := c.softwarePixels()
	for y := range r.height {
		dstOffset := ((r.dstY+y)*c.width + r.dstX) * 4
		srcOffset := ((r.srcY+y)*img.Width + r.srcX) * 4
		copy(pixels[dstOffset:dstOffset+r.width*4], img.Pixels[srcOffset:srcOffset+r.width*4])
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

	pixels := c.softwarePixels()
	for y := range height {
		dstOffset := ((r.dstY+y)*c.width + r.dstX) * 4
		srcOffset := ((r.srcY+y)*img.Width + r.srcX) * 4
		dstBytes := pixels[dstOffset : dstOffset+width*4]
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

func (c *Canvas) fillRectSoftware(x0, y0, x1, y1 int, color Color) {
	pixels := c.softwarePixels()
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
				line0 = pixels[offset : offset+(x1-x0)*4]
				for i := 0; i < (x1-x0)*4; i += 4 {
					p := pixels[offset+i : offset+i+4]
					*(*uint32)(unsafe.Pointer(&p[0])) = uint32(color)
				}
			} else {
				copy(pixels[offset:], line0)
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
	pixels := c.softwarePixels()
	for yy := y0; yy < y1; yy++ {
		offset := c.width*4*yy + x0*4
		for i := 0; i < (x1-x0)*4; i += 4 {
			p := pixels[offset+i : offset+i+4]
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
	pixels := c.softwarePixels()

	for yy := y0; yy < y1; yy++ {
		offset := (yy*c.width + x0) * 4
		p := pixels[offset : offset+rowBytes]

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
	pixels := c.softwarePixels()

	for yy := y0; yy < y1; yy++ {
		offset := (yy*c.width + x0) * 4
		p := pixels[offset : offset+rowBytes]

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
	pixels := c.softwarePixels()

	for yy := y0; yy < y1; yy++ {
		offset := (yy*c.width + x0) * 4

		for x := range width {
			p := (*uint32)(unsafe.Pointer(&pixels[offset+x*4]))
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
	pixelBuffer := c.softwarePixels()

	for yy := y0; yy < y1; yy++ {
		offset := (yy*c.width + x0) * 4

		row := pixelBuffer[offset : offset+width*4]

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

func TestDiv255(t *testing.T) {
	// 混色公式的分子只可能落在 [0, 255²]。穷举整个有效范围，确保移位
	// 公式与整数除法逐值相等，防止性能优化悄悄改变文字边缘的像素颜色。
	for value := uint32(0); value <= 255*255; value++ {
		if got, want := div255(value), uint8(value/255); got != want {
			t.Fatalf("div255(%d) = %d, want %d", value, got, want)
		}
	}
}

func TestDrawImageVersions(t *testing.T) {
	// 两个版本从相同的非零背景开始，最终 Canvas 必须逐字节完全一致。
	// 图片 Alpha 同时包含全透明、半透明和完全不透明像素。
	const canvasWidth, canvasHeight = 43, 29
	const imageWidth, imageHeight = 37, 23
	image := DecodedImage{
		Pixels: make([]byte, imageWidth*imageHeight*4),
		Width:  imageWidth,
		Height: imageHeight,
	}
	for i := 0; i < len(image.Pixels); i += 4 {
		image.Pixels[i+0] = uint8(i*11 + 3)
		image.Pixels[i+1] = uint8(i*17 + 5)
		image.Pixels[i+2] = uint8(i*23 + 7)
		image.Pixels[i+3] = []uint8{0, 1, 63, 127, 128, 191, 254, 255}[(i/4)%8]
	}

	tests := []struct {
		x, y          int
		width, height int
	}{
		{0, 0, imageWidth, imageHeight},
		{5, 3, 17, 11},
		{canvasWidth - 9, canvasHeight - 7, imageWidth, imageHeight},
		{-10, -5, imageWidth, imageHeight},
		{-imageWidth, 0, imageWidth, imageHeight},
		{0, -imageHeight, imageWidth, imageHeight},
		{0, 0, imageWidth + 10, imageHeight + 10},
		{0, 0, 0, imageHeight},
	}

	for _, tc := range tests {
		buffer1 := make([]byte, canvasWidth*canvasHeight*4)
		for i := range buffer1 {
			buffer1[i] = uint8(i*29 + 13)
		}
		buffer2 := bytes.Clone(buffer1)
		buffer3 := bytes.Clone(buffer1)
		buffer4 := bytes.Clone(buffer1)
		buffer5 := bytes.Clone(buffer1)
		canvas1 := testSoftwareCanvas(canvasWidth, canvasHeight, buffer1, tc.x, tc.y)
		canvas2 := testSoftwareCanvas(canvasWidth, canvasHeight, buffer2, tc.x, tc.y)
		canvas3 := testSoftwareCanvas(canvasWidth, canvasHeight, buffer3, tc.x, tc.y)
		canvas4 := testSoftwareCanvas(canvasWidth, canvasHeight, buffer4, tc.x, tc.y)
		canvas5 := testSoftwareCanvas(canvasWidth, canvasHeight, buffer5, tc.x, tc.y)

		canvas1.drawImage1(image, tc.width, tc.height)
		canvas2.drawImage2(image, tc.width, tc.height)
		canvas3.drawImage3(image, tc.width, tc.height)
		canvas4.drawImage4(image, tc.width, tc.height)
		canvas5.drawImage5(image, tc.width, tc.height)
		if !bytes.Equal(buffer1, buffer2) {
			t.Fatalf("offset=(%d,%d) size=(%d,%d): drawImage1 和 drawImage2 的结果不同", tc.x, tc.y, tc.width, tc.height)
		}
		if !bytes.Equal(buffer1, buffer3) {
			t.Fatalf("offset=(%d,%d) size=(%d,%d): drawImage1 和 drawImage3 的结果不同", tc.x, tc.y, tc.width, tc.height)
		}
		if !bytes.Equal(buffer1, buffer4) {
			t.Fatalf("offset=(%d,%d) size=(%d,%d): drawImage1 和 drawImage4 的结果不同", tc.x, tc.y, tc.width, tc.height)
		}
		// 版本 1-4 保留旧 framebuffer 强制不透明 Alpha 的实现用于历史
		// 性能对照；生产版本 5 已改为标准 Source Over，只比较 RGB。
		if !equalRGB(buffer1, buffer5) {
			t.Fatalf("offset=(%d,%d) size=(%d,%d): drawImage1 和 drawImage5 的 RGB 结果不同", tc.x, tc.y, tc.width, tc.height)
		}
	}
}

func equalRGB(a, b []byte) bool {
	if len(a) != len(b) || len(a)%4 != 0 {
		return false
	}
	for i := 0; i < len(a); i += 4 {
		if !bytes.Equal(a[i:i+3], b[i:i+3]) {
			return false
		}
	}
	return true
}

func TestDrawImage5Opaque(t *testing.T) {
	// Opaque=true 时版本 5 会绕过混色直接复制，结果仍须与基线完全一致。
	const width, height = 19, 13
	image := DecodedImage{
		Pixels: make([]byte, width*height*4),
		Width:  width,
		Height: height,
		Opaque: true,
	}
	for i := 0; i < len(image.Pixels); i += 4 {
		image.Pixels[i+0] = uint8(i*7 + 1)
		image.Pixels[i+1] = uint8(i*11 + 2)
		image.Pixels[i+2] = uint8(i*13 + 3)
		image.Pixels[i+3] = 255
	}
	buffer1 := make([]byte, width*height*4)
	for i := range buffer1 {
		buffer1[i] = uint8(i*17 + 9)
	}
	buffer5 := bytes.Clone(buffer1)
	canvas1 := testSoftwareCanvas(width, height, buffer1, 0, 0)
	canvas5 := testSoftwareCanvas(width, height, buffer5, 0, 0)
	canvas1.drawImage1(image, width, height)
	canvas5.drawImage5(image, width, height)
	if !bytes.Equal(buffer1, buffer5) {
		t.Fatal("不透明图片的 drawImage1 和 drawImage5 结果不同")
	}
}

/*
root@TinaLinux:~# /tmp/fbiw.test \
>   -test.run='^TestDrawImage' \
>   -test.bench='^BenchmarkDrawImage(|Opaque)$' \
>   -test.benchmem
goos: linux
goarch: arm64
pkg: github.com/movsb/fbiw
BenchmarkDrawImage/dev1-4    	      21	  48731653 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawImage/dev2-4    	      46	  25339835 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawImage/dev3-4    	      60	  19420638 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawImage/dev4-4    	      55	  21422946 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawImage/dev5-4    	      61	  19186723 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawImageOpaque/dev1-4         	     123	   9657613 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawImageOpaque/dev2-4         	     211	   5684236 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawImageOpaque/dev3-4         	      61	  19225165 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawImageOpaque/dev4-4         	     278	   4288107 ns/op	       0 B/op	       0 allocs/op
BenchmarkDrawImageOpaque/dev5-4         	     373	   3211028 ns/op	       0 B/op	       0 allocs/op
PASS
*/
func BenchmarkDrawImage(b *testing.B) {
	const width, height = 1024, 768
	image := DecodedImage{
		Pixels: make([]byte, width*height*4),
		Width:  width,
		Height: height,
	}
	for i := 0; i < len(image.Pixels); i += 4 {
		image.Pixels[i+0] = uint8(i*11 + 3)
		image.Pixels[i+1] = uint8(i*17 + 5)
		image.Pixels[i+2] = uint8(i*23 + 7)
		image.Pixels[i+3] = []uint8{0, 1, 63, 127, 128, 191, 254, 255}[(i/4)%8]
	}

	b.Run("dev1", func(b *testing.B) {
		canvas := testSoftwareCanvas(width, height, make([]byte, width*height*4), 0, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			canvas.drawImage1(image, width, height)
		}
	})
	b.Run("dev2", func(b *testing.B) {
		canvas := testSoftwareCanvas(width, height, make([]byte, width*height*4), 0, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			canvas.drawImage2(image, width, height)
		}
	})
	b.Run("dev3", func(b *testing.B) {
		canvas := testSoftwareCanvas(width, height, make([]byte, width*height*4), 0, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			canvas.drawImage3(image, width, height)
		}
	})
	b.Run("dev4", func(b *testing.B) {
		canvas := testSoftwareCanvas(width, height, make([]byte, width*height*4), 0, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			canvas.drawImage4(image, width, height)
		}
	})
	b.Run("dev5", func(b *testing.B) {
		canvas := testSoftwareCanvas(width, height, make([]byte, width*height*4), 0, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			canvas.drawImage5(image, width, height)
		}
	})
}

func BenchmarkDrawImageOpaque(b *testing.B) {
	// 完全不透明图片主要衡量直接复制快速路径，防止 SIMD 混色版本在常见的
	// 无透明通道图片上出现性能倒退。
	const width, height = 1024, 768
	image := DecodedImage{
		Pixels: make([]byte, width*height*4),
		Width:  width,
		Height: height,
		Opaque: true,
	}
	for i := 0; i < len(image.Pixels); i += 4 {
		image.Pixels[i+0] = uint8(i*11 + 3)
		image.Pixels[i+1] = uint8(i*17 + 5)
		image.Pixels[i+2] = uint8(i*23 + 7)
		image.Pixels[i+3] = 255
	}

	versions := []struct {
		name string
		draw func(*Canvas, DecodedImage, int, int)
	}{
		{"dev1", (*Canvas).drawImage1},
		{"dev2", (*Canvas).drawImage2},
		{"dev3", (*Canvas).drawImage3},
		{"dev4", (*Canvas).drawImage4},
		{"dev5", (*Canvas).drawImage5},
	}
	for _, version := range versions {
		b.Run(version.name, func(b *testing.B) {
			canvas := testSoftwareCanvas(width, height, make([]byte, width*height*4), 0, 0)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				version.draw(&canvas, image, width, height)
			}
		})
	}
}

/*

$ GOOS=linux GOARCH=arm64 go test -c -o canvas.test

这个结果只在游戏机上面有差异，macos上很小。

我以为构造表的过程对于小矩形和大矩形的差别可能较大，在实际游戏机上面
测试几乎无异，均只有3~5倍性能提升。

root@TinaLinux:~# ./canvas.test -test.run=^$ -test.bench=BenchmarkFillAlphaBlend -test.benchmem
goos: linux
goarch: arm64
pkg: github.com/movsb/fbiw
BenchmarkFillAlphaBlend/fillAlphaBlend1-4                     22          50758666 ns/op               0 B/op          0 allocs/op
BenchmarkFillAlphaBlend/fillAlphaBlend2-4                     81          13306740 ns/op               0 B/op          0 allocs/op
BenchmarkFillAlphaBlend/fillAlphaBlend3-4                    100          10598213 ns/op               0 B/op          0 allocs/op
BenchmarkFillAlphaBlend/fillAlphaBlend4-4                    132           9045205 ns/op               0 B/op          0 allocs/op
BenchmarkFillAlphaBlend/fillAlphaBlend5-4                    166           7131298 ns/op               0 B/op          0 allocs/op
PASS

*/

func BenchmarkFillAlphaBlend(b *testing.B) {
	const (
		width  = 1024
		height = 768
	)

	color := Color(0x8000ff00)

	b.Run("fillAlphaBlend_baseline", func(b *testing.B) {
		canvas := NewCanvas(width, height)

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			fillAlphaBlend1(canvas, color, 0, width, 0, height)
		}
	})

	b.Run("fillAlphaBlend_lut", func(b *testing.B) {
		canvas := NewCanvas(width, height)

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			fillAlphaBlend2(canvas, color, 0, width, 0, height)
		}
	})

	b.Run("fillAlphaBlend_lut&/256", func(b *testing.B) {
		canvas := NewCanvas(width, height)

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			fillAlphaBlend3(canvas, color, 0, width, 0, height)
		}
	})

	b.Run("fillAlphaBlend_uint32&swar&/256", func(b *testing.B) {
		canvas := NewCanvas(width, height)

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			fillAlphaBlend4(canvas, color, 0, width, 0, height)
		}
	})
	b.Run("fillAlphaBlend_simd", func(b *testing.B) {
		canvas := NewCanvas(width, height)

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			fillAlphaBlend5(canvas, color, 0, width, 0, height)
		}
	})
}
