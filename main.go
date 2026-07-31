package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"path/filepath"
	"runtime"
	"time"
)

// pprof 性能测试用。
//
// go tool pprof -web  http://localhost:8888/debug/pprof/profile?seconds=30
func init() {
	go http.ListenAndServe(`0.0.0.0:8888`, nil)
}

// 加载字体
func loadFonts(fontManager *FontManager) error {
	for dir, faces := range map[string][]struct {
		FileName string
		Family   string
		Bold     bool
		Italic   bool
	}{
		`fonts/`: {
			{
				FileName: `MapleMonoNormalNL-NF-CN-Regular.ttf`,
				Family:   `MapleMono`,
				Bold:     false,
				Italic:   false,
			},
			{
				FileName: `MapleMonoNormalNL-NF-CN-Italic.ttf`,
				Family:   `MapleMono`,
				Bold:     false,
				Italic:   true,
			},
			{
				FileName: `MapleMonoNormalNL-NF-CN-Bold.ttf`,
				Family:   `MapleMono`,
				Bold:     true,
				Italic:   false,
			},
			{
				FileName: `MapleMonoNormalNL-NF-CN-BoldItalic.ttf`,
				Family:   `MapleMono`,
				Bold:     true,
				Italic:   true,
			},
		},
	} {
		for _, face := range faces {
			if err := fontManager.AddFont(
				filepath.Join(dir, face.FileName),
				face.Family, face.Bold, face.Italic,
			); err != nil {
				return fmt.Errorf(`加载字体失败：%w`, err)
			}
		}
	}
	return nil
}

func main() {
	display := openDisplay()
	defer display.Close()

	fontManager := NewFontManager()
	if err := fontManager.AddFont(defaultFontFileRegular, `system`, false, false); err != nil {
		log.Fatalln(`加载默认字体失败：`, err)
	}
	if defaultFontFileBold != `` {
		if err := fontManager.AddFont(defaultFontFileBold, `system`, true, false); err != nil {
			log.Fatalln(`加载默认字体失败：`, err)
		}
	}
	if err := loadFonts(fontManager); err != nil {
		panic(`字体加载失败：` + err.Error())
	}

	canvas := &Canvas{
		buffer:        display.Data,
		bytesPerPixel: display.Bpp / 8,
		x:             0,
		y:             0,
		width:         display.Width,
		height:        display.Height,
	}

	doc := NewDocument(fontManager)
	box := loadBox(doc, _main)
	styles := loadStyles()
	applyStyles(box, []*Sheet{styles})
	box.Base().Dirty = true

	box.Calc(display.Width, display.Height)
	box.Draw(canvas)
	display.Sync()

	// 没有缓冲，满了（或处理不过来）即丢。
	ctx, cancel := context.WithCancel(context.Background())

	menuPressed := false
	startPressed := false

	pollEvents(ctx, func(event Event) {
		if event.Type == QuitEvent {
			cancel()
			return
		}
		if event.Type == KeyboardEvent {
			switch event.Keyboard.Name {
			case Menu:
				menuPressed = event.Keyboard.Pressed
			case Start:
				startPressed = event.Keyboard.Pressed
			}
			if menuPressed && startPressed {
				cancel()
				return
			}
			// 在linux上测试总是跟随按键更新重绘。
			box.Base().Dirty = true
		}
		// 在MacOS上更方便观察帧率。
		if box.Base().IsDirty() || runtime.GOOS == `darwin` {
			now := time.Now()
			box.Calc(display.Width, display.Height)
			box.Draw(canvas)
			log.Println(`帧绘制时长：`, time.Since(now).Round(time.Microsecond*100))
			display.Sync()
		}
	})
}
