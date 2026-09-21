package table

import (
	"fmt"
	"strconv"

	"github.com/movsb/fbiw"
)

// Table implements a content-sized, auto-layout table with collapsed borders.
// Rows and cells retain their real parent/child layout coordinates; the table
// owns all grid lines so adjacent cell borders are never drawn twice.
type Table struct {
	fbiw.BaseBox

	layout     _Layout
	cells      []*TableCell
	colWidths  []int
	rowHeights []int
}

// _Drop is an internal parser-only container. Table consumes its children
// instead of retaining the container itself in the public box tree.
type _Drop struct{ fbiw.BaseBox }

func newDrop(doc *fbiw.Document, tag string) *_Drop {
	return &_Drop{BaseBox: fbiw.NewBaseBox(doc, tag)}
}

func init() {
	fbiw.DefineStyles(`th { bold: true; align: both; }`)
	fbiw.Define(`table`, false, newTable)
	fbiw.Define(`tr`, false, newTableRow)
	fbiw.Define(`td`, false, newTableCell)
	fbiw.Define(`th`, false, newTableHeaderCell)
	fbiw.Define(`tbody`, false, func(doc *fbiw.Document) *_Drop { return newDrop(doc, `tbody`) })
	fbiw.Define(`thead`, false, func(doc *fbiw.Document) *_Drop { return newDrop(doc, `thead`) })
	fbiw.Define(`tfoot`, false, func(doc *fbiw.Document) *_Drop { return newDrop(doc, `tfoot`) })
}

func newTable(doc *fbiw.Document) *Table {
	return &Table{BaseBox: fbiw.NewBaseBox(doc, `table`)}
}

type TableRow struct{ fbiw.BaseBox }

func newTableRow(doc *fbiw.Document) *TableRow {
	return &TableRow{BaseBox: fbiw.NewBaseBox(doc, `tr`)}
}

type TableCell struct {
	fbiw.BaseBox
	rowSpan int
	colSpan int
}

func newTableCell(doc *fbiw.Document) *TableCell { return newCell(doc, `td`) }

func newTableHeaderCell(doc *fbiw.Document) *TableCell { return newCell(doc, `th`) }

func newCell(doc *fbiw.Document, tag string) *TableCell {
	return &TableCell{
		BaseBox: fbiw.NewBaseBox(doc, tag),
		rowSpan: 1,
		colSpan: 1,
	}
}

func (b *Table) AppendChild(child any) {
	if drop, ok := child.(*_Drop); ok {
		for _, row := range drop.Children() {
			b.BaseBox.AppendChild(row)
		}
		return
	}
	b.BaseBox.AppendChild(child.(fbiw.Box))
}

func (b *Table) ValidateChildren() error {
	for _, child := range b.Children() {
		if child.GetTag() != `tr` {
			return fmt.Errorf(`table 只能包含 tr，实际为 %s`, child.GetTag())
		}
	}
	return nil
}

func (b *TableRow) ValidateChildren() error {
	for _, child := range b.Children() {
		if child.GetTag() != `td` && child.GetTag() != `th` {
			return fmt.Errorf(`tr 只能包含 td 或 th，实际为 %s`, child.GetTag())
		}
	}
	return nil
}

func (b *TableCell) SetProp(key, value string) error {
	switch key {
	case `rowspan`, `colspan`:
		span, err := strconv.Atoi(value)
		if err != nil || span <= 0 {
			return fmt.Errorf(`%s 需要正整数：%s`, key, value)
		}
		if key == `rowspan` {
			b.rowSpan = span
		} else {
			b.colSpan = span
		}
		if b.Document() != nil {
			b.Document().RequestLayout()
		}
		return nil
	default:
		return b.BaseBox.SetProp(key, value)
	}
}

func (b *TableRow) Calc(availWidth, availHeight int, constraints fbiw.Constraints) {
	// Table owns row sizing. Keeping this method deterministic also makes a row
	// harmless if a caller measures it directly.
	b.SetLayoutBox(fbiw.Rect{Width: max(0, availWidth)})
}

