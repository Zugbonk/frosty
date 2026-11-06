package movement

import (
	"bytes"
	"errors"
	"fmt"
)

// These guarded instruction ranges originate from the user-tested previous production application
// source and apply ONLY to verified WoW build 12340 processes in memory.
// Never write a code range that differs from the expected original bytes.
type patchSite struct {
	at                       uint32
	before, old, replacement []byte
}

var textureSites = []patchSite{
	{0x007A50CF, []byte{0xf7, 0xc2, 0x0f, 0x00, 0xf0, 0x00}, []byte{0x0f, 0x84, 0x62, 0x01, 0x00, 0x00}, []byte{0xe9, 0x63, 0x01, 0x00, 0x00, 0x90}},
	{0x007A52EC, []byte{0x86, 0xa0, 0xfe, 0xff, 0x84, 0xc0}, []byte{0x74, 0x17}, []byte{0xeb, 0x17}},
	{0x007AE7EA, []byte{0xb9, 0xe0, 0x01, 0x00, 0x00, 0x00}, []byte{0x75, 0x06}, []byte{0x90, 0x90}},
}
var jumpPrefix = []byte{0x8b, 0x7e, 0x44, 0xf7, 0xc7, 0x00, 0x18, 0x00, 0x02}
var jumpSuffix = []byte{0x0f, 0xb7, 0x5e, 0x48}
var jumpOriginal = []byte{0x75, 0x44}
var jumpReplacement = []byte{0x90, 0x90}

func checkPatch(m Memory, s patchSite, expect []byte) error {
	ctx, err := read(m, s.at-uint32(len(s.before)), len(s.before))
	if err != nil {
		return err
	}
	if !bytes.Equal(ctx, s.before) {
		return fmt.Errorf("code signature mismatch at 0x%08x", s.at)
	}
	got, err := read(m, s.at, len(expect))
	if err != nil {
		return err
	}
	if !bytes.Equal(got, expect) {
		return fmt.Errorf("code patch mismatch at 0x%08x", s.at)
	}
	return nil
}
func writePatch(m Memory, addr uint32, from, to []byte) error {
	found, err := read(m, addr, len(from))
	if err != nil {
		return err
	}
	if !bytes.Equal(found, from) {
		return fmt.Errorf("code bytes unexpected at 0x%08x", addr)
	}
	if err = m.PatchCode(addr, to); err != nil {
		return err
	}
	confirm, err := read(m, addr, len(to))
	if err != nil {
		return err
	}
	if !bytes.Equal(confirm, to) {
		return fmt.Errorf("code patch verification failed at 0x%08x", addr)
	}
	return nil
}
func verifyJump(m Memory, current []byte) error {
	p, err := read(m, jumpSite-uint32(len(jumpPrefix)), len(jumpPrefix))
	if err != nil {
		return err
	}
	if !bytes.Equal(p, jumpPrefix) {
		return errors.New("AirJump signature mismatch before patch")
	}
	s, err := read(m, jumpSite+2, len(jumpSuffix))
	if err != nil {
		return err
	}
	if !bytes.Equal(s, jumpSuffix) {
		return errors.New("AirJump signature mismatch after patch")
	}
	actual, err := read(m, jumpSite, 2)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, current) {
		return errors.New("AirJump original/owned bytes mismatch")
	}
	return nil
}
func (e *Engine) SetAirJump(on bool) error {
	if !on {
		if !e.jump {
			return nil
		}
		if err := verifyJump(e.mem, jumpReplacement); err != nil {
			return err
		}
		if err := writePatch(e.mem, jumpSite, jumpReplacement, jumpOriginal); err != nil {
			return err
		}
		e.jump = false
		return nil
	}
	if e.jump {
		return nil
	}
	if err := e.identityOK(); err != nil {
		return err
	}
	if err := verifyJump(e.mem, jumpOriginal); err != nil {
		return err
	}
	if err := writePatch(e.mem, jumpSite, jumpOriginal, jumpReplacement); err != nil {
		return err
	}
	e.jump = true
	return nil
}
func (e *Engine) SetThru(on bool) error {
	if !on {
		if !e.clip.on {
			return nil
		}
		var first error
		// Reverse order; restore ONLY our own patch bytes. Keep ownership if a
		// mismatch prevents safe restoration, and show it as an error to the user.
		for i := len(textureSites) - 1; i >= 0; i-- {
			s := textureSites[i]
			if err := checkPatch(e.mem, s, s.replacement); err != nil {
				if first == nil {
					first = err
				}
				continue
			}
			if err := writePatch(e.mem, s.at, s.replacement, s.old); err != nil && first == nil {
				first = err
			}
		}
		if e.identityOK() == nil {
			now, err := f32(e.mem, e.identity.Player+clipWidth)
			if err == nil && now == 0 {
				if err = putF(e.mem, e.identity.Player+clipWidth, e.clip.savedWidth); err != nil && first == nil {
					first = err
				}
			}
		}
		if first == nil {
			e.clip = clipState{}
		}
		return first
	}
	if e.clip.on {
		return nil
	}
	if err := e.identityOK(); err != nil {
		return err
	}
	if !e.speeds[Fly].on {
		return errors.New("enable Fly first; Fly Thru requires Fly")
	}
	for _, s := range textureSites {
		if err := checkPatch(e.mem, s, s.old); err != nil {
			return err
		}
	}
	width, err := f32(e.mem, e.identity.Player+clipWidth)
	if err != nil || width <= 0.001 || width >= 25 {
		return errors.New("collision width is not a supported positive value")
	}
	applied := 0
	for i, s := range textureSites {
		if err = writePatch(e.mem, s.at, s.old, s.replacement); err != nil {
			applied = i
			break
		}
		applied = i + 1
	}
	if err == nil {
		err = putF(e.mem, e.identity.Player+clipWidth, 0)
	}
	if err != nil {
		if current, readErr := f32(e.mem, e.identity.Player+clipWidth); readErr == nil && current == 0 {
			_ = putF(e.mem, e.identity.Player+clipWidth, width)
		}
		for i := applied - 1; i >= 0; i-- {
			s := textureSites[i]
			if checkPatch(e.mem, s, s.replacement) == nil {
				_ = writePatch(e.mem, s.at, s.replacement, s.old)
			}
		}
		return err
	}
	e.clip = clipState{savedWidth: width, on: true}
	return nil
}
func (e *Engine) VerifyPatches() error {
	if e.jump {
		if err := verifyJump(e.mem, jumpReplacement); err != nil {
			return err
		}
	}
	if e.clip.on {
		for _, s := range textureSites {
			if err := checkPatch(e.mem, s, s.replacement); err != nil {
				return err
			}
		}
		v, err := f32(e.mem, e.identity.Player+clipWidth)
		if err != nil {
			return err
		}
		if v == e.clip.savedWidth {
			return putF(e.mem, e.identity.Player+clipWidth, 0)
		}
		if v != 0 {
			return errors.New("collision width changed externally")
		}
	}
	return nil
}
