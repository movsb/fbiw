// Package table contains the box-independent parts of table layout.
package table

import (
	"fmt"
	"slices"
)

const _Empty = -1

type _Span struct {
	Rows int
	Cols int
}

type _Placement struct {
	Cell int
	Row  int
	Col  int
	Rows int
	Cols int
}

type _Layout struct {
	Grid       [][]int
	Placements []_Placement
}

func build(rows [][]_Span) (_Layout, error) {
	grid := make([][]int, len(rows))
	placements := make([]_Placement, 0)
	maxCols := 0
	cell := 0
	ensureCols := func(cols int) {
		for row := range grid {
			for len(grid[row]) < cols {
				grid[row] = append(grid[row], _Empty)
			}
		}
	}

	for row, cells := range rows {
		col := 0
		for _, span := range cells {
			for col < len(grid[row]) && grid[row][col] != _Empty {
				col++
			}
			rowsSpanned := min(span.Rows, len(rows)-row)
			endCol := col + span.Cols
			ensureCols(endCol)
			for r := row; r < row+rowsSpanned; r++ {
				for c := col; c < endCol; c++ {
					if grid[r][c] != _Empty {
						return _Layout{}, fmt.Errorf(`表格单元格在第 %d 行第 %d 列发生重叠`, r+1, c+1)
					}
					grid[r][c] = cell
				}
			}
			placements = append(placements, _Placement{
				Cell: cell, Row: row, Col: col, Rows: rowsSpanned, Cols: span.Cols,
			})
			cell++
			col = endCol
			maxCols = max(maxCols, endCol)
		}
	}
	ensureCols(maxCols)
	return _Layout{Grid: grid, Placements: placements}, nil
}

type _Intrinsic struct {
	Min       int
	Preferred int
}

func columnLimits(layout _Layout, widths []_Intrinsic, border int) ([]int, []int) {
	cols := 0
	if len(layout.Grid) > 0 {
		cols = len(layout.Grid[0])
	}
	mins := make([]int, cols)
	prefs := make([]int, cols)
	for _, placement := range layout.Placements {
		width := widths[placement.Cell]
		if placement.Cols == 1 {
			mins[placement.Col] = max(mins[placement.Col], width.Min)
			prefs[placement.Col] = max(prefs[placement.Col], width.Preferred)
		}
	}
	for span := 2; span <= cols; span++ {
		for _, placement := range layout.Placements {
			if placement.Cols != span {
				continue
			}
			indices := make([]int, span)
			minTotal, prefTotal := 0, 0
			for i := range span {
				indices[i] = placement.Col + i
				minTotal += mins[placement.Col+i]
				prefTotal += prefs[placement.Col+i]
			}
			width := widths[placement.Cell]
			spanBorders := max(0, placement.Cols-1) * border
			distributeDeficit(mins, indices, max(0, width.Min-spanBorders)-minTotal)
			distributeDeficit(prefs, indices, max(0, width.Preferred-spanBorders)-prefTotal)
			for _, index := range indices {
				prefs[index] = max(prefs[index], mins[index])
			}
		}
	}
	return mins, prefs
}

func resolveRows(layout _Layout, heights []int, border int) []int {
	rows := make([]int, len(layout.Grid))
	for _, placement := range layout.Placements {
		if placement.Rows == 1 {
			rows[placement.Row] = max(rows[placement.Row], heights[placement.Cell])
		}
	}
	for span := 2; span <= len(rows); span++ {
		for _, placement := range layout.Placements {
			if placement.Rows != span {
				continue
			}
			current := sum(rows, placement.Row, placement.Rows) + max(0, placement.Rows-1)*border
			if deficit := heights[placement.Cell] - current; deficit > 0 {
				grow(rows[placement.Row:placement.Row+placement.Rows], deficit)
			}
		}
	}
	return rows
}

func grow(values []int, amount int) {
	if amount <= 0 || len(values) == 0 {
		return
	}
	base, remainder := amount/len(values), amount%len(values)
	for i := range values {
		values[i] += base
		if i < remainder {
			values[i]++
		}
	}
}

func sum(values []int, start, count int) int {
	total := 0
	for _, value := range values[start : start+count] {
		total += value
	}
	return total
}

func distributeDeficit(values []int, indices []int, deficit int) {
	if deficit <= 0 || len(indices) == 0 {
		return
	}
	weight := 0
	for _, index := range indices {
		weight += max(1, values[index])
	}
	remaining := deficit
	for position, index := range indices {
		add := remaining
		if position != len(indices)-1 {
			add = deficit * max(1, values[index]) / weight
			remaining -= add
		}
		values[index] += add
	}
}

func fitTracks(mins, preferred []int, target int) []int {
	out := slices.Clone(preferred)
	prefTotal := sum(preferred, 0, len(preferred))
	minTotal := sum(mins, 0, len(mins))
	if target >= prefTotal {
		indices := make([]int, len(out))
		for i := range indices {
			indices[i] = i
		}
		distributeDeficit(out, indices, target-prefTotal)
		return out
	}
	if target <= minTotal {
		return slices.Clone(mins)
	}

	shrinkNeeded := prefTotal - target
	capacity := prefTotal - minTotal
	remaining := shrinkNeeded
	for i := range out {
		shrink := remaining
		if i != len(out)-1 && capacity > 0 {
			shrink = shrinkNeeded * (preferred[i] - mins[i]) / capacity
			remaining -= shrink
		}
		out[i] = max(mins[i], out[i]-shrink)
	}
	return out
}
