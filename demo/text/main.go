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
	app.Show(doc)
	app.Run()
}
