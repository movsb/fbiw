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

	doc := app.New(embedded, `main.html`)

	scroll := doc.GetBoxByID(`scroll`).(*fbiw.Scroll)

	type _Item struct {
		root fbiw.Box
		text *fbiw.Text `css:"text"`
	}

	scroll.SetItems(7,
		func() (fbiw.Box, any) {
			item := fbiw.Unmarshal[_Item](doc, `<block background-color="tan"><text></text></block>`)
			return item.root, item
		},
		func(item any, index int) {
			item2 := item.(*_Item)
			item2.text.SetText(fmt.Sprint(index))
		},
	)

	scroll.Activate()

	app.Show(doc)

	app.Run()
}
