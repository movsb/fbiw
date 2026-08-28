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
	selects := doc.QuerySelectorAll[*fbiw.SelectBox](`select`)
	status := doc.GetBoxByID[*fbiw.Text](`status`)

	selects[0].SetItems([]string{`简体中文`, `English`, `日本語`, `한국어`})
	selects[1].SetItems([]string{`扬声器`, `蓝牙耳机`, `HDMI`, `USB DAC`, `网络音箱`, `虚拟设备`, `默认设备`, `远程设备`, `测试设备`})
	_ = selects[1].SetIndex(2)
	selects[3].SetItems([]string{`不可选择`})

	for index, selectBox := range selects {
		selectBox.OnChange(func(selected int) {
			value, _ := selectBox.Selected()
			status.SetText(fmt.Sprintf(`第 %d 项：%s`, index+1, value))
		})
	}

	selected := 0
	activate := func(next int) {
		selects[selected].ClassRemove(`active`)
		selected = next
		selects[selected].ClassAdd(`active`)
		selects[selected].Activate()
	}
	selects[0].ClassAdd(`active`)
	selects[0].Activate()

	doc.Listen(fbiw.StickDownEvent, func(event *fbiw.Event) {
		if event.Stick.Repeat {
			return
		}
		switch event.Stick.Name {
		case fbiw.Up:
			activate((selected - 1 + len(selects)) % len(selects))
		case fbiw.Down:
			activate((selected + 1) % len(selects))
		}
	})

	app.Run()
}
