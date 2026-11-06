package teleport

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
)

type fake struct {
	words  map[uint32]byte
	fail   bool
	writes int
}

func (f *fake) Read(a uint32, b []byte) error {
	for i := range b {
		v, ok := f.words[a+uint32(i)]
		if !ok {
			return errors.New("unmapped")
		}
		b[i] = v
	}
	return nil
}
func (f *fake) Write(a uint32, b []byte) error {
	f.writes++
	if f.fail {
		return errors.New("no write")
	}
	for i, v := range b {
		f.words[a+uint32(i)] = v
	}
	return nil
}
func (f *fake) put(a uint32, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	for i, x := range b {
		f.words[a+uint32(i)] = x
	}
}
func standard() *fake {
	f := &fake{words: map[uint32]byte{}}
	f.put(mapAddress, 571)
	f.put(playerPtrRoot, 0x10000000)
	f.put(0x10000034, 0x10001000)
	f.put(0x10001024, 0x10002000)
	f.put(0x10002000+0x798, math.Float32bits(100))
	f.put(0x10002000+0x79c, math.Float32bits(-300))
	f.put(0x10002000+0x7a0, math.Float32bits(500))
	f.put(0x10002000+0x7a8, math.Float32bits(0))
	f.put(0x10002000+0x81c, math.Float32bits(7))
	return f
}
func TestSnapshotAndTeleport(t *testing.T) {
	m := standard()
	s, e := Snapshot(m)
	if e != nil || s.MapID != 571 || s.X != 100 {
		t.Fatalf("snapshot: %+v %v", s, e)
	}
	if e = Teleport(m, s, 120, 240, 360); e != nil {
		t.Fatal(e)
	}
	next, e := Snapshot(m)
	if e != nil || next.X != 120 || next.Y != 240 || next.Z != 360 {
		t.Fatalf("next: %+v %v", next, e)
	}
}
func TestChangedMapProtects(t *testing.T) {
	m := standard()
	s, _ := Snapshot(m)
	m.put(mapAddress, 1)
	e := Teleport(m, s, 1, 2, 3)
	if e == nil || !strings.Contains(e.Error(), "MapID") || m.writes != 0 {
		t.Fatalf("%v %d", e, m.writes)
	}
}
func TestChangedPlayerProtects(t *testing.T) {
	m := standard()
	s, _ := Snapshot(m)
	m.put(0x10001024, 0x10003000)
	e := Teleport(m, s, 1, 2, 3)
	if e == nil || m.writes != 0 {
		t.Fatalf("%v %d", e, m.writes)
	}
}
func TestInvalidDestinationProtects(t *testing.T) {
	m := standard()
	s, _ := Snapshot(m)
	e := Teleport(m, s, float32(math.NaN()), 2, 3)
	if e == nil || m.writes != 0 {
		t.Fatal("invalid was accepted")
	}
}
func TestForward(t *testing.T) {
	m := standard()
	s, _ := Snapshot(m)
	if e := ForwardOneYard(m, s); e != nil {
		t.Fatal(e)
	}
	next, _ := Snapshot(m)
	if next.X != 101 || next.Y != -300 {
		t.Fatalf("forward %+v", next)
	}
}
func TestWriteFailure(t *testing.T) {
	m := standard()
	s, _ := Snapshot(m)
	m.fail = true
	if e := Teleport(m, s, 1, 2, 3); e == nil {
		t.Fatal("failed write not reported")
	}
}
func TestLoadingGuard(t *testing.T) {
	m := standard()
	m.put(playerPtrRoot, 0)
	if _, e := Snapshot(m); e == nil {
		t.Fatal("invalid player accepted")
	}
}
