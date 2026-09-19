package metal

import (
	"crypto/sha256"
	"fmt"
	"image"
	"runtime"
	"unsafe"

	"github.com/movsb/fbiw/internal/canvas"
	"github.com/veandco/go-sdl2/sdl"
)

type textureKey struct {
	pixels                uintptr
	length, width, height int
	opaque                bool
}
type maskKey struct {
	digest        [sha256.Size]byte
	width, height int
	color         canvas.Color
}
type cachedTexture struct {
	texture *sdl.Texture
	pixels  []byte
}

const (
	maxCachedImages = 256
	maxCachedMasks  = 2048
)

type Renderer struct {
	width, height int
	renderer      *sdl.Renderer
	target        *sdl.Texture
	images        map[textureKey]cachedTexture
	masks         map[maskKey]*sdl.Texture
	closePlatform func()
}

func Open(window *sdl.Window, width, height int, closePlatform func()) (*Renderer, error) {
	sdl.SetHint(sdl.HINT_RENDER_DRIVER, "metal")
	sr, err := sdl.CreateRenderer(window, -1, sdl.RENDERER_ACCELERATED|sdl.RENDERER_PRESENTVSYNC|sdl.RENDERER_TARGETTEXTURE)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Renderer, error) { sr.Destroy(); return nil, err }
	info, err := sr.GetInfo()
	if err != nil {
		return fail(err)
	}
	if info.Name != "metal" {
		return fail(fmt.Errorf("SDL selected %q instead of metal", info.Name))
	}
	if !sr.RenderTargetSupported() {
		return fail(fmt.Errorf("SDL Metal render targets unavailable"))
	}
	target, err := sr.CreateTexture(sdl.PIXELFORMAT_ARGB8888, sdl.TEXTUREACCESS_TARGET, int32(width), int32(height))
	if err != nil {
		return fail(err)
	}
	if err := target.SetBlendMode(sdl.BLENDMODE_NONE); err != nil {
		target.Destroy()
		return fail(err)
	}
	if err := sr.SetRenderTarget(target); err != nil {
		target.Destroy()
		return fail(err)
	}
	return &Renderer{width: width, height: height, renderer: sr, target: target, images: map[textureKey]cachedTexture{}, masks: map[maskKey]*sdl.Texture{}, closePlatform: closePlatform}, nil
}

