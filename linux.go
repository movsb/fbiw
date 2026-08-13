//go:build linux

package fbiw

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func openDisplay() *Display {
	d := &Display{}

	fd, err := unix.Open("/dev/fb0", unix.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}

	var v [256]uint32
	unix.Syscall(unix.SYS_IOCTL, uintptr(fd), 0x4600, uintptr(unsafe.Pointer(&v[0])))

	xRes, yRes, bpp := int(v[0]), int(v[1]), int(v[6])
	fmt.Printf("xRes=%d yRes=%d bpp=%d\n", xRes, yRes, bpp)
	d.Width = xRes
	d.Height = yRes
	d.Bpp = 32

	stride := xRes * bpp / 8
	s, _ := os.ReadFile("/sys/class/graphics/fb0/stride")
	if n, err := strconv.ParseInt(strings.TrimSpace(string(s)), 10, 64); err == nil && n > 0 {
		stride = int(n)
	}
	fmt.Printf("stride=%d\n", stride)
	d.Stride = stride

	mapSize := stride * yRes
	fmt.Printf("mmap size=%d ... ", mapSize)
	data, err := unix.Mmap(fd, 0, mapSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		panic(err)
	}
	fmt.Println("OK!")

	d.Data = make([]byte, len(data))

	d.close = func() {
		unix.Munmap(data)
		unix.Close(fd)
	}

	d.sync = func() {
		copy(data, d.Data)
		// 对fb来说，很难有用，非原子的。
		waitForVSync(fd)
		// 任何时候改offset都能导致直接从新的地方读，跟v sync无关，fb的缺陷。
		// 有一点用：有些程序会切换到其它地方写，我接管后强制切回来。
		setYOffset(fd, 0)
	}

	return d
}

type fbVarScreenInfo struct {
	Xres         uint32
	Yres         uint32
	XresVirtual  uint32
	YresVirtual  uint32
	Xoffset      uint32
	Yoffset      uint32
	BitsPerPixel uint32
	Grayscale    uint32
	_            [200]byte
}

func setYOffset(fd int, y uint32) error {
	var v fbVarScreenInfo

	// 先读取当前参数
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(0x4600), // FBIOGET_VSCREENINFO
		uintptr(unsafe.Pointer(&v)),
	)
	if errno != 0 {
		return errno
	}

	v.Yoffset = y

	// 切换显示区域
	_, _, errno = unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(0x4606), // FBIOPAN_DISPLAY
		uintptr(unsafe.Pointer(&v)),
	)
	if errno != 0 {
		return errno
	}

	return nil
}

const FBIO_WAITFORVSYNC = 0x4680

func waitForVSync(fd int) error {
	var arg uint32

	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(FBIO_WAITFORVSYNC),
		uintptr(unsafe.Pointer(&arg)),
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func pollEvents(ctx context.Context, system chan *Event, sync func(), handler func(*Event)) {
	ch := make(chan *Event)
	go _pollEvents(ctx, func(e *Event) {
		select {
		case ch <- e:
			// default:
		}
	})
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-ch:
			handler(e)
		case e := <-system:
			switch e.Type {
			case asyncCallback:
				e.asyncCallback()
			case appDirty:
			default:
				handler(e)
			}
		}
		sync()
	}
}

func _pollEvents(ctx context.Context, handler func(*Event)) {
	matches, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		panic(err)
	}
	if len(matches) == 0 {
		fmt.Println("未检测到输入事件设备")
		return
	}

	device := matches[3]
	fmt.Println("发现设备：", device)

	f, err := os.Open(device)
	if err != nil {
		fmt.Printf("打开 %s 失败: %v\n", device, err)
		return
	}
	defer f.Close()

	// fmt.Println("正在监听按键事件...")

	ev := struct {
		Time  syscall.Timeval
		Type  uint16
		Code  uint16
		Value int32
	}{}

	send := func(name KeyName, pressed bool) {
		handler(&Event{
			Type: Iif(pressed, StickDownEvent, StickUpEvent),
			Stick: KeyEventArgs{
				Name: name,
			},
		})
	}

	keyMaps := map[uint16]KeyName{
		305: A,
		304: B,
		307: Y,
		308: X,
		316: Menu,
		314: Select,
		315: Start,
		310: L1,
		311: R1,
		59:  Fn1,
		60:  Fn2,
		115: VolumeUp,
		114: VolumeDown,
		173: Home,
	}

	var upOrLeft bool

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := binary.Read(f, binary.LittleEndian, &ev); err != nil {
			fmt.Println("读取失败:", err)
			return
		}
		switch ev.Type {
		case 1:
			// 重复按键
			if ev.Code == 2 {
				continue
			}
			pressed := ev.Value == 1
			if mapped, ok := keyMaps[ev.Code]; ok {
				send(mapped, pressed)
			}
		case 3:
			switch ev.Code {
			// 好奇怪，同时用17表示上和下，松开都是0，无法直接区分。
			case 17:
				switch ev.Value {
				case -1:
					send(Up, true)
					upOrLeft = true
				case 1:
					send(Down, true)
					upOrLeft = false
				case 0:
					if upOrLeft {
						send(Up, false)
					} else {
						send(Down, false)
					}
				}
			case 16:
				switch ev.Value {
				case -1:
					send(Left, true)
					upOrLeft = true
				case 1:
					send(Right, true)
					upOrLeft = false
				case 0:
					if upOrLeft {
						send(Left, false)
					} else {
						send(Right, false)
					}
				}
			}
		}
		// fmt.Printf("Keyboard: type=%d code=%d value=%d\n", ev.Type, ev.Code, ev.Value)
	}
}
