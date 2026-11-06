// Package tracking implements the three original native WoW 3.3.5a build 12340
// minimap tracking families, without importing any of the lost earlier development build core.
// The session's exact game build and PE image base MUST be verified by the caller.
package tracking

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/zugbonk/frosty/internal/movement"
)

// Memory exposes only the operations required by a single validated client.
// Alloc reserves game-private memory; executable pages are intentionally not
// freed while WoW is alive because native rendering threads may be inside.
type Memory interface {
	Read(uint32, []byte) error
	Write(uint32, []byte) error
	PatchCode(uint32, []byte) error
	Alloc(size uint32, protection uint32) (uint32, error)
	FlushCode(addr uint32, n uint32) error
}

const (
	resourceOffset       = 0x0FFC
	creatureOffset       = 0x0FF8
	gameTrackPointer     = 0x00BEBA64
	gameRepairDefinition = 0x00A11C50
	npcPredicate         = 0x0071ACF0
	mailboxPredicate     = 0x0070F850
	questPredicate       = 0x0057BF30
	mailboxStubOffset    = 0x80
	questStubOffset      = 0xC0
)

// Family is the native update-field group or Blizzard's native service renderer.
type Family uint8

const (
	Resources Family = iota
	Creatures
	Services
)

type Option struct {
	ID     string
	Family Family
	Bit    uint32
}

const resourceNamed = (1 << 1) | (1 << 2) | (1 << 5) | (1 << 18)
const creatureNamed = (1 << 0) | (1 << 1) | (1 << 2) | (1 << 3) | (1 << 4) | (1 << 5) | (1 << 6) | (1 << 7) | (1 << 8) | (1 << 10) | (1 << 11)

// Legacy masks and the eleven supported named creatures are deliberately kept
// separate: "All Other" applies only to the IDs not explicitly listed.
var options = []Option{
	{"herb", Resources, 1 << 1}, {"mine", Resources, 1 << 2}, {"treasure", Resources, 1 << 5}, {"fish", Resources, 1 << 18}, {"object_other", Resources, ^uint32(resourceNamed)},
	{"beasts", Creatures, 1 << 0}, {"dragons", Creatures, 1 << 1}, {"demons", Creatures, 1 << 2}, {"elementals", Creatures, 1 << 3},
	{"mechanical", Creatures, 1 << 8}, {"totems", Creatures, 1 << 10}, {"giants", Creatures, 1 << 4}, {"undead", Creatures, 1 << 5},
	{"human", Creatures, 1 << 6}, {"critters", Creatures, 1 << 7}, {"pets", Creatures, 1 << 11}, {"creature_other", Creatures, ^uint32(creatureNamed)},
	{"repair", Services, 0x00001000}, {"food", Services, 0x00000200}, {"poison", Services, 0x00000400}, {"ammo", Services, 0x00000100},
	{"reagents", Services, 0x00000800}, {"innkeeper", Services, 0x00010000}, {"flightmaster", Services, 0x00002000},
	{"battlemaster", Services, 0x00100000}, {"trainer", Services, 0x00000020}, {"profession", Services, 0x00000040},
	{"auctioneer", Services, 0x00200000}, {"banker", Services, 0x00020000}, {"mailbox", Services, 0}, {"lowquests", Services, 0},
}
var optionByID = func() map[string]Option {
	m := make(map[string]Option, len(options))
	for _, o := range options {
		m[o.ID] = o
	}
	return m
}()

func Known(id string) bool { _, ok := optionByID[id]; return ok }
func List() []Option       { return append([]Option(nil), options...) }

type groupState struct {
	original, last uint32
	managed        bool
}

type Engine struct {
	mem        Memory
	identity   movement.Character
	descriptor uint32
	enabled    map[string]bool
	groups     [2]groupState
	// Selected Blizzard service descriptor and optional native predicate bridges.
	nativeDesc     uint32
	originalNative uint32
	nativeSelected bool
	hookPage       uint32
	hookMask       uint8
	// The native questgiver overhead refresh is queued from WoW's own renderer
	// AFTER the low-level quest flag changes. Preserve the bridge until the
	// renderer acknowledges the OFF transition; otherwise the UI manager may
	// retire the session before the cached overhead icons are refreshed.
	questOffPending bool
	questOffCount   uint32
}

