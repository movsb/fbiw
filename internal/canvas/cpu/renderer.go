package cpu

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"unsafe"

	canvas "github.com/movsb/fbiw/internal/canvas"
)

type Display interface {
	Sync(pixels []byte)
	Close()
}

type Renderer struct {
	Width, Height int
	Pixels        []byte
	Display       Display
}

var _ interface {
	canvas.Renderer
	canvas.TestRenderer
} = (*Renderer)(nil)

// 创建一个纯CPU的渲染器。
//
// 渲染器只负责在内存中渲染，如果需要显示，需要设置 [Renderer.Display] 接口。
func New(width, height int) *Renderer {
	return &Renderer{
		Width:  width,
		Height: height,
		Pixels: make([]byte, width*height*4),
	}
}

func (r *Renderer) Close() error {
	if r.Display != nil {
		r.Display.Close()
	}
	return nil
}

func (r *Renderer) Size() (int, int) {
	return r.Width, r.Height
}

func (r *Renderer) Resize(width, height int) error {
	if width <= 0 || height <= 0 || width == r.Width && height == r.Height {
		return nil
	}
	if r.Display != nil {
		display, ok := r.Display.(interface {
			Resize(width, height int) error
		})
		if !ok {
			return fmt.Errorf("display does not support resizing")
		}
		if err := display.Resize(width, height); err != nil {
			return err
		}
	}
	r.Width, r.Height = width, height
	r.Pixels = make([]byte, width*height*4)
	return nil
}

func (*Renderer) BeginFrame() {}

func (r *Renderer) EndFrame() {
	if r.Display != nil {
		r.Display.Sync(r.Pixels)
	}
}

func (r *Renderer) Clear() {
	clear(r.Pixels)
}

func (r *Renderer) FillRect(rect, clip image.Rectangle, fill canvas.Color) {
	rect = rect.Intersect(clip).Intersect(image.Rect(0, 0, r.Width, r.Height))
	if rect.Empty() {
		return
	}
	if fill.IsClear() {
		fill = 0
	}
	a := uint32(fill.A())
	if a == 255 || a == 0 {
		var first []byte
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			offset := (y*r.Width + rect.Min.X) * 4
			row := r.Pixels[offset : offset+rect.Dx()*4]
			if first == nil {
				first = row
				for x := 0; x < len(row); x += 4 {
					*(*uint32)(unsafe.Pointer(&row[x])) = uint32(fill)
				}
			} else {
				copy(row, first)
			}
		}
		return
	}
	ia := uint32(255) - a
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			p := r.Pixels[(y*r.Width+x)*4:]
			p[0] = div255(uint32(fill.B())*a + uint32(p[0])*ia)
			p[1] = div255(uint32(fill.G())*a + uint32(p[1])*ia)
			p[2] = div255(uint32(fill.R())*a + uint32(p[2])*ia)
			p[3] = div255(255*a + uint32(p[3])*ia)
		}
	}
}

func div255(value uint32) uint8 { return uint8((value + 1 + (value >> 8)) >> 8) }

func (r *Renderer) DrawImage(img canvas.Image, src image.Rectangle, dst image.Point, clip image.Rectangle) {
	w, h := src.Dx(), src.Dy()
	sx, sy := src.Min.X, src.Min.Y
	if sx < 0 {
		dst.X -= sx
		w += sx
		sx = 0
	}
	if sy < 0 {
		dst.Y -= sy
		h += sy
		sy = 0
	}
	w, h = min(w, img.Width-sx), min(h, img.Height-sy)
	if dst.X < clip.Min.X {
		d := clip.Min.X - dst.X
		dst.X += d
		sx += d
		w -= d
	}
	if dst.Y < clip.Min.Y {
		d := clip.Min.Y - dst.Y
		dst.Y += d
		sy += d
		h -= d
	}
	w = min(w, clip.Max.X-dst.X)
	h = min(h, clip.Max.Y-dst.Y)
	if w <= 0 || h <= 0 {
		return
	}
	if img.Opaque {
		for y := 0; y < h; y++ {
			srcOffset := ((sy+y)*img.Width + sx) * 4
			dstOffset := ((dst.Y+y)*r.Width + dst.X) * 4
			copy(r.Pixels[dstOffset:dstOffset+w*4], img.Pixels[srcOffset:srcOffset+w*4])
		}
		return
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s := img.Pixels[((sy+y)*img.Width+sx+x)*4:]
			d := r.Pixels[((dst.Y+y)*r.Width+dst.X+x)*4:]
			a := uint32(s[3])
			if a == 255 {
				copy(d[:4], s[:4])
				continue
			}
			if a == 0 {
				continue
			}
			ia := uint32(255) - a
			d[0] = div255(uint32(s[0])*a + uint32(d[0])*ia)
			d[1] = div255(uint32(s[1])*a + uint32(d[1])*ia)
			d[2] = div255(uint32(s[2])*a + uint32(d[2])*ia)
			d[3] = div255(255*a + uint32(d[3])*ia)
		}
	}
}

