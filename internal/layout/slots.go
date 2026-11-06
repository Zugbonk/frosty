// Package layout defines frozen future control locations in the 1590x860
// Frosty design coordinate space. No Windows HWND is allocated for hidden slots.
package layout

type Rect struct{ X, Y, W, H int }
type Slot struct {
	ID, Region string
	Bounds     Rect
}

// Reserved contains exactly eight initially hidden feature slots.
// To activate one later, bind a function to its ID; do not move existing UI.
var Reserved = []Slot{
	{"future_move_1", "movement", Rect{212, 368, 48, 43}},
	{"future_move_2", "movement", Rect{280, 368, 48, 43}},
	{"future_tp_1", "teleport", Rect{386, 215, 122, 22}},
	{"future_tp_2", "teleport", Rect{532, 215, 122, 22}},
	{"future_tp_3", "teleport", Rect{678, 215, 122, 22}},
	{"future_side_1", "sidebar", Rect{1351, 605, 174, 22}},
	{"future_side_2", "sidebar", Rect{1351, 632, 174, 22}},
	{"future_side_3", "sidebar", Rect{1351, 659, 174, 22}},
}
