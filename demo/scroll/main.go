package main

import (
	"embed"
	_ "embed"
	"fmt"
	_ "net/http/pprof"
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

	type _Item struct {
		root fbiw.Box
		text *fbiw.Text `css:"text"`
	}

	scroll.SetItems(7,
		func() (fbiw.Box, *_Item) {
			item := doc.Instantiate[_Item](`item`)
			return item.root, item
		},
		func(item *_Item, index int) {
			item.text.SetText(fmt.Sprint(index))
		},
	)

	scroll.Activate()

	app.Run()
}
