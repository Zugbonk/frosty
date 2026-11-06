// Package infoscroll contains viewport-independent scrollbar calculations.
package infoscroll

func clamp(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// Thumb calculates the offset and size of a scrollbar thumb. A non-positive
// maximum scroll means that the complete content fits and no thumb is shown.
func Thumb(trackHeight, maxScroll, position int) (offset, size int) {
	if trackHeight <= 0 || maxScroll <= 0 {
		return 0, 0
	}
	size = trackHeight * trackHeight / (trackHeight + maxScroll)
	size = clamp(size, 28, trackHeight)
	travel := trackHeight - size
	offset = (clamp(position, 0, maxScroll)*travel + maxScroll/2) / maxScroll
	return
}

func Drag(trackHeight, maxScroll, pointerPosition, grabOffset int) int {
	if maxScroll <= 0 {
		return 0
	}
	_, size := Thumb(trackHeight, maxScroll, 0)
	travel := trackHeight - size
	if travel <= 0 {
		return 0
	}
	off := clamp(pointerPosition-grabOffset, 0, travel)
	return (off*maxScroll + travel/2) / travel
}
