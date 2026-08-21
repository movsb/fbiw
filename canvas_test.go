package fbiw

import "testing"

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