func New(mem Memory) (*Engine, error) {
	if mem == nil {
		return nil, errors.New("no WoW process attached")
	}
	id, err := movement.Snapshot(mem)
	if err != nil {
		return nil, err
	}
	d, err := getDescriptor(mem, id.Player, id.GUID)
	if err != nil {
		return nil, err
	}
	return &Engine{mem: mem, identity: id, descriptor: d, enabled: make(map[string]bool)}, nil
}
func read(mem Memory, addr uint32, n int) ([]byte, error) {
	if addr < 0x10000 || addr >= 0x7ff00000 || n < 1 || n > 128 {
		return nil, fmt.Errorf("invalid WoW address 0x%08x", addr)
	}
	b := make([]byte, n)
	if err := mem.Read(addr, b); err != nil {
		return nil, err
	}
	return b, nil
}
func getU(mem Memory, addr uint32) (uint32, error) {
	b, e := read(mem, addr, 4)
	if e != nil {
		return 0, e
	}
	return binary.LittleEndian.Uint32(b), nil
}
func put(mem Memory, addr uint32, b []byte) error {
	if err := mem.Write(addr, b); err != nil {
		return err
	}
	chk, err := read(mem, addr, len(b))
	if err != nil {
		return err
	}
	if !bytes.Equal(chk, b) {
		return fmt.Errorf("tracking read-back failed at 0x%08x", addr)
	}
	return nil
}
func putU(mem Memory, addr, value uint32) error {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, value)
	return put(mem, addr, b)
}
func validPtr(p uint32) bool { return p >= 0x10000 && p < 0x7fe00000 && p%4 == 0 }
func getDescriptor(mem Memory, player uint32, expectedGUID [8]byte) (uint32, error) {
	d, err := getU(mem, player+8)
	if err != nil || !validPtr(d) {
		return 0, errors.New("tracking update-field descriptor unavailable")
	}
	g, err := read(mem, d, 8)
	if err != nil || !bytes.Equal(g, expectedGUID[:]) {
		return 0, errors.New("tracking descriptor GUID mismatch")
	}
	hp, err := getU(mem, d+0x60)
	if err != nil {
		return 0, err
	}
	maxHP, err := getU(mem, d+0x80)
	if err != nil || maxHP == 0 || maxHP > 1000000000 || hp > maxHP {
		return 0, errors.New("invalid tracking descriptor health")
	}
	return d, nil
}
func (e *Engine) validate() error {
	cur, err := movement.Snapshot(e.mem)
	if err != nil {
		return err
	}
	if cur.Player != e.identity.Player || cur.GUID != e.identity.GUID {
		return errors.New("character changed; tracking session invalidated")
	}
	if cur.MapID != e.identity.MapID {
		return errors.New("map changed; tracking session must be refreshed")
	}
	desc, err := getDescriptor(e.mem, cur.Player, cur.GUID)
	if err != nil {
		return err
	}
	if desc != e.descriptor {
		return errors.New("player update fields changed; tracking session invalidated")
	}
	return nil
}
func (e *Engine) Enabled(id string) bool { return e.enabled[id] }
func (e *Engine) AnyEnabled() bool {
	for _, x := range e.enabled {
		if x {
			return true
		}
	}
	return false
}
func (e *Engine) Set(id string, on bool) error {
	o, exists := optionByID[id]
	if !exists {
		return fmt.Errorf("unknown tracking control %s", id)
	}
	if err := e.validate(); err != nil {
		return err
	}
	if e.enabled[id] == on {
		return nil
	}
	old := e.enabled[id]
	e.enabled[id] = on
	var err error
	if o.Family == Services {
		err = e.applyServices()
	} else {
		err = e.applyGroup(int(o.Family))
	}
	if err != nil {
		e.enabled[id] = old
		// Best-effort rollback to previously accepted state. Caller keeps old UI.
		if o.Family == Services {
			// A partial bridge install must never remain enabled following a
			// failed toggle; immediately remove any code patches we own if
			// no optional service is currently requested.
			_ = e.applyNPCServices()
			if e.specialFlags() == 0 && e.hookMask != 0 {
				_ = e.restoreSpecial()
			} else if e.specialFlags() != 0 {
				_ = e.applySpecial()
			}
		} else {
			_ = e.applyGroup(int(o.Family))
		}
		return fmt.Errorf("tracking %s: %w", id, err)
	}
	return nil
}
func (e *Engine) Tick() error {
	if !e.AnyEnabled() && e.hookMask == 0 && !e.nativeSelected {
		return nil
	}
	if err := e.validate(); err != nil {
		return err
	}
	for group := 0; group < 2; group++ {
		if err := e.applyGroup(group); err != nil {
			return err
		}
	}
	return e.applyServices()
}
func (e *Engine) mask(group int) (uint32, bool) {
	var m uint32
	has := false
	for _, o := range options {
		if int(o.Family) == group && e.enabled[o.ID] {
			has = true
			m |= o.Bit
		}
	}
	return m, has
}
func (e *Engine) applyGroup(group int) error {
	var off uint32 = resourceOffset
	if group == 1 {
		off = creatureOffset
	}
	state := &e.groups[group]
	live, err := getU(e.mem, e.descriptor+off)
	if err != nil {
		return err
	}
	target, active := e.mask(group)
	if !active {
		if !state.managed {
			return nil
		}
		changed := state.original ^ state.last
		restored := (live &^ changed) | (state.original & changed)
		if restored != live {
			if err := putU(e.mem, e.descriptor+off, restored); err != nil {
				return err
			}
		}
		state.managed = false
		return nil
	}
	if !state.managed {
		state.original = live
		state.last = live
	}
	if live != target {
		if err := putU(e.mem, e.descriptor+off, target); err != nil {
			return err
		}
	}
	state.last = target
	state.managed = true
	return nil
}

