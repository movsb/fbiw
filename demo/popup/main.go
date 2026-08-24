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

	app.AddFont(`system`, false, false, os.DirFS(`.`), `regular.ttf`)

	doc := app.NewDesktop(embedded, `main.html`)

	items := []MenuItem{
		{Name: `24`},
		{Name: `24`},
	}

	scroll := doc.QuerySelector[*fbiw.Scroll](`scroll`)
	scroll.SetItems(len(items),
		func() (fbiw.Box, *_ItemView) {
			item := fbiw.Unmarshal[_ItemView](doc, `
	<block padding="0 10" align=middle>
		<text></text>
	</block>
	`)
			return item.root, item
		},
		func(item *_ItemView, index int) {
			item.name.SetText(items[index].Name)
		},
	)

	scroll.Activate()

	app.Run()
}
