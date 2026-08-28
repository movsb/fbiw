package main

import (
	"embed"
	"os"

	"github.com/movsb/fbiw"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()

	doc := app.NewDesktop(embedded, `main.html`)
	buttons := doc.QuerySelectorAll[*fbiw.Button](`button`)
	status := doc.GetBoxByID[*fbiw.Text](`status`)
	labels := []string{`主按钮`, `普通按钮`, `危险操作`, `禁用按钮`}
	for index, button := range buttons {
		button.OnClick(func() {
			status.SetText(`点击：` + labels[index])
		})
	}

	selected := 0
	activate := func(next int) {
		buttons[selected].ClassRemove(`selected`)
		selected = next
		buttons[selected].ClassAdd(`selected`)
		buttons[selected].Activate()
	}
	buttons[selected].ClassAdd(`selected`)
	buttons[selected].Activate()

	doc.Listen(fbiw.StickDownEvent, func(event *fbiw.Event) {
		if event.Stick.Repeat {
			return
		}
		switch event.Stick.Name {
		case fbiw.Up:
			activate((selected - 1 + len(buttons)) % len(buttons))
		case fbiw.Down:
			activate((selected + 1) % len(buttons))
		}
	})

	app.Run()
}
