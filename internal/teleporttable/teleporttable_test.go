package teleporttable

import (
	"image/color"
	"testing"
)

func TestColorPriorityAndCityFaction(t *testing.T) {
	tests := []struct {
		cat, fac string
		want     color.RGBA
	}{
		{"World", "A", color.RGBA{0, 120, 255, 255}},
		{"City", "A", color.RGBA{0, 120, 255, 255}},
		{"City", "H", color.RGBA{255, 48, 48, 255}},
		{"World", "H", color.RGBA{255, 48, 48, 255}},
		{"City", "N", color.RGBA{255, 218, 36, 255}},
		{"World", "X", color.RGBA{255, 255, 255, 255}},
		{"City", "", color.RGBA{255, 255, 255, 255}},
		{"Dungeon", "A", color.RGBA{30, 255, 0, 255}},
		{"Raid", "H", color.RGBA{163, 53, 238, 255}},
		{"Battleground", "N", color.RGBA{245, 140, 186, 255}},
	}
	for _, tc := range tests {
		if got := RowColor(tc.cat, tc.fac); got != tc.want {
			t.Errorf("%q/%q = %v, want %v", tc.cat, tc.fac, got, tc.want)
		}
	}
}
func TestScrollbarThumbDraggingAndTrackClick(t *testing.T) {
	for _, total := range []int{0, 10, 34, 35, 44, 1043, 1500} {
		max := MaxScroll(total)
		if ClampScroll(-50, total) != 0 || ClampScroll(9000, total) != max {
			t.Fatalf("bad clamp total=%d", total)
		}
		top, h := ThumbMetrics(max, total)
		if top < TrackTop || top+h > TrackTop+TrackHeight {
			t.Fatalf("thumb outside track total=%d top=%d height=%d", total, top, h)
		}
		if ScrollForThumbTop(-999, total) != 0 || ScrollForThumbTop(99999, total) != max {
			t.Fatalf("bad boundary drag total=%d", total)
		}
		if max > 0 {
			middleTop, _ := ThumbMetrics(max/2, total)
			got := ScrollForThumbTop(middleTop, total)
			if d := got - max/2; d < -2 || d > 2 {
				t.Fatalf("drag not aligned total=%d got=%d", total, got)
			}
			if got := TrackPageClick(TrackTop, max, total); got != ClampScroll(max-PageRows, total) {
				t.Fatalf("page up total=%d: got %d", total, got)
			}
			if got := TrackPageClick(TrackTop+TrackHeight-1, 0, total); got != ClampScroll(PageRows, total) {
				t.Fatalf("page down total=%d: got %d", total, got)
			}
		}
	}
}
