//go:build linux

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

func loop(render func(buffer []byte)) {
	fd, err := unix.Open("/dev/fb0", unix.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer unix.Close(fd)

	var v [256]uint32
	unix.Syscall(unix.SYS_IOCTL, uintptr(fd), 0x4600, uintptr(unsafe.Pointer(&v[0])))

	xres, yres, bpp := int(v[0]), int(v[1]), int(v[6])
	fmt.Printf("xres=%d yres=%d bpp=%d\n", xres, yres, bpp)

	stride := xres * bpp / 8
	s, _ := os.ReadFile("/sys/class/graphics/fb0/stride")
	if n, err := strconv.ParseInt(strings.TrimSpace(string(s)), 10, 64); err == nil && n > 0 {
		stride = int(n)
	}
	fmt.Printf("stride=%d\n", stride)

	mapSize := stride * yres
	fmt.Printf("mmap size=%d ... ", mapSize)
	data, err := unix.Mmap(fd, 0, mapSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		panic(err)
	}
	defer unix.Munmap(data)
	fmt.Println("OK!")

	dup := make([]byte, len(data))

	for {
		render(dup)
		copy(data, dup)
	}
}