func (r *Renderer) DrawImageTransformed(img canvas.Image, degrees, scale, cx, cy float64, clip image.Rectangle) {
	sin, cos := math.Sincos(degrees * math.Pi / 180)
	rx := (math.Abs(cos)*float64(img.Width)+math.Abs(sin)*float64(img.Height))*scale/2 + scale
	ry := (math.Abs(sin)*float64(img.Width)+math.Abs(cos)*float64(img.Height))*scale/2 + scale
	sin, cos = sin/scale, cos/scale
	clip = clip.Intersect(image.Rect(0, 0, r.Width, r.Height))
	minX, maxX := max(clip.Min.X, int(math.Floor(cx-rx))), min(clip.Max.X, int(math.Ceil(cx+rx)))
	minY, maxY := max(clip.Min.Y, int(math.Floor(cy-ry))), min(clip.Max.Y, int(math.Ceil(cy+ry)))
	for y := minY; y < maxY; y++ {
		dx, dy := float64(minX)+.5-cx, float64(y)+.5-cy
		sx, sy := cos*dx+sin*dy+float64(img.Width)/2-.5, -sin*dx+cos*dy+float64(img.Height)/2-.5
		for x := minX; x < maxX; x++ {
			ix, iy := int(math.Floor(sx)), int(math.Floor(sy))
			fx, fy := sx-float64(ix), sy-float64(iy)
			var a, b, g, red float64
			for oy := range 2 {
				for ox := range 2 {
					px, py := ix+ox, iy+oy
					if px < 0 || px >= img.Width || py < 0 || py >= img.Height {
						continue
					}
					wx, wy := 1-fx, 1-fy
					if ox == 1 {
						wx = fx
					}
					if oy == 1 {
						wy = fy
					}
					p := img.Pixels[(py*img.Width+px)*4:]
					wa := wx * wy * float64(p[3]) / 255
					a += wa
					b += wa * float64(p[0])
					g += wa * float64(p[1])
					red += wa * float64(p[2])
				}
			}
			if a > 0 {
				p := r.Pixels[(y*r.Width+x)*4:]
				p[0] = uint8(math.Round(min(255, b+(1-a)*float64(p[0]))))
				p[1] = uint8(math.Round(min(255, g+(1-a)*float64(p[1]))))
				p[2] = uint8(math.Round(min(255, red+(1-a)*float64(p[2]))))
				p[3] = uint8(math.Round(min(255, a*255+(1-a)*float64(p[3]))))
			}
			sx += cos
			sy -= sin
		}
	}
}

func (r *Renderer) DrawMask(mask []byte, mw, mh int, dst image.Point, clip image.Rectangle, fill canvas.Color) {
	visible := image.Rect(dst.X, dst.Y, dst.X+mw, dst.Y+mh).Intersect(clip).Intersect(image.Rect(0, 0, r.Width, r.Height))
	for y := visible.Min.Y; y < visible.Max.Y; y++ {
		for x := visible.Min.X; x < visible.Max.X; x++ {
			a := uint32(mask[(y-dst.Y)*mw+x-dst.X])
			if a == 0 {
				continue
			}
			p := r.Pixels[(y*r.Width+x)*4:]
			if a == 255 {
				p[0], p[1], p[2], p[3] = fill.B(), fill.G(), fill.R(), 255
				continue
			}
			ia := uint32(255) - a
			p[0] = div255(uint32(fill.B())*a + uint32(p[0])*ia)
			p[1] = div255(uint32(fill.G())*a + uint32(p[1])*ia)
			p[2] = div255(uint32(fill.R())*a + uint32(p[2])*ia)
			p[3] = div255(255*a + uint32(p[3])*ia)
		}
	}
}

func (r *Renderer) Pixel(p image.Point) color.NRGBA {
	b := r.Pixels[(p.Y*r.Width+p.X)*4:]
	return color.NRGBA{R: b[2], G: b[1], B: b[0], A: b[3]}
}

func (r *Renderer) SetPixel(p image.Point, c color.NRGBA) {
	b := r.Pixels[(p.Y*r.Width+p.X)*4:]
	b[0], b[1], b[2], b[3] = c.B, c.G, c.R, c.A
}

func (r *Renderer) Snapshot() image.Image {
	out := image.NewNRGBA(image.Rect(0, 0, r.Width, r.Height))
	for y := 0; y < r.Height; y++ {
		for x := 0; x < r.Width; x++ {
			s := r.Pixels[(y*r.Width+x)*4:]
			d := out.Pix[y*out.Stride+x*4:]
			d[0], d[1], d[2], d[3] = s[2], s[1], s[0], s[3]
		}
	}
	return out
}
