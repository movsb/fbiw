package main

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"time"

	"github.com/movsb/fbiw"
)

// pprof 性能测试用。
//
// go tool pprof -web  http://localhost:8888/debug/pprof/profile?seconds=30
func init() {
	go http.ListenAndServe(`0.0.0.0:8888`, nil)
}

func initFonts(app *fbiw.App) {
	if err := app.AddFont(
		`system`, false, false,
		`./fonts/MapleMonoNormalNL-NF-CN-Regular.ttf`,
	); err != nil {
		log.Panic(`加载默认字体失败：`, err)
	}
	for dir, faces := range map[string][]struct {
		FileName string
		Family   string
		Bold     bool
		Italic   bool
	}{
		`fonts/`: {
			{
				FileName: `MapleMonoNormalNL-NF-CN-Italic.ttf`,
				Family:   `system`,
				Bold:     false,
				Italic:   true,
			},
			{
				FileName: `MapleMonoNormalNL-NF-CN-Bold.ttf`,
				Family:   `system`,
				Bold:     true,
				Italic:   false,
			},
			{
				FileName: `MapleMonoNormalNL-NF-CN-BoldItalic.ttf`,
				Family:   `system`,
				Bold:     true,
				Italic:   true,
			},
		},
	} {
		for _, face := range faces {
			if err := app.AddFont(
				face.Family, face.Bold, face.Italic,
				filepath.Join(dir, face.FileName),
			); err != nil {
				log.Printf(`加载字体失败：%w`, err)
			}
		}
	}
}

func main() {
	app := fbiw.NewApp(context.Background(), os.DirFS(`.`))
	defer app.Close()

	initFonts(app)

	doc := app.New(`main.html`, `skin`)

	txtTime := doc.QuerySelector(`#time`).(*fbiw.Text)
	go func() {
		for range time.Tick(time.Second * 1) {
			app.Async(func() {
				now := time.Now().Format(`15:04:05`)
				txtTime.SetText(now)
			})
		}
	}()

	app.Show(doc)

	app.Run()
}
