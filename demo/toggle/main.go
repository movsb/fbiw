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
	toggle := doc.GetBoxByID[*fbiw.Toggle](`wifi`)
	toggle.Activate()

	app.Run()
}
