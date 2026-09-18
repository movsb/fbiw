//go:build linux

package gpu

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"runtime"

	"github.com/ebitengine/purego"
	canvas "github.com/movsb/fbiw/internal/canvas"
)

const (
	eglFalse             = 0
	eglNone              = 0x3038
	eglRenderableType    = 0x3040
	eglOpenGLES2Bit      = 0x0004
	eglRedSize           = 0x3024
	eglGreenSize         = 0x3023
	eglBlueSize          = 0x3022
	eglAlphaSize         = 0x3021
	eglSamples           = 0x3031
	eglContextClientVers = 0x3098
	eglWidth             = 0x3057
	eglHeight            = 0x3056
	glColorBufferBit     = 0x00004000
	glScissorTest        = 0x0c11
)

type api struct {
	getDisplay          func(uintptr) uintptr
	initialize          func(uintptr, *int32, *int32) uint32
	chooseConfig        func(uintptr, *int32, *uintptr, int32, *int32) uint32
	getConfigAttrib     func(uintptr, uintptr, int32, *int32) uint32
	createWindowSurface func(uintptr, uintptr, uintptr, *int32) uintptr
	createContext       func(uintptr, uintptr, uintptr, *int32) uintptr
	makeCurrent         func(uintptr, uintptr, uintptr, uintptr) uint32
	querySurface        func(uintptr, uintptr, int32, *int32) uint32
	swapBuffers         func(uintptr, uintptr) uint32
	getError            func() int32
	destroySurface      func(uintptr, uintptr) uint32
	destroyContext      func(uintptr, uintptr) uint32
	terminate           func(uintptr) uint32
	clearColor          func(float32, float32, float32, float32)
	clear               func(uint32)
	enable              func(uint32)
	disable             func(uint32)
	scissor             func(int32, int32, int32, int32)
}

func loadAPI() (*api, func(), error) {
	egl, err := purego.Dlopen("libEGL.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, nil, fmt.Errorf("load libEGL.so.1: %w", err)
	}
	gles, err := purego.Dlopen("libGLESv2.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		purego.Dlclose(egl)
		return nil, nil, fmt.Errorf("load libGLESv2.so.2: %w", err)
	}
	closeLibraries := func() { purego.Dlclose(gles); purego.Dlclose(egl) }
	a := &api{}
	purego.RegisterLibFunc(&a.getDisplay, egl, "eglGetDisplay")
	purego.RegisterLibFunc(&a.initialize, egl, "eglInitialize")
	purego.RegisterLibFunc(&a.chooseConfig, egl, "eglChooseConfig")
	purego.RegisterLibFunc(&a.getConfigAttrib, egl, "eglGetConfigAttrib")
	purego.RegisterLibFunc(&a.createWindowSurface, egl, "eglCreateWindowSurface")
	purego.RegisterLibFunc(&a.createContext, egl, "eglCreateContext")
	purego.RegisterLibFunc(&a.makeCurrent, egl, "eglMakeCurrent")
	purego.RegisterLibFunc(&a.querySurface, egl, "eglQuerySurface")
	purego.RegisterLibFunc(&a.swapBuffers, egl, "eglSwapBuffers")
	purego.RegisterLibFunc(&a.getError, egl, "eglGetError")
	purego.RegisterLibFunc(&a.destroySurface, egl, "eglDestroySurface")
	purego.RegisterLibFunc(&a.destroyContext, egl, "eglDestroyContext")
	purego.RegisterLibFunc(&a.terminate, egl, "eglTerminate")
	purego.RegisterLibFunc(&a.clearColor, gles, "glClearColor")
	purego.RegisterLibFunc(&a.clear, gles, "glClear")
	purego.RegisterLibFunc(&a.enable, gles, "glEnable")
	purego.RegisterLibFunc(&a.disable, gles, "glDisable")
	purego.RegisterLibFunc(&a.scissor, gles, "glScissor")
	return a, closeLibraries, nil
}

type Renderer struct {
	api              *api
	display, surface uintptr
	width, height    int
}

