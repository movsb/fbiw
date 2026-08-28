package fbiw

import (
	"testing"
	"testing/fstest"

	"golang.org/x/image/font/gofont/goregular"
)

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
	b.Run(`dev`, func(b *testing.B) {
		canvas := Canvas{
			buffer: make([]byte, 1024*768*4),
			width:  1024,
			height: 768,
		}
		for b.Loop() {
			canvas.drawStringDevice(`Canvas text rendering benchmark`, []*FontFace{face}, ColorValueFromString(`red`).Color, 1024, 768)
		}
	})
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
}
