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
	transitions := make([]*fbiw.Transition[float64], len(pictures))
	for index, picture := range pictures {
		transitions[index] = doc.NewTransition(1.0,
			fbiw.TransitionOptions[float64]{
				Duration: 200 * time.Millisecond,
				Easing:   fbiw.EaseOut,
				Animator: fbiw.NumberAnimator,
				OnUpdate: picture.SetScale,
			},
		)
	}
	defer func() {
		for _, transition := range transitions {
			transition.Cancel()
		}
	}()
	selected := 0
	spinning.Activate()
	transitions[selected].SetTarget(activeScales[selected])
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
		transitions[selected].SetTarget(1)
		selected = next
		pictures[selected].Activate()
		transitions[selected].SetTarget(activeScales[selected])
	})
	finite.Rotate(fbiw.RotationOptions{Duration: time.Second, Iterations: 5, Reverse: true})
	app.Run()
}
