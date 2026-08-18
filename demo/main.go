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

func main() {
	app := fbiw.NewApp()
	defer app.Close()

	app.AddFont(`system`, false, false, os.DirFS(`.`), `regular.ttf`)

	doc := app.New(embedded, `main.html`)

	app.Show(doc)

	app.Run()
}
