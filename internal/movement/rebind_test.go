package movement

import "testing"

func TestMapChangeRetainsDesiredControllerAndOriginalSpeed(t *testing.T) {
	f := newFake()
	e, err := New(f, 78)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.SetSpeed(Run, "3", true); err != nil {
		t.Fatal(err)
	}
	if err = e.SetWall(true); err != nil {
		t.Fatal(err)
	}
	f.setU(mapIDAddr, 571)
	c, err := Snapshot(f)
	if err != nil {
		t.Fatal(err)
	}
	if c.MapID != 571 {
		t.Fatal("wrong new map")
	}
	if err = e.RefreshMap(c); err != nil {
		t.Fatal(err)
	}
	f.setF(0x40000+0x81C, 7) // game reset after teleport
	f.setF(0x40000+climbParam, 1)
	if err = e.Tick(); err != nil {
		t.Fatal(err)
	}
	if got := readFakeF(f, 0x40000+0x81C); got != 21 {
		t.Fatalf("reapply run: %f", got)
	}
	if got := readFakeF(f, 0x40000+climbParam); got != 200 {
		t.Fatalf("reapply wall: %f", got)
	}
	if err = e.StopAll(); err != nil {
		t.Fatal(err)
	}
	if readFakeF(f, 0x40000+0x81C) != 7 || readFakeF(f, 0x40000+climbParam) != 1 {
		t.Fatal("original snapshots lost on map change")
	}
}
func TestCharacterChangeRetiresCodeHooksWithoutTouchingNewPlayer(t *testing.T) {
	f := newFake()
	e, err := New(f, 12)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.SetSpeed(Fly, "2", true); err != nil {
		t.Fatal(err)
	}
	if err = e.SetThru(true); err != nil {
		t.Fatal(err)
	}
	if err = e.SetAirJump(true); err != nil {
		t.Fatal(err)
	}
	f.b[0x40000+guidOffset] = 99 // same player pointer, different actor
	f.setF(0x40000+0x81C, 11)    // unrelated current actor's field
	if err = e.Retire(); err != nil {
		t.Fatal(err)
	}
	if got := readFakeF(f, 0x40000+0x81C); got != 11 {
		t.Fatal("changed new player's speed")
	}
	for _, s := range textureSites {
		got, err := read(f, s.at, len(s.old))
		if err != nil {
			t.Fatal(err)
		}
		for j := range got {
			if got[j] != s.old[j] {
				t.Fatal("texture code hook not restored")
			}
		}
	}
	got, err := read(f, jumpSite, len(jumpOriginal))
	if err != nil {
		t.Fatal(err)
	}
	for j := range got {
		if got[j] != jumpOriginal[j] {
			t.Fatal("airjump code not restored")
		}
	}
}
