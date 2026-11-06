package tracking

import (
	"github.com/zugbonk/frosty/internal/movement"
	"testing"
)

func TestSamePlayerMapTransitionKeepsMasksAndRestoration(t *testing.T) {
	m := fixture()
	e, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Set("herb", true); err != nil {
		t.Fatal(err)
	}
	m.setU(0x00BD088C, 530)
	if e.Tick() == nil {
		t.Fatal("old map snapshot must not be blindly used")
	}
	c, err := movement.Snapshot(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.RefreshMap(c); err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(); err != nil {
		t.Fatal(err)
	}
	if got := m.u(0x260000 + resourceOffset); got != 2 {
		t.Fatalf("tracking not retained: %x", got)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if got := m.u(0x260000 + resourceOffset); got != 0x100 {
		t.Fatalf("tracking original not restored: %x", got)
	}
}
