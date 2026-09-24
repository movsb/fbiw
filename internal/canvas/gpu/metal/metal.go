//go:build darwin

package metal

import (
	"crypto/sha256"
	"fmt"
	"image"
	"math"
	"runtime"
	"unsafe"

	"github.com/movsb/fbiw/internal/canvas"
	"github.com/veandco/go-sdl2/sdl"
)

type textureKey struct {
	pixels                uintptr
	length, width, height int
	opaque                bool
	// 旋转绘制使用带透明边框的预乘纹理，不能和普通图片纹理共用缓存。
	padded bool
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
	// CopyExF 负责旋转和缩放。线性采样才能匹配 CPU/GLES 的双线性插值；
	// 普通图片目前都是 1:1 Copy，因此不会因此变模糊。
	sdl.SetHint(sdl.HINT_RENDER_SCALE_QUALITY, "linear")
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
func (r *Renderer) Resize(width, height int) error {
	if width <= 0 || height <= 0 || width == r.width && height == r.height {
		return nil
	}
	target, err := r.renderer.CreateTexture(sdl.PIXELFORMAT_ARGB8888, sdl.TEXTUREACCESS_TARGET, int32(width), int32(height))
	if err != nil {
		return err
	}
	if err := target.SetBlendMode(sdl.BLENDMODE_NONE); err != nil {
		target.Destroy()
		return err
	}
	if err := r.renderer.SetRenderTarget(target); err != nil {
		target.Destroy()
		return err
	}
	old := r.target
	r.target = target
	r.width, r.height = width, height
	old.Destroy()
	return nil
}
func (r *Renderer) BeginFrame() { must(r.renderer.SetRenderTarget(r.target)) }
func (r *Renderer) EndFrame() {
	// target 是稳定的离屏帧内容。每帧只在这里复制到 SDL 管理的 Metal
	// drawable；Present 后再切回 target，使 Snapshot 不依赖 drawable 是否保留。
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
func (r *Renderer) DrawImage(img canvas.Image, src image.Rectangle, degrees, scaleX, scaleY, cx, cy float64, clip, roundedClip image.Rectangle, radius float64) {
	if img.Width <= 0 || img.Height <= 0 || scaleX <= 0 || scaleY <= 0 || len(img.Pixels) < img.Width*img.Height*4 {
		return
	}
	src = src.Intersect(image.Rect(0, 0, img.Width, img.Height))
	if src.Empty() {
		return
	}
	clip = clip.Intersect(image.Rect(0, 0, r.width, r.height))
	if clip.Empty() {
		return
	}
	// CPU/GLES 的双线性采样允许图片边缘外一像素的透明样本参与插值。
	// SDL_RenderCopyExF 只栅格化目标四边形，所以这里把旋转专用纹理的
	// 一像素透明边框也计入目标尺寸，否则边缘会被提前截断。
	padded := src == image.Rect(0, 0, img.Width, img.Height) && (degrees != 0 || scaleX != 1 || scaleY != 1)
	w, h := float64(src.Dx())*scaleX, float64(src.Dy())*scaleY
	var s *sdl.Rect
	if padded {
		w, h = float64(img.Width+2)*scaleX, float64(img.Height+2)*scaleY
	} else {
		s = &sdl.Rect{X: int32(src.Min.X), Y: int32(src.Min.Y), W: int32(src.Dx()), H: int32(src.Dy())}
	}
	d := sdl.FRect{X: float32(cx - w/2), Y: float32(cy - h/2), W: float32(w), H: float32(h)}
	texture := r.imageTexture(img, padded)
	draw := func(band image.Rectangle) {
		band = band.Intersect(clip)
		if band.Empty() {
			return
		}
		setClip(r.renderer, band)
		must(r.renderer.CopyExF(texture, s, &d, degrees, nil, sdl.FLIP_NONE))
	}
	defer r.renderer.SetClipRect(nil)
	if radius <= 0 || roundedClip.Empty() {
		draw(clip)
		return
	}
	radius = min(radius, float64(min(roundedClip.Dx(), roundedClip.Dy()))/2)
	cornerRows := int(math.Ceil(radius))
	draw(image.Rect(roundedClip.Min.X, roundedClip.Min.Y+cornerRows, roundedClip.Max.X, roundedClip.Max.Y-cornerRows))
	for y := roundedClip.Min.Y; y < roundedClip.Min.Y+cornerRows; y++ {
		dy := radius - (float64(y-roundedClip.Min.Y) + .5)
		inset := int(math.Ceil(radius - math.Sqrt(max(0, radius*radius-dy*dy))))
		draw(image.Rect(roundedClip.Min.X+inset, y, roundedClip.Max.X-inset, y+1))
		bottom := roundedClip.Max.Y - 1 - (y - roundedClip.Min.Y)
		draw(image.Rect(roundedClip.Min.X+inset, bottom, roundedClip.Max.X-inset, bottom+1))
	}
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
			p[0], p[1], p[2], p[3] = c.B(), c.G(), c.R(), uint8(uint16(a)*uint16(c.A())/255)
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
	// ABGR8888 在当前小端平台的内存字节顺序正好是 NRGBA 所需的 RGBA。
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

func (r *Renderer) imageTexture(img canvas.Image, padded bool) *sdl.Texture {
	key := textureKey{uintptr(unsafe.Pointer(&img.Pixels[0])), len(img.Pixels), img.Width, img.Height, img.Opaque, padded}
	if v, ok := r.images[key]; ok {
		return v.texture
	}
	if len(r.images) >= maxCachedImages {
		for key, texture := range r.images {
			texture.texture.Destroy()
			delete(r.images, key)
		}
	}
	width, height, pixels := img.Width, img.Height, img.Pixels
	if padded {
		// SDL 的线性过滤会直接插值纹理中的 RGB。若保留 straight-alpha
		// 颜色，透明边缘的 RGB 也会参与插值并产生色边。先预乘 RGB，便与
		// CPU 及 GLES transform shader 的“颜色×Alpha 后插值”语义一致。
		width, height = img.Width+2, img.Height+2
		pixels = make([]byte, width*height*4)
		for y := range img.Height {
			for x := range img.Width {
				source := img.Pixels[(y*img.Width+x)*4:]
				target := pixels[((y+1)*width+x+1)*4:]
				a := uint32(source[3])
				target[0] = uint8((uint32(source[0])*a + 127) / 255)
				target[1] = uint8((uint32(source[1])*a + 127) / 255)
				target[2] = uint8((uint32(source[2])*a + 127) / 255)
				target[3] = source[3]
			}
		}
	}
	t, err := r.renderer.CreateTexture(sdl.PIXELFORMAT_ARGB8888, sdl.TEXTUREACCESS_STATIC, int32(width), int32(height))
	must(err)
	if padded {
		// pixels 已经预乘过 Alpha，所以颜色源因子必须是 ONE；若再使用
		// SDL_BLENDMODE_BLEND 的 SRC_ALPHA，会把 Alpha 乘两次。Alpha 通道
		// 仍用标准 Source Over：src + dst*(1-srcAlpha)。
		premultiplied := sdl.ComposeCustomBlendMode(
			sdl.BLENDFACTOR_ONE, sdl.BLENDFACTOR_ONE_MINUS_SRC_ALPHA, sdl.BLENDOPERATION_ADD,
			sdl.BLENDFACTOR_ONE, sdl.BLENDFACTOR_ONE_MINUS_SRC_ALPHA, sdl.BLENDOPERATION_ADD,
		)
		must(t.SetBlendMode(premultiplied))
	} else if img.Opaque {
		must(t.SetBlendMode(sdl.BLENDMODE_NONE))
	} else {
		must(t.SetBlendMode(sdl.BLENDMODE_BLEND))
	}
	must(t.Update(nil, unsafe.Pointer(&pixels[0]), width*4))
	runtime.KeepAlive(pixels)
	r.images[key] = cachedTexture{t, pixels}
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
