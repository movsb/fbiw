//go:build js && wasm

package webgl

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"math"
	"syscall/js"
	"unsafe"

	"github.com/movsb/fbiw/internal/canvas"
)

//go:embed shaders/renderer.vert.glsl
var vertex string

//go:embed shaders/renderer.frag.glsl
var fragment string

type Renderer struct {
	gl, element, program, buffer, framebuffer, target, empty               js.Value
	rect, viewport, uv, tint, mode, inverse, center, imageSize, sourceRect js.Value
	clipRect, clipParams                                                   js.Value

	width, height int
	position      int
	textures      map[textureKey]imageTexture
	masks         map[maskKey]js.Value
	closed        bool
}

type imageTexture struct {
	id     js.Value
	pixels []byte
}

type maskKey struct {
	digest        [32]byte
	width, height int
}

type textureKey struct {
	pointer               uintptr
	length, width, height int
	opaque                bool
}

var _ canvas.Renderer = (*Renderer)(nil)

func New(element js.Value) (*Renderer, error) {
	g := element.Call("getContext", "webgl", map[string]interface{}{"alpha": true, "premultipliedAlpha": false, "preserveDrawingBuffer": true})
	if g.IsNull() || g.IsUndefined() {
		return nil, fmt.Errorf("WebGL unavailable")
	}
	compile := func(kind int, source string) (js.Value, error) {
		s := g.Call("createShader", kind)
		g.Call("shaderSource", s, source)
		g.Call("compileShader", s)
		if !g.Call("getShaderParameter", s, g.Get("COMPILE_STATUS")).Bool() {
			return js.Value{}, fmt.Errorf("WebGL shader: %s", g.Call("getShaderInfoLog", s).String())
		}
		return s, nil
	}
	v, e := compile(g.Get("VERTEX_SHADER").Int(), vertex)
	if e != nil {
		return nil, e
	}
	f, e := compile(g.Get("FRAGMENT_SHADER").Int(), fragment)
	if e != nil {
		return nil, e
	}
	p := g.Call("createProgram")
	g.Call("attachShader", p, v)
	g.Call("attachShader", p, f)
	g.Call("linkProgram", p)
	g.Call("deleteShader", v)
	g.Call("deleteShader", f)
	if !g.Call("getProgramParameter", p, g.Get("LINK_STATUS")).Bool() {
		return nil, fmt.Errorf("WebGL program: %s", g.Call("getProgramInfoLog", p).String())
	}
	r := &Renderer{gl: g, element: element, program: p, textures: map[textureKey]imageTexture{}, masks: map[maskKey]js.Value{}}
	r.rect = g.Call("getUniformLocation", p, "u_rect")
	r.viewport = g.Call("getUniformLocation", p, "u_viewport")
	r.uv = g.Call("getUniformLocation", p, "u_uv")
	r.tint = g.Call("getUniformLocation", p, "u_color")
	r.mode = g.Call("getUniformLocation", p, "u_mode")
	r.inverse = g.Call("getUniformLocation", p, "u_inverse")
	r.center = g.Call("getUniformLocation", p, "u_center")
	r.imageSize = g.Call("getUniformLocation", p, "u_image_size")
	r.sourceRect = g.Call("getUniformLocation", p, "u_source_rect")
	r.clipRect = g.Call("getUniformLocation", p, "u_clip_rect")
	r.clipParams = g.Call("getUniformLocation", p, "u_clip_params")
	r.position = g.Call("getAttribLocation", p, "a_position").Int()
	r.buffer = g.Call("createBuffer")
	g.Call("bindBuffer", g.Get("ARRAY_BUFFER"), r.buffer)
	q := js.Global().Get("Float32Array").New(8)
	for i, x := range []float64{0, 0, 1, 0, 0, 1, 1, 1} {
		q.SetIndex(i, x)
	}
	g.Call("bufferData", g.Get("ARRAY_BUFFER"), q, g.Get("STATIC_DRAW"))
	r.framebuffer = g.Call("createFramebuffer")
	r.target = g.Call("createTexture")
	r.empty = r.upload([]byte{0, 0, 0, 0}, 1, 1, false)
	if e := r.Resize(max(1, element.Get("clientWidth").Int()), max(1, element.Get("clientHeight").Int())); e != nil {
		return nil, e
	}
	return r, nil
}
func (r *Renderer) Size() (int, int) { return r.width, r.height }
func (r *Renderer) Resize(w, h int) error {
	if w <= 0 || h <= 0 {
		return nil
	}
	r.width, r.height = w, h
	r.element.Set("width", w)
	r.element.Set("height", h)
	g := r.gl
	g.Call("bindTexture", g.Get("TEXTURE_2D"), r.target)
	r.textureParams()
	g.Call("texImage2D", g.Get("TEXTURE_2D"), 0, g.Get("RGBA"), w, h, 0, g.Get("RGBA"), g.Get("UNSIGNED_BYTE"), js.Null())
	g.Call("bindFramebuffer", g.Get("FRAMEBUFFER"), r.framebuffer)
	g.Call("framebufferTexture2D", g.Get("FRAMEBUFFER"), g.Get("COLOR_ATTACHMENT0"), g.Get("TEXTURE_2D"), r.target, 0)
	if g.Call("checkFramebufferStatus", g.Get("FRAMEBUFFER")).Int() != g.Get("FRAMEBUFFER_COMPLETE").Int() {
		return fmt.Errorf("WebGL framebuffer incomplete")
	}
	g.Call("viewport", 0, 0, w, h)
	return nil
}
func (r *Renderer) BeginFrame() {
	r.gl.Call("bindFramebuffer", r.gl.Get("FRAMEBUFFER"), r.framebuffer)
	r.gl.Call("viewport", 0, 0, r.width, r.height)
	r.gl.Call("bindTexture", r.gl.Get("TEXTURE_2D"), r.empty)
}
func (r *Renderer) EndFrame() {
	g := r.gl
	g.Call("bindFramebuffer", g.Get("FRAMEBUFFER"), js.Null())
	g.Call("disable", g.Get("SCISSOR_TEST"))
	g.Call("disable", g.Get("BLEND"))
	g.Call("viewport", 0, 0, r.width, r.height)
	g.Call("bindTexture", g.Get("TEXTURE_2D"), r.target)
	g.Call("uniform4f", r.uv, 0, 0, 1, 1)
	r.draw(image.Rect(0, 0, r.width, r.height), 4, color.NRGBA{}, false)
	g.Call("bindFramebuffer", g.Get("FRAMEBUFFER"), r.framebuffer)
	g.Call("bindTexture", g.Get("TEXTURE_2D"), r.empty)
}
func (r *Renderer) Clear() {
	g := r.gl
	g.Call("disable", g.Get("SCISSOR_TEST"))
	g.Call("disable", g.Get("BLEND"))
	g.Call("clearColor", 0, 0, 0, 0)
	g.Call("clear", g.Get("COLOR_BUFFER_BIT"))
}
func (r *Renderer) draw(rect image.Rectangle, mode int, tint color.NRGBA, blend bool) {
	g := r.gl
	g.Call("useProgram", r.program)
	g.Call("uniform4f", r.rect, rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy())
	g.Call("uniform2f", r.viewport, r.width, r.height)
	g.Call("uniform1i", r.mode, mode)
	g.Call("uniform4f", r.tint, float64(tint.R)/255, float64(tint.G)/255, float64(tint.B)/255, float64(tint.A)/255)
	g.Call("bindBuffer", g.Get("ARRAY_BUFFER"), r.buffer)
	g.Call("enableVertexAttribArray", r.position)
	g.Call("vertexAttribPointer", r.position, 2, g.Get("FLOAT"), false, 0, 0)
	if blend {
		g.Call("enable", g.Get("BLEND"))
		g.Call("blendFuncSeparate", g.Get("SRC_ALPHA"), g.Get("ONE_MINUS_SRC_ALPHA"), g.Get("ONE"), g.Get("ONE_MINUS_SRC_ALPHA"))
	} else {
		g.Call("disable", g.Get("BLEND"))
	}
	g.Call("drawArrays", g.Get("TRIANGLE_STRIP"), 0, 4)
}
func (r *Renderer) FillRect(rect, clip image.Rectangle, c canvas.Color) {
	rect = rect.Intersect(clip).Intersect(image.Rect(0, 0, r.width, r.height))
	if rect.Empty() {
		return
	}
	g := r.gl
	if c.IsClear() {
		g.Call("enable", g.Get("SCISSOR_TEST"))
		g.Call("scissor", rect.Min.X, r.height-rect.Max.Y, rect.Dx(), rect.Dy())
		g.Call("clearColor", 0, 0, 0, 0)
		g.Call("clear", g.Get("COLOR_BUFFER_BIT"))
		g.Call("disable", g.Get("SCISSOR_TEST"))
		return
	}
	r.draw(rect, 0, c.NRGBA(), c.A() > 0 && c.A() < 255)
}
func (r *Renderer) textureParams() {
	g := r.gl
	for _, p := range [][2]string{{"TEXTURE_MIN_FILTER", "NEAREST"}, {"TEXTURE_MAG_FILTER", "NEAREST"}, {"TEXTURE_WRAP_S", "CLAMP_TO_EDGE"}, {"TEXTURE_WRAP_T", "CLAMP_TO_EDGE"}} {
		g.Call("texParameteri", g.Get("TEXTURE_2D"), g.Get(p[0]), g.Get(p[1]))
	}
}
func (r *Renderer) upload(p []byte, w, h int, alpha bool) js.Value {
	g := r.gl
	t := g.Call("createTexture")
	g.Call("bindTexture", g.Get("TEXTURE_2D"), t)
	r.textureParams()
	g.Call("pixelStorei", g.Get("UNPACK_ALIGNMENT"), 1)
	a := js.Global().Get("Uint8Array").New(len(p))
	js.CopyBytesToJS(a, p)
	format := g.Get("RGBA")
	if alpha {
		format = g.Get("ALPHA")
	}
	g.Call("texImage2D", g.Get("TEXTURE_2D"), 0, format, w, h, 0, format, g.Get("UNSIGNED_BYTE"), a)
	return t
}
func (r *Renderer) imageTexture(img canvas.Image) js.Value {
	k := textureKey{uintptr(unsafe.Pointer(&img.Pixels[0])), len(img.Pixels), img.Width, img.Height, img.Opaque}
	if t, ok := r.textures[k]; ok {
		return t.id
	}
	if len(r.textures) >= 256 {
		for _, t := range r.textures {
			r.gl.Call("deleteTexture", t.id)
		}
		clear(r.textures)
	}
	t := r.upload(img.Pixels, img.Width, img.Height, false)
	r.textures[k] = imageTexture{t, img.Pixels}
	return t
}
func (r *Renderer) DrawMask(mask []byte, w, h int, dst image.Point, clip image.Rectangle, c canvas.Color) {
	if w <= 0 || h <= 0 || len(mask) < w*h {
		return
	}
	rect := image.Rect(dst.X, dst.Y, dst.X+w, dst.Y+h)
	v := rect.Intersect(clip).Intersect(image.Rect(0, 0, r.width, r.height))
	if v.Empty() {
		return
	}
	key := maskKey{sha256.Sum256(mask[:w*h]), w, h}
	t, ok := r.masks[key]
	if !ok {
		if len(r.masks) >= 512 {
			for _, old := range r.masks {
				r.gl.Call("deleteTexture", old)
			}
			clear(r.masks)
		}
		t = r.upload(mask[:w*h], w, h, true)
		r.masks[key] = t
	}
	r.gl.Call("bindTexture", r.gl.Get("TEXTURE_2D"), t)
	g := r.gl
	g.Call("useProgram", r.program)
	g.Call("uniform4f", r.uv, float64(v.Min.X-dst.X)/float64(w), float64(v.Min.Y-dst.Y)/float64(h), float64(v.Dx())/float64(w), float64(v.Dy())/float64(h))
	r.draw(v, 2, c.NRGBA(), true)
}
func (r *Renderer) DrawImage(img canvas.Image, src image.Rectangle, degrees, scaleX, scaleY, cx, cy float64, clip, roundedClip image.Rectangle, radius float64) {
	if img.Width <= 0 || img.Height <= 0 || len(img.Pixels) < img.Width*img.Height*4 || scaleX <= 0 || scaleY <= 0 {
		return
	}
	src = src.Intersect(image.Rect(0, 0, img.Width, img.Height))
	if src.Empty() {
		return
	}
	s, c := math.Sincos(degrees * math.Pi / 180)
	rx := (math.Abs(c)*float64(src.Dx())*scaleX+math.Abs(s)*float64(src.Dy())*scaleY)/2 + max(scaleX, scaleY)
	ry := (math.Abs(s)*float64(src.Dx())*scaleX+math.Abs(c)*float64(src.Dy())*scaleY)/2 + max(scaleX, scaleY)
	rect := image.Rect(int(math.Floor(cx-rx)), int(math.Floor(cy-ry)), int(math.Ceil(cx+rx)), int(math.Ceil(cy+ry))).Intersect(clip).Intersect(image.Rect(0, 0, r.width, r.height))
	if rect.Empty() {
		return
	}
	g := r.gl
	g.Call("bindTexture", g.Get("TEXTURE_2D"), r.imageTexture(img))
	g.Call("useProgram", r.program)
	g.Call("uniform2f", r.center, cx, cy)
	g.Call("uniform2f", r.imageSize, img.Width, img.Height)
	g.Call("uniform4f", r.sourceRect, src.Min.X, src.Min.Y, src.Dx(), src.Dy())
	g.Call("uniform4f", r.clipRect, roundedClip.Min.X, roundedClip.Min.Y, roundedClip.Dx(), roundedClip.Dy())
	g.Call("uniform2f", r.clipParams, radius, 0)
	g.Call("uniform4f", r.inverse, c/scaleX, s/scaleX, -s/scaleY, c/scaleY)
	r.draw(rect, 3, color.NRGBA{}, true)
}
func (r *Renderer) Snapshot() image.Image {
	g := r.gl
	g.Call("bindFramebuffer", g.Get("FRAMEBUFFER"), r.framebuffer)
	a := js.Global().Get("Uint8Array").New(r.width * r.height * 4)
	g.Call("readPixels", 0, 0, r.width, r.height, g.Get("RGBA"), g.Get("UNSIGNED_BYTE"), a)
	p := make([]byte, r.width*r.height*4)
	js.CopyBytesToGo(p, a)
	out := image.NewNRGBA(image.Rect(0, 0, r.width, r.height))
	stride := r.width * 4
	for y := 0; y < r.height; y++ {
		copy(out.Pix[y*stride:(y+1)*stride], p[(r.height-y-1)*stride:(r.height-y)*stride])
	}
	return out
}
func (r *Renderer) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	g := r.gl
	for _, t := range r.textures {
		g.Call("deleteTexture", t.id)
	}
	for _, t := range r.masks {
		g.Call("deleteTexture", t)
	}
	g.Call("deleteTexture", r.target)
	g.Call("deleteTexture", r.empty)
	g.Call("deleteFramebuffer", r.framebuffer)
	g.Call("deleteBuffer", r.buffer)
	g.Call("deleteProgram", r.program)
	return nil
}
