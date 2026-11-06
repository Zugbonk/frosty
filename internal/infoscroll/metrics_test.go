package infoscroll

import "testing"

func TestNoOverflow(t *testing.T) {
	x, h := Thumb(300, 0, 0)
	if x != 0 || h != 0 {
		t.Fatalf("unexpected scrollbar %d %d", x, h)
	}
	if Drag(300, 0, 90, 0) != 0 {
		t.Fatal("zero max scroll must stay zero")
	}
}
func TestThumbTracksScroll(t *testing.T) {
	const th = 300
	const max = 680
	at0, h := Thumb(th, max, 0)
	atMax, _ := Thumb(th, max, max)
	if at0 != 0 || atMax != th-h || h >= th {
		t.Fatalf("bad endpoints: %d %d h=%d", at0, atMax, h)
	}
	for _, s := range []int{0, 45, 230, 510, 680} {
		off, _ := Thumb(th, max, s)
		got := Drag(th, max, off+6, 6)
		if d := got - s; d < -3 || d > 3 {
			t.Fatalf("round trip %d -> %d", s, got)
		}
	}
	if Drag(th, max, 9999, 0) != max {
		t.Fatal("bottom drag did not clamp")
	}
	if Drag(th, max, -9999, 0) != 0 {
		t.Fatal("top drag did not clamp")
	}
}
