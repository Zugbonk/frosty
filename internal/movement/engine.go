// Package movement contains the source-owned, testable single-character
// movement controller for WoW 3.3.5a build 12340. No compiled legacy core.
// A caller MUST verify the target executable before providing its Memory.
package movement

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Memory is deliberately platform-independent. PatchCode must change page
// permissions, write, verify, flush the instruction cache, then restore them.
type Memory interface {
	Read(addr uint32, into []byte) error
	Write(addr uint32, from []byte) error
	PatchCode(addr uint32, from []byte) error
}

type Mode string

const (
	Run     Mode = "run"
	Fly     Mode = "fly"
	Swim    Mode = "swim"
	Water   Mode = "water"
	Wall    Mode = "wall"
	Thru    Mode = "thru"
	AirJump Mode = "airjump"
)
const (
	root       uint32 = 0x00CD87A8
	mapIDAddr  uint32 = 0x00BD088C
	pos        uint32 = 0x798
	flags      uint32 = 0x7CC
	guidOffset uint32 = 0x30
	canFly     uint32 = 0x01000000
	flying     uint32 = 0x02000000
	waterWalk  uint32 = 0x10000000
	climbParam uint32 = 0x858
	clipWidth  uint32 = 0x850
	jumpSite   uint32 = 0x00988433
)

var speedFields = map[Mode]struct {
	forward, back uint32
	base          float64
}{
	Run: {0x81C, 0x820, 7}, Fly: {0x82C, 0x830, 7}, Swim: {0x824, 0x828, 4.722222},
}

type Character struct {
	Player, MapID uint32
	GUID          [8]byte
}
type speedState struct {
	before, back, target, targetBack float32
	on                               bool
	flyAdded                         uint32
}
type wallState struct {
	before float32
	on     bool
}
type clipState struct {
	savedWidth float32
	on         bool
}
type Engine struct {
	mem                 Memory
	PID                 uint32
	identity            Character
	speeds              map[Mode]speedState
	waterOn, waterOwned bool
	wall                wallState
	wallHold            wallHold
	clip                clipState
	jump                bool
}

func New(mem Memory, pid uint32) (*Engine, error) {
	if mem == nil || pid == 0 {
		return nil, errors.New("no selected game process")
	}
	c, e := Snapshot(mem)
	if e != nil {
		return nil, e
	}
	return &Engine{mem: mem, PID: pid, identity: c, speeds: map[Mode]speedState{}}, nil
}
func read(m Memory, a uint32, n int) ([]byte, error) {
	if a < 0x10000 || a >= 0x7fff0000 || n < 1 || n > 64 {
		return nil, fmt.Errorf("invalid memory access 0x%08x", a)
	}
	b := make([]byte, n)
	if e := m.Read(a, b); e != nil {
		return nil, e
	}
	return b, nil
}
func u32(m Memory, a uint32) (uint32, error) {
	b, e := read(m, a, 4)
	if e != nil {
		return 0, e
	}
	return binary.LittleEndian.Uint32(b), nil
}
func f32(m Memory, a uint32) (float32, error) { u, e := u32(m, a); return math.Float32frombits(u), e }
func putU(m Memory, a uint32, u uint32) error {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], u)
	return verifyWrite(m, a, b[:])
}
func putF(m Memory, a uint32, v float32) error { return putU(m, a, math.Float32bits(v)) }
func verifyWrite(m Memory, a uint32, b []byte) error {
	if e := m.Write(a, b); e != nil {
		return e
	}
	got, e := read(m, a, len(b))
	if e != nil {
		return e
	}
	if !bytes.Equal(got, b) {
		return fmt.Errorf("write verification failed at 0x%08x", a)
	}
	return nil
}
func validFloat(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) && v > 0
}
func pointer(u uint32) bool { return u >= 0x10000 && u < 0x7ff00000 && u%4 == 0 }
func Snapshot(m Memory) (Character, error) {
	var c Character
	mapID, e := u32(m, mapIDAddr)
	if e != nil || mapID >= 2000 {
		return c, errors.New("invalid or unavailable MapID")
	}
	a, e := u32(m, root)
	if e != nil || !pointer(a) {
		return c, errors.New("character not loaded (player root)")
	}
	b, e := u32(m, a+0x34)
	if e != nil || !pointer(b) {
		return c, errors.New("character not loaded (second pointer)")
	}
	p, e := u32(m, b+0x24)
	if e != nil || !pointer(p) {
		return c, errors.New("character not loaded (player pointer)")
	}
	xyz, e := read(m, p+pos, 12)
	if e != nil {
		return c, e
	}
	x, y, z := math.Float32frombits(binary.LittleEndian.Uint32(xyz)), math.Float32frombits(binary.LittleEndian.Uint32(xyz[4:])), math.Float32frombits(binary.LittleEndian.Uint32(xyz[8:]))
	if math.IsNaN(float64(x)) || math.IsNaN(float64(y)) || math.IsNaN(float64(z)) || math.Abs(float64(x)) > 100000 || math.Abs(float64(y)) > 100000 || math.Abs(float64(z)) > 50000 || (x == 0 && y == 0 && z == 0) {
		return c, errors.New("character coordinates invalid")
	}
	run, e := f32(m, p+0x81C)
	if e != nil || !validFloat(run) {
		return c, errors.New("run speed unavailable")
	}
	g, e := read(m, p+guidOffset, 8)
	if e != nil {
		return c, e
	}
	copy(c.GUID[:], g)
	if c.GUID == ([8]byte{}) {
		return c, errors.New("character GUID not yet available")
	}
	c.Player = p
	c.MapID = mapID
	return c, nil
}
func (e *Engine) identityOK() error {
	cur, err := Snapshot(e.mem)
	if err != nil {
		return err
	}
	if cur.Player != e.identity.Player || cur.GUID != e.identity.GUID {
		return errors.New("character changed; movement controller detached")
	}
	return nil
}
func (e *Engine) MapChanged() bool {
	cur, err := Snapshot(e.mem)
	return err != nil || cur.MapID != e.identity.MapID
}
func (e *Engine) Enabled(m Mode) bool {
	if x, ok := e.speeds[m]; ok {
		return x.on
	}
	switch m {
	case Water:
		return e.waterOn
	case Wall:
		return e.wall.on
	case Thru:
		return e.clip.on
	case AirJump:
		return e.jump
	}
	return false
}
func BaseSpeed(m Mode) (float64, bool) { v, ok := speedFields[m]; return v.base, ok }

