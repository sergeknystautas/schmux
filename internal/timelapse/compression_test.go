package timelapse

import "testing"

func TestCountChangedCells(t *testing.T) {
	makeGrid := func(rows ...string) [][]rune {
		grid := make([][]rune, len(rows))
		for i, row := range rows {
			grid[i] = []rune(row)
		}
		return grid
	}

	tests := []struct {
		name string
		prev [][]rune
		curr [][]rune
		want int
	}{
		{
			name: "identical grids",
			prev: makeGrid("hello", "world"),
			curr: makeGrid("hello", "world"),
			want: 0,
		},
		{
			name: "spinner change (1 cell)",
			prev: makeGrid("⠋ Thinking...", "             "),
			curr: makeGrid("⠙ Thinking...", "             "),
			want: 1,
		},
		{
			name: "timer update (few cells)",
			prev: makeGrid("Status: 3s elapsed"),
			curr: makeGrid("Status: 4s elapsed"),
			want: 1,
		},
		{
			name: "full row rewrite",
			prev: makeGrid("aaaaaaaaaa", "bbbbbbbbbb"),
			curr: makeGrid("cccccccccc", "bbbbbbbbbb"),
			want: 10,
		},
		{
			name: "many rows changed",
			prev: makeGrid("aaa", "bbb", "ccc", "ddd"),
			curr: makeGrid("xxx", "yyy", "zzz", "www"),
			want: 12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width := len(tt.prev[0])
			height := len(tt.prev)
			got := countChangedCells(tt.prev, tt.curr, width, height)
			if got != tt.want {
				t.Errorf("countChangedCells() = %d, want %d", got, tt.want)
			}
		})
	}
}