func (r *Renderer) Size() (int, int) { return r.width, r.height }
func (r *Renderer) BeginFrame()      { must(r.renderer.SetRenderTarget(r.target)) }
func (r *Renderer) EndFrame() {
	must(r.renderer.SetRenderTarget(nil))
	must(r.renderer.SetClipRect(nil))
	must(r.renderer.Copy(r.target, nil, nil))
	r.renderer.Present()
	must(r.renderer.SetRenderTarget(r.target))
}
func (r *Renderer) Clear() {
	must(r.renderer.SetDrawBlendMode(sdl.BLENDMODE_NONE))
	must(r.renderer.SetDrawColor(0, 0, 0, 0))
	must(r.renderer.Clear())
}
func (r *Renderer) FillRect(rect, clip image.Rectangle, c canvas.Color) {
	rect = rect.Intersect(clip).Intersect(image.Rect(0, 0, r.width, r.height))
	if rect.Empty() {
		return
	}
	mode := sdl.BlendMode(sdl.BLENDMODE_BLEND)
	if c.A() == 0 || c.A() == 255 {
		mode = sdl.BLENDMODE_NONE
	}
	must(r.renderer.SetDrawBlendMode(mode))
	must(r.renderer.SetDrawColor(c.R(), c.G(), c.B(), c.A()))
	sr := sdl.Rect{X: int32(rect.Min.X), Y: int32(rect.Min.Y), W: int32(rect.Dx()), H: int32(rect.Dy())}
	must(r.renderer.FillRect(&sr))
}
func (r *Renderer) DrawImage(img canvas.Image, src image.Rectangle, dst image.Point, clip image.Rectangle) {
	w, h, sx, sy := src.Dx(), src.Dy(), src.Min.X, src.Min.Y
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
	w = min(w, img.Width-sx)
	h = min(h, img.Height-sy)
	clip = clip.Intersect(image.Rect(0, 0, r.width, r.height))
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
	if w <= 0 || h <= 0 || len(img.Pixels) < img.Width*img.Height*4 {
		return
	}
	t := r.imageTexture(img)
	s := sdl.Rect{X: int32(sx), Y: int32(sy), W: int32(w), H: int32(h)}
	d := sdl.Rect{X: int32(dst.X), Y: int32(dst.Y), W: int32(w), H: int32(h)}
	must(r.renderer.Copy(t, &s, &d))
}
func (r *Renderer) DrawImageTransformed(img canvas.Image, degrees, scale, cx, cy float64, clip image.Rectangle) {
	if img.Width <= 0 || img.Height <= 0 || scale <= 0 || len(img.Pixels) < img.Width*img.Height*4 {
		return
	}
	clip = clip.Intersect(image.Rect(0, 0, r.width, r.height))
	if clip.Empty() {
		return
	}
	setClip(r.renderer, clip)
	defer r.renderer.SetClipRect(nil)
	w, h := float64(img.Width)*scale, float64(img.Height)*scale
	d := sdl.FRect{X: float32(cx - w/2), Y: float32(cy - h/2), W: float32(w), H: float32(h)}
	must(r.renderer.CopyExF(r.imageTexture(img), nil, &d, degrees, nil, sdl.FLIP_NONE))
}
func (r *Renderer) DrawMask(mask []byte, w, h int, dst image.Point, clip image.Rectangle, c canvas.Color) {
	if w <= 0 || h <= 0 || len(mask) < w*h {
		return
	}
	key := maskKey{sha256.Sum256(mask[:w*h]), w, h, c}
	t := r.masks[key]
	if t == nil {
		if len(r.masks) >= maxCachedMasks {
			for key, texture := range r.masks {
				texture.Destroy()
				delete(r.masks, key)
			}
		}
		pixels := make([]byte, w*h*4)
		for i, a := range mask[:w*h] {
			p := pixels[i*4:]
			p[0], p[1], p[2], p[3] = c.B(), c.G(), c.R(), a
		}
		var err error
		t, err = r.renderer.CreateTexture(sdl.PIXELFORMAT_ARGB8888, sdl.TEXTUREACCESS_STATIC, int32(w), int32(h))
		must(err)
		must(t.SetBlendMode(sdl.BLENDMODE_BLEND))
		must(t.Update(nil, unsafe.Pointer(&pixels[0]), w*4))
		runtime.KeepAlive(pixels)
		r.masks[key] = t
	}
	visible := image.Rect(dst.X, dst.Y, dst.X+w, dst.Y+h).Intersect(clip).Intersect(image.Rect(0, 0, r.width, r.height))
	if visible.Empty() {
		return
	}
	s := sdl.Rect{X: int32(visible.Min.X - dst.X), Y: int32(visible.Min.Y - dst.Y), W: int32(visible.Dx()), H: int32(visible.Dy())}
	d := sdl.Rect{X: int32(visible.Min.X), Y: int32(visible.Min.Y), W: s.W, H: s.H}
	must(r.renderer.Copy(t, &s, &d))
}
func (r *Renderer) Snapshot() image.Image {
	must(r.renderer.SetRenderTarget(r.target))
	out := image.NewNRGBA(image.Rect(0, 0, r.width, r.height))
	must(r.renderer.ReadPixels(nil, sdl.PIXELFORMAT_ABGR8888, unsafe.Pointer(&out.Pix[0]), out.Stride))
	runtime.KeepAlive(out.Pix)
	return out
}
func (r *Renderer) Close() error {
	for _, v := range r.images {
		v.texture.Destroy()
	}
	for _, v := range r.masks {
		v.Destroy()
	}
	if r.target != nil {
		r.target.Destroy()
	}
	err := r.renderer.Destroy()
	r.closePlatform()
	return err
}

func (r *Renderer) imageTexture(img canvas.Image) *sdl.Texture {
	key := textureKey{uintptr(unsafe.Pointer(&img.Pixels[0])), len(img.Pixels), img.Width, img.Height, img.Opaque}
	if v, ok := r.images[key]; ok {
		return v.texture
	}
	if len(r.images) >= maxCachedImages {
		for key, texture := range r.images {
			texture.texture.Destroy()
			delete(r.images, key)
		}
	}
	t, err := r.renderer.CreateTexture(sdl.PIXELFORMAT_ARGB8888, sdl.TEXTUREACCESS_STATIC, int32(img.Width), int32(img.Height))
	must(err)
	if img.Opaque {
		must(t.SetBlendMode(sdl.BLENDMODE_NONE))
	} else {
		must(t.SetBlendMode(sdl.BLENDMODE_BLEND))
	}
	must(t.Update(nil, unsafe.Pointer(&img.Pixels[0]), img.Width*4))
	runtime.KeepAlive(img.Pixels)
	r.images[key] = cachedTexture{t, img.Pixels}
	return t
}
func setClip(r *sdl.Renderer, v image.Rectangle) {
	x := sdl.Rect{X: int32(v.Min.X), Y: int32(v.Min.Y), W: int32(v.Dx()), H: int32(v.Dy())}
	must(r.SetClipRect(&x))
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}

var _ canvas.Renderer = (*Renderer)(nil)
