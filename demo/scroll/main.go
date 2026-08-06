package main

import (
	"context"
	"embed"
	_ "embed"
	"fmt"
	_ "net/http/pprof"

	"github.com/movsb/fbiw"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(
		context.Background(), embedded,
		fbiw.WithSystemFont(`../regular.ttf`),
	)
	defer app.Close()

	doc := app.New(`main.html`, `skin`)

	scroll := doc.GetBoxByID(`scroll`).(*fbiw.Scroll)

	type _Item struct {
		Root fbiw.Box
		Text *fbiw.Text `css:"text"`
	}

	scroll.SetItems(7,
		func() (fbiw.Box, any) {
			item := fbiw.Unmarshal[_Item](doc, `<block background-color="tan"><text></text></block>`)
			return item.Root, item
		},
		func(item any, index int) {
			item2 := item.(*_Item)
			item2.Text.SetText(fmt.Sprint(index))
		},
	)

	doc.SetDelegator(&Window{
		doc:    doc,
		scroll: scroll,
	})

	app.Show(doc)

	app.Run()
}

type Window struct {
	doc    *fbiw.Document
	scroll *fbiw.Scroll
}

func (w *Window) HandleKeyboardEvent(name fbiw.KeyName, pressed bool) {
	if pressed {
		w.scroll.Navigate(name)
	}
}
