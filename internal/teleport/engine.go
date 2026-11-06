// Package teleport implements a source-owned, UI-independent WoW 3.3.5a
// build 12340 coordinate teleport experiment. It does not inject code,
// hook the game, or rely on the original lost Teleport Manager binary.
package teleport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Memory is deliberately small so the engine can be tested with an in-memory
// fake, independently of Windows or an actual game client.
type Memory interface {
	Read(address uint32, dst []byte) error
	Write(address uint32, src []byte) error
}

const (
	mapAddress     uint32 = 0x00BD088C // transport-safe, earlier development build reference
	playerPtrRoot  uint32 = 0x00CD87A8
	positionOffset uint32 = 0x798
	facingOffset   uint32 = 0x7A8
	speedOffset    uint32 = 0x81C
)

type State struct {
	MapID           uint32
	Player          uint32
	X, Y, Z, Facing float32
}

func read4(m Memory, address uint32) (uint32, error) {
	var b [4]byte
	if e := m.Read(address, b[:]); e != nil {
		return 0, fmt.Errorf("read 0x%08X: %w", address, e)
	}
	return binary.LittleEndian.Uint32(b[:]), nil
}
func readFloat(m Memory, address uint32) (float32, error) {
	v, e := read4(m, address)
	return math.Float32frombits(v), e
}
func pointer(v uint32) bool { return v >= 0x10000 && v < 0x7FF00000 && v%4 == 0 }
func coordinates(x, y, z float32) bool {
	return finite(x) && finite(y) && finite(z) && x > -100000 && x < 100000 && y > -100000 && y < 100000 && z > -50000 && z < 50000 && (x != 0 || y != 0 || z != 0)
}
func finite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
func destination(x, y, z float32) error {
	if !coordinates(x, y, z) {
		return errors.New("invalid XYZ coordinates")
	}
	return nil
}

// Snapshot verifies the 12340 player pointer chain and plausible live state.
// A different game patch or a loading screen should produce an error.
func Snapshot(m Memory) (State, error) {
	var s State
	mapID, e := read4(m, mapAddress)
	if e != nil {
		return s, e
	}
	if mapID >= 2000 {
		return s, fmt.Errorf("invalid MapID %d", mapID)
	}
	a, e := read4(m, playerPtrRoot)
	if e != nil || !pointer(a) {
		return s, errors.New("player root unavailable; character may be loading")
	}
	b, e := read4(m, a+0x34)
	if e != nil || !pointer(b) {
		return s, errors.New("second player pointer unavailable")
	}
	p, e := read4(m, b+0x24)
	if e != nil || !pointer(p) {
		return s, errors.New("live player pointer unavailable")
	}
	x, e := readFloat(m, p+positionOffset)
	if e != nil {
		return s, e
	}
	y, e := readFloat(m, p+positionOffset+4)
	if e != nil {
		return s, e
	}
	z, e := readFloat(m, p+positionOffset+8)
	if e != nil {
		return s, e
	}
	f, e := readFloat(m, p+facingOffset)
	if e != nil {
		return s, e
	}
	run, e := readFloat(m, p+speedOffset)
	if e != nil {
		return s, e
	}
	if !coordinates(x, y, z) || !finite(f) || f < -7 || f > 7 || !finite(run) || run <= 0 {
		return s, errors.New("player data invalid or character not fully loaded")
	}
	s = State{MapID: mapID, Player: p, X: x, Y: y, Z: z, Facing: f}
	return s, nil
}

func floats(x, y, z float32) []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b[0:4], math.Float32bits(x))
	binary.LittleEndian.PutUint32(b[4:8], math.Float32bits(y))
	binary.LittleEndian.PutUint32(b[8:12], math.Float32bits(z))
	return b
}

// Teleport reads the character state immediately before writing and rechecks
// MapID/player identity. A failed write/verification attempts to restore XYZ.
// Like the historical client, this is not an atomic game-server action.
func Teleport(m Memory, expected State, x, y, z float32) error {
	if err := destination(x, y, z); err != nil {
		return err
	}
	cur, e := Snapshot(m)
	if e != nil {
		return e
	}
	if cur.MapID != expected.MapID {
		return fmt.Errorf("MapID changed (%d to %d); teleport cancelled", expected.MapID, cur.MapID)
	}
	if cur.Player != expected.Player {
		return errors.New("character pointer changed; teleport cancelled")
	}
	old := floats(cur.X, cur.Y, cur.Z)
	dest := floats(x, y, z)
	if e = m.Write(cur.Player+positionOffset, dest); e != nil {
		_ = m.Write(cur.Player+positionOffset, old)
		return fmt.Errorf("write failed; attempted original-position restore: %w", e)
	}
	verify := make([]byte, 12)
	if e = m.Read(cur.Player+positionOffset, verify); e != nil {
		_ = m.Write(cur.Player+positionOffset, old)
		return fmt.Errorf("cannot verify write; attempted original-position restore: %w", e)
	}
	if string(verify) != string(dest) {
		_ = m.Write(cur.Player+positionOffset, old)
		return errors.New("position did not match requested XYZ; attempted restore")
	}
	// Check after write as well. Do not claim server acceptance from RPM data.
	mm, e := read4(m, mapAddress)
	if e != nil || mm != expected.MapID {
		_ = m.Write(cur.Player+positionOffset, old)
		return errors.New("MapID changed during operation; attempted restore")
	}
	return nil
}

func ForwardOneYard(m Memory, current State) error {
	x := current.X + float32(math.Cos(float64(current.Facing)))
	y := current.Y + float32(math.Sin(float64(current.Facing)))
	return Teleport(m, current, x, y, current.Z)
}
