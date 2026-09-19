package ports

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/movsb/fbiw/input"
	"github.com/movsb/fbiw/input/sticks"
	"github.com/movsb/fbiw/internal/canvas"
	"github.com/movsb/fbiw/internal/canvas/cpu"
	"github.com/movsb/fbiw/internal/canvas/gpu/gles"
	"github.com/movsb/fbiw/internal/event"
	"golang.org/x/sys/unix"
)

func OpenDisplay() canvas.Renderer {
	renderer, err1 := gles.Open()
	if err1 == nil {
		return renderer
	} else {
		log.Println(`failed to open gpu:`, err1)
	}

	display := openFramebuffer()
	width, height, stride := display.GetSize()
	if stride != width*4 {
		panic(`暂时不支持Stride!=Width*4的显示设备。`)
	}
	r := cpu.New(width, height)
	r.Display = display
	return r
}

type _FramebufferDisplay struct {
	width, height, stride int

	fd     int
	mapped []byte
}

func (d *_FramebufferDisplay) GetSize() (int, int, int) {
	return d.width, d.height, d.stride
}

func (d *_FramebufferDisplay) Sync(pixels []byte) {
	copy(d.mapped, pixels)
	// 对fb来说，很难有用，非原子的。
	waitForVSync(d.fd)
	// 任何时候改offset都能导致直接从新的地方读，跟v sync无关，fb的缺陷。
	// 有一点用：有些程序会切换到其它地方写，我接管后强制切回来。
	setYOffset(d.fd, 0)
}

func (d *_FramebufferDisplay) Close() {
	unix.Munmap(d.mapped)
	unix.Close(d.fd)
}

func openFramebuffer() *_FramebufferDisplay {
	if os.Getenv("FBIW_GPU_PROBE") == "1" {
		if err := gles.RunProbe(); err != nil {
			panic(err)
		}
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}

	fd, err := unix.Open("/dev/fb0", unix.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}

	var v [256]uint32
	unix.Syscall(unix.SYS_IOCTL, uintptr(fd), 0x4600, uintptr(unsafe.Pointer(&v[0])))

	xRes, yRes, bpp := int(v[0]), int(v[1]), int(v[6])
	fmt.Printf("xRes=%d yRes=%d bpp=%d\n", xRes, yRes, bpp)

	stride := xRes * bpp / 8
	s, _ := os.ReadFile("/sys/class/graphics/fb0/stride")
	if n, err := strconv.ParseInt(strings.TrimSpace(string(s)), 10, 64); err == nil && n > 0 {
		stride = int(n)
	}
	fmt.Printf("stride=%d\n", stride)

	mapSize := stride * yRes
	fmt.Printf("mmap size=%d ... ", mapSize)
	data, err := unix.Mmap(fd, 0, mapSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		panic(err)
	}
	fmt.Println("OK!")

	return &_FramebufferDisplay{
		width:  xRes,
		height: yRes,
		stride: stride,

		fd:     fd,
		mapped: data,
	}
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

func PollEvents(
	ctx context.Context, cancel context.CancelFunc,
	unblock chan struct{}, unblockHandler func(),
	sync func(), eventHandler func(*event.Message),
) {
	keyEvents := make(chan *event.Message)
	go _pollKeyboardEvents(ctx, func(e *event.Message) {
		select {
		case keyEvents <- e:
			// default:
		}
	})
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-keyEvents:
			eventHandler(e)
		case <-unblock:
			unblockHandler()
		}
		sync()
	}
}

func _pollKeyboardEvents(ctx context.Context, handler func(*event.Message)) {
	// 系统服务启动较早时虚拟手柄还不存在，持续等待，不能因为一次 glob 结果
	// 不足四项就退出甚至访问 matches[3] 越界。
	device := ""
	for device == "" {
		names, _ := filepath.Glob("/sys/class/input/event*/device/name")
		for _, namePath := range names {
			name, _ := os.ReadFile(namePath)
			if strings.TrimSpace(string(name)) == "TRIMUI Player1" {
				event := filepath.Base(filepath.Dir(filepath.Dir(namePath)))
				device = filepath.Join("/dev/input", event)
				break
			}
		}
		if device == "" {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
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

	repeater := newKeyRepeater(ctx, 500*time.Millisecond, 100*time.Millisecond, handler)
	defer repeater.close()
	send := repeater.send

	// EV_ABS 的方向键在松开时只上报 0，所以分别记录两个轴当前
	// 按下的方向。不能共用一个 bool，否则同时操作横纵轴会串键。
	axes := map[uint16]input.Name{}
	sendAxis := func(code uint16, value int32, negative, positive input.Name) {
		if value == 0 {
			if old, ok := axes[code]; ok {
				send(old, false)
				delete(axes, code)
			}
			return
		}

		name := positive
		if value < 0 {
			name = negative
		}
		if old, ok := axes[code]; ok {
			if old == name {
				return
			}
			send(old, false)
		}
		axes[code] = name
		send(name, true)
	}

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
			if mapped, ok := keyMaps[ev.Code]; ok {
				switch ev.Value {
				case 0:
					send(mapped, false)
				case 1:
					send(mapped, true)
				case 2:
					// 内核自动重复。这里统一由 keyRepeater 产生，避免
					// 不同输入设备的重复行为和频率不一致。
				}
			}
		case 3:
			switch ev.Code {
			case 17:
				sendAxis(ev.Code, ev.Value, sticks.Up, sticks.Down)
			case 16:
				sendAxis(ev.Code, ev.Value, sticks.Left, sticks.Right)
			}
		}
		fmt.Printf("Keyboard: type=%d code=%d value=%d\n", ev.Type, ev.Code, ev.Value)
	}
}

/*

/usr/include/linux/input-event-codes.h

#define BTN_SOUTH               0x130       (304)
#define BTN_A                   BTN_SOUTH
#define BTN_EAST                0x131       (305)
#define BTN_B                   BTN_EAST
#define BTN_NORTH               0x133       (307)
#define BTN_X                   BTN_NORTH
#define BTN_WEST                0x134       (308)
#define BTN_Y                   BTN_WEST


         307 (307)
304(308)            308(305)
         305 (304)

*/

var keyMaps = map[uint16]input.Name{
	305: sticks.A,
	304: sticks.B,
	307: sticks.Y,
	308: sticks.X,
	316: sticks.Menu,
	314: sticks.Select,
	315: sticks.Start,
	310: sticks.L1,
	311: sticks.R1,
	59:  sticks.Fn1,
	60:  sticks.Fn2,
	115: sticks.VolumeUp,
	114: sticks.VolumeDown,
	173: sticks.Home,
}
