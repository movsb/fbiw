package fbiw

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"testing"
	"testing/fstest"
	"time"

	"github.com/movsb/fbiw/internal/canvas"
	"github.com/movsb/fbiw/internal/canvas/cpu"
)

// softwarePixels exposes the CPU framebuffer only to root-package tests.
func (c *Canvas) softwarePixels() []byte { return c.renderer.(*cpu.Renderer).Pixels }

type recordingCanvasRenderer struct {
	width, height                      int
	fillRectRect                       image.Rectangle
	fillRectClip                       image.Rectangle
	imageSrc                           image.Rectangle
	imageDst                           image.Point
	imageClip                          image.Rectangle
	transformScaleX, transformScaleY   float64
	transformCenterX, transformCenterY float64
}

func (r *recordingCanvasRenderer) Size() (int, int) { return r.width, r.height }
func (r *recordingCanvasRenderer) Resize(width, height int) error {
	r.width, r.height = width, height
	return nil
}
func (*recordingCanvasRenderer) BeginFrame() {}
func (*recordingCanvasRenderer) EndFrame()   {}
func (*recordingCanvasRenderer) Clear()      {}
func (r *recordingCanvasRenderer) FillRect(rect, clip image.Rectangle, _ canvas.Color) {
	r.fillRectRect, r.fillRectClip = rect, clip
}
func (r *recordingCanvasRenderer) DrawImage(_ canvas.Image, src image.Rectangle, _ float64, scaleX, scaleY, cx, cy float64, clip, _ image.Rectangle, _ float64) {
	r.imageSrc, r.imageDst, r.imageClip = src, image.Pt(int(cx)-src.Dx()/2, int(cy)-src.Dy()/2), clip
	r.transformScaleX, r.transformScaleY = scaleX, scaleY
	r.transformCenterX, r.transformCenterY = cx, cy
}
func (*recordingCanvasRenderer) DrawMask([]byte, int, int, image.Point, image.Rectangle, canvas.Color) {
}
func (*recordingCanvasRenderer) Pixel(image.Point) color.NRGBA     { return color.NRGBA{} }
func (*recordingCanvasRenderer) SetPixel(image.Point, color.NRGBA) {}
func (r *recordingCanvasRenderer) Snapshot() image.Image {
	return image.NewNRGBA(image.Rect(0, 0, r.width, r.height))
}
func (r *recordingCanvasRenderer) Close() error { return nil }

func TestCanvasResizeUpdatesRendererAndViewport(t *testing.T) {
	renderer := &recordingCanvasRenderer{width: 20, height: 12}
	canvas := NewCanvas(renderer)

	if err := canvas.resize(31, 19); err != nil {
		t.Fatal(err)
	}
	if renderer.width != 31 || renderer.height != 19 {
		t.Fatalf("renderer size = %dx%d, want 31x19", renderer.width, renderer.height)
	}
	if canvas.width != 31 || canvas.height != 19 {
		t.Fatalf("canvas size = %dx%d, want 31x19", canvas.width, canvas.height)
	}
	if canvas.clip != image.Rect(0, 0, 31, 19) {
		t.Fatalf("canvas clip = %v", canvas.clip)
	}
}

func TestCanvasDelegatesAbsoluteCoordinatesAndClip(t *testing.T) {
	renderer := &recordingCanvasRenderer{width: 20, height: 12}
	root := NewCanvas(renderer)
	canvas := root.Offset(3, 2).Clip(1, 1, 8, 6).Offset(2, 1)

	if root.renderer != canvas.renderer {
		t.Fatal("derived Canvas does not share its renderer")
	}

	canvas.FillRect(-10, -10, 20, 20, ColorFromString("red"))
	wantClip := image.Rect(4, 3, 12, 9)
	if renderer.fillRectRect != wantClip || renderer.fillRectClip != wantClip {
		t.Fatalf("FillRect rect/clip = %v/%v, want %v", renderer.fillRectRect, renderer.fillRectClip, wantClip)
	}

	img := DecodedImage{Width: 5, Height: 4, Pixels: make([]byte, 5*4*4)}
	canvas.DrawImage(img, 0, 1, 1, 2.5, 2)
	if renderer.imageSrc != image.Rect(0, 0, 5, 4) {
		t.Fatalf("DrawImage source = %v", renderer.imageSrc)
	}
	if renderer.imageDst != image.Pt(5, 3) || renderer.imageClip != wantClip {
		t.Fatalf("DrawImage dst/clip = %v/%v, want %v/%v", renderer.imageDst, renderer.imageClip, image.Pt(5, 3), wantClip)
	}
}

