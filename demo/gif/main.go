package main

import (
	"embed"
	"os"

	"github.com/movsb/fbiw"
)

//go:embed main.html brick.gif
var assets embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFontFile(os.DirFS(".."), "regular.ttf"))
	defer app.Close()
	doc := app.NewDesktop(assets, "main.html")
	doc.GetBoxByID[*fbiw.Image]("original").SetPath(nil, `brick.gif`)
	doc.GetBoxByID[*fbiw.Image]("scaled").SetPath(nil, `brick.gif`)
	app.Run()
}
