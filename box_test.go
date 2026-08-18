package fbiw

import "testing"

func TestScrollStateNavigate(t *testing.T) {
	tests := []struct {
		name    string
		state   _ScrollState
		key     KeyName
		want    _ScrollState
		changed bool
	}{
		{
			name:    `down selects the first item`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: -1, colIndex: 0},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 0},
			changed: true,
		},
		{
			name:    `down moves to the next visible row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 1, colIndex: 1},
			changed: true,
		},
		{
			name:    `down scrolls past the last visible row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 1, colIndex: 0},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 1, colIndex: 0, itemOffset: 3},
			changed: true,
		},
		{
			name:    `down adjusts the column for a partial last row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 1, colIndex: 2},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 1, colIndex: 1, itemOffset: 3},
			changed: true,
		},
		{
			name:    `down stops at the last data row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 1, colIndex: 1, itemOffset: 3},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 1, colIndex: 1, itemOffset: 3},
			changed: false,
		},
		{
			name:    `up moves to the previous visible row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 1, colIndex: 1},
			key:     Up,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			changed: true,
		},
		{
			name:    `up scrolls before the first visible row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 1, itemOffset: 3},
			key:     Up,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			changed: true,
		},
		{
			name:    `up stops at the first data row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			key:     Up,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			changed: false,
		},
		{
			name:    `left moves to the previous column`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 2},
			key:     Left,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			changed: true,
		},
		{
			name:    `right moves to the next column`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			key:     Right,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 2},
			changed: true,
		},
		{
			name:    `right stops at the end of a partial last row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 1, colIndex: 1, itemOffset: 3},
			key:     Right,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 1, colIndex: 1, itemOffset: 3},
			changed: false,
		},
		{
			name:    `left pages a single column and keeps the selected row`,
			state:   _ScrollState{count: 10, rows: 3, cols: 1, rowIndex: 1, colIndex: 0, itemOffset: 3},
			key:     Left,
			want:    _ScrollState{rows: 3, cols: 1, rowIndex: 1, colIndex: 0},
			changed: true,
		},
		{
			name:    `left selects the first item when a full page is unavailable`,
			state:   _ScrollState{count: 10, rows: 3, cols: 1, rowIndex: 2, colIndex: 0, itemOffset: 1},
			key:     Left,
			want:    _ScrollState{rows: 3, cols: 1, rowIndex: 0, colIndex: 0},
			changed: true,
		},
		{
			name:    `right pages a single column and keeps the selected row`,
			state:   _ScrollState{count: 10, rows: 3, cols: 1, rowIndex: 1, colIndex: 0},
			key:     Right,
			want:    _ScrollState{rows: 3, cols: 1, rowIndex: 1, colIndex: 0, itemOffset: 3},
			changed: true,
		},
		{
			name:    `right selects the last item when a full page is unavailable`,
			state:   _ScrollState{count: 8, rows: 3, cols: 1, rowIndex: 2, colIndex: 0, itemOffset: 3},
			key:     Right,
			want:    _ScrollState{rows: 3, cols: 1, rowIndex: 2, colIndex: 0, itemOffset: 5},
			changed: true,
		},
		{
			name:    `navigation does nothing when the list is empty`,
			state:   _ScrollState{count: 0, rows: 2, cols: 3, rowIndex: -1, colIndex: 0},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: -1, colIndex: 0},
			changed: false,
		},
		{
			name:    `non-navigation keys are ignored`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			key:     A,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			changed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state
			want := tt.want
			want.count = tt.state.count
			changed := got.navigate(tt.key)
			if got != want {
				t.Errorf(`state = %+v, want %+v`, got, want)
			}
			if changed != tt.changed {
				t.Errorf(`changed = %t, want %t`, changed, tt.changed)
			}
		})
	}
}