func (r *Renderer) Size() (int, int) { return r.width, r.height }
func (*Renderer) BeginFrame()        {}
func (r *Renderer) EndFrame() {
	if r.api.swapBuffers(r.display, r.surface) == eglFalse {
		panic(fmt.Sprintf("eglSwapBuffers: 0x%x", r.api.getError()))
	}
}
func (r *Renderer) Clear() {
	r.api.disable(glScissorTest)
	r.api.clearColor(0, 0, 0, 0)
	r.api.clear(glColorBufferBit)
}
func (r *Renderer) FillRect(rect, clip image.Rectangle, fill canvas.Color) {
	if fill.A() != 255 && !fill.IsClear() {
		panic("GLES renderer暂不支持半透明矩形")
	}
	rect = rect.Intersect(clip).Intersect(image.Rect(0, 0, r.width, r.height))
	if rect.Empty() {
		return
	}
	r.api.enable(glScissorTest)
	r.api.scissor(int32(rect.Min.X), int32(r.height-rect.Max.Y), int32(rect.Dx()), int32(rect.Dy()))
	if fill.IsClear() {
		fill = 0
	}
	r.api.clearColor(float32(fill.R())/255, float32(fill.G())/255, float32(fill.B())/255, float32(fill.A())/255)
	r.api.clear(glColorBufferBit)
}
func (*Renderer) DrawImage(canvas.Image, image.Rectangle, image.Point, image.Rectangle) {
	panic("GLES renderer图片纹理尚未实现")
}
func (*Renderer) DrawImageTransformed(canvas.Image, float64, float64, float64, float64, image.Rectangle) {
	panic("GLES renderer图片变换尚未实现")
}
func (*Renderer) DrawMask([]byte, int, int, image.Point, image.Rectangle, canvas.Color) {
	panic("GLES renderer字形纹理尚未实现")
}
func (*Renderer) Pixel(image.Point) color.NRGBA { panic("GLES renderer单像素读取尚未实现") }
func (*Renderer) SetPixel(image.Point, color.NRGBA) {
	panic("GLES renderer单像素写入尚未实现")
}
func (*Renderer) Snapshot() image.Image { panic("GLES renderer截图尚未实现") }

func RunProbe() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	a, closeLibraries, err := loadAPI()
	if err != nil {
		return err
	}
	defer closeLibraries()
	display := a.getDisplay(0)
	if display == 0 {
		return errors.New("eglGetDisplay returned EGL_NO_DISPLAY")
	}
	var major, minor int32
	if a.initialize(display, &major, &minor) == eglFalse {
		return fmt.Errorf("eglInitialize: 0x%x", a.getError())
	}
	defer a.terminate(display)
	attrs := []int32{eglRedSize, 8, eglGreenSize, 8, eglBlueSize, 8, eglAlphaSize, 8, eglSamples, 4, eglRenderableType, eglOpenGLES2Bit, eglNone}
	var count int32
	if a.chooseConfig(display, &attrs[0], nil, 0, &count) == eglFalse || count == 0 {
		return fmt.Errorf("eglChooseConfig: count=%d error=0x%x", count, a.getError())
	}
	configs := make([]uintptr, count)
	if a.chooseConfig(display, &attrs[0], &configs[0], count, &count) == eglFalse {
		return fmt.Errorf("eglChooseConfig list: 0x%x", a.getError())
	}
	wanted := []struct{ attribute, value int32 }{{eglRedSize, 8}, {eglGreenSize, 8}, {eglBlueSize, 8}, {eglAlphaSize, 8}, {eglSamples, 4}}
	var config uintptr
	for _, candidate := range configs[:count] {
		matches := true
		for _, want := range wanted {
			var value int32
			if a.getConfigAttrib(display, candidate, want.attribute, &value) == eglFalse || value != want.value {
				matches = false
				break
			}
		}
		if matches {
			config = candidate
			break
		}
	}
	if config == 0 {
		return errors.New("no exact RGBA8 MSAA4 EGL config")
	}
	surface := a.createWindowSurface(display, config, 0, nil)
	if surface == 0 {
		return fmt.Errorf("eglCreateWindowSurface: 0x%x", a.getError())
	}
	defer a.destroySurface(display, surface)
	contextAttrs := []int32{eglContextClientVers, 2, eglNone}
	context := a.createContext(display, config, 0, &contextAttrs[0])
	if context == 0 {
		return fmt.Errorf("eglCreateContext: 0x%x", a.getError())
	}
	defer a.destroyContext(display, context)
	if a.makeCurrent(display, surface, surface, context) == eglFalse {
		return fmt.Errorf("eglMakeCurrent: 0x%x", a.getError())
	}
	var width, height int32
	if a.querySurface(display, surface, eglWidth, &width) == eglFalse || a.querySurface(display, surface, eglHeight, &height) == eglFalse {
		return fmt.Errorf("eglQuerySurface: 0x%x", a.getError())
	}
	r := &Renderer{api: a, display: display, surface: surface, width: int(width), height: int(height)}
	r.BeginFrame()
	r.Clear()
	bounds := image.Rect(0, 0, r.width, r.height)
	r.FillRect(bounds, bounds, canvas.Color(0xff1f5cc7))
	r.FillRect(image.Rect(80, 80, 360, 260), bounds, canvas.Color(0xffff8a20))
	r.FillRect(image.Rect(160, 160, 440, 340), image.Rect(200, 120, 400, 300), canvas.Color(0xff38c972))
	r.EndFrame()
	fmt.Printf("GLES renderer probe OK: EGL %d.%d, surface %dx%d\n", major, minor, width, height)
	return nil
}
