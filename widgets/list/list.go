// Package list provides content-sized HTML ordered and unordered lists.
package list

import (
	"fmt"
	"strconv"

	"github.com/movsb/fbiw"
)

// List is an <ol> or <ul>. It is separate from fbiw.List, which is virtualized.
type List struct {
	fbiw.BaseBox
	ordered bool
	start   int
}

// Item is an <li> with a marker and vertically stacked content.
type Item struct {
	fbiw.BaseBox
	value        int
	hasValue     bool
	marker       string
	markerWidth  int
	markerHeight int
	gutter       int
	markerGap    int
}

func init() {
	fbiw.Define(`ol`, false, newOrderedList)
	fbiw.Define(`ul`, false, newUnorderedList)
	fbiw.Define(`li`, false, newItem)
}

func newOrderedList(doc *fbiw.Document) *List {
	return &List{BaseBox: fbiw.NewBaseBox(doc, `ol`), ordered: true, start: 1}
}

func newUnorderedList(doc *fbiw.Document) *List {
	return &List{BaseBox: fbiw.NewBaseBox(doc, `ul`), start: 1}
}

func newItem(doc *fbiw.Document) *Item {
	return &Item{BaseBox: fbiw.NewBaseBox(doc, `li`)}
}

func (b *List) SetProp(key, value string) error {
	if key == `start` && b.ordered {
		start, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf(`ol start 需要整数：%s`, value)
		}
		b.start = start
		if b.Document() != nil {
			b.Document().RequestLayout()
		}
		return nil
	}
	return b.BaseBox.SetProp(key, value)
}

func (b *Item) SetProp(key, value string) error {
	if key == `value` {
		number, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf(`li value 需要整数：%s`, value)
		}
		b.value, b.hasValue = number, true
		if b.Document() != nil {
			b.Document().RequestLayout()
		}
		return nil
	}
	return b.BaseBox.SetProp(key, value)
}

func (b *List) ValidateChildren() error {
	for _, child := range b.Children() {
		if _, ok := child.(*Item); !ok {
			return fmt.Errorf(`%s 只能包含 li，实际为 %s`, b.GetTag(), child.GetTag())
		}
	}
	return nil
}

func (b *List) Calc(availWidth, availHeight int, constraints fbiw.Constraints) {
	if !b.IsDisplaying() {
		return
	}
	rows := make([]*Item, 0, len(b.Children()))
	value := b.start
	markerWidth := 0
	for _, child := range b.Children() {
		if !child.IsDisplaying() {
			continue
		}
		item := child.(*Item)
		if b.ordered {
			if item.hasValue {
				value = item.value
			}
			item.marker = strconv.Itoa(value) + `.`
			value++
		} else {
			item.marker = `•`
		}
		faces := b.Document().LoadFaces(item)
		_, item.markerWidth, _ = fbiw.SegmentText(item.marker, 1<<24, faces)
		item.markerHeight = faces[0].TextHeight()
		markerWidth = max(markerWidth, item.markerWidth)
		rows = append(rows, item)
	}
	gap := max(4, int(b.GetComputedStyles().FontSize.Number())/4)
	for _, item := range rows {
		item.gutter = markerWidth + gap
		item.markerGap = gap
	}
	contentWidth, contentHeight := layoutColumn(&b.BaseBox, b.Children(), availWidth, availHeight, constraints, 0)
	b.SetLayoutBox(fbiw.Rect{Width: contentWidth, Height: contentHeight})
}

func (b *Item) Calc(availWidth, availHeight int, constraints fbiw.Constraints) {
	if !b.IsDisplaying() {
		return
	}
	width, height := layoutColumn(&b.BaseBox, b.Children(), availWidth, availHeight, constraints, b.gutter)
	height = max(height, b.VerticalInsets()+b.markerHeight)
	b.SetLayoutBox(fbiw.Rect{Width: width, Height: height})
}

func layoutColumn(box *fbiw.BaseBox, children []fbiw.Box, availWidth, availHeight int, constraints fbiw.Constraints, extraLeft int) (int, int) {
	styles := box.GetComputedStyles()
	width := fbiw.ResolveLayoutLength(styles.Width, constraints.ParentContentWidth)
	height := fbiw.ResolveLayoutLength(styles.Height, constraints.ParentContentHeight)
	if constraints.FixedWidth.IsNumber() {
		width = constraints.FixedWidth
	}
	if constraints.FixedHeight.IsNumber() {
		height = constraints.FixedHeight
	}
	outerWidth := max(0, availWidth)
	if width.IsNumber() {
		outerWidth = max(0, int(width.Number()))
	}
	contentWidth := max(0, outerWidth-box.HorizontalInsets()-extraLeft)
	contentHeight := max(0, availHeight-box.VerticalInsets())
	y := box.InsetTop()
	maxChildWidth := 0
	for _, child := range children {
		if !child.IsDisplaying() {
			continue
		}
		child.Calc(contentWidth, max(0, contentHeight-(y-box.InsetTop())), fbiw.Constraints{
			ParentContentWidth:  contentWidth,
			ParentContentHeight: contentHeight,
			PrefersMaxWidth:     true,
			UnboundedWidth:      constraints.UnboundedWidth,
			UnboundedHeight:     constraints.UnboundedHeight,
		})
		layout := child.GetLayoutBox()
		layout.X, layout.Y = box.InsetLeft()+extraLeft, y
		child.Base().SetLayoutBox(layout)
		y += layout.Height
		maxChildWidth = max(maxChildWidth, layout.Width)
	}
	actualWidth := box.HorizontalInsets() + extraLeft + maxChildWidth
	if !constraints.UnboundedWidth {
		actualWidth = min(max(0, availWidth), actualWidth)
	}
	if width.IsNumber() {
		outerWidth = max(0, int(width.Number()))
	} else if constraints.PrefersMaxWidth && !constraints.UnboundedWidth {
		outerWidth = max(0, availWidth)
	} else {
		outerWidth = actualWidth
	}
	actualHeight := y + box.InsetBottom()
	if height.IsNumber() {
		actualHeight = max(0, int(height.Number()))
	} else if constraints.PrefersMaxHeight && !constraints.UnboundedHeight {
		actualHeight = max(0, availHeight)
	}
	return outerWidth, actualHeight
}

func (b *Item) Draw(canvas *fbiw.Canvas) {
	b.BaseBox.Draw(canvas)
	if b.marker == `` || b.markerWidth == 0 {
		return
	}
	x := b.InsetLeft() + b.gutter - b.markerGap - b.markerWidth
	canvas.Offset(x, b.InsetTop()).DrawString(b.marker, b.Document().LoadFaces(b), b.GetComputedStyles().Color)
}
