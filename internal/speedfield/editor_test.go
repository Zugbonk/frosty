package speedfield

import "testing"

func TestSpeedEditingAtActualCaret(t *testing.T) {
	got, pos := Edit("15", 1, false, '0')
	if got != "105" || pos != 2 {
		t.Fatalf("insert at caret: %q %d", got, pos)
	}
	got, pos = Edit(got, pos, false, 8)
	if got != "15" || pos != 1 {
		t.Fatalf("backspace: %q %d", got, pos)
	}
	got, pos, ok := Navigate("12.5", 2, 0x2e)
	if !ok || got != "125" || pos != 2 {
		t.Fatalf("delete: %q %d", got, pos)
	}
	got, pos = Edit("5", 1, true, '2')
	if got != "2" || pos != 1 {
		t.Fatalf("replace selected: %q %d", got, pos)
	}
	got, pos, ok = Navigate("25", 1, 0x24)
	if !ok || got != "25" || pos != 0 {
		t.Fatalf("home: %q %d", got, pos)
	}
	got, pos, ok = Navigate(got, pos, 0x23)
	if !ok || pos != 2 {
		t.Fatalf("end: %q %d", got, pos)
	}
}
