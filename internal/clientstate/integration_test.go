package clientstate_test

// Integration tests keep two simulated PID address spaces independent. This
// tests the intended controller contract, not the live Windows adapters.
import (
	"encoding/binary"
	"github.com/zugbonk/frosty/internal/clientstate"
	"github.com/zugbonk/frosty/internal/movement"
	"github.com/zugbonk/frosty/internal/tracking"
	"math"
	"testing"
)

type gameMemory struct{ b map[uint32]byte }

func (m *gameMemory) Read(a uint32, buf []byte) error {
	for i := range buf {
		buf[i] = m.b[a+uint32(i)]
	}
	return nil
}
func (m *gameMemory) Write(a uint32, b []byte) error {
	for i, v := range b {
		m.b[a+uint32(i)] = v
	}
	return nil
}
func (m *gameMemory) PatchCode(a uint32, b []byte) error { return m.Write(a, b) }
func (m *gameMemory) Alloc(n, p uint32) (uint32, error)  { return 0x3200000, nil }
func (m *gameMemory) FlushCode(a, n uint32) error        { return nil }
func (m *gameMemory) U(a, v uint32) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	_ = m.Write(a, b)
}
func (m *gameMemory) F(a uint32, v float32) { m.U(a, math.Float32bits(v)) }
func (m *gameMemory) Float(a uint32) float32 {
	b := make([]byte, 4)
	_ = m.Read(a, b)
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}
func (m *gameMemory) Uint(a uint32) uint32 {
	b := make([]byte, 4)
	_ = m.Read(a, b)
	return binary.LittleEndian.Uint32(b)
}
func fakeGame(seed byte) *gameMemory {
	m := &gameMemory{b: map[uint32]byte{}}
	m.U(0x00BD088C, 571)
	m.U(0x00CD87A8, 0x200000)
	m.U(0x200034, 0x210000)
	m.U(0x210024, 0x220000)
	m.F(0x220000+0x798, 100)
	m.F(0x220000+0x79c, 200)
	m.F(0x220000+0x7a0, 300)
	m.F(0x220000+0x81c, 7)
	m.F(0x220000+0x820, 4.5)
	m.U(0x220000+8, 0x260000)
	guid := []byte{seed, 2, 3, 4, 5, 6, 7, 8}
	_ = m.Write(0x220000+0x30, guid)
	_ = m.Write(0x260000, guid)
	m.U(0x260000+0x60, 100)
	m.U(0x260000+0x80, 100)
	m.U(0x260000+0xffc, 0x100)
	return m
}
func TestTwoPIDsRetainIndependentOriginalsAndSameSharedConfig(t *testing.T) {
	a, b := fakeGame(1), fakeGame(2)
	ea, err := movement.New(a, 111)
	if err != nil {
		t.Fatal(err)
	}
	eb, err := movement.New(b, 222)
	if err != nil {
		t.Fatal(err)
	}
	ta, err := tracking.New(a)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := tracking.New(b)
	if err != nil {
		t.Fatal(err)
	}
	all := clientstate.Scope{Selected: 111, All: true}
	for _, pid := range all.Targets([]uint32{111, 222}) {
		e, tr := ea, ta
		if pid == 222 {
			e, tr = eb, tb
		}
		if err = e.SetSpeed(movement.Run, "2", true); err != nil {
			t.Fatal(err)
		}
		if err = tr.Set("herb", true); err != nil {
			t.Fatal(err)
		}
	}
	if a.Float(0x220000+0x81c) != 14 || b.Float(0x220000+0x81c) != 14 {
		t.Fatal("ALL failed")
	}
	if a.Uint(0x260000+0xffc) != 2 || b.Uint(0x260000+0xffc) != 2 {
		t.Fatal("independent masks failed")
	}
	// Transition ALL -> SOLO: restore only the deselected PID 222.
	solo := clientstate.Scope{Selected: 111}
	if solo.Wants(222) || !solo.Wants(111) {
		t.Fatal("wrong new scope")
	}
	if err = tb.Close(); err != nil {
		t.Fatal(err)
	}
	if err = eb.StopAll(); err != nil {
		t.Fatal(err)
	}
	if a.Float(0x220000+0x81c) != 14 || a.Uint(0x260000+0xffc) != 2 {
		t.Fatal("SOLO altered selected PID")
	}
	if b.Float(0x220000+0x81c) != 7 || b.Uint(0x260000+0xffc) != 0x100 {
		t.Fatal("SOLO failed to restore unselected PID")
	}
	if err = ta.Close(); err != nil {
		t.Fatal(err)
	}
	if err = ea.StopAll(); err != nil {
		t.Fatal(err)
	}
}
