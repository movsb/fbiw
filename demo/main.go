package main

import (
	"context"
	"embed"
	_ "embed"
	"net/http"
	_ "net/http/pprof"

	"github.com/movsb/fbiw"
)

// pprof 性能测试用。
//
// go tool pprof -web  http://localhost:8888/debug/pprof/profile?seconds=30
func init() {
	go http.ListenAndServe(`0.0.0.0:8888`, nil)
}

//go:embed *.html skin
var embedded embed.FS

func main() {
	app := fbiw.NewApp(context.Background(), embedded)
	defer app.Close()

	doc := app.New(`main.html`, `skin`)

	app.Show(doc)

	app.Run()
}
