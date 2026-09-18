package fbiw

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"testing/fstest"

	"github.com/movsb/fbiw/internal/canvas"
	"github.com/movsb/fbiw/internal/canvas/cpu"
)

// softwarePixels exposes the CPU framebuffer only to root-package tests.
func (c *Canvas) softwarePixels() []byte { return c.renderer.(*cpu.Renderer).Pixels }

type recordingCanvasRenderer struct {
	width, height int
	fillRectRect  image.Rectangle
	fillRectClip  image.Rectangle
	imageSrc      image.Rectangle
	imageDst      image.Point
	imageClip     image.Rectangle
}

func testSoftwareCanvas(width, height int, x, y int) *Canvas {
	c := NewCanvas(cpu.New(width, height))
	c.x, c.y = x, y
	return c
}

func (r *recordingCanvasRenderer) Size() (int, int) { return r.width, r.height }
func (*recordingCanvasRenderer) BeginFrame()        {}
func (*recordingCanvasRenderer) EndFrame()          {}
func (*recordingCanvasRenderer) Clear()             {}
func (r *recordingCanvasRenderer) FillRect(rect, clip image.Rectangle, _ canvas.Color) {
	r.fillRectRect, r.fillRectClip = rect, clip
}
func (r *recordingCanvasRenderer) DrawImage(_ canvas.Image, src image.Rectangle, dst image.Point, clip image.Rectangle) {
	r.imageSrc, r.imageDst, r.imageClip = src, dst, clip
}
func (*recordingCanvasRenderer) DrawImageTransformed(canvas.Image, float64, float64, float64, float64, image.Rectangle) {
}
func (*recordingCanvasRenderer) DrawMask([]byte, int, int, image.Point, image.Rectangle, canvas.Color) {
}
func (*recordingCanvasRenderer) Pixel(image.Point) color.NRGBA     { return color.NRGBA{} }
func (*recordingCanvasRenderer) SetPixel(image.Point, color.NRGBA) {}
func (r *recordingCanvasRenderer) Snapshot() image.Image {
	return image.NewNRGBA(image.Rect(0, 0, r.width, r.height))
}
func (r *recordingCanvasRenderer) Close() error { return nil }

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
	canvas.DrawImageRegion(img, 1, 2, 3, 2)
	if renderer.imageSrc != image.Rect(1, 2, 4, 4) {
		t.Fatalf("DrawImageRegion source = %v", renderer.imageSrc)
	}
	if renderer.imageDst != image.Pt(5, 3) || renderer.imageClip != wantClip {
		t.Fatalf("DrawImageRegion dst/clip = %v/%v, want %v/%v", renderer.imageDst, renderer.imageClip, image.Pt(5, 3), wantClip)
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

	untrimmed, err := manager.GetImageCached(fsys, "border.png", ImageDecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if untrimmed.Width != 6 || untrimmed.Height != 5 {
		t.Fatalf("未开启裁剪时尺寸错误：%dx%d", untrimmed.Width, untrimmed.Height)
	}

	trimmed, err := manager.GetImageCached(fsys, "border.png", ImageDecodeOptions{
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

func TestDrawImageRegion(t *testing.T) {
	img := DecodedImage{Width: 4, Height: 3, Pixels: make([]byte, 4*3*4), Opaque: true}
	for i := range 12 {
		img.Pixels[i*4] = byte(i + 1)
		img.Pixels[i*4+3] = 255
	}
	canvas := testSoftwareCanvas(3, 2, 0, 0)
	canvas.DrawImageRegion(img, 1, 1, 3, 2)

	for y := range 2 {
		for x := range 3 {
			got := canvas.softwarePixels()[(y*3+x)*4]
			want := byte((y+1)*4 + (x + 1) + 1)
			if got != want {
				t.Fatalf("pixel (%d,%d)=%d, want %d", x, y, got, want)
			}
		}
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
