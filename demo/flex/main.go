package main

import (
	"embed"
	"fmt"
	"os"
	"strconv"

	"github.com/movsb/fbiw"
)

//go:embed main.html
var embedded embed.FS

func main() {
	app := fbiw.NewApp(fbiw.WithSystemFont(os.DirFS(`..`), `regular.ttf`))
	defer app.Close()

	doc := app.NewDesktop(embedded, `main.html`)
	middle := doc.GetBoxByID[*fbiw.Block](`grow-middle`)
	weight := doc.GetBoxByID[*fbiw.Text](`weight`)
	alignment := doc.GetBoxByID[*fbiw.Flex](`alignment`)
	alignmentLabel := doc.GetBoxByID[*fbiw.Text](`alignment-label`)
	last := doc.GetBoxByID[*fbiw.Block](`alignment-last`)
	visibilityLabel := doc.GetBoxByID[*fbiw.Text](`visibility-label`)

	// 属性更新会触发样式计算和重新布局，无需手动修改 layoutBox 或请求绘制。
	set := func(box fbiw.Box, name, value string) {
		if err := box.SetProp(name, value); err != nil {
			panic(err)
		}
	}
	grow := 2
	justifications := []string{`start`, `center`, `end`, `space-between`, `space-around`, `space-evenly`}
	alignments := []string{`start`, `center`, `end`, `stretch`}
	justifyIndex, alignIndex := 3, 1
	visible := true
	updateLabels := func() {
		weight.SetText(fmt.Sprintf(`grow = %d`, grow))
		alignmentLabel.SetText(justifications[justifyIndex] + ` / ` + alignments[alignIndex])
		if visible {
			visibilityLabel.SetText(`B：隐藏橙色元素`)
		} else {
			visibilityLabel.SetText(`B：显示橙色元素`)
		}
	}
	updateLabels()

	doc.Listen(fbiw.StickDownEvent, func(event *fbiw.Event) {
		if event.Stick.Repeat {
			return
		}
		switch event.Stick.Name {
		case fbiw.Left:
			grow = max(1, grow-1)
			set(middle, `flex-grow`, strconv.Itoa(grow))
		case fbiw.Right:
			grow = min(5, grow+1)
			set(middle, `flex-grow`, strconv.Itoa(grow))
		case fbiw.Up:
			justifyIndex = (justifyIndex - 1 + len(justifications)) % len(justifications)
			set(alignment, `justify-content`, justifications[justifyIndex])
		case fbiw.Down:
			justifyIndex = (justifyIndex + 1) % len(justifications)
			set(alignment, `justify-content`, justifications[justifyIndex])
		case fbiw.A:
			alignIndex = (alignIndex + 1) % len(alignments)
			set(alignment, `align-items`, alignments[alignIndex])
		case fbiw.B:
			visible = !visible
			set(last, `display`, strconv.FormatBool(visible))
		default:
			return
		}
		updateLabels()
	})

	app.Run()
}
