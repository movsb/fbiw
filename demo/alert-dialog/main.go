package main

import (
	"embed"
	"os"
	"strings"

	"github.com/movsb/fbiw"
	"github.com/movsb/fbiw/input/sticks"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFontFile(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()

	doc := app.NewDesktop(embedded, `main.html`)
	buttons := doc.QuerySelectorAll[*fbiw.Button](`button`)
	status := doc.GetBoxByID[*fbiw.Text](`status`)

	buttons[0].OnClick(func() {
		app.ShowAlertDialog(doc, fbiw.AlertDialogOptions{
			Title: `使用说明`,
			Description: strings.Repeat(
				`这是一段可以使用上下键逐行滚动的长说明文字。`, 30,
			),
			ActionText: `知道了`,
			OnAction: func() {
				status.SetText(`已阅读说明`)
			},
		})
	})

	buttons[1].OnClick(func() {
		app.ShowAlertDialog(doc, fbiw.AlertDialogOptions{
			Title:         `删除存档？`,
			Description:   `此操作无法撤销。按 A 确认删除，按 B 取消。`,
			ActionText:    `删除`,
			ActionVariant: fbiw.ButtonDestructive,
			CancelText:    `取消`,
			OnAction: func() {
				status.SetText(`已删除存档`)
			},
			OnCancel: func() {
				status.SetText(`已取消删除`)
			},
		})
	})

	selected := 0
	activate := func(next int) {
		buttons[selected].ClassRemove(`selected`)
		selected = next
		buttons[selected].ClassAdd(`selected`)
		buttons[selected].Activate()
	}
	buttons[selected].ClassAdd(`selected`)
	buttons[selected].Activate()

	doc.Listen(fbiw.InputDownEvent, func(event *fbiw.Event) {
		if event.Input.Repeat {
			return
		}
		switch event.Input.Name {
		case sticks.Up:
			activate((selected - 1 + len(buttons)) % len(buttons))
		case sticks.Down:
			activate((selected + 1) % len(buttons))
		}
	})

	app.Run()
}
