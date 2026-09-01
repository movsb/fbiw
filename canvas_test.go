package fbiw

import (
	"bytes"
	"image"
	"testing"
	"testing/fstest"

	"golang.org/x/image/font/gofont/goregular"
)

func TestCanvasImageAlwaysRepresentsEntireFramebuffer(t *testing.T) {
	const width, height = 160, 48
	tests := []struct {
		x, y                       int
		wantImage, wantFramebuffer image.Rectangle
	}{
		{0, 0, image.Rect(0, 0, width, height), image.Rect(0, 0, width, height)},
		{-20, -10, image.Rect(0, 0, width, height), image.Rect(20, 10, width+20, height+10)},
		{20, 10, image.Rect(0, 0, width, height), image.Rect(-20, -10, width-20, height-10)},
		{width, height, image.Rect(0, 0, width, height), image.Rect(-width, -height, 0, 0)},
	}

	for _, tt := range tests {
		canvas := Canvas{
			buffer: make([]byte, width*height*4),
			x:      tt.x, y: tt.y,
			width: width, height: height,
		}
		if got := canvas.framebuffer().Bounds(); got != tt.wantImage {
			t.Errorf("offset=(%d,%d): Image.Bounds()=%v, want %v", tt.x, tt.y, got, tt.wantImage)
		}
		if got := canvas.drawable().Bounds(); got != tt.wantFramebuffer {
			t.Errorf("offset=(%d,%d): framebuffer Bounds()=%v, want %v", tt.x, tt.y, got, tt.wantFramebuffer)
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
		canvas1 := Canvas{buffer: buffer1, x: tc.x, y: tc.y, width: canvasWidth, height: canvasHeight}
		canvas2 := Canvas{buffer: buffer2, x: tc.x, y: tc.y, width: canvasWidth, height: canvasHeight}
		canvas3 := Canvas{buffer: buffer3, x: tc.x, y: tc.y, width: canvasWidth, height: canvasHeight}
		canvas4 := Canvas{buffer: buffer4, x: tc.x, y: tc.y, width: canvasWidth, height: canvasHeight}
		canvas5 := Canvas{buffer: buffer5, x: tc.x, y: tc.y, width: canvasWidth, height: canvasHeight}

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
		if !bytes.Equal(buffer1, buffer5) {
			t.Fatalf("offset=(%d,%d) size=(%d,%d): drawImage1 和 drawImage5 的结果不同", tc.x, tc.y, tc.width, tc.height)
		}
	}
}

func TestDrawImageRegion(t *testing.T) {
	img := DecodedImage{Width: 4, Height: 3, Pixels: make([]byte, 4*3*4), Opaque: true}
	for i := 0; i < 12; i++ {
		img.Pixels[i*4] = byte(i + 1)
		img.Pixels[i*4+3] = 255
	}
	canvas := Canvas{buffer: make([]byte, 3*2*4), width: 3, height: 2}
	canvas.DrawImageRegion(img, 1, 1, 3, 2)

	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			got := canvas.buffer[(y*3+x)*4]
			want := byte((y+1)*4 + (x + 1) + 1)
			if got != want {
				t.Fatalf("pixel (%d,%d)=%d, want %d", x, y, got, want)
			}
		}
	}
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
	canvas1 := Canvas{buffer: buffer1, width: width, height: height}
	canvas5 := Canvas{buffer: buffer5, width: width, height: height}
	canvas1.drawImage1(image, width, height)
	canvas5.drawImage5(image, width, height)
	if !bytes.Equal(buffer1, buffer5) {
		t.Fatal("不透明图片的 drawImage1 和 drawImage5 结果不同")
	}
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
				canvas1 := Canvas{buffer: buffer1, x: offset.x, y: offset.y, width: width, height: height}
				canvas2 := Canvas{buffer: buffer2, x: offset.x, y: offset.y, width: width, height: height}
				canvas3 := Canvas{buffer: buffer3, x: offset.x, y: offset.y, width: width, height: height}

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
		canvas := Canvas{buffer: make([]byte, width*height*4), width: width, height: height}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			canvas.drawImage1(image, width, height)
		}
	})
	b.Run("dev2", func(b *testing.B) {
		canvas := Canvas{buffer: make([]byte, width*height*4), width: width, height: height}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			canvas.drawImage2(image, width, height)
		}
	})
	b.Run("dev3", func(b *testing.B) {
		canvas := Canvas{buffer: make([]byte, width*height*4), width: width, height: height}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			canvas.drawImage3(image, width, height)
		}
	})
	b.Run("dev4", func(b *testing.B) {
		canvas := Canvas{buffer: make([]byte, width*height*4), width: width, height: height}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			canvas.drawImage4(image, width, height)
		}
	})
	b.Run("dev5", func(b *testing.B) {
		canvas := Canvas{buffer: make([]byte, width*height*4), width: width, height: height}
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
			canvas := Canvas{buffer: make([]byte, width*height*4), width: width, height: height}
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
		canvas := &Canvas{
			buffer: make([]byte, width*height*4),
			width:  width,
			height: height,
		}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			fillAlphaBlend1(canvas, color, 0, width, 0, height)
		}
	})

	b.Run("fillAlphaBlend_lut", func(b *testing.B) {
		canvas := &Canvas{
			buffer: make([]byte, width*height*4),
			width:  width,
			height: height,
		}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			fillAlphaBlend2(canvas, color, 0, width, 0, height)
		}
	})

	b.Run("fillAlphaBlend_lut&/256", func(b *testing.B) {
		canvas := &Canvas{
			buffer: make([]byte, width*height*4),
			width:  width,
			height: height,
		}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			fillAlphaBlend3(canvas, color, 0, width, 0, height)
		}
	})

	b.Run("fillAlphaBlend_uint32&swar&/256", func(b *testing.B) {
		canvas := &Canvas{
			buffer: make([]byte, width*height*4),
			width:  width,
			height: height,
		}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			fillAlphaBlend4(canvas, color, 0, width, 0, height)
		}
	})
	b.Run("fillAlphaBlend_simd", func(b *testing.B) {
		canvas := &Canvas{
			buffer: make([]byte, width*height*4),
			width:  width,
			height: height,
		}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			fillAlphaBlend5(canvas, color, 0, width, 0, height)
		}
	})
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
		canvas := Canvas{
			buffer: make([]byte, 1024*768*4),
			width:  1024,
			height: 768,
		}
		for b.Loop() {
			canvas.drawStringStd(`Canvas text rendering benchmark`, []*FontFace{face}, ColorValueFromString(`red`).Color())
		}
	})
	b.Run(`dev1`, func(b *testing.B) {
		canvas := Canvas{
			buffer: make([]byte, 1024*768*4),
			width:  1024,
			height: 768,
		}
		for b.Loop() {
			canvas.drawStringDevice1(`Canvas text rendering benchmark`, []*FontFace{face}, ColorValueFromString(`red`).Color())
		}
	})
	b.Run(`dev2`, func(b *testing.B) {
		canvas := Canvas{
			buffer: make([]byte, 1024*768*4),
			width:  1024,
			height: 768,
		}
		for b.Loop() {
			canvas.drawStringDevice2(`Canvas text rendering benchmark`, []*FontFace{face}, ColorValueFromString(`red`).Color())
		}
	})
}
