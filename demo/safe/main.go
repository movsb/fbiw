package main

import (
	"embed"
	_ "embed"
	_ "net/http/pprof"
	"os"

	"github.com/movsb/fbiw"
	"github.com/movsb/fbiw/input/sticks"
)

//go:embed *.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()

	doc := app.NewDesktop(embedded, `main.html`)

	overlay := app.NewOverlay(embedded, `status.html`)
	app.SetOverlay(overlay)

	text := doc.QuerySelector[*fbiw.Text](`#scroll`)
	text.Activate()
	text.Listen(fbiw.InputDownEvent, func(e *fbiw.Event) {
		switch e.Input.Name {
		case sticks.Up:
			text.ScrollLineUp()
		case sticks.Down:
			text.ScrollLineDown()
		}
	})

	app.Run()
}
