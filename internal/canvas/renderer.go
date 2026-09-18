package canvas

import (
	"image"
	"image/color"
)

// Color uses the same little-endian BGRA word layout as fbiw.Color.
type Color uint32

func (c Color) B() uint8      { return uint8(c) }
func (c Color) G() uint8      { return uint8(c >> 8) }
func (c Color) R() uint8      { return uint8(c >> 16) }
func (c Color) A() uint8      { return uint8(c >> 24) }
func (c Color) IsClear() bool { return c == 1 }

type Image struct {
	Pixels        []byte
	Width, Height int
	Opaque        bool
}

type Renderer interface {
	Size() (int, int)
	BeginFrame()
	EndFrame()
	Clear()
	FillRect(rect, clip image.Rectangle, color Color)
	DrawImage(img Image, src image.Rectangle, dst image.Point, clip image.Rectangle)
	DrawImageTransformed(img Image, degrees, scale, cx, cy float64, clip image.Rectangle)
	DrawMask(mask []byte, maskWidth, maskHeight int, dst image.Point, clip image.Rectangle, color Color)
	Pixel(point image.Point) color.NRGBA
	SetPixel(point image.Point, color color.NRGBA)
	Snapshot() image.Image
}
