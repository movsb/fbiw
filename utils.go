package fbiw

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

func Must(err error) {
	if err != nil {
		panic(err)
	}
}

func Must1[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

func DropLast1[T1 any, T2 any](t1 T1, t2 T2) T1 {
	return t1
}

func MustParseInt(s string) int {
	return Must1(strconv.Atoi(s))
}

func Iif[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

// 返回此函数的调用者的目录文件系统。
func CallerDir() fs.FS {
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		panic(`无法获取路径。`)
	}
	return os.DirFS(filepath.Dir(file))
}