var repairSignature = []byte{0xa1, 0x64, 0xba, 0xbe, 0x00, 0x85, 0xc0, 0x74, 0x1c, 0x83, 0x38, 0x01, 0x75, 0x17, 0x8b}
var originalFive = []byte{0xa1, 0x64, 0xba, 0xbe, 0x00}
var mailboxTail = []byte{0x85, 0xc0, 0x74, 0x1a, 0x83, 0x38, 0x02, 0x75, 0x15, 0x8b, 0x89}
var questTail = []byte{0x85, 0xc0, 0x74, 0x0b, 0x33, 0xc9, 0x83, 0x38, 0x03, 0x0f, 0x94}

func (e *Engine) verifiedRenderer() error {
	sig, err := read(e.mem, npcPredicate, 15)
	if err != nil {
		return err
	}
	if !bytes.Equal(sig, repairSignature) {
		return errors.New("unsupported native minimap renderer signature")
	}
	raw, err := read(e.mem, gameRepairDefinition, 20)
	if err != nil {
		return err
	}
	if binary.LittleEndian.Uint32(raw[:4]) != 1 || binary.LittleEndian.Uint32(raw[4:8]) != 0x1000 || binary.LittleEndian.Uint32(raw[8:12]) != 0x00A11C1C || binary.LittleEndian.Uint32(raw[12:16]) != 0x00A11C14 {
		return errors.New("native service descriptor signature mismatch")
	}
	return nil
}
func (e *Engine) serviceMask() uint32 {
	var mask uint32
	for _, o := range options {
		if o.Family == Services && e.enabled[o.ID] {
			mask |= o.Bit
		}
	}
	return mask
}
func (e *Engine) specialFlags() uint32 {
	var flags uint32
	if e.enabled["mailbox"] {
		flags |= 1
	}
	if e.enabled["lowquests"] {
		flags |= 2
	}
	return flags
}
func (e *Engine) applyServices() error {
	if err := e.applyNPCServices(); err != nil {
		return err
	}
	return e.applySpecial()
}
func (e *Engine) applyNPCServices() error {
	mask := e.serviceMask()
	if mask == 0 {
		return e.restoreNPCServices()
	}
	if err := e.verifiedRenderer(); err != nil {
		return err
	}
	if e.nativeDesc == 0 {
		addr, err := e.mem.Alloc(4096, 4)
		if err != nil {
			return fmt.Errorf("allocate native service descriptor: %w", err)
		}
		if !validPtr(addr) {
			return errors.New("invalid allocated descriptor address")
		}
		e.nativeDesc = addr
	}
	def, err := read(e.mem, gameRepairDefinition, 20)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(def[4:8], mask)
	if err = put(e.mem, e.nativeDesc, def); err != nil {
		return err
	}
	current, err := getU(e.mem, gameTrackPointer)
	if err != nil {
		return err
	}
	if current != e.nativeDesc {
		e.originalNative = current // capture most recent in-game tracking selection
		if err = putU(e.mem, gameTrackPointer, e.nativeDesc); err != nil {
			return err
		}
	}
	check, err := getU(e.mem, gameTrackPointer)
	if err != nil {
		return err
	}
	if check != e.nativeDesc {
		return errors.New("native service selection was not retained")
	}
	e.nativeSelected = true
	return nil
}
func (e *Engine) restoreNPCServices() error {
	if !e.nativeSelected {
		return nil
	}
	current, err := getU(e.mem, gameTrackPointer)
	if err != nil {
		return err
	}
	if current == e.nativeDesc {
		if err = putU(e.mem, gameTrackPointer, e.originalNative); err != nil {
			return err
		}
	}
	e.nativeSelected = false
	return nil
}

