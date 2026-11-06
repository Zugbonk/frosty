package movement

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// WallHoldInput is a snapshot of *physical* input for the selected WoW window.
// Collect it on Windows; the movement engine remains platform-independent.
type WallHoldInput struct {
	GameFocused bool
	Forward     bool // W, or both mouse buttons held together
	Backward    bool // S
	Jump        bool // Space
}

const (
	gravityDisabled uint32 = 0x00000400
	onTransport     uint32 = 0x00000200
	// Only translation/falling/pitch flags may be cleared while anchored.
	// 0x10/0x20 (turning) are intentionally preserved.
	holdClear uint32 = 0x0000000F | 0x000000C0 | 0x000F0000 | 0x00C00000 | 0x00003000
)

type wallHold struct {
	active, resuming, gravityOwned       bool
	forwardSession, previousForward      bool
	jumpRecent, focusMissed, resumeTicks int
	start, anchor, resumeStart           [3]float32
	peakZ                                float32
}

func (e *Engine) readPosition() ([3]float32, error) {
	var xyz [3]float32
	b, err := read(e.mem, e.identity.Player+pos, 12)
	if err != nil {
		return xyz, err
	}
	for i := 0; i < 3; i++ {
		xyz[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4 : i*4+4]))
	}
	if !validXYZ(xyz) {
		return xyz, errors.New("invalid character position during wall hold")
	}
	return xyz, nil
}
func validXYZ(x [3]float32) bool {
	for i, v := range x {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return false
		}
		limit := float64(100000)
		if i == 2 {
			limit = 50000
		}
		if math.Abs(float64(v)) > limit {
			return false
		}
	}
	return x != ([3]float32{})
}
func (e *Engine) writePosition(x [3]float32) error {
	var b [12]byte
	for i, v := range x {
		binary.LittleEndian.PutUint32(b[4*i:4*i+4], math.Float32bits(v))
	}
	return verifyWrite(e.mem, e.identity.Player+pos, b[:])
}
func holdAbs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
func holdDiff(a, b [3]float32) (dx, dy, dz float32) {
	return holdAbs(a[0] - b[0]), holdAbs(a[1] - b[1]), holdAbs(a[2] - b[2])
}

// releaseWallHold only clears gravity when THIS engine set that bit. It never
// touches other movement bits or a different player. Call only after identity
// has been validated, or pass false when the player/session is no longer valid.
func (e *Engine) releaseWallHold(validSession bool) error {
	owned := e.wallHold.gravityOwned
	e.wallHold = wallHold{}
	if !owned || !validSession {
		return nil
	}
	bits, err := u32(e.mem, e.identity.Player+flags)
	if err != nil {
		return err
	}
	if bits&gravityDisabled != 0 {
		return putU(e.mem, e.identity.Player+flags, bits & ^gravityDisabled)
	}
	return nil
}
func (e *Engine) holdFlags(bits uint32) error {
	newFlags := (bits | gravityDisabled) &^ holdClear
	if bits != newFlags {
		return putU(e.mem, e.identity.Player+flags, newFlags)
	}
	return nil
}

