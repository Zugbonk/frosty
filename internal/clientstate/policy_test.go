package clientstate

import "testing"

func TestSoloAllScopes(t *testing.T) {
	pids := []uint32{30, 12, 30, 40}
	solo := Scope{Selected: 30}
	a := solo.Targets(pids)
	if len(a) != 1 || a[0] != 30 || solo.Wants(12) {
		t.Fatalf("solo: %v", a)
	}
	all := Scope{Selected: 30, All: true}
	a = all.Targets(pids)
	if len(a) != 3 || a[0] != 12 || a[1] != 30 || a[2] != 40 || !all.Wants(12) {
		t.Fatalf("all: %v", a)
	}
	if len((Scope{Selected: 999}).Targets(pids)) != 0 {
		t.Fatal("phantom PID")
	}
}
func TestPlayerIdentityNeverCrossesPIDs(t *testing.T) {
	g := [8]byte{1, 2, 3}
	if !SamePlayer(12, 12, 0x40000, 0x40000, g, g) {
		t.Fatal("same player rejected")
	}
	for _, s := range []struct {
		p, q, old, new uint32
		x, y           [8]byte
	}{
		{12, 13, 0x40000, 0x40000, g, g},
		{12, 12, 0x40000, 0x50000, g, g},
		{12, 12, 0x40000, 0x40000, g, [8]byte{3}},
	} {
		if SamePlayer(s.p, s.q, s.old, s.new, s.x, s.y) {
			t.Fatal("foreign player accepted")
		}
	}
}
func TestStableAttachGate(t *testing.T) {
	var o StableObserved
	g := [8]byte{1}
	if o.Observe(12, 100, g, 0, true) || !o.Observe(12, 100, g, 0, true) {
		t.Fatal("must wait two matches")
	}
	if o.Observe(12, 100, g, 1, true) || !o.Observe(12, 100, g, 1, true) {
		t.Fatal("map change should restabilize")
	}
	if o.Observe(12, 100, g, 1, false) || o.Count != 0 {
		t.Fatal("loading must reset")
	}
	if o.Observe(12, 200, g, 1, true) || !o.Observe(12, 200, g, 1, true) {
		t.Fatal("relog should stabilize")
	}
}
