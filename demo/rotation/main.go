package main

import (
	"embed"
	"image"
	"image/color"
	"os"
	"time"

	"github.com/movsb/fbiw"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(".."), "regular.ttf"))
	defer app.Close()
	doc := app.NewDesktop(embedded, "main.html")
	source := image.NewNRGBA(image.Rect(0, 0, 180, 180))
	for y := 20; y < 160; y++ {
		for x := 20; x < 160; x++ {
			if x < 90 {
				source.SetNRGBA(x, y, color.NRGBA{R: 240, G: 80, B: 80, A: 255})
			} else {
				source.SetNRGBA(x, y, color.NRGBA{R: 80, G: 160, B: 240, A: 200})
			}
		}
	}
	spinning := doc.GetBoxByID[*fbiw.Image]("spinning")
	spinning.SetImage(source)
	stop := spinning.Rotate(fbiw.RotationOptions{Duration: 3 * time.Second, Overflow: true})
	defer stop()
	finite := doc.GetBoxByID[*fbiw.Image]("finite")
	finite.SetImage(source)
	finite.Rotate(fbiw.RotationOptions{Duration: time.Second, Iterations: 5, Reverse: true})
	app.Run()
}
