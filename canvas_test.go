package fbiw

import (
	"bytes"
	"testing"
	"testing/fstest"

	"golang.org/x/image/font/gofont/goregular"
)

func TestDiv255(t *testing.T) {
	// 混色公式的分子只可能落在 [0, 255²]。穷举整个有效范围，确保移位
	// 公式与整数除法逐值相等，防止性能优化悄悄改变文字边缘的像素颜色。
	for value := uint32(0); value <= 255*255; value++ {
		if got, want := div255(value), uint8(value/255); got != want {
			t.Fatalf("div255(%d) = %d, want %d", value, got, want)
		}
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
				canvas1 := Canvas{buffer: buffer1, x: offset.x, y: offset.y, width: width, height: height}
				canvas2 := Canvas{buffer: buffer2, x: offset.x, y: offset.y, width: width, height: height}

				canvas1.drawStringDevice1(text, []*FontFace{face}, color, width, height)
				canvas2.drawStringDevice2(text, []*FontFace{face}, color, width, height)
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
			canvas.drawStringStd(`Canvas text rendering benchmark`, []*FontFace{face}, ColorValueFromString(`red`).Color, 1024, 768)
		}
	})
	b.Run(`dev1`, func(b *testing.B) {
		canvas := Canvas{
			buffer: make([]byte, 1024*768*4),
			width:  1024,
			height: 768,
		}
		for b.Loop() {
			canvas.drawStringDevice1(`Canvas text rendering benchmark`, []*FontFace{face}, ColorValueFromString(`red`).Color, 1024, 768)
		}
	})
	b.Run(`dev2`, func(b *testing.B) {
		canvas := Canvas{
			buffer: make([]byte, 1024*768*4),
			width:  1024,
			height: 768,
		}
		for b.Loop() {
			canvas.drawStringDevice2(`Canvas text rendering benchmark`, []*FontFace{face}, ColorValueFromString(`red`).Color, 1024, 768)
		}
	})
}
