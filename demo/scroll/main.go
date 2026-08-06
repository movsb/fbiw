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

	scroll.SetItems(7,
		func() fbiw.Box {
			btn := fbiw.NewBlock(doc)
			btn.Set(`background-color`, `tan`)
			txt := fbiw.NewText(doc)
			btn.AppendChild(txt)
			return btn
		},
		func(box fbiw.Box, index int) {
			txt := box.(*fbiw.Block).Children[0].(*fbiw.Text)
			txt.SetText(fmt.Sprint(index))
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