// WallHoldTick implements the previous production application wall pause/resume behavior using NEW
// Go source. It must be invoked about every 40ms when Wall Climb is enabled.
// It anchors only a position that normal uphill movement has already reached.
// It never adds height, never overrides server corrections >1.5 yards, and
// gives up on changing characters/maps or unsupported movement states.
func (e *Engine) WallHoldTick(input WallHoldInput) error {
	if !e.wall.on {
		return nil
	}
	if err := e.identityOK(); err != nil {
		_ = e.releaseWallHold(false)
		return err
	}
	c, err := Snapshot(e.mem)
	if err != nil {
		_ = e.releaseWallHold(false)
		return err
	}
	if c.MapID != e.identity.MapID {
		_ = e.releaseWallHold(true)
		return errors.New("map changed; wall climb hold released")
	}
	xyz, err := e.readPosition()
	if err != nil {
		_ = e.releaseWallHold(true)
		return err
	}
	bits, err := u32(e.mem, e.identity.Player+flags)
	if err != nil {
		_ = e.releaseWallHold(true)
		return err
	}
	if bits&flying != 0 {
		return e.releaseWallHold(true)
	}

	h := &e.wallHold
	focused := input.GameFocused
	forward := focused && input.Forward
	jump := focused && input.Jump
	detach := focused && (input.Backward || jump)
	if jump {
		h.jumpRecent = 20
	} else if h.jumpRecent > 0 {
		h.jumpRecent--
	}
	if !focused {
		if h.focusMissed < 100 {
			h.focusMissed++
		}
	} else {
		h.focusMissed = 0
	}

	if h.active {
		if detach || h.focusMissed > 10 {
			return e.releaseWallHold(true)
		}
		if forward {
			h.active = false
			h.resuming = true
			h.resumeTicks = 0
			h.resumeStart = xyz
			h.previousForward = true
			return nil
		}
		dx, dy, dz := holdDiff(xyz, h.anchor)
		if dx > 1.5 || dy > 1.5 || dz > 1.5 {
			return e.releaseWallHold(true)
		}
		if dx+dy+dz > 0.018 {
			if err = e.writePosition(h.anchor); err != nil {
				_ = e.releaseWallHold(true)
				return fmt.Errorf("wall anchor write: %w", err)
			}
		}
		if err = e.holdFlags(bits); err != nil {
			_ = e.releaseWallHold(true)
			return fmt.Errorf("wall hold flags: %w", err)
		}
		return nil
	}
	if h.resuming {
		if !focused || detach || h.focusMissed > 10 {
			return e.releaseWallHold(true)
		}
		if !forward {
			dx, dy, dz := holdDiff(xyz, h.anchor)
			if dx > 1.5 || dy > 1.5 || dz > 1.5 {
				return e.releaseWallHold(true)
			}
			h.anchor = xyz
			if err = e.holdFlags(bits); err != nil {
				_ = e.releaseWallHold(true)
				return err
			}
			h.active = true
			h.resuming = false
			h.forwardSession = false
			h.previousForward = false
			return nil
		}
		h.resumeTicks++
		dx, dy, zdiff := holdDiff(xyz, h.resumeStart)
		dz := xyz[2] - h.resumeStart[2]
		if dx > 2.5 || dy > 2.5 || dz < -1.0 || dz > 3.5 {
			return e.releaseWallHold(true)
		}
		horizontal := dx + dy
		if (dz >= 0.12 && horizontal >= 0.025) || (horizontal >= 0.9 && zdiff <= 0.3 && dz >= -0.12) || h.resumeTicks >= 20 {
			if err = e.releaseWallHold(true); err != nil {
				return err
			}
			e.wallHold.forwardSession = true
			e.wallHold.previousForward = true
			e.wallHold.start = xyz
			e.wallHold.peakZ = xyz[2]
		}
		return nil
	}
	if !focused || detach {
		h.forwardSession = false
		h.previousForward = false
		return nil
	}
	if forward {
		if !h.forwardSession {
			h.start = xyz
			h.peakZ = xyz[2]
			h.forwardSession = true
		}
		if xyz[2] > h.peakZ {
			h.peakZ = xyz[2]
		}
	} else if h.previousForward && h.forwardSession {
		dz := xyz[2] - h.start[2]
		peak := h.peakZ - h.start[2]
		traveled := holdAbs(xyz[0]-h.start[0]) + holdAbs(xyz[1]-h.start[1])
		if peak >= 0.27 && dz >= 0.22 && traveled >= 0.12 && h.jumpRecent == 0 && bits&onTransport == 0 {
			next := (bits | gravityDisabled) &^ holdClear
			if err = putU(e.mem, e.identity.Player+flags, next); err != nil {
				return err
			}
			// Verify gravity really became disabled before claiming ownership.
			verify, er := u32(e.mem, e.identity.Player+flags)
			if er != nil || verify&gravityDisabled == 0 {
				return errors.New("could not verify wall gravity hold")
			}
			h.anchor = xyz
			h.active = true
			h.gravityOwned = bits&gravityDisabled == 0
		}
		h.forwardSession = false
	}
	h.previousForward = forward
	return nil
}