func TestDecodeImageTrimTransparentBorder(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 6, 5))
	source.SetNRGBA(2, 1, color.NRGBA{R: 10, G: 20, B: 30, A: 128})
	source.SetNRGBA(3, 1, color.NRGBA{R: 40, G: 50, B: 60, A: 255})
	source.SetNRGBA(2, 2, color.NRGBA{R: 70, G: 80, B: 90, A: 255})
	source.SetNRGBA(3, 2, color.NRGBA{R: 100, G: 110, B: 120, A: 255})

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	mapFS := fstest.MapFS{"border.png": &fstest.MapFile{Data: encoded.Bytes()}}
	fsys := &mapFS
	manager := NewImageManager()

	untrimmed, err := manager.Load(fsys, "border.png", false, ImageDecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if untrimmed.Width != 6 || untrimmed.Height != 5 {
		t.Fatalf("未开启裁剪时尺寸错误：%dx%d", untrimmed.Width, untrimmed.Height)
	}

	trimmed, err := manager.Load(fsys, "border.png", false, ImageDecodeOptions{
		TrimTransparentBorder: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if trimmed.Width != 2 || trimmed.Height != 2 {
		t.Fatalf("裁剪后尺寸错误：%dx%d", trimmed.Width, trimmed.Height)
	}
	if got := trimmed.Pixels[:4]; !bytes.Equal(got, []byte{30, 20, 10, 128}) {
		t.Fatalf("裁剪后的首个像素错误：%v", got)
	}
	if trimmed.Opaque {
		t.Fatal("裁剪不应改变内容像素的半透明状态")
	}

}

func TestTrimTransparentBorderFullyTransparent(t *testing.T) {
	trimmed := trimTransparentBorder(image.NewNRGBA(image.Rect(0, 0, 8, 6)))
	if trimmed.Bounds() != image.Rect(0, 0, 1, 1) {
		t.Fatalf("全透明图片应保留一个透明像素：%v", trimmed.Bounds())
	}
	_, _, _, alpha := trimmed.At(0, 0).RGBA()
	if alpha != 0 {
		t.Fatalf("保留的像素应为全透明：alpha=%d", alpha)
	}
}

func trimTransparentBorder(img image.Image) image.Image {
	return trimImageBorder(img, func(_, _, _, alpha uint32) bool { return alpha == 0 }, color.NRGBA{})
}

func TestDecodeImageTrimBlackBorder(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 7, 6))
	for y := range 6 {
		for x := range 7 {
			source.SetNRGBA(x, y, color.NRGBA{A: 255})
		}
	}
	source.SetNRGBA(2, 1, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	source.SetNRGBA(3, 1, color.NRGBA{R: 40, G: 50, B: 60, A: 255})
	source.SetNRGBA(2, 2, color.NRGBA{R: 70, G: 80, B: 90, A: 255})
	source.SetNRGBA(3, 2, color.NRGBA{R: 100, G: 110, B: 120, A: 255})

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	manager := NewImageManager()
	mapFS := fstest.MapFS{"black.png": &fstest.MapFile{Data: encoded.Bytes()}}
	trimmed, err := manager.Load(
		&mapFS, "black.png", false, ImageDecodeOptions{TrimBlackBorder: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if trimmed.Width != 2 || trimmed.Height != 2 {
		t.Fatalf("裁剪后尺寸错误：%dx%d", trimmed.Width, trimmed.Height)
	}
	if got := trimmed.Pixels[:4]; !bytes.Equal(got, []byte{30, 20, 10, 255}) {
		t.Fatalf("裁剪后的首个像素错误：%v", got)
	}
}

func TestTrimBlackBorderFullyBlack(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for i := 3; i < len(source.Pix); i += 4 {
		source.Pix[i] = 255
	}
	trimmed := trimImageBorder(source,
		func(r, g, b, a uint32) bool { return r == 0 && g == 0 && b == 0 && a == 0xffff },
		color.NRGBA{A: 255},
	)
	if trimmed.Bounds() != image.Rect(0, 0, 1, 1) {
		t.Fatalf("全黑图片应保留一个黑色像素：%v", trimmed.Bounds())
	}
	_, _, _, alpha := trimmed.At(0, 0).RGBA()
	if alpha != 0xffff {
		t.Fatalf("保留的黑色像素应不透明：alpha=%d", alpha)
	}
}

func TestDrawImageUsesRendererTransform(t *testing.T) {
	renderer := &recordingCanvasRenderer{width: 100, height: 80}
	canvas := NewCanvas(renderer).Offset(7, 9)
	canvas.DrawImage(DecodedImage{Width: 20, Height: 10, Pixels: make([]byte, 20*10*4)}, 0, 2.5, 3, 25, 15)
	if renderer.transformScaleX != 2.5 || renderer.transformScaleY != 3 {
		t.Fatalf("scale = %v,%v", renderer.transformScaleX, renderer.transformScaleY)
	}
	if renderer.transformCenterX != 32 || renderer.transformCenterY != 24 {
		t.Fatalf("center = %v,%v", renderer.transformCenterX, renderer.transformCenterY)
	}
}

func TestCanvasClipLimitsDrawing(t *testing.T) {
	canvas := NewCanvas(cpu.New(10, 10))
	clipped := canvas.Clip(2, 3, 4, 2)
	clipped.FillRect(0, 0, 10, 10, ColorFromRGBA(255, 255, 255, 255))

	for y := range 10 {
		for x := range 10 {
			painted := canvas.softwarePixels()[(y*10+x)*4+3] != 0
			want := x >= 2 && x < 6 && y >= 3 && y < 5
			if painted != want {
				t.Fatalf(`pixel (%d,%d) painted=%t, want %t`, x, y, painted, want)
			}
		}
	}
}

func TestCanvasRoundedClipLimitsImage(t *testing.T) {
	canvas := NewCanvas(cpu.New(8, 8))
	pixels := make([]byte, 8*8*4)
	for i := range 8 * 8 {
		pixels[i*4+0] = 255
		pixels[i*4+1] = 255
		pixels[i*4+2] = 255
		pixels[i*4+3] = 255
	}
	canvas.ClipRounded(0, 0, 8, 8, 3).DrawImage(
		DecodedImage{Width: 8, Height: 8, Pixels: pixels},
		0, 1, 1, 4, 4,
	)

	if got := canvas.testGetPixel(0, 0); got.A != 0 {
		t.Fatalf("rounded image corner alpha = %d, want 0", got.A)
	}
	if got := canvas.testGetPixel(4, 4); got.A != 255 {
		t.Fatalf("rounded image center alpha = %d, want 255", got.A)
	}

	plain := NewCanvas(cpu.New(8, 8))
	plain.ClipRounded(0, 0, 8, 8, 0).DrawImage(
		DecodedImage{Width: 8, Height: 8, Pixels: pixels},
		0, 1, 1, 4, 4,
	)
	if got := plain.testGetPixel(0, 0); got.A != 255 {
		t.Fatalf("zero-radius image corner alpha = %d, want 255", got.A)
	}
}

func TestDrawRoundedRect(t *testing.T) {
	canvas := NewCanvas(cpu.New(9, 9)).Offset(1, 1)
	fill := ColorFromRGBA(0, 255, 0, 255)
	border := ColorFromRGBA(255, 0, 0, 255)
	canvas.DrawRect(0, 0, 7, 7, 3, 1, fill, border)

	if got := canvas.testGetPixel(0, 0); got.A != 0 {
		t.Fatalf("corner alpha = %d, want 0", got.A)
	}
	if got := canvas.testGetPixel(3, 0); got.R == 0 || got.G != 0 {
		t.Fatalf("border pixel = %v", got)
	}
	if got := canvas.testGetPixel(3, 3); got.G == 0 || got.R != 0 {
		t.Fatalf("fill pixel = %v", got)
	}
}

func TestDecodedPixelsFastPathWithSubimage(t *testing.T) {
	for _, source := range []image.Image{
		image.NewNRGBA(image.Rect(0, 0, 3, 2)),
		image.NewRGBA(image.Rect(0, 0, 3, 2)),
	} {
		switch img := source.(type) {
		case *image.NRGBA:
			img.SetNRGBA(1, 1, color.NRGBA{R: 12, G: 34, B: 56, A: 255})
			source = img.SubImage(image.Rect(1, 1, 2, 2))
		case *image.RGBA:
			img.SetRGBA(1, 1, color.RGBA{R: 12, G: 34, B: 56, A: 255})
			source = img.SubImage(image.Rect(1, 1, 2, 2))
		}
		got := decodedPixels(source)
		if got.Width != 1 || got.Height != 1 || !got.Opaque || !bytes.Equal(got.Pixels, []byte{56, 34, 12, 255}) {
			t.Fatalf("%T: %+v", source, got)
		}
	}
}

func TestDecodedPixelsPalettedOffset(t *testing.T) {
	img := image.NewPaletted(image.Rect(4, 5, 5, 6), color.Palette{color.NRGBA{R: 12, G: 34, B: 56, A: 128}})
	got := decodedPixels(img)
	if got.Width != 1 || got.Height != 1 || got.Opaque || !bytes.Equal(got.Pixels, []byte{56, 34, 12, 128}) {
		t.Fatalf("paletted offset: %+v", got)
	}
}

func TestDrawGIFFrameMatchesDrawOver(t *testing.T) {
	palette := color.Palette{
		color.NRGBA{},
		color.NRGBA{R: 240, G: 20, B: 30, A: 255},
		color.NRGBA{R: 30, G: 210, B: 90, A: 128},
	}
	paletted := image.NewPaletted(image.Rect(1, 1, 5, 3), palette)
	copy(paletted.Pix, []byte{0, 1, 2, 1, 2, 0, 1, 2})
	nrgba := image.NewNRGBA(image.Rect(0, 0, 6, 4))
	for y := 1; y < 3; y++ {
		for x := 1; x < 5; x++ {
			nrgba.SetNRGBA(x, y, palette[(x+y)%len(palette)].(color.NRGBA))
		}
	}
	for _, source := range []image.Image{paletted, nrgba.SubImage(image.Rect(1, 1, 5, 3))} {
		got := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		want := image.NewNRGBA(got.Bounds())
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				c := color.NRGBA{R: 60, G: 100, B: 170, A: uint8(80 + x*25)}
				got.SetNRGBA(x, y, c)
				want.SetNRGBA(x, y, c)
			}
		}
		drawGIFFrame(got, source)
		r := source.Bounds().Intersect(want.Bounds())
		draw.Draw(want, r, source, r.Min, draw.Over)
		for i := range got.Pix {
			delta := int(got.Pix[i]) - int(want.Pix[i])
			if delta < -1 || delta > 1 {
				t.Fatalf("%T byte %d: got %d, want %d", source, i, got.Pix[i], want.Pix[i])
			}
		}
	}
}

func BenchmarkGIFFrameComposite(b *testing.B) {
	palette := color.Palette{color.NRGBA{}, color.NRGBA{R: 255, G: 90, B: 40, A: 255}}
	frame := image.NewPaletted(image.Rect(0, 0, 256, 256), palette)
	for i := range frame.Pix {
		frame.Pix[i] = uint8(i % 2)
	}
	for _, tc := range []struct {
		name string
		draw func(*image.NRGBA)
	}{
		{"standard", func(dst *image.NRGBA) { draw.Draw(dst, frame.Bounds(), frame, frame.Bounds().Min, draw.Over) }},
		{"specialized", func(dst *image.NRGBA) { drawGIFFrame(dst, frame) }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			dst := image.NewNRGBA(frame.Bounds())
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tc.draw(dst)
			}
		})
	}
}