func (b *TableRow) Draw(canvas *fbiw.Canvas) {
	// A row is structural. Its own border is ignored in collapsed mode, while a
	// background may still be useful behind transparent cells.
	b.DrawOptions(canvas, fbiw.BaseBoxDrawOptions{NoBorder: true})
}

func (b *TableCell) Calc(availWidth, availHeight int, constraints fbiw.Constraints) {
	width := max(0, availWidth)
	if constraints.FixedWidth.IsNumber() {
		width = max(0, int(constraints.FixedWidth.Number()))
	}
	height := b.layoutContent(width, availHeight, constraints, false)
	if constraints.FixedHeight.IsNumber() {
		height = max(0, int(constraints.FixedHeight.Number()))
	}
	b.SetLayoutBox(fbiw.Rect{Width: width, Height: height})
}

func (b *TableCell) layoutContent(width, availHeight int, constraints fbiw.Constraints, measureOnly bool) int {
	padding := b.GetComputedStyles().Padding
	contentWidth := max(0, width-padding.PaddingLeft()-padding.PaddingRight())
	contentHeight := 0
	visible := make([]fbiw.Box, 0, len(b.Children()))
	for _, child := range b.Children() {
		if !child.IsDisplaying() {
			continue
		}
		visible = append(visible, child)
		child.Calc(contentWidth, max(0, availHeight-contentHeight), fbiw.Constraints{
			ParentContentWidth:  contentWidth,
			ParentContentHeight: max(0, availHeight-padding.PaddingTop()-padding.PaddingBottom()),
			PrefersMaxWidth:     true,
			UnboundedHeight:     constraints.UnboundedHeight,
		})
		contentHeight += child.GetLayoutBox().Height
	}
	height := padding.PaddingTop() + contentHeight + padding.PaddingBottom()
	styles := b.GetComputedStyles()
	if h := fbiw.ResolveLayoutLength(styles.Height, constraints.ParentContentHeight); h.IsNumber() {
		height = max(height, int(h.Number()))
	}
	if !measureOnly {
		y := padding.PaddingTop()
		if styles.Align == `middle` || styles.Align == `both` {
			y += max(0, height-padding.PaddingTop()-padding.PaddingBottom()-contentHeight) / 2
		}
		for _, child := range visible {
			layout := child.GetLayoutBox()
			x := padding.PaddingLeft()
			if styles.Align == `center` || styles.Align == `both` {
				x += max(0, contentWidth-layout.Width) / 2
			}
			layout.X, layout.Y = x, y
			child.Base().SetLayoutBox(layout)
			y += layout.Height
		}
	}
	return height
}

func (b *TableCell) Draw(canvas *fbiw.Canvas) {
	b.DrawOptions(canvas, fbiw.BaseBoxDrawOptions{NoBorder: true})
}

func (b *Table) gridBorderWidth() int {
	styles := b.GetComputedStyles()
	if styles.BorderWidth <= 0 || styles.BorderColor == fbiw.ColorNone {
		return 0
	}
	return styles.BorderWidth
}

func (b *Table) buildGrid() error {
	rows := make([]*TableRow, 0, len(b.Children()))
	for _, child := range b.Children() {
		if !child.IsDisplaying() {
			continue
		}
		row, ok := child.(*TableRow)
		if !ok {
			return fmt.Errorf(`table 只能包含 tr，实际为 %s`, child.GetTag())
		}
		rows = append(rows, row)
	}
	spans := make([][]_Span, len(rows))
	cells := make([]*TableCell, 0)
	for rowIndex, row := range rows {
		for _, child := range row.Children() {
			if !child.IsDisplaying() {
				continue
			}
			cell, ok := child.(*TableCell)
			if !ok {
				return fmt.Errorf(`tr 只能包含 td 或 th，实际为 %s`, child.GetTag())
			}
			spans[rowIndex] = append(spans[rowIndex], _Span{Rows: cell.rowSpan, Cols: cell.colSpan})
			cells = append(cells, cell)
		}
	}
	result, err := build(spans)
	if err != nil {
		return err
	}
	b.layout = result
	b.cells = cells
	return nil
}

type _IntrinsicWidths struct{ Min, Preferred int }

