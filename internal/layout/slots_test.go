package layout

import "testing"

func TestReservedPositions(t *testing.T) {
	if len(Reserved) != 8 {
		t.Fatal("need eight reserved slots")
	}
	groups := map[string]int{}
	seen := map[string]bool{}
	for _, s := range Reserved {
		if seen[s.ID] {
			t.Fatalf("duplicate ID %s", s.ID)
		}
		seen[s.ID] = true
		groups[s.Region]++
		r := s.Bounds
		if r.X < 0 || r.Y < 0 || r.X+r.W > 1590 || r.Y+r.H > 860 || r.W < 20 || r.H < 20 {
			t.Fatalf("out of bounds: %+v", s)
		}
	}
	if groups["movement"] != 2 || groups["teleport"] != 3 || groups["sidebar"] != 3 {
		t.Fatalf("bad group sizes: %+v", groups)
	}
	for i, a := range Reserved {
		for j, b := range Reserved {
			if i >= j {
				continue
			}
			x := a.Bounds
			y := b.Bounds
			if x.X < y.X+y.W && y.X < x.X+x.W && x.Y < y.Y+y.H && y.Y < x.Y+x.H {
				t.Fatalf("overlapping slots %s %s", a.ID, b.ID)
			}
		}
	}
}
