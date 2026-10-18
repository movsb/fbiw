package main

import (
	"embed"
	"fmt"
	"os"
	"strconv"

	"github.com/movsb/fbiw"
	"github.com/movsb/fbiw/input/sticks"
)

//go:embed main.html background.png
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFontFile(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()
	doc := app.NewDesktop(embedded, `main.html`)
	preview := doc.GetBoxByID[*fbiw.Block](`preview`)
	status := doc.GetBoxByID[*fbiw.Text](`status`)
	width := 2
	colors := []string{`#000000a0`, `#000000ff`, `#183060ff`}
	names := []string{`半透明黑色`, `不透明黑色`, `深蓝色`}
	colorIndex := 0
	update := func() {
		if err := preview.SetProp(`text-outline-width`, strconv.Itoa(width)); err != nil {
			panic(err)
		}
		if err := preview.SetProp(`text-outline-color`, colors[colorIndex]); err != nil {
			panic(err)
		}
		status.SetText(fmt.Sprintf(`自定义：%d px · %s`, width, names[colorIndex]))
	}
	update()
	doc.Listen(fbiw.InputDownEvent, func(e *fbiw.Event) {
		switch e.Input.Name {
		case sticks.Left:
			width = max(0, width-1)
		case sticks.Right:
			width = min(8, width+1)
		case sticks.A:
			colorIndex = (colorIndex + 1) % len(colors)
		case sticks.B:
			width, colorIndex = 2, 0
		default:
			return
		}
		update()
	})
	app.Run()
}
