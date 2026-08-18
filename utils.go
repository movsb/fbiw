package fbiw

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/goccy/go-yaml"
	"golang.org/x/sys/unix"
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

func captureStdoutStderr(w io.Writer) error {
	r, pipeWriter, err := os.Pipe()
	if err != nil {
		return err
	}

	if err := unix.Dup2(int(pipeWriter.Fd()), int(os.Stdout.Fd())); err != nil {
		r.Close()
		pipeWriter.Close()
		return err
	}
	if err := unix.Dup2(int(pipeWriter.Fd()), int(os.Stderr.Fd())); err != nil {
		r.Close()
		pipeWriter.Close()
		return err
	}
	pipeWriter.Close()

	go func() {
		defer r.Close()
		io.Copy(w, r)
	}()

	return nil
}

func init() {
	if runtime.GOOS == `linux` {
		// 文件不用关。
		logFile, err := os.OpenFile(`/tmp/fbiw.log`, os.O_WRONLY|os.O_CREATE|os.O_APPEND|os.O_SYNC, 0600)
		if err == nil {
			captureStdoutStderr(logFile)
		}
	}
}

func LoadTestCases[T any](path string) []*T {
	fp, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer fp.Close()

	var t []*T

	if err := yaml.NewDecoder(fp, yaml.DisallowUnknownField()).Decode(&t); err != nil {
		panic(err)
	}

	return t
}

// 返回此函数的调用者的目录文件系统。
func CallerDir() fs.FS {
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		panic(`无法获取路径。`)
	}
	return os.DirFS(filepath.Dir(file))
}
