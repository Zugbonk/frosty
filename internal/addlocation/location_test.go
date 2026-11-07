package addlocation

import (
	"encoding/binary"
	"testing"
)

type fakeMemory map[uint32][]byte

func (m fakeMemory) Read(address uint32, dst []byte) error {
	data, ok := m[address]
	if !ok || len(data) < len(dst) {
		return errUnreadable
	}
	copy(dst, data[:len(dst)])
	return nil
}

type readError struct{}

func (readError) Error() string { return "unreadable memory" }

var errUnreadable error = readError{}

func putPointer(m fakeMemory, address, value uint32) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, value)
	m[address] = b
}

func putText(m fakeMemory, address uint32, value string) {
	b := make([]byte, maxTextBytes)
	copy(b, value)
	m[address] = b
}

func setupLabels() fakeMemory {
	m := fakeMemory{}
	putPointer(m, ZoneTextPointer, 0x10000)
	putPointer(m, SubzoneTextPointer, 0x10100)
	putPointer(m, MinimapTextPointer, 0x10200)
	putText(m, 0x10000, "Elwynn Forest")
	putText(m, 0x10100, "Goldshire")
	putText(m, 0x10200, "Lion's Pride Inn")
	return m
}

func TestReadLabelsPrefersMinimapAndKeepsParentZone(t *testing.T) {
	got := ReadLabels(setupLabels())
	if got.Location != "Lion's Pride Inn" || got.Zone != "Elwynn Forest" {
		t.Fatalf("unexpected labels: %#v", got)
	}
}

func TestReadLabelsFallsBackToSubzoneThenZone(t *testing.T) {
	m := setupLabels()
	putText(m, 0x10200, "")
	if got := ReadLabels(m); got.Location != "Goldshire" || got.Zone != "Elwynn Forest" {
		t.Fatalf("subzone fallback: %#v", got)
	}
	putText(m, 0x10100, "")
	if got := ReadLabels(m); got.Location != "Elwynn Forest" || got.Zone != "Elwynn Forest" {
		t.Fatalf("zone fallback: %#v", got)
	}
}

func TestReadLabelsRejectsOutOfRangePointersAndText(t *testing.T) {
	m := setupLabels()
	putPointer(m, MinimapTextPointer, 0x7FFF0000) // outside the safe client range
	if got := ReadLabels(m); got.Location != "Goldshire" {
		t.Fatalf("out-of-range minimap pointer should fall back: %#v", got)
	}
	putPointer(m, MinimapTextPointer, 0x10300)
	putText(m, 0x10300, "A\nB")
	if got := ReadLabels(m); got.Location != "Goldshire" {
		t.Fatalf("control character should be rejected: %#v", got)
	}
}

func TestReadLabelsHandlesUnreadableMemory(t *testing.T) {
	if got := ReadLabels(fakeMemory{}); got != (Labels{}) {
		t.Fatalf("unreadable addresses must produce empty labels: %#v", got)
	}
}
