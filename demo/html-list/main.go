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
	viewport := doc.GetBoxByID[*fbiw.Scroll](`viewport`)
	content := doc.GetBoxByID[fbiw.Box](`content`)
	extra := doc.GetBoxByID[*widgets.ListItem](`optional-step`)
	status := doc.GetBoxByID[*fbiw.Text](`status`)

	fontSize := 22
	extraVisible := true
	updateStatus := func() {
		state := `隐藏可选步骤`
		if !extraVisible {
			state = `显示可选步骤`
		}
		status.SetText(fmt.Sprintf(`字号 %d　方向键滚动　L1/R1 调整字号　A：%s`, fontSize, state))
	}
	updateStatus()

	viewport.Listen(fbiw.InputDownEvent, func(event *fbiw.Event) {
		if event.Input.Repeat {
			return
		}
		switch event.Input.Name {
		case sticks.L1:
			fontSize = max(16, fontSize-2)
			if err := content.SetProp(`font-size`, strconv.Itoa(fontSize)); err != nil {
				panic(err)
			}
		case sticks.R1:
			fontSize = min(36, fontSize+2)
			if err := content.SetProp(`font-size`, strconv.Itoa(fontSize)); err != nil {
				panic(err)
			}
		case sticks.A:
			extraVisible = !extraVisible
			if err := extra.SetProp(`display`, strconv.FormatBool(extraVisible)); err != nil {
				panic(err)
			}
		default:
			return
		}
		updateStatus()
		event.StopPropagation()
	})
	viewport.Activate()
	app.Run()
}