func measureCellWidths(cell *TableCell, referenceWidth, referenceHeight int) _IntrinsicWidths {
	styles := cell.GetComputedStyles()
	padding := styles.Padding.PaddingLeft() + styles.Padding.PaddingRight()
	widths := _IntrinsicWidths{Min: padding, Preferred: padding}
	for _, child := range cell.Children() {
		if !child.IsDisplaying() {
			continue
		}
		measured := fbiw.MeasureIntrinsicWidths(child, referenceWidth, referenceHeight)
		widths.Min = max(widths.Min, measured.Min+padding)
		widths.Preferred = max(widths.Preferred, measured.Preferred+padding)
	}
	if hint := fbiw.ResolveLayoutLength(styles.Width, referenceWidth); hint.IsNumber() {
		widths.Preferred = max(widths.Preferred, int(hint.Number()))
	}
	widths.Preferred = max(widths.Preferred, widths.Min)
	return widths
}

func (b *Table) Calc(availWidth, availHeight int, constraints fbiw.Constraints) {
	if !b.IsDisplaying() {
		return
	}
	if err := b.buildGrid(); err != nil {
		panic(err)
	}
	cols, rows := 0, len(b.layout.Grid)
	if rows > 0 {
		cols = len(b.layout.Grid[0])
	}
	border := b.gridBorderWidth()
	internalWidth := max(0, cols-1) * border
	outerInsets := b.HorizontalInsets()
	availableTracks := max(0, availWidth-outerInsets-internalWidth)

	widths := make([]_Intrinsic, len(b.cells))
	for cellIndex, cell := range b.cells {
		measured := measureCellWidths(cell, availableTracks, availHeight)
		widths[cellIndex] = _Intrinsic{Min: measured.Min, Preferred: measured.Preferred}
	}
	mins, prefs := columnLimits(b.layout, widths, border)

	prefTotal := sum(prefs, 0, len(prefs))
	minTotal := sum(mins, 0, len(mins))
	styles := b.GetComputedStyles()
	width := fbiw.ResolveLayoutLength(styles.Width, constraints.ParentContentWidth)
	height := fbiw.ResolveLayoutLength(styles.Height, constraints.ParentContentHeight)
	if constraints.FixedWidth.IsNumber() {
		width = constraints.FixedWidth
	}
	if constraints.FixedHeight.IsNumber() {
		height = constraints.FixedHeight
	}
	target := min(prefTotal, availableTracks)
	if constraints.UnboundedWidth && !width.IsNumber() {
		target = prefTotal
	}
	if width.IsNumber() {
		target = max(0, int(width.Number())-outerInsets-internalWidth)
	}
	target = max(target, minTotal)
	b.colWidths = fitTracks(mins, prefs, target)

	heights := make([]int, len(b.cells))
	for _, placement := range b.layout.Placements {
		cell := b.cells[placement.Cell]
		cellWidth := sum(b.colWidths, placement.Col, placement.Cols) + max(0, placement.Cols-1)*border
		heights[placement.Cell] = cell.layoutContent(cellWidth, availHeight, fbiw.Constraints{
			ParentContentWidth:  cellWidth,
			ParentContentHeight: max(0, availHeight),
			UnboundedHeight:     constraints.UnboundedHeight,
		}, true)
	}
	b.rowHeights = resolveRows(b.layout, heights, border)

	gridWidth := sum(b.colWidths, 0, cols) + internalWidth
	internalHeight := max(0, rows-1) * border
	if height.IsNumber() && rows > 0 {
		targetRows := max(0, int(height.Number())-b.VerticalInsets()-internalHeight)
		currentRows := sum(b.rowHeights, 0, rows)
		if targetRows > currentRows {
			grow(b.rowHeights, targetRows-currentRows)
		}
	}
	gridHeight := sum(b.rowHeights, 0, rows) + internalHeight
	b.SetLayoutBox(fbiw.Rect{Width: outerInsets + gridWidth, Height: b.VerticalInsets() + gridHeight})

	rowY := b.InsetTop()
	for r, child := range visibleTableRows(b) {
		row := child
		row.SetLayoutBox(fbiw.Rect{X: b.InsetLeft(), Y: rowY, Width: gridWidth, Height: b.rowHeights[r]})
		rowY += b.rowHeights[r] + border
	}
	for _, placement := range b.layout.Placements {
		cell := b.cells[placement.Cell]
		x := sum(b.colWidths, 0, placement.Col) + placement.Col*border
		cellWidth := sum(b.colWidths, placement.Col, placement.Cols) + max(0, placement.Cols-1)*border
		cellHeight := sum(b.rowHeights, placement.Row, placement.Rows) + max(0, placement.Rows-1)*border
		cell.SetLayoutBox(fbiw.Rect{X: x, Y: 0, Width: cellWidth, Height: cellHeight})
		cell.layoutContent(cellWidth, cellHeight, fbiw.Constraints{
			ParentContentWidth: cellWidth, ParentContentHeight: cellHeight,
		}, false)
	}
}

