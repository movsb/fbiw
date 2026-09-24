package list

import (
	"testing"
	"testing/fstest"

	"github.com/movsb/fbiw"
	"github.com/movsb/fbiw/internal/canvas/cpu"
	"golang.org/x/image/font/gofont/goregular"
)

func testDocument(t *testing.T, body string, width int) *fbiw.Document {
	t.Helper()
	app := fbiw.NewApp(fbiw.WithRenderer(cpu.New(width, 240)), fbiw.WithSystemFontData(goregular.TTF))
	t.Cleanup(app.Close)
	doc := app.NewDesktop(fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(`<document>` + body + `</document>`)},
	}, `main.html`)
	doc.Root().Calc(width, 240, fbiw.Constraints{ParentContentWidth: width, ParentContentHeight: 240, PrefersMaxWidth: true})
	return doc
}

func TestOrderedListNumberingAndReflow(t *testing.T) {
	doc := testDocument(t, `<ol start="8"><li id="first"><text>Long content that wraps onto another line</text></li><li value="12"><text>second</text></li><li id="third"><text>third</text></li></ol>`, 120)
	list := doc.Root().(*List)
	first, second, third := list.Children()[0].(*Item), list.Children()[1].(*Item), list.Children()[2].(*Item)
	if first.marker != `8.` || second.marker != `12.` || third.marker != `13.` {
		t.Fatalf(`markers = %q, %q, %q`, first.marker, second.marker, third.marker)
	}
	if first.GetLayoutBox().Height <= second.GetLayoutBox().Height {
		t.Fatalf(`wrapped item height %d did not exceed single-line item height %d`, first.GetLayoutBox().Height, second.GetLayoutBox().Height)
	}
	if second.GetLayoutBox().Y != first.GetLayoutBox().Height {
		t.Fatalf(`second item y = %d, want %d`, second.GetLayoutBox().Y, first.GetLayoutBox().Height)
	}
	if err := second.SetProp(`display`, `false`); err != nil {
		t.Fatal(err)
	}
	list.Calc(120, 240, fbiw.Constraints{ParentContentWidth: 120, ParentContentHeight: 240})
	if third.marker != `9.` || third.GetLayoutBox().Y != first.GetLayoutBox().Height {
		t.Fatalf(`reflow marker = %q, third y = %d`, third.marker, third.GetLayoutBox().Y)
	}
}

func TestNestedUnorderedList(t *testing.T) {
	doc := testDocument(t, `<ul><li><text>outer</text><ul><li><text>inner</text></li></ul></li><li><text>next</text></li></ul>`, 180)
	outer := doc.Root().(*List)
	first := outer.Children()[0].(*Item)
	nested := first.Children()[1].(*List)
	if first.marker != `•` || nested.Children()[0].(*Item).marker != `•` {
		t.Fatal(`nested bullets were not assigned`)
	}
	if nested.GetLayoutBox().X <= 0 || outer.Children()[1].GetLayoutBox().Y != first.GetLayoutBox().Height {
		t.Fatalf(`nested layout: x=%d next y=%d first height=%d`, nested.GetLayoutBox().X, outer.Children()[1].GetLayoutBox().Y, first.GetLayoutBox().Height)
	}
}

func TestListMarkerDrawsInGutter(t *testing.T) {
	doc := testDocument(t, `<ul><li><text>item</text></li></ul>`, 180)
	item := doc.Root().(*List).Children()[0].(*Item)
	renderer := cpu.New(180, 60)
	item.Draw(fbiw.NewCanvas(renderer))
	markerDrawn := false
	for y := 0; y < item.markerHeight; y++ {
		for x := 0; x < item.gutter; x++ {
			if renderer.Pixels[(y*renderer.Width+x)*4+3] != 0 {
				markerDrawn = true
				break
			}
		}
	}
	if !markerDrawn {
		t.Fatal(`bullet marker did not draw in the gutter`)
	}
}

func TestListPropertiesAndStructure(t *testing.T) {
	ordered := NewOrderedList(nil)
	if err := ordered.SetProp(`start`, `bad`); err == nil {
		t.Fatal(`invalid start accepted`)
	}
	item := NewItem(nil)
	if err := item.SetProp(`value`, `bad`); err == nil {
		t.Fatal(`invalid value accepted`)
	}
	if err := ordered.ValidateChildren(); err != nil {
		t.Fatal(err)
	}
}
