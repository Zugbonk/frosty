package teleporttable

import (
	"image/color"
	"strings"
)

// RowColor is the shared display rule for all cells of a teleport record.
// Dungeon/Raid/Battleground are instance types and override any faction value.
// World and City have NO independent type color; they use the faction.
func RowColor(category, faction string) color.RGBA {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "dungeon":
		return color.RGBA{0x1e, 0xff, 0x00, 0xff} // WoW uncommon item
	case "raid":
		return color.RGBA{0xa3, 0x35, 0xee, 0xff} // WoW epic item
	case "battleground":
		return color.RGBA{0xf5, 0x8c, 0xba, 0xff} // WoW paladin class
	}
	switch strings.ToUpper(strings.TrimSpace(faction)) {
	case "A", "ALLIANCE":
		return color.RGBA{0x00, 0x78, 0xff, 0xff}
	case "H", "HORDE":
		return color.RGBA{0xff, 0x30, 0x30, 0xff}
	case "N", "NEUTRAL":
		return color.RGBA{0xff, 0xda, 0x24, 0xff} // original Zugbonk yellow
	default: // Other, Empty, unknown: white, never the former generic yellow/cyan
		return color.RGBA{0xff, 0xff, 0xff, 0xff}
	}
}

const PageRows = 34
const TrackTop = 258
const TrackHeight = 541
const TrackLeft = 1276
const TrackWidth = 11

func MaxScroll(total int) int {
	if total <= PageRows {
		return 0
	}
	return total - PageRows
}

func ClampScroll(scroll, total int) int {
	if scroll < 0 {
		return 0
	}
	if scroll > MaxScroll(total) {
		return MaxScroll(total)
	}
	return scroll
}

// ThumbMetrics uses the same logical viewport pixels as the drawn scrollbar.
// An item count smaller than one page fills the track and cannot be dragged.
func ThumbMetrics(scroll, total int) (top, height int) {
	if MaxScroll(total) == 0 {
		return TrackTop, TrackHeight
	}
	height = TrackHeight * PageRows / total
	if height < 22 {
		height = 22
	}
	if height > TrackHeight {
		height = TrackHeight
	}
	travel := TrackHeight - height
	top = TrackTop + travel*ClampScroll(scroll, total)/MaxScroll(total)
	return top, height
}

// ScrollForThumbTop maps thumb motion (including the current grab offset) back
// to a row index, clamping moves outside the viewport to either end.
func ScrollForThumbTop(top, total int) int {
	max := MaxScroll(total)
	if max == 0 {
		return 0
	}
	_, h := ThumbMetrics(0, total)
	travel := TrackHeight - h
	if travel == 0 {
		return 0
	}
	position := top - TrackTop
	if position <= 0 {
		return 0
	}
	if position >= travel {
		return max
	}
	return (position*max + travel/2) / travel
}

func TrackPageClick(y, scroll, total int) int {
	top, h := ThumbMetrics(scroll, total)
	if y < top {
		return ClampScroll(scroll-PageRows, total)
	}
	if y >= top+h {
		return ClampScroll(scroll+PageRows, total)
	}
	return ClampScroll(scroll, total)
}
