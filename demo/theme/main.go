package main

import (
	"embed"
	"os"

	"github.com/movsb/fbiw"
)

//go:embed main.html light.css dark.css
var embedded embed.FS

func main() {
	app := fbiw.NewApp(
		fbiw.WithSystemFont(os.DirFS(`..`), `regular.ttf`),
		fbiw.WithThemeLight(embedded, `light.css`),
		fbiw.WithThemeDark(embedded, `dark.css`),
	)
	defer app.Close()

	doc := app.NewDesktop(embedded, `main.html`)
	button := doc.GetBoxByID[*fbiw.Button](`switch`)
	label := doc.GetBoxByID[*fbiw.Text](`switch-label`)
	status := doc.GetBoxByID[*fbiw.Text](`status`)
	manual := false
	button.OnClick(func() {
		if manual {
			if err := app.SetThemeLight(`light`); err != nil {
				status.SetText(`恢复失败：` + err.Error())
				return
			}
			if err := app.SetThemeDark(`dark`); err != nil {
				status.SetText(`恢复失败：` + err.Error())
				return
			}
			manual = false
			label.SetText(`手动切换主题`)
			status.SetText(`已恢复自动模式，当前：` + app.ThemeName())
			return
		}

		next := `dark`
		if app.ThemeName() == `dark` {
			next = `light`
		}
		if err := app.SetThemeLight(next); err != nil {
			status.SetText(`切换失败：` + err.Error())
			return
		}
		if err := app.SetThemeDark(next); err != nil {
			status.SetText(`切换失败：` + err.Error())
			return
		}
		manual = true
		label.SetText(`恢复自动模式`)
		status.SetText(`手动主题：` + app.ThemeName())
	})
	button.ClassAdd(`selected`)
	button.Activate()

	app.Run()
}
