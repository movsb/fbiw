package main

import (
	"embed"
	"fmt"
	"os"
	"strconv"

	"github.com/movsb/fbiw"
	"github.com/movsb/fbiw/input/sticks"
	"github.com/movsb/fbiw/widgets"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFontFile(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()

	doc := app.NewDesktop(embedded, `main.html`)
	widgetTable := doc.GetBoxByID[*widgets.Table](`devices`)
	detail := doc.GetBoxByID[*widgets.TableRow](`detail-row`)
	status := doc.GetBoxByID[*fbiw.Text](`status`)

	fontSize := 22
	detailVisible := true
	set := func(box fbiw.Box, name, value string) {
		if err := box.SetProp(name, value); err != nil {
			panic(err)
		}
	}
	updateStatus := func() {
		state := `显示扩展行`
		if detailVisible {
			state = `隐藏扩展行`
		}
		status.SetText(fmt.Sprintf(`字号 %d　L1/R1 调整字号　A：%s`, fontSize, state))
	}
	updateStatus()

	widgetTable.Listen(fbiw.InputDownEvent, func(event *fbiw.Event) {
		if event.Input.Repeat {
			return
		}
		switch event.Input.Name {
		case sticks.L1:
			fontSize = max(16, fontSize-2)
			set(widgetTable, `font-size`, strconv.Itoa(fontSize))
		case sticks.R1:
			fontSize = min(36, fontSize+2)
			set(widgetTable, `font-size`, strconv.Itoa(fontSize))
		case sticks.A:
			detailVisible = !detailVisible
			set(detail, `display`, strconv.FormatBool(detailVisible))
		default:
			return
		}
		updateStatus()
		event.StopPropagation()
	})
	widgetTable.Activate()

	app.Run()
}