func (e *Engine) confirmPredicate(addr uint32, tail []byte) error {
	b, err := read(e.mem, addr, 16)
	if err != nil {
		return err
	}
	if !bytes.Equal(b[:5], originalFive) || !bytes.Equal(b[5:], tail) {
		return fmt.Errorf("native function signature mismatch at 0x%08x", addr)
	}
	return nil
}
func jump(from, to uint32) []byte {
	b := make([]byte, 5)
	b[0] = 0xe9
	binary.LittleEndian.PutUint32(b[1:], to-(from+5))
	return b
}

// Native bridge machine code follows the verified 3.3.5a renderer contracts.
// It calls Blizzard's original predicate/refresher on the game's own thread.
// The caller verifies BOTH target signatures before changing either one.
func makeStubs(page uint32) ([]byte, []byte) {
	mail := []byte{
		0xf6, 0x05, 0, 0, 0, 0, 0x01, 0x74, 0x12,
		0x8b, 0x81, 0xd0, 0, 0, 0, 0x80, 0x78, 0x2d, 0x13, 0x75, 0x06,
		0xb8, 0x01, 0, 0, 0, 0xc3,
		0xa1, 0x64, 0xba, 0xbe, 0x00, 0xe9, 0, 0, 0, 0,
	}
	quest := []byte{
		0xa0, 0x44, 0x33, 0x22, 0x11, 0x24, 0x02, 0x3a, 0x05, 0x48, 0x33, 0x22,
		0x11, 0x74, 0x20, 0xa2, 0x48, 0x33, 0x22, 0x11, 0x9c, 0x60, 0x6a, 0x00,
		0x68, 0x20, 0xe0, 0x57, 0x00, 0xb8, 0x30, 0x4b, 0x4d, 0x00, 0xff, 0xd0,
		0x83, 0xc4, 0x08, 0x61, 0x9d, 0xff, 0x05, 0x4c, 0x33, 0x22, 0x11, 0xf6,
		0x05, 0x44, 0x33, 0x22, 0x11, 0x02, 0x74, 0x06, 0xb8, 0x01, 0x00, 0x00,
		0x00, 0xc3, 0xa1, 0x64, 0xba, 0xbe, 0x00, 0xe9, 0x00, 0x00, 0x00, 0x00,
	}
	binary.LittleEndian.PutUint32(mail[2:], page)
	binary.LittleEndian.PutUint32(mail[33:], mailboxPredicate+5-(page+mailboxStubOffset+uint32(len(mail))))
	binary.LittleEndian.PutUint32(quest[1:], page)
	binary.LittleEndian.PutUint32(quest[9:], page+4)
	binary.LittleEndian.PutUint32(quest[16:], page+4)
	binary.LittleEndian.PutUint32(quest[43:], page+8)
	binary.LittleEndian.PutUint32(quest[49:], page)
	binary.LittleEndian.PutUint32(quest[68:], questPredicate+5-(page+questStubOffset+uint32(len(quest))))
	return mail, quest
}
func (e *Engine) applySpecial() error {
	flags := e.specialFlags()
	if flags == 0 && e.hookMask == 0 {
		return nil
	}
	if e.hookPage == 0 {
		if err := e.verifiedRenderer(); err != nil {
			return err
		}
		if err := e.confirmPredicate(mailboxPredicate, mailboxTail); err != nil {
			return err
		}
		if err := e.confirmPredicate(questPredicate, questTail); err != nil {
			return err
		}
		p, err := e.mem.Alloc(4096, 0x40)
		if err != nil {
			return fmt.Errorf("allocate special native tracker bridge: %w", err)
		}
		if !validPtr(p) {
			return errors.New("invalid executable bridge address")
		}
		mail, quest := makeStubs(p)
		if len(mail) != 37 || len(quest) != 72 {
			return errors.New("invalid native stub lengths")
		}
		if err = put(e.mem, p, make([]byte, 12)); err != nil {
			return err
		}
		if err = put(e.mem, p+mailboxStubOffset, mail); err != nil {
			return err
		}
		if err = put(e.mem, p+questStubOffset, quest); err != nil {
			return err
		}
		if err = e.mem.FlushCode(p+mailboxStubOffset, uint32(len(mail))); err != nil {
			return err
		}
		if err = e.mem.FlushCode(p+questStubOffset, uint32(len(quest))); err != nil {
			return err
		}
		e.hookPage = p
	}
	if e.hookMask&1 == 0 {
		if err := e.confirmPredicate(mailboxPredicate, mailboxTail); err != nil {
			return err
		}
		patch := jump(mailboxPredicate, e.hookPage+mailboxStubOffset)
		if err := e.mem.PatchCode(mailboxPredicate, patch); err != nil {
			return err
		}
		e.hookMask |= 1
	}
	if e.hookMask&2 == 0 {
		if err := e.confirmPredicate(questPredicate, questTail); err != nil {
			return err
		}
		patch := jump(questPredicate, e.hookPage+questStubOffset)
		if err := e.mem.PatchCode(questPredicate, patch); err != nil {
			return err
		}
		e.hookMask |= 2
	}
	previous, err := getU(e.mem, e.hookPage)
	if err != nil {
		return err
	}
	// The refresher increments hookPage+8 on the game's own thread. Record the
	// counter BEFORE writing OFF, so a delayed completion can be observed.
	var offCount uint32
	if previous&2 != 0 && flags&2 == 0 {
		offCount, err = getU(e.mem, e.hookPage+8)
		if err != nil {
			return err
		}
	}
	if err := putU(e.mem, e.hookPage, flags); err != nil {
		return err
	}
	if flags&2 != 0 {
		e.questOffPending = false
	} else if previous&2 != 0 {
		e.questOffPending = true
		e.questOffCount = offCount
	}
	return nil
}

