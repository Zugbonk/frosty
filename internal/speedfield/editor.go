package speedfield

// Edit preserves an insertion position within a speed multiplier string.
// Characters are edited as runes; validation remains the movement engine's job.
func Edit(old string, caret int, selected bool, key rune) (string, int) {
	r := []rune(old)
	if caret < 0 {
		caret = 0
	}
	if caret > len(r) {
		caret = len(r)
	}
	if selected {
		r = nil
		caret = 0
	}
	switch {
	case key == 8:
		if caret > 0 {
			r = append(r[:caret-1], r[caret:]...)
			caret--
		}
	case key >= 32 && len(r) < 32:
		r = append(r[:caret], append([]rune{key}, r[caret:]...)...)
		caret++
	}
	return string(r), caret
}

// Navigate handles Left, Right, Home, End and Delete as native Windows keys.
func Navigate(old string, caret int, key uintptr) (string, int, bool) {
	r := []rune(old)
	if caret < 0 {
		caret = 0
	}
	if caret > len(r) {
		caret = len(r)
	}
	switch key {
	case 0x25:
		if caret > 0 {
			caret--
		}
	case 0x27:
		if caret < len(r) {
			caret++
		}
	case 0x24:
		caret = 0
	case 0x23:
		caret = len(r)
	case 0x2E:
		if caret < len(r) {
			r = append(r[:caret], r[caret+1:]...)
		}
	default:
		return old, caret, false
	}
	return string(r), caret, true
}
