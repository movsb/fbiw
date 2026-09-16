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

type _Item struct {
	root fbiw.Box
	text *fbiw.Text `css:"text"`
}

func (i *_Item) ListSelectionChanged(selected bool) {
	i.text.SetMarqueeRunning(selected)
}

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()

	doc := app.NewDesktop(embedded, `main.html`)

	list := doc.GetBoxByID[*fbiw.List](`list`)

	list.SetItems(7,
		func() (fbiw.Box, *_Item) {
			item := doc.Instantiate[_Item](`item`)
			return item.root, item
		},
		func(item *_Item, index int) {
			item.text.SetText(fmt.Sprintf(`项目 %d：这是一段只有选中后才会往返滚动的长长长长长长长长长长长长长标题`, index))
		},
	)

	list.Activate()

	app.Run()
}
