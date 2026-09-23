//go:build linux

package gles

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"runtime"

	"github.com/ebitengine/purego"
	"github.com/movsb/fbiw/internal/canvas"
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
)

type platformAPI struct {
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
}

func loadAPI() (*platformAPI, *api, func(), error) {
	egl, err := purego.Dlopen("libEGL.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load libEGL.so.1: %w", err)
	}
	gles, err := purego.Dlopen("libGLESv2.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		purego.Dlclose(egl)
		return nil, nil, nil, fmt.Errorf("load libGLESv2.so.2: %w", err)
	}
	closeLibraries := func() { purego.Dlclose(gles); purego.Dlclose(egl) }
	p, a := &platformAPI{}, &api{}
	purego.RegisterLibFunc(&p.getDisplay, egl, "eglGetDisplay")
	purego.RegisterLibFunc(&p.initialize, egl, "eglInitialize")
	purego.RegisterLibFunc(&p.chooseConfig, egl, "eglChooseConfig")
	purego.RegisterLibFunc(&p.getConfigAttrib, egl, "eglGetConfigAttrib")
	purego.RegisterLibFunc(&p.createWindowSurface, egl, "eglCreateWindowSurface")
	purego.RegisterLibFunc(&p.createContext, egl, "eglCreateContext")
	purego.RegisterLibFunc(&p.makeCurrent, egl, "eglMakeCurrent")
	purego.RegisterLibFunc(&p.querySurface, egl, "eglQuerySurface")
	purego.RegisterLibFunc(&p.swapBuffers, egl, "eglSwapBuffers")
	purego.RegisterLibFunc(&p.getError, egl, "eglGetError")
	purego.RegisterLibFunc(&p.destroySurface, egl, "eglDestroySurface")
	purego.RegisterLibFunc(&p.destroyContext, egl, "eglDestroyContext")
	purego.RegisterLibFunc(&p.terminate, egl, "eglTerminate")
	purego.RegisterLibFunc(&a.clearColor, gles, "glClearColor")
	purego.RegisterLibFunc(&a.clear, gles, "glClear")
	purego.RegisterLibFunc(&a.enable, gles, "glEnable")
	purego.RegisterLibFunc(&a.disable, gles, "glDisable")
	purego.RegisterLibFunc(&a.scissor, gles, "glScissor")
	purego.RegisterLibFunc(&a.viewport, gles, "glViewport")
	purego.RegisterLibFunc(&a.createShader, gles, "glCreateShader")
	purego.RegisterLibFunc(&a.shaderSource, gles, "glShaderSource")
	purego.RegisterLibFunc(&a.compileShader, gles, "glCompileShader")
	purego.RegisterLibFunc(&a.getShaderiv, gles, "glGetShaderiv")
	purego.RegisterLibFunc(&a.getShaderInfoLog, gles, "glGetShaderInfoLog")
	purego.RegisterLibFunc(&a.deleteShader, gles, "glDeleteShader")
	purego.RegisterLibFunc(&a.createProgram, gles, "glCreateProgram")
	purego.RegisterLibFunc(&a.attachShader, gles, "glAttachShader")
	purego.RegisterLibFunc(&a.linkProgram, gles, "glLinkProgram")
	purego.RegisterLibFunc(&a.getProgramiv, gles, "glGetProgramiv")
	purego.RegisterLibFunc(&a.getProgramInfoLog, gles, "glGetProgramInfoLog")
	purego.RegisterLibFunc(&a.deleteProgram, gles, "glDeleteProgram")
	purego.RegisterLibFunc(&a.useProgram, gles, "glUseProgram")
	purego.RegisterLibFunc(&a.getAttribLocation, gles, "glGetAttribLocation")
	purego.RegisterLibFunc(&a.getUniformLocation, gles, "glGetUniformLocation")
	purego.RegisterLibFunc(&a.uniform2f, gles, "glUniform2f")
	purego.RegisterLibFunc(&a.uniform4f, gles, "glUniform4f")
	purego.RegisterLibFunc(&a.genBuffers, gles, "glGenBuffers")
	purego.RegisterLibFunc(&a.deleteBuffers, gles, "glDeleteBuffers")
	purego.RegisterLibFunc(&a.bindBuffer, gles, "glBindBuffer")
	purego.RegisterLibFunc(&a.bufferData, gles, "glBufferData")
	purego.RegisterLibFunc(&a.enableVertexAttrib, gles, "glEnableVertexAttribArray")
	purego.RegisterLibFunc(&a.vertexAttribPointer, gles, "glVertexAttribPointer")
	purego.RegisterLibFunc(&a.drawArrays, gles, "glDrawArrays")
	purego.RegisterLibFunc(&a.blendColor, gles, "glBlendColor")
	purego.RegisterLibFunc(&a.blendFuncSeparate, gles, "glBlendFuncSeparate")
	purego.RegisterLibFunc(&a.glGetError, gles, "glGetError")
	purego.RegisterLibFunc(&a.uniform1i, gles, "glUniform1i")
	purego.RegisterLibFunc(&a.genTextures, gles, "glGenTextures")
	purego.RegisterLibFunc(&a.deleteTextures, gles, "glDeleteTextures")
	purego.RegisterLibFunc(&a.bindTexture, gles, "glBindTexture")
	purego.RegisterLibFunc(&a.texParameteri, gles, "glTexParameteri")
	purego.RegisterLibFunc(&a.texImage2D, gles, "glTexImage2D")
	purego.RegisterLibFunc(&a.texSubImage2D, gles, "glTexSubImage2D")
	purego.RegisterLibFunc(&a.pixelStorei, gles, "glPixelStorei")
	purego.RegisterLibFunc(&a.readPixels, gles, "glReadPixels")
	purego.RegisterLibFunc(&a.genFramebuffers, gles, "glGenFramebuffers")
	purego.RegisterLibFunc(&a.deleteFramebuffers, gles, "glDeleteFramebuffers")
	purego.RegisterLibFunc(&a.bindFramebuffer, gles, "glBindFramebuffer")
	purego.RegisterLibFunc(&a.framebufferTexture2D, gles, "glFramebufferTexture2D")
	purego.RegisterLibFunc(&a.checkFramebufferStatus, gles, "glCheckFramebufferStatus")
	return p, a, closeLibraries, nil
}