// Multiplier parsing: x1 is normal speed, x2 is double. Fly shares Run's
// 7 yards/sec baseline. Swim has its own 4.722222 yards/sec baseline.
func Target(m Mode, input string) (float32, error) {
	v, ok := speedFields[m]
	if !ok {
		return 0, errors.New("unknown speed mode")
	}
	s := strings.TrimSpace(input)
	if s == "" {
		return 0, errors.New("enter a speed multiplier")
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			return 0, errors.New("multiplier must be a positive decimal number")
		}
	}
	x, err := strconv.ParseFloat(s, 64)
	if err != nil || x <= 0 || math.IsInf(x, 0) || math.IsNaN(x) {
		return 0, errors.New("invalid positive speed multiplier")
	}
	dest := v.base * x
	if dest <= 0 || dest > math.MaxFloat32 {
		return 0, errors.New("speed multiplier exceeds game float range")
	}
	if float32(dest) <= 0 {
		return 0, errors.New("speed multiplier underflows game float range")
	}
	return float32(dest), nil
}

// SetSpeed changes both forward and backward fields. The backward speed is
// sampled once and scaled by the same multiplier, never compounded on refresh.
func (e *Engine) SetSpeed(m Mode, text string, on bool) error {
	v, ok := speedFields[m]
	if !ok {
		return errors.New("unknown speed mode")
	}
	if !on {
		return e.StopSpeed(m)
	}
	target, err := Target(m, text)
	if err != nil {
		return err
	}
	if err = e.identityOK(); err != nil {
		return err
	}
	old := e.speeds[m]
	if old.on {
		backTarget := float32(float64(old.back) * float64(target) / v.base)
		if !validFloat(backTarget) {
			return errors.New("backward speed exceeds float range")
		}
		if err = putF(e.mem, e.identity.Player+v.forward, target); err != nil {
			return err
		}
		if err = putF(e.mem, e.identity.Player+v.back, backTarget); err != nil {
			return err
		}
		old.target = target
		old.targetBack = backTarget
		e.speeds[m] = old
		return nil
	}
	before, err := f32(e.mem, e.identity.Player+v.forward)
	if err != nil || !validFloat(before) {
		return errors.New("unable to read original forward speed")
	}
	back, err := f32(e.mem, e.identity.Player+v.back)
	if err != nil || !validFloat(back) {
		return errors.New("unable to read original backward speed")
	}
	backTarget := float32(float64(back) * float64(target) / v.base)
	if !validFloat(backTarget) {
		return errors.New("backward speed exceeds float range")
	}
	origFlags := uint32(0)
	if m == Fly {
		origFlags, err = u32(e.mem, e.identity.Player+flags)
		if err != nil {
			return err
		}
	}
	if err = putF(e.mem, e.identity.Player+v.forward, target); err != nil {
		return err
	}
	if err = putF(e.mem, e.identity.Player+v.back, backTarget); err != nil {
		_ = putF(e.mem, e.identity.Player+v.forward, before)
		return err
	}
	added := uint32(0)
	if m == Fly {
		added = ^origFlags & (canFly | flying)
		if err = putU(e.mem, e.identity.Player+flags, origFlags|canFly); err != nil {
			_ = putF(e.mem, e.identity.Player+v.forward, before)
			_ = putF(e.mem, e.identity.Player+v.back, back)
			return err
		}
	}
	e.speeds[m] = speedState{before: before, back: back, target: target, targetBack: backTarget, on: true, flyAdded: added}
	return nil
}
func (e *Engine) StopSpeed(m Mode) error {
	v, ok := speedFields[m]
	if !ok {
		return errors.New("unknown speed mode")
	}
	s := e.speeds[m]
	if !s.on {
		return nil
	}
	// If character changed, never write to a new/foreign character.
	if err := e.identityOK(); err != nil {
		delete(e.speeds, m)
		return err
	}
	var first error
	if m == Fly && s.flyAdded != 0 {
		now, err := u32(e.mem, e.identity.Player+flags)
		if err == nil {
			err = putU(e.mem, e.identity.Player+flags, now & ^s.flyAdded)
		}
		if err != nil {
			first = err
		}
	}
	if now, err := f32(e.mem, e.identity.Player+v.forward); err == nil && now == s.target {
		if er := putF(e.mem, e.identity.Player+v.forward, s.before); er != nil && first == nil {
			first = er
		}
	}
	if now, err := f32(e.mem, e.identity.Player+v.back); err == nil && now == s.targetBack {
		if er := putF(e.mem, e.identity.Player+v.back, s.back); er != nil && first == nil {
			first = er
		}
	}
	delete(e.speeds, m)
	return first
}
func (e *Engine) SetWater(on bool) error {
	if !on {
		if !e.waterOn {
			return nil
		}
		if err := e.identityOK(); err != nil {
			e.waterOn = false
			return err
		}
		if e.waterOwned {
			f, err := u32(e.mem, e.identity.Player+flags)
			if err != nil {
				return err
			}
			if err = putU(e.mem, e.identity.Player+flags, f & ^waterWalk); err != nil {
				return err
			}
		}
		e.waterOn = false
		e.waterOwned = false
		return nil
	}
	if err := e.identityOK(); err != nil {
		return err
	}
	f, err := u32(e.mem, e.identity.Player+flags)
	if err != nil {
		return err
	}
	if err = putU(e.mem, e.identity.Player+flags, f|waterWalk); err != nil {
		return err
	}
	if !e.waterOn {
		e.waterOwned = (f & waterWalk) == 0
	}
	e.waterOn = true
	return nil
}
func (e *Engine) SetWall(on bool) error {
	if !on {
		if !e.wall.on {
			return nil
		}
		if err := e.identityOK(); err != nil {
			_ = e.releaseWallHold(false)
			e.wall.on = false
			return err
		}
		if err := e.releaseWallHold(true); err != nil {
			return err
		}
		current, err := f32(e.mem, e.identity.Player+climbParam)
		if err != nil {
			return err
		}
		if current == 200 {
			if err = putF(e.mem, e.identity.Player+climbParam, e.wall.before); err != nil {
				return err
			}
		}
		e.wall.on = false
		return nil
	}
	if err := e.identityOK(); err != nil {
		return err
	}
	if e.wall.on {
		return nil
	}
	original, err := f32(e.mem, e.identity.Player+climbParam)
	if err != nil || !validFloat(original) || original >= 200 {
		return errors.New("unsupported wall climb setting")
	}
	if err = putF(e.mem, e.identity.Player+climbParam, 200); err != nil {
		return err
	}
	e.wall = wallState{before: original, on: true}
	e.wallHold = wallHold{}
	return nil
}

