package main

import (
	"embed"
	_ "embed"
	_ "net/http/pprof"
	"os"

	"github.com/movsb/fbiw"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()
	doc := app.New(embedded, `main.html`)
	text := doc.QuerySelector[*fbiw.Text](`text`)
	doc.Listen(fbiw.StickDownEvent, handle(text), fbiw.EventOptions{})
	app.Show(doc)
	app.Run()
}

func handle(text *fbiw.Text) func(e *fbiw.Event) {
	return func(e *fbiw.Event) {
		switch e.Stick.Name {
		case fbiw.Left:
			text.PageLeft()
		case fbiw.Right:
			text.PageRight()
		case fbiw.Up:
			text.ScrollLineUp()
		case fbiw.Down:
			text.ScrollLineDown()
		}
	}
}
