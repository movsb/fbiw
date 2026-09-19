package main

import (
	"embed"
	"log"
	"os"

	"github.com/movsb/fbiw"
	"github.com/movsb/fbiw/input/sticks"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()

	doc := app.NewDesktop(embedded, `main.html`)
	scroll := doc.GetBoxByID[*fbiw.Scroll](`scroll`)
	scroll.Listen(fbiw.ScrollChange, func(event *fbiw.Event) {
		position := event.Data[fbiw.ScrollChangeArgs]()
		log.Printf("滚动位置：x=%d y=%d", position.X, position.Y)
	})
	scroll.Listen(fbiw.ScrollEnd, func(event *fbiw.Event) {
		position := event.Data[fbiw.ScrollEndArgs]()
		log.Printf("滚动结束：x=%d y=%d", position.X, position.Y)
	})
	targets := []fbiw.Box{
		doc.GetBoxByID[fbiw.Box](`row-1`),
		doc.GetBoxByID[fbiw.Box](`row-2`),
		doc.GetBoxByID[fbiw.Box](`row-3`),
		doc.GetBoxByID[fbiw.Box](`row-4`),
		doc.GetBoxByID[fbiw.Box](`row-5`),
		doc.GetBoxByID[fbiw.Box](`row-6`),
		doc.GetBoxByID[fbiw.Box](`row-7`),
	}
	selected := -1
	scroll.Listen(fbiw.InputDownEvent, func(event *fbiw.Event) {
		delta := 0
		switch event.Input.Name {
		case sticks.L1:
			delta = -1
		case sticks.R1:
			delta = 1
		default:
			return
		}
		if selected >= 0 {
			targets[selected].ClassRemove(`selected`)
			selected = (selected + delta + len(targets)) % len(targets)
		} else if delta > 0 {
			selected = 0
		} else {
			selected = len(targets) - 1
		}
		targets[selected].ClassAdd(`selected`)
		moved := scroll.ScrollIntoView(targets[selected])
		log.Printf("定位第 %d 行：滚动=%t", selected+1, moved)
		event.StopPropagation()
	})
	scroll.Activate()
	app.Run()
}