// Tick is called only when a single client has been explicitly selected.
// It never opens a dialog, and never writes after a character change.
func (e *Engine) Tick() error {
	if err := e.identityOK(); err != nil {
		return err
	}
	for m, s := range e.speeds {
		if !s.on {
			continue
		}
		v := speedFields[m]
		current, err := f32(e.mem, e.identity.Player+v.forward)
		if err != nil {
			return err
		}
		if current != s.target {
			if err = putF(e.mem, e.identity.Player+v.forward, s.target); err != nil {
				return err
			}
		}
		back, err := f32(e.mem, e.identity.Player+v.back)
		if err != nil {
			return err
		}
		if back != s.targetBack {
			if err = putF(e.mem, e.identity.Player+v.back, s.targetBack); err != nil {
				return err
			}
		}
	}
	fl, err := u32(e.mem, e.identity.Player+flags)
	if err != nil {
		return err
	}
	if e.speeds[Fly].on && fl&canFly == 0 {
		if err = putU(e.mem, e.identity.Player+flags, fl|canFly); err != nil {
			return err
		}
		fl |= canFly
	}
	if e.waterOn && fl&waterWalk == 0 {
		if err = putU(e.mem, e.identity.Player+flags, fl|waterWalk); err != nil {
			return err
		}
		e.waterOwned = true
	}
	if err := e.VerifyPatches(); err != nil {
		return err
	}
	if e.wall.on {
		if e.MapChanged() {
			_ = e.SetWall(false)
			return errors.New("map changed; wall climb stopped")
		}
		v, err := f32(e.mem, e.identity.Player+climbParam)
		if err != nil {
			return err
		}
		if v == e.wall.before {
			return putF(e.mem, e.identity.Player+climbParam, 200)
		}
		if v != 200 {
			return errors.New("wall climb value changed externally")
		}
	}
	return nil
}
func (e *Engine) StopAll() error {
	// Disable code patches before flight mode, then other changes.
	var first error
	for _, m := range []Mode{Thru, AirJump, Wall, Water, Fly, Run, Swim} {
		var err error
		switch m {
		case Thru:
			err = e.SetThru(false)
		case AirJump:
			err = e.SetAirJump(false)
		case Wall:
			err = e.SetWall(false)
		case Water:
			err = e.SetWater(false)
		default:
			err = e.StopSpeed(m)
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Identity is a read-only record of the player object for which this
// controller took its original-value snapshots. Never share it across PIDs.
func (e *Engine) Identity() Character { return e.identity }

// RefreshMap preserves the existing speed and patch ownership when the SAME
// player object/GUID moves to another map. A mere MapID transition is not a
// reason to discard the user's enabled settings or overwrite a new baseline.
// Release a temporary wall anchor before accepting the new map.
func (e *Engine) RefreshMap(c Character) error {
	if c.Player != e.identity.Player || c.GUID != e.identity.GUID {
		return errors.New("different character cannot inherit movement ownership")
	}
	if c.MapID == e.identity.MapID {
		return nil
	}
	if err := e.releaseWallHold(true); err != nil {
		return err
	}
	// A map transition can replace the game's normal wall/collision values
	// without replacing the player pointer. Capture the NEW original once,
	// then reapply the enabled option on the next controller Tick. Do not
	// rebase arbitrary external edits while the MapID has stayed unchanged.
	if e.wall.on {
		value, err := f32(e.mem, e.identity.Player+climbParam)
		if err != nil {
			return err
		}
		if value != 200 {
			if !validFloat(value) || value >= 200 {
				return errors.New("invalid wall parameter after map transition")
			}
			e.wall.before = value
		}
	}
	if e.clip.on {
		width, err := f32(e.mem, e.identity.Player+clipWidth)
		if err != nil {
			return err
		}
		if width != 0 {
			if !validFloat(width) || width >= 25 {
				return errors.New("invalid collision width after map transition")
			}
			e.clip.savedWidth = width
		}
	}
	e.identity.MapID = c.MapID
	return nil
}

// Retire is used on an actual player object/GUID replacement. Character-field
// writes are forbidden after the old identity disappears. Executable code
// patches, however, are process-owned and must still be restored before a new
// movement controller can patch the same sites. A failing patch restore stops
// reattachment rather than abandoning the ownership record.
func (e *Engine) Retire() error {
	if e.identityOK() == nil {
		return e.StopAll()
	}
	var first error
	if err := e.SetThru(false); err != nil {
		first = err
	}
	if err := e.SetAirJump(false); err != nil && first == nil {
		first = err
	}
	_ = e.releaseWallHold(false)
	return first
}
