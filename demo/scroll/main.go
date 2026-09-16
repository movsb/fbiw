package main

import (
	"embed"
	"log"
	"os"

	"github.com/movsb/fbiw"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()

	doc := app.NewDesktop(embedded, `main.html`)
	scroll := doc.GetBoxByID[*fbiw.Scroll](`scroll`)
	scroll.Listen(fbiw.ScrollChange, func(event *fbiw.Event) {
		position := event.Data[fbiw.ScrollChangeArgs]()
		log.Printf("滚动位置：x=%d y=%d", position.X, position.Y)
	})
	scroll.Listen(fbiw.ScrollEnd, func(event *fbiw.Event) {
		position := event.Data[fbiw.ScrollEndArgs]()
		log.Printf("滚动结束：x=%d y=%d", position.X, position.Y)
	})
	scroll.Activate()
	app.Run()
}