func visibleTableRows(b *Table) []*TableRow {
	rows := make([]*TableRow, 0, len(b.Children()))
	for _, child := range b.Children() {
		if child.IsDisplaying() {
			rows = append(rows, child.(*TableRow))
		}
	}
	return rows
}

func (b *Table) Draw(canvas *fbiw.Canvas) {
	// Draw backgrounds first, then cell contents, and finally only the grid
	// segments which are not crossed by a spanning cell.
	b.DrawOptions(canvas, fbiw.BaseBoxDrawOptions{NoChildren: true})
	rows := visibleTableRows(b)
	for _, row := range rows {
		layout := row.GetLayoutBox()
		row.DrawOptions(canvas.Offset(layout.X, layout.Y), fbiw.BaseBoxDrawOptions{NoBorder: true, NoChildren: true})
	}
	for _, row := range rows {
		for _, child := range row.Children() {
			if !child.IsDisplaying() {
				continue
			}
			layout := child.GetLayoutBox()
			rowLayout := row.GetLayoutBox()
			child.Draw(canvas.Offset(rowLayout.X+layout.X, rowLayout.Y+layout.Y))
		}
	}

	border := b.gridBorderWidth()
	if border > 0 {
		color := b.GetComputedStyles().BorderColor
		xs := make([]int, len(b.colWidths)+1)
		ys := make([]int, len(b.rowHeights)+1)
		xs[0], ys[0] = b.InsetLeft(), b.InsetTop()
		for c := range b.colWidths {
			xs[c+1] = xs[c] + b.colWidths[c]
			if c+1 < len(b.colWidths) {
				xs[c+1] += border
			}
		}
		for r := range b.rowHeights {
			ys[r+1] = ys[r] + b.rowHeights[r]
			if r+1 < len(b.rowHeights) {
				ys[r+1] += border
			}
		}
		vertical := func(r, c int) bool {
			return b.layout.Grid[r][c-1] == _Empty ||
				b.layout.Grid[r][c] == _Empty ||
				b.layout.Grid[r][c-1] != b.layout.Grid[r][c]
		}
		horizontal := func(r, c int) bool {
			return b.layout.Grid[r-1][c] == _Empty ||
				b.layout.Grid[r][c] == _Empty ||
				b.layout.Grid[r-1][c] != b.layout.Grid[r][c]
		}
		for c := 1; c < len(b.colWidths); c++ {
			x := xs[c] - border
			for r := range b.rowHeights {
				if vertical(r, c) {
					canvas.FillRect(x, ys[r], border, b.rowHeights[r], color)
				}
			}
		}
		for r := 1; r < len(b.rowHeights); r++ {
			y := ys[r] - border
			for c := range b.colWidths {
				if horizontal(r, c) {
					canvas.FillRect(xs[c], y, b.colWidths[c], border, color)
				}
			}
		}
		// Complete crossings and T-junctions without restoring a line through
		// the interior of a cell that spans both axes.
		for r := 1; r < len(b.rowHeights); r++ {
			for c := 1; c < len(b.colWidths); c++ {
				incident := vertical(r-1, c) || vertical(r, c) || horizontal(r, c-1) || horizontal(r, c)
				if incident {
					canvas.FillRect(xs[c]-border, ys[r]-border, border, border, color)
				}
			}
		}
	}
}