// Open creates the EGL surface and GLES context for a renderer. The calling
// goroutine remains locked to its current OS thread until Close is called.
func Open() (_ *Renderer, err error) {
	runtime.LockOSThread()
	var (
		p              *platformAPI
		a              *api
		closeLibraries func()
		display        uintptr
		surface        uintptr
		context        uintptr
		current        bool
	)
	defer func() {
		if err == nil {
			return
		}
		if p != nil {
			if current {
				p.makeCurrent(display, 0, 0, 0)
			}
			if context != 0 {
				p.destroyContext(display, context)
			}
			if surface != 0 {
				p.destroySurface(display, surface)
			}
			if display != 0 {
				p.terminate(display)
			}
		}
		if closeLibraries != nil {
			closeLibraries()
		}
		runtime.UnlockOSThread()
	}()

	p, a, closeLibraries, err = loadAPI()
	if err != nil {
		return nil, err
	}
	display = p.getDisplay(0)
	if display == 0 {
		return nil, errors.New("eglGetDisplay returned EGL_NO_DISPLAY")
	}
	var major, minor int32
	if p.initialize(display, &major, &minor) == eglFalse {
		return nil, fmt.Errorf("eglInitialize: 0x%x", p.getError())
	}
	attrs := []int32{eglRedSize, 8, eglGreenSize, 8, eglBlueSize, 8, eglAlphaSize, 8, eglSamples, 4, eglRenderableType, eglOpenGLES2Bit, eglNone}
	var count int32
	if p.chooseConfig(display, &attrs[0], nil, 0, &count) == eglFalse || count == 0 {
		return nil, fmt.Errorf("eglChooseConfig: count=%d error=0x%x", count, p.getError())
	}
	configs := make([]uintptr, count)
	if p.chooseConfig(display, &attrs[0], &configs[0], count, &count) == eglFalse {
		return nil, fmt.Errorf("eglChooseConfig list: 0x%x", p.getError())
	}
	wanted := []struct{ attribute, value int32 }{{eglRedSize, 8}, {eglGreenSize, 8}, {eglBlueSize, 8}, {eglAlphaSize, 8}, {eglSamples, 4}}
	var config uintptr
	for _, candidate := range configs[:count] {
		matches := true
		for _, want := range wanted {
			var value int32
			if p.getConfigAttrib(display, candidate, want.attribute, &value) == eglFalse || value != want.value {
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
		return nil, errors.New("no exact RGBA8 MSAA4 EGL config")
	}
	surface = p.createWindowSurface(display, config, 0, nil)
	if surface == 0 {
		return nil, fmt.Errorf("eglCreateWindowSurface: 0x%x", p.getError())
	}
	contextAttrs := []int32{eglContextClientVers, 2, eglNone}
	context = p.createContext(display, config, 0, &contextAttrs[0])
	if context == 0 {
		return nil, fmt.Errorf("eglCreateContext: 0x%x", p.getError())
	}
	if p.makeCurrent(display, surface, surface, context) == eglFalse {
		return nil, fmt.Errorf("eglMakeCurrent: 0x%x", p.getError())
	}
	current = true
	var width, height int32
	if p.querySurface(display, surface, eglWidth, &width) == eglFalse || p.querySurface(display, surface, eglHeight, &height) == eglFalse {
		return nil, fmt.Errorf("eglQuerySurface: 0x%x", p.getError())
	}
	renderer := &Renderer{
		api: a, width: int(width), height: int(height),
		swapBuffers: func() error {
			if p.swapBuffers(display, surface) == eglFalse {
				return fmt.Errorf("eglSwapBuffers: 0x%x", p.getError())
			}
			return nil
		},
		closePlatform: func() {
			p.makeCurrent(display, 0, 0, 0)
			p.destroyContext(display, context)
			p.destroySurface(display, surface)
			p.terminate(display)
			closeLibraries()
			runtime.UnlockOSThread()
		},
		platformInfo:  fmt.Sprintf("EGL %d.%d", major, minor),
		maskGlyphs:    make(map[maskCacheKey]maskGlyph),
		imageTextures: make(map[imageCacheKey]imageTexture),
	}
	if err = renderer.initColorPipeline(); err != nil {
		return nil, err
	}
	if err = renderer.initFramebuffer(); err != nil {
		renderer.releaseGLResources()
		return nil, err
	}
	if err = renderer.initPresentPipeline(); err != nil {
		renderer.releaseGLResources()
		return nil, err
	}
	if err = renderer.initMaskPipeline(); err != nil {
		renderer.releaseGLResources()
		return nil, err
	}
	if err = renderer.initTransformPipeline(); err != nil {
		renderer.releaseGLResources()
		return nil, err
	}
	return renderer, nil
}

func RunProbe() error {
	r, err := Open()
	if err != nil {
		return err
	}
	defer r.Close()
	r.BeginFrame()
	r.Clear()
	bounds := image.Rect(0, 0, r.width, r.height)
	r.FillRect(bounds, bounds, canvas.Color(0xff1f5cc7))
	r.FillRect(image.Rect(80, 80, 360, 260), bounds, canvas.Color(0xffff8a20))
	r.FillRect(image.Rect(160, 160, 440, 340), image.Rect(200, 120, 400, 300), canvas.Color(0xff38c972))
	r.FillRect(image.Rect(300, 220, 620, 460), bounds, canvas.Color(0x808b3dff))
	probeImage := canvas.Image{Width: 160, Height: 120, Pixels: make([]byte, 160*120*4)}
	for y := 0; y < probeImage.Height; y++ {
		for x := 0; x < probeImage.Width; x++ {
			pixel := probeImage.Pixels[(y*probeImage.Width+x)*4:]
			pixel[0], pixel[1], pixel[2], pixel[3] = 0x30, 0xd0, 0xff, 0xd0
			if (x/20+y/20)%2 == 0 {
				pixel[0], pixel[1], pixel[2] = 0xe0, 0x40, 0x30
			}
		}
	}
	r.DrawImage(probeImage, image.Rect(20, 10, 150, 110), 0, 1, 1, 715, 210, image.Rect(680, 180, 790, 250), image.Rectangle{}, 0)
	cachedImages := len(r.imageTextures)
	r.DrawImage(probeImage, image.Rect(0, 0, 80, 60), 0, 1, 1, 860, 190, bounds, image.Rectangle{}, 0)
	if len(r.imageTextures) != cachedImages {
		return errors.New("GLES image texture cache missed identical storage")
	}
	r.DrawImage(probeImage, image.Rect(0, 0, probeImage.Width, probeImage.Height), 28, 1.35, 1.35, 850, 390, image.Rect(730, 270, 970, 510), image.Rectangle{}, 0)
	maskWidth, maskHeight := 127, 96
	probeMask := make([]byte, maskWidth*maskHeight)
	for y := 0; y < maskHeight; y++ {
		for x := 0; x < maskWidth; x++ {
			dx, dy := x-maskWidth/2, y-maskHeight/2
			distance := dx*dx + dy*dy
			if distance < 42*42 {
				probeMask[y*maskWidth+x] = uint8(min(255, (42*42-distance)/4))
			}
		}
	}
	r.DrawMask(probeMask, maskWidth, maskHeight, image.Pt(610, 360), image.Rect(630, 375, 720, 440), canvas.Color(0xffffe050))
	cachedMasks := len(r.maskGlyphs)
	r.DrawMask(probeMask, maskWidth, maskHeight, image.Pt(750, 360), bounds, canvas.Color(0xff50e0ff))
	if len(r.maskGlyphs) != cachedMasks {
		return errors.New("GLES mask texture cache missed identical content")
	}
	if code := r.api.glGetError(); code != glNoError {
		return fmt.Errorf("draw GLES color probe: 0x%x", code)
	}
	snapshot, ok := r.Snapshot().(*image.NRGBA)
	if !ok || snapshot.Bounds() != bounds {
		return fmt.Errorf("GLES snapshot bounds: %v", snapshot.Bounds())
	}
	if got := snapshot.NRGBAAt(100, 100); got != (color.NRGBA{R: 0xff, G: 0x8a, B: 0x20, A: 0xff}) {
		return fmt.Errorf("GLES snapshot pixel at (100,100): %v", got)
	}
	r.EndFrame()
	postSwap := r.Snapshot().(*image.NRGBA)
	if got := postSwap.NRGBAAt(100, 100); got != (color.NRGBA{R: 0xff, G: 0x8a, B: 0x20, A: 0xff}) {
		return fmt.Errorf("GLES post-swap snapshot pixel at (100,100): %v", got)
	}
	fmt.Printf("GLES renderer probe OK: %s, surface %dx%d\n", r.platformInfo, r.width, r.height)
	return nil
}
