package table

import (
	"testing"
	"testing/fstest"

	"github.com/movsb/fbiw"
	"github.com/movsb/fbiw/internal/canvas/cpu"
	"golang.org/x/image/font/gofont/goregular"
)

func TestConstructors(t *testing.T) {
	for _, test := range []struct {
		box fbiw.Box
		tag string
	}{
		{NewTable(nil), `table`},
		{NewTableRow(nil), `tr`},
		{NewTableCell(nil), `td`},
		{NewTableHeaderCell(nil), `th`},
	} {
		if got := test.box.GetTag(); got != test.tag {
			t.Fatalf(`tag = %q, want %q`, got, test.tag)
		}
	}
}

func TestCellSpans(t *testing.T) {
	cell := NewTableCell(nil)
	if err := cell.SetProp(`rowspan`, `2`); err != nil {
		t.Fatal(err)
	}
	if err := cell.SetProp(`colspan`, `3`); err != nil {
		t.Fatal(err)
	}
	if cell.rowSpan != 2 || cell.colSpan != 3 {
		t.Fatalf(`span = %dx%d, want 2x3`, cell.rowSpan, cell.colSpan)
	}
	for _, value := range []string{`0`, `-1`, `bad`} {
		if err := cell.SetProp(`rowspan`, value); err == nil {
			t.Errorf(`rowspan=%q did not fail`, value)
		}
	}
}

func TestRegisteredTableLayout(t *testing.T) {
	app := fbiw.NewApp(
		fbiw.WithRenderer(cpu.New(200, 100)),
		fbiw.WithSystemFontData(goregular.TTF),
	)
	defer app.Close()
	doc := app.NewDesktop(fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(`<document><table border-width="1" border-color="#ff0000"><tbody><tr><td padding="2"><text>A</text></td><td padding="2"><text>BBBB</text></td></tr></tbody></table></document>`)},
	}, `main.html`)
	widget, ok := doc.Root().(*Table)
	if !ok {
		t.Fatalf(`root = %T, want *Table`, doc.Root())
	}
	if len(widget.Children()) != 1 {
		t.Fatalf(`transparent tbody left %d table children, want 1 row`, len(widget.Children()))
	}
	widget.Calc(200, 100, fbiw.Constraints{ParentContentWidth: 200, ParentContentHeight: 100})
	if got := widget.GetLayoutBox(); got.Width <= 0 || got.Height <= 0 {
		t.Fatalf(`table layout = %+v`, got)
	}
}

func TestAsymmetricTableBorderDoesNotCreateGrid(t *testing.T) {
	app := fbiw.NewApp(
		fbiw.WithRenderer(cpu.New(200, 100)),
		fbiw.WithSystemFontData(goregular.TTF),
	)
	defer app.Close()
	doc := app.NewDesktop(fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(`<document><table border-width="1 2 3 4" border-color="red"><tr><td><text>A</text></td><td><text>B</text></td></tr></table></document>`)},
	}, `main.html`)
	widget := doc.Root().(*Table)
	if widget.gridBorderWidth() != 0 {
		t.Fatal(`asymmetric outer border should not produce a uniform inner grid`)
	}
	if widget.InsetTop() != 1 || widget.InsetRight() != 2 || widget.InsetBottom() != 3 || widget.InsetLeft() != 4 {
		t.Fatalf(`table insets = %d %d %d %d`, widget.InsetTop(), widget.InsetRight(), widget.InsetBottom(), widget.InsetLeft())
	}
}
