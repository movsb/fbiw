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
	pictures := []*fbiw.Image{spinning, finite}
	activeScales := []float64{1.5, .5}
	scales := []float64{1, 1}
	cancels := make([]func(), len(pictures))
	defer func() {
		for _, cancel := range cancels {
			if cancel != nil {
				cancel()
			}
		}
	}()
	scaleTo := func(index int, target float64) {
		if cancels[index] != nil {
			cancels[index]()
			cancels[index] = nil
		}
		interpolate := fbiw.NumberAnimator(scales[index], target)
		cancels[index] = doc.Animate(fbiw.AnimationOptions{
			Duration: 200 * time.Millisecond, Easing: fbiw.EaseOut,
			OnUpdate: func(progress float64) {
				scales[index] = interpolate(progress)
				pictures[index].SetScale(scales[index])
			},
			OnComplete: func() { cancels[index] = nil },
		})
	}
	selected := 0
	spinning.Activate()
	scaleTo(selected, activeScales[selected])
	doc.Listen(fbiw.StickDownEvent, func(event *fbiw.Event) {
		next := selected
		switch event.Stick.Name {
		case fbiw.Up:
			next = 0
		case fbiw.Down:
			next = 1
		default:
			return
		}
		if next == selected {
			return
		}
		scaleTo(selected, 1)
		selected = next
		pictures[selected].Activate()
		scaleTo(selected, activeScales[selected])
	})
	finite.Rotate(fbiw.RotationOptions{Duration: time.Second, Iterations: 5, Reverse: true})
	app.Run()
}