// QuestOFFPending returns true while WoW has yet to acknowledge the request
// to clear its native overhead questgiver markers. Do not retire a bridge
// whose OFF transition still needs processing. The native +4 word is the
// game-thread refresh bit; +8 increments only after the engine's refresh call.
func (e *Engine) QuestOFFPending() (bool, error) {
	if e == nil || !e.questOffPending || e.hookPage == 0 {
		return false, nil
	}
	count, err := getU(e.mem, e.hookPage+8)
	if err != nil {
		return true, err
	}
	lastBit, err := getU(e.mem, e.hookPage+4)
	if err != nil {
		return true, err
	}
	if count != e.questOffCount && lastBit&2 == 0 {
		e.questOffPending = false
	}
	return e.questOffPending, nil
}
func (e *Engine) restoreSpecial() error {
	if e.hookPage == 0 {
		return nil
	}
	// First disable the two optional paths, then unhook ONLY our own jumps.
	var first error
	if err := putU(e.mem, e.hookPage, 0); err != nil {
		first = err
	}
	for _, entry := range []struct {
		bit       uint8
		site, off uint32
	}{{2, questPredicate, questStubOffset}, {1, mailboxPredicate, mailboxStubOffset}} {
		if e.hookMask&entry.bit == 0 {
			continue
		}
		current, err := read(e.mem, entry.site, 5)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if bytes.Equal(current, jump(entry.site, e.hookPage+entry.off)) {
			if err = e.mem.PatchCode(entry.site, originalFive); err != nil {
				if first == nil {
					first = err
				}
				continue
			}
		} else if !bytes.Equal(current, originalFive) {
			if first == nil {
				first = fmt.Errorf("native tracking patch at 0x%08x modified elsewhere", entry.site)
			}
			continue
		}
		e.hookMask &^= entry.bit
	}
	if e.hookMask == 0 {
		e.questOffPending = false
		e.hookPage = 0
	} // never remotely free this page
	return first
}

// Close releases only effects owned by this session. No writes to another
// player's update fields after logout, character switch, or map transition.
func (e *Engine) Close() error {
	if e == nil {
		return nil
	}
	var first error
	// Code hooks are in executable memory, not player update fields: remove
	// them regardless of whether the character has changed (same live process).
	if err := e.restoreSpecial(); err != nil {
		first = err
	}
	if err := e.restoreNPCServices(); err != nil && first == nil {
		first = err
	}
	if e.validate() == nil {
		for group := 0; group < 2; group++ {
			for _, o := range options {
				if int(o.Family) == group {
					e.enabled[o.ID] = false
				}
			}
			if err := e.applyGroup(group); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// RefreshMap retains native tracking masks and hooks when MapID changes but
// both the loaded player and update-field descriptor are still identical.
// If the descriptor has changed, the caller must Close and create a new Engine;
// stale descriptor fields must never be written.
func (e *Engine) RefreshMap(c movement.Character) error {
	if c.Player != e.identity.Player || c.GUID != e.identity.GUID {
		return errors.New("different character cannot inherit native tracking")
	}
	newDescriptor, err := getDescriptor(e.mem, c.Player, c.GUID)
	if err != nil {
		return err
	}
	if newDescriptor != e.descriptor {
		return errors.New("tracking descriptor changed; fresh controller required")
	}
	e.identity.MapID = c.MapID
	return nil
}
