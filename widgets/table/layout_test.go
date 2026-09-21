package table

import (
	"slices"
	"testing"
)

func TestFitTracks(t *testing.T) {
	for _, test := range []struct {
		name      string
		minimum   []int
		preferred []int
		target    int
		want      []int
	}{
		{`natural`, []int{10, 10}, []int{20, 30}, 50, []int{20, 30}},
		{`grow`, []int{10, 10}, []int{20, 30}, 60, []int{24, 36}},
		{`minimum`, []int{10, 12}, []int{20, 30}, 5, []int{10, 12}},
		{`shrink`, []int{10, 10}, []int{20, 30}, 30, []int{14, 16}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := fitTracks(test.minimum, test.preferred, test.target)
			if !slices.Equal(got, test.want) {
				t.Fatalf(`fitTracks() = %v, want %v`, got, test.want)
			}
		})
	}
}

func TestBuildSpans(t *testing.T) {
	layout, err := build([][]_Span{
		{{Rows: 2, Cols: 1}, {Rows: 1, Cols: 2}},
		{{Rows: 1, Cols: 1}, {Rows: 1, Cols: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]int{{0, 1, 1}, {0, 2, 3}}
	if len(layout.Grid) != len(want) {
		t.Fatalf(`grid rows = %d, want %d`, len(layout.Grid), len(want))
	}
	for row := range want {
		if !slices.Equal(layout.Grid[row], want[row]) {
			t.Fatalf(`grid row %d = %v, want %v`, row, layout.Grid[row], want[row])
		}
	}
}

func TestResolveRows(t *testing.T) {
	layout, err := build([][]_Span{{{Rows: 2, Cols: 1}}, {}})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveRows(layout, []int{11}, 1); !slices.Equal(got, []int{5, 5}) {
		t.Fatalf(`resolveRows() = %v, want [5 5]`, got)
	}
}
