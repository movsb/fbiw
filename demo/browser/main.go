//go:build js && wasm

package main

import (
	"embed"
	"time"

	"github.com/movsb/fbiw"
	"github.com/movsb/fbiw/input/sticks"
	"golang.org/x/image/font/gofont/goregular"
)

//go:embed main.html image.png
var assets embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFontData(goregular.TTF))
	defer app.Close()
	doc := app.NewDesktop(assets, "main.html")
	spinning := doc.GetBoxByID[*fbiw.Image]("spinning")
	stop := spinning.Rotate(fbiw.RotationOptions{Duration: 3 * time.Second, Overflow: true})
	defer func() { stop() }()
	status := doc.GetBoxByID[*fbiw.Text]("status")
	scale := 1.0
	doc.Listen(fbiw.InputDownEvent, func(e *fbiw.Event) {
		switch e.Input.Name {
		case sticks.Up:
			scale = min(1.75, scale+0.1)
			spinning.SetScale(scale)
			status.SetTextFormat("Zoom: %.0f%%", scale*100)
		case sticks.Down:
			scale = max(0.5, scale-0.1)
			spinning.SetScale(scale)
			status.SetTextFormat("Zoom: %.0f%%", scale*100)
		case sticks.Left, sticks.Right:
			reverse := e.Input.Name == sticks.Left
			stop = spinning.Rotate(fbiw.RotationOptions{Duration: 3 * time.Second, Overflow: true, Reverse: reverse})
			if reverse {
				status.SetText("Spin: counterclockwise")
			} else {
				status.SetText("Spin: clockwise")
			}
		case sticks.A:
			scale = 1.25
			spinning.SetScale(scale)
			status.SetText("Zoom: 125%")
		case sticks.B:
			scale = 1
			spinning.SetScale(scale)
			status.SetText("Zoom: 100%")
		}
	})
	app.Run()
}
