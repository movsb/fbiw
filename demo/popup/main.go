package main

import (
	"embed"
	_ "embed"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/movsb/fbiw"
)

// pprof 性能测试用。
//
// go tool pprof -web  http://localhost:8888/debug/pprof/profile?seconds=30
func init() {
	go http.ListenAndServe(`0.0.0.0:8888`, nil)
}

//go:embed *.html
var embedded embed.FS

type MenuItem struct {
	Name  string
	Click func()
}

type _ItemView struct {
	root fbiw.Box
	name *fbiw.Text `css:"text"`
}

func main() {
	app := fbiw.NewApp()
	defer app.Close()

	app.AddFontFile(`system`, false, false, os.DirFS(`..`), `regular.ttf`)

	doc := app.NewDesktop(embedded, `main.html`)

	items := []MenuItem{
		{Name: `24`},
		{Name: `24`},
	}

	list := doc.QuerySelector[*fbiw.List](`list`)
	list.SetItems(len(items),
		func() (fbiw.Box, *_ItemView) {
			item := doc.Instantiate[_ItemView](`item`)
			return item.root, item
		},
		func(item *_ItemView, index int) {
			item.name.SetText(items[index].Name)
		},
	)

	list.Activate()

	app.Run()
}