func TestDecodeGIFFrames(t *testing.T) {
	palette := color.Palette{color.NRGBA{}, color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255}}
	frame := func(rect image.Rectangle, pixels []uint8) *image.Paletted {
		p := image.NewPaletted(rect, palette)
		copy(p.Pix, pixels)
		return p
	}
	g := &gif.GIF{Image: []*image.Paletted{
		frame(image.Rect(0, 0, 3, 1), []uint8{1, 1, 1}),
		frame(image.Rect(1, 0, 2, 1), []uint8{2}),
		frame(image.Rect(2, 0, 3, 1), []uint8{2}),
		frame(image.Rect(0, 0, 1, 1), []uint8{2}),
	}, Delay: []int{2, 3, 4, 0}, Disposal: []byte{gif.DisposalNone, gif.DisposalPrevious, gif.DisposalBackground, gif.DisposalNone}, Config: image.Config{Width: 3, Height: 1, ColorModel: palette}}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, g); err != nil {
		t.Fatal(err)
	}
	got, err := decodeGIF(fstest.MapFS{"a.gif": &fstest.MapFile{Data: encoded.Bytes()}}, "a.gif")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.frames) != 4 || got.delays[0] != 20*time.Millisecond || got.delays[3] != 10*time.Millisecond {
		t.Fatalf("frames/delays: %d %v", len(got.frames), got.delays)
	}
	for i, want := range [][]byte{{1, 1, 1}, {1, 2, 1}, {1, 1, 2}, {2, 1, 0}} {
		for x, index := range want {
			p := got.frames[i].Pixels[x*4 : x*4+4]
			if index == 0 && p[3] != 0 || index == 1 && (p[2] != 255 || p[3] != 255) || index == 2 && (p[1] != 255 || p[3] != 255) {
				t.Fatalf("frame %d pixel %d = %v", i, x, p)
			}
		}
	}
}

func TestSingleFrameGIFAndStaticPNG(t *testing.T) {
	palette := color.Palette{color.NRGBA{}, color.NRGBA{R: 255, A: 255}}
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
	frame.Pix[0] = 1
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &gif.GIF{Image: []*image.Paletted{frame}, Delay: []int{1}, Config: image.Config{Width: 1, Height: 1, ColorModel: palette}}); err != nil {
		t.Fatal(err)
	}
	fsy := fstest.MapFS{"one.gif": &fstest.MapFile{Data: encoded.Bytes()}}
	g, err := decodeGIF(fsy, "one.gif")
	if err != nil || len(g.frames) != 1 {
		t.Fatalf("single frame: %v %v", g, err)
	}
	_, doc, _ := newAnimationTestApp(t)
	defer doc.Close()
	img := NewImage(doc)
	img.gif, img.decodedImage = g, g.frames[0]
	img.startGIF()
	if len(doc.timers) != 0 {
		t.Fatal("single frame scheduled playback")
	}
	encoded.Reset()
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := NewImageManager().Load(&fstest.MapFS{"still.png": &fstest.MapFile{Data: encoded.Bytes()}}, "still.png", false, ImageDecodeOptions{}); err != nil {
		t.Fatal(err)
	}
}
