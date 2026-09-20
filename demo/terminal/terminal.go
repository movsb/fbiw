package main

import (
	"bufio"
	"embed"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/movsb/fbiw"
	"github.com/rcarmo/go-te/pkg/te"

	"golang.org/x/image/font/gofont/gomono"
)

type Terminal struct {
	fbiw.BaseBox

	// 默认文字颜色。
	defaultColor fbiw.Color

	// 当前使用的窗口大小。
	old fbiw.Rect

	// 水平和垂直间距。
	spacingHorizontal, spacingVertical int
	// 格子的字符宽高（英语，中文需要x2）
	cellWidth, cellHeight int

	// 伪终端控制端
	// 不需要锁保护，线程里面只使用一次，然后主线程写入。
	// 后期可以考虑在其它线程中写。
	master *os.File

	// 屏幕缓冲。
	// 可能无，比如窗口大小设置失败的时候。
	// 它们在线程中被写入，需要保护起来。
	screen *te.Screen
	stream *te.Stream
	lock   sync.Mutex
}

func NewTerminal(doc *fbiw.Document) *Terminal {
	return &Terminal{
		BaseBox: fbiw.NewBaseBox(doc, `terminal`),
		// 不使用黑、白的原因是防止可能默认的黑、白背景相同，然后啥也看不见。
		defaultColor: fbiw.ColorFromRGBA(0xFF, 0, 0, 0xFF),
	}
}

func init() {
	fbiw.Define(`terminal`, true, NewTerminal)
}

func (b *Terminal) SetProp(key, val string) error {
	switch key {
	case `default-color`:
		val, err := fbiw.ParseColor(val)
		if err != nil {
			return err
		}
		b.defaultColor = val
		// NOTE 暂未支持动态修改
		return nil
	default:
		return b.Base().SetProp(key, val)
	}
}

func (b *Terminal) Draw(canvas *fbiw.Canvas) {
	if new := b.BaseBox.GetLayoutBox(); new != b.old {
		b.old = new
		if b.master == nil {
			b.start()
		} else {
			b.resize()
		}
	}

	b.lock.Lock()
	defer b.lock.Unlock()

	faces := b.Document().LoadFaces(b)
	offsetX, offsetY := b.InsetLeft(), b.InsetTop()

	for r := range b.screen.Lines {
		for c := range b.screen.Columns {
			canvas := canvas.Offset(offsetX, offsetY)
			cell := b.screen.Buffer[r][c]

			fgColor := b.defaultColor
			// TODO 处理颜色查表。

			// 非默认背景色时暂时简单描个反色。
			if cell.Attr.Bg.Mode != te.ColorDefault {
				bgColor := b.defaultColor
				canvas.FillRect(0, 0, b.cellWidth, b.cellHeight, bgColor)
				fgColor = fbiw.ColorNone
			}

			canvas.DrawString(cell.Data, faces, fgColor)

			offsetX += b.cellWidth + b.spacingHorizontal
		}
		offsetY += b.cellHeight + b.spacingVertical
		offsetX = b.InsetLeft()
	}

	if cursor := b.screen.Cursor; !cursor.Hidden {
		offsetX := b.InsetLeft() + (b.cellWidth+b.spacingHorizontal)*cursor.Col
		offsetY := b.InsetTop() + (b.cellHeight+b.spacingVertical)*cursor.Row
		canvas := canvas.Offset(offsetX, offsetY)
		canvas.FillRect(0, 0+3, b.cellWidth, b.cellHeight-6, fbiw.ColorFromRGBA(0, 0, 0, 0xFF))
	}
}

func (b *Terminal) resize() {
	defer log.Println(`已重新调整终端大小。`)

	// 粗略根据当前字体计算格子的大小。
	faces := b.Document().LoadFaces(b)

	// 高度应该是各字符统一的。
	b.cellHeight = faces[0].TextHeight()

	// 英文和中文（日、韩同）
	advance, ok := faces[0].GlyphAdvance('A')
	if !ok {
		// ??? 还有不包含alphabetic的script？
		log.Println(`无法获取字体宽度`)
		return
	}
	if h2, ok := faces[0].GlyphAdvance('桃'); ok {
		if math.Abs(float64(h2.Ceil())/float64(advance.Ceil())-2) > 0.001 {
			log.Println(`可能使用了非等宽字体，渲染可能错位`)
		}

	} else {
		log.Println(`字体可能不支持中文`)
	}

	// 英语字符的宽度
	b.cellWidth = advance.Ceil()

	maxWindowWidth := b.old.Width - b.HorizontalInsets()
	maxWindowHeight := b.old.Height - b.VerticalInsets()

	cols := (maxWindowWidth + b.spacingHorizontal) / (b.cellWidth + b.spacingHorizontal)
	rows := (maxWindowHeight + b.spacingVertical) / (b.cellHeight + b.spacingVertical)

	if cols < 10 || rows < 5 {
		log.Println(`警告：可能没设置终端窗口的大小。`)
	}

	// 先更新屏幕缓冲，然后再告诉终端。
	b.screen = te.NewScreen(cols, rows)
	b.stream = te.NewStream(b.screen, false)

	if err := pty.Setsize(b.master, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}); err != nil {
		log.Println(`设置终端窗口大小失败:`, err)
		b.stop()
		return
	}
}

func (b *Terminal) stop() {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.master.Close()
	b.master = nil
	b.screen = nil
	b.stream = nil
}

func (b *Terminal) start() {
	sh := os.Getenv(`SHELL`)
	if sh == `` {
		sh = `/bin/sh`
	}

	// 默认是 /，对于需要访问 HOME 的用户来说不友好。
	// 比如终端程序。
	home, _ := os.UserHomeDir()
	// 游戏机上面莫名其秒搞成/就很离谱。
	if home == `` || (runtime.GOOS == `linux` && home == `/`) {
		home = `/root`
	}

	cmd := exec.Command(sh, `-l`)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf(`HOME=%s`, home),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
	master, err := pty.Start(cmd)
	if err != nil {
		log.Println(err)
		return
	}

	b.master = master

	b.resize()

	go func() {
		reader := bufio.NewReader(master)
		buf := make([]byte, 4096)

		for {
			b.lock.Lock()
			screen := b.screen
			b.lock.Unlock()

			n, err := reader.Read(buf)
			if err != nil {
				return
			}

			b.lock.Lock()
			if screen == b.screen && b.stream != nil {
				b.stream.FeedBytes(buf[:n])
			}
			b.lock.Unlock()

			// 任何有数据流入都尝试更新？
			b.Document().RequestPaintAsync()
		}
	}()

	go func() {
		for {
			time.Sleep(time.Second)
			b.master.WriteString("date;ifconfig\n")
			time.Sleep(time.Second * 5)
			b.master.Write([]byte("\x1b[2J\x1b[H"))
		}
	}()
}

//go:embed terminal.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(
		fbiw.WithSystemFontData(gomono.TTF),
	)
	defer app.Close()

	app.NewDesktop(embedded, `terminal.html`)

	app.Run()
}
