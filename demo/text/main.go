package main

import (
	"embed"
	_ "embed"
	_ "net/http/pprof"
	"os"

	"github.com/movsb/fbiw"
	"github.com/movsb/fbiw/input/sticks"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFontFile(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()
	doc := app.NewDesktop(embedded, `main.html`)
	text := doc.QuerySelector[*fbiw.Text](`text`)
	doc.Listen(fbiw.InputDownEvent, handle(text))
	app.Run()
}

func handle(text *fbiw.Text) func(e *fbiw.Event) {
	return func(e *fbiw.Event) {
		switch e.Input.Name {
		case sticks.Left:
			text.PageLeft()
		case sticks.Right:
			text.PageRight()
		case sticks.Up:
			text.ScrollLineUp()
		case sticks.Down:
			text.ScrollLineDown()
		}
	}
}
