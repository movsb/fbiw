package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/movsb/fbiw"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()

	doc := app.NewDesktop(embedded, `main.html`)
	progresses := doc.QuerySelectorAll[*fbiw.ProgressBar](`progress`)
	values := []float64{0.35, 0.68, 0.82, 1}
	for index, progress := range progresses {
		if err := progress.SetValue(values[index]); err != nil {
			panic(err)
		}
	}

	// 使用文档定时器模拟一个持续更新的下载进度。
	download := progresses[0]
	downloadValue := doc.GetBoxByID[*fbiw.Text](`download-value`)
	step := 35
	var cancelTimer func()
	var updateProgress func()
	updateProgress = func() {
		step++
		if step > 100 {
			step = 0
		}
		if err := download.SetValue(float64(step) / 100); err != nil {
			panic(err)
		}
		downloadValue.SetText(fmt.Sprintf(`%d/100`, step))
		cancelTimer = doc.SetTimeout(60, updateProgress)
	}
	cancelTimer = doc.SetTimeout(60, updateProgress)
	defer func() { cancelTimer() }()

	app.Run()
}
