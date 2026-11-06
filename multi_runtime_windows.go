//go:build windows

package main

// Phase 3: one reversible controller per verified WoW PID, with ONE shared
// UI settings configuration. SOLO restores all effects from non-selected
// clients. ALL applies the same UI configuration independently to each client.
// The accepted the earlier development build rendering / widget positioning is untouched.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/zugbonk/frosty/internal/clientstate"
	"github.com/zugbonk/frosty/internal/movement"
	"github.com/zugbonk/frosty/internal/tracking"
)

const mmMaxSessions = clientstate.MaxClients

var mmMovementIDs = []string{"run", "fly", "swim", "water", "wall", "thru", "airjump"}
var mmSpeedIDs = []string{"run", "fly", "swim"}

type mmSession struct {
	pid       uint32
	proc      *mvProc
	move      *movement.Engine
	track     *tracking.Engine
	name      string
	basename  string
	loaded    bool
	observed  clientstate.StableObserved
	lastSpeed map[movement.Mode]string
	lastError string
}
type mmRuntime struct {
	sessions   map[uint32]*mmSession
	manual     bool
	lastStatus string
}

var mm mmRuntime

func mmInit() {
	if mm.sessions == nil {
		mm.sessions = map[uint32]*mmSession{}
	}
}
func mmScope() clientstate.Scope {
	return clientstate.Scope{Selected: app.selectedPID, All: app.allClients}
}
func mmIsTarget(pid uint32) bool { return mmScope().Wants(pid) }
func mmPIDs() []uint32 {
	pids := make([]uint32, 0, len(mm.sessions))
	for pid := range mm.sessions {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	return pids
}
func mmStatus(s *mmSession, err error) {
	if s == nil || err == nil {
		return
	}
	msg := fmt.Sprintf("WoW PID %d: %v", s.pid, err)
	if s.lastError != msg {
		s.lastError = msg
		app.status = msg
		mainRedraw()
	}
}
func mmCurrent(s *mmSession) (movement.Character, error) {
	if s == nil || s.proc == nil {
		return movement.Character{}, errors.New("no WoW process")
	}
	return movement.Snapshot(s.proc)
}

// A process name is NOT a character name. Check the live GUID independently
// before using WoW's bounded 12340 name-cache chain; otherwise show the PID.
func mmReadU(p *mvProc, addr uint32) (uint32, bool) {
	var b [4]byte
	if p.Read(addr, b[:]) != nil {
		return 0, false
	}
	return binary.LittleEndian.Uint32(b[:]), true
}
func mmReadNameText(buf []byte) string {
	end := bytes.IndexByte(buf, 0)
	if end < 2 || end >= len(buf) {
		return ""
	}
	name := string(buf[:end])
	if !utf8.ValidString(name) {
		return ""
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return ""
		}
	}
	return name
}
func mmCharacterName(p *mvProc, c movement.Character) string {
	if p == nil || c.Player == 0 {
		return ""
	}
	fields, ok := mmReadU(p, c.Player+8)
	if !ok || fields < 0x10000 || fields >= 0x7ff00000 || fields%4 != 0 {
		return ""
	}
	var check [8]byte
	if p.Read(fields, check[:]) != nil || check != c.GUID {
		return ""
	}
	// GUID-validated local-player static record: exact source/reference from
	// the readable legacy previous production application name subsystem.
	var direct [8]byte
	if p.Read(0x00C27D80, direct[:]) == nil && direct == c.GUID {
		var name [40]byte
		if p.Read(0x00C27D88, name[:]) == nil {
			if v := mmReadNameText(name[:]); v != "" {
				return v
			}
		}
	}
	// Name cache fallback (not a heap scan): verify local object GUID first,
	// then follow at most 32 links with pointer/overflow/cycle checks.
	mask, ok := mmReadU(p, 0x00C5D964)
	if !ok || mask > 0xffff {
		return ""
	}
	base, ok := mmReadU(p, 0x00C5D95C)
	if !ok || base < 0x10000 || base >= 0x7ff00000 {
		return ""
	}
	slot := mask & binary.LittleEndian.Uint32(c.GUID[:4])
	if slot > 0x10000 || base+12*slot < base || base+12*slot > 0x7ff00000-12 {
		return ""
	}
	step, ok := mmReadU(p, base+12*slot)
	if !ok || step > 0x1000000 {
		return ""
	}
	cur, ok := mmReadU(p, base+12*slot+8)
	if !ok {
		return ""
	}
	previous := uint32(0)
	for i := 0; i < 32; i++ {
		if cur < 0x10000 || cur > 0x7ff00000-48 || cur&1 != 0 || cur == previous {
			return ""
		}
		low, ok := mmReadU(p, cur)
		if !ok {
			return ""
		}
		if low == binary.LittleEndian.Uint32(c.GUID[:4]) {
			var text [40]byte
			if p.Read(cur+0x20, text[:]) == nil {
				return mmReadNameText(text[:])
			}
			return ""
		}
		if cur+step+4 < cur || cur+step+4 >= 0x7ff00000 {
			return ""
		}
		previous = cur
		cur, ok = mmReadU(p, cur+step+4)
		if !ok {
			return ""
		}
	}
	return ""
}
func mmDisplayName(s *mmSession) string {
	if s.name != "" {
		return s.name
	}
	if s.basename != "" {
		return s.basename
	}
	return fmt.Sprintf("WoW PID %d", s.pid)
}
func mmUpdateChoices() {
	choices := make([]clientChoice, 0, len(mm.sessions))
	for _, pid := range mmPIDs() {
		s := mm.sessions[pid]
		choices = append(choices, clientChoice{PID: pid, Name: mmDisplayName(s)})
	}
	app.clients = choices
}
func mmEnsure(pid uint32, basename string) (*mmSession, error) {
	mmInit()
	if existing := mm.sessions[pid]; existing != nil {
		return existing, nil
	}
	if len(mm.sessions) >= mmMaxSessions {
		return nil, errors.New("too many WoW clients (limit 12)")
	}
	p, err := mvOpenVerified(pid)
	if err != nil {
		return nil, err
	}
	s := &mmSession{pid: pid, proc: p, basename: basename, lastSpeed: make(map[movement.Mode]string)}
	mm.sessions[pid] = s
	return s, nil
}
func mmStopEffects(s *mmSession) error {
	// Stop native hooks FIRST, then movement code patches. Never close a
	// live process handle until owned executable patches are restored.
	if s.track != nil {
		// Scope changes are equivalent to switching every tracker OFF.
		// Do not unhook the low-quest bridge before the game-thread overhead
		// refresh has consumed the OFF transition (previous reference implementation behavior).
		if !s.loaded {
			// On logout/loading the old descriptor cannot safely receive ON/OFF
			// field writes. Preserve earlier development build's safe immediate hook cleanup;
			// the game recreates overhead markers when the character loads.
			if err := s.track.Close(); err != nil {
				return fmt.Errorf("unloaded native tracking restoration: %w", err)
			}
			s.track = nil
		} else {
			for _, id := range trackingIDs {
				if s.track.Enabled(id) {
					if err := s.track.Set(id, false); err != nil {
						// An in-flight character replacement can invalidate the
						// descriptor between the loaded check and Set(). Safely
						// retire the old owned patches, as earlier development build did.
						if clean := s.track.Close(); clean != nil {
							return fmt.Errorf("tracking scope %v; restoration %w", err, clean)
						}
						s.track = nil
						break
					}
				}
			}
			if err := mmFinishQuestOFF(s); err != nil {
				return err
			}
		}
	}
	if s.move != nil {
		if err := s.move.Retire(); err != nil {
			return fmt.Errorf("movement restoration: %w", err)
		}
		s.move = nil
	}
	s.lastSpeed = map[movement.Mode]string{}
	s.loaded = false
	s.observed = clientstate.StableObserved{}
	return nil
}

// A deselected client may still need one native questgiver refresh on WoW's
// own renderer thread. All visible tracking masks are already OFF; only the
// reversible pass-through bridge survives until its completion counter moves.
func mmFinishQuestOFF(s *mmSession) error {
	if s.track == nil || s.track.AnyEnabled() {
		return nil
	}
	pending, err := s.track.QuestOFFPending()
	if err != nil {
		return fmt.Errorf("native quest overhead refresh: %w", err)
	}
	if pending {
		return nil
	}
	if err := s.track.Close(); err != nil {
		return fmt.Errorf("native tracking restoration: %w", err)
	}
	s.track = nil
	return nil
}
func mmRetireChanged(s *mmSession) error {
	if s.track != nil {
		if err := s.track.Close(); err != nil {
			return fmt.Errorf("old tracking hooks: %w", err)
		}
		s.track = nil
	}
	if s.move != nil {
		if err := s.move.Retire(); err != nil {
			return fmt.Errorf("old movement patches: %w", err)
		}
		s.move = nil
	}
	s.lastSpeed = map[movement.Mode]string{}
	s.observed = clientstate.StableObserved{}
	return nil
}

// Refresh MapID without invalidating player-snapshot ownership. If the
// descriptor changed, retire only the old tracking engine before a new one.
func mmRefreshMap(s *mmSession, c movement.Character) error {
	if s.move != nil && s.move.Identity().MapID != c.MapID {
		if err := s.move.RefreshMap(c); err != nil {
			return err
		}
	}
	if s.track != nil {
		if err := s.track.RefreshMap(c); err != nil {
			if stop := s.track.Close(); stop != nil {
				return fmt.Errorf("tracking refresh %v; cleanup %w", err, stop)
			}
			s.track = nil
		}
	}
	return nil
}
func mmApply(s *mmSession) error {
	if s.move == nil {
		return nil
	}
	e := s.move
	// Order matters: Fly must be present before Fly Thru Textures is applied;
	// turn OFF texture patches before turning Fly OFF.
	if !app.toggles["thru"] && e.Enabled(movement.Thru) {
		if err := e.SetThru(false); err != nil {
			return err
		}
	}
	if !app.toggles["airjump"] && e.Enabled(movement.AirJump) {
		if err := e.SetAirJump(false); err != nil {
			return err
		}
	}
	if !app.toggles["wall"] && e.Enabled(movement.Wall) {
		if err := e.SetWall(false); err != nil {
			return err
		}
	}
	if !app.toggles["water"] && e.Enabled(movement.Water) {
		if err := e.SetWater(false); err != nil {
			return err
		}
	}
	for _, id := range mmSpeedIDs {
		mode := movement.Mode(id)
		if !app.toggles[id] && e.Enabled(mode) {
			if err := e.StopSpeed(mode); err != nil {
				return err
			}
			delete(s.lastSpeed, mode)
		}
	}
	for _, id := range mmSpeedIDs {
		if !app.toggles[id] {
			continue
		}
		mode := movement.Mode(id)
		value := app.text[id+"_speed"]
		if !e.Enabled(mode) || s.lastSpeed[mode] != value {
			if err := e.SetSpeed(mode, value, true); err != nil {
				return err
			}
			s.lastSpeed[mode] = value
		}
	}
	if app.toggles["water"] && !e.Enabled(movement.Water) {
		if err := e.SetWater(true); err != nil {
			return err
		}
	}
	if app.toggles["wall"] && !e.Enabled(movement.Wall) {
		if err := e.SetWall(true); err != nil {
			return err
		}
	}
	if app.toggles["airjump"] && !e.Enabled(movement.AirJump) {
		if err := e.SetAirJump(true); err != nil {
			return err
		}
	}
	if app.toggles["thru"] && !e.Enabled(movement.Thru) {
		if err := e.SetThru(true); err != nil {
			return err
		}
	}
	// Three independent tracking families. Keep a native engine only while
	// some tracking setting is desired; do not allocate executable memory
	// in idle clients.
	wantsTracking := false
	for _, id := range trackingIDs {
		if app.toggles[id] {
			wantsTracking = true
			break
		}
	}
	if wantsTracking && s.track == nil {
		tr, err := tracking.New(trackingMemory{s.proc})
		if err != nil {
			return err
		}
		s.track = tr
	}
	if s.track != nil {
		// Turn off unwanted options before enabling others, preserving
		// the original game's tracking selection when everything turns OFF.
		for _, id := range trackingIDs {
			if !app.toggles[id] && s.track.Enabled(id) {
				if err := s.track.Set(id, false); err != nil {
					return err
				}
			}
		}
		for _, id := range trackingIDs {
			if app.toggles[id] && !s.track.Enabled(id) {
				if err := s.track.Set(id, true); err != nil {
					return err
				}
			}
		}
		if !wantsTracking {
			// In previous reference implementation, the native overhead quest-refresh bridge stayed
			// installed after OFF. The earlier development build manager retired it too soon:
			// the minimap was already reset, but WoW could retain the cached
			// questgiver overhead icons. Keep this single process-bound engine
			// only until the game's own callback acknowledges the OFF refresh.
			pending, err := s.track.QuestOFFPending()
			if err != nil {
				return fmt.Errorf("quest overhead refresh pending: %w", err)
			}
			if !pending {
				if err := s.track.Close(); err != nil {
					return err
				}
				s.track = nil
			}
		}
	}
	return nil
}
func mmPulse(s *mmSession) {
	if s.proc == nil || mvProcessExited(s.proc) {
		return
	}
	current, err := mmCurrent(s)
	if err != nil {
		// Loading screens are normal. Keep desired values, but show the
		// distinction between requested ON state and actual live effect.
		s.loaded = false
		s.observed = clientstate.StableObserved{}
		if mmIsTarget(s.pid) {
			mmStatus(s, fmt.Errorf("character unavailable; settings pending (%w)", err))
		}
		return
	}
	if s.move != nil {
		old := s.move.Identity()
		if old.Player != current.Player || old.GUID != current.GUID {
			if err := mmRetireChanged(s); err != nil {
				mmStatus(s, err)
				return
			}
		} else if err := mmRefreshMap(s, current); err != nil {
			mmStatus(s, err)
			return
		}
	}
	stable := s.observed.Observe(s.pid, current.Player, current.GUID, current.MapID, true)
	if !stable {
		s.loaded = false
		return
	}
	if s.move == nil {
		e, err := movement.New(s.proc, s.pid)
		if err != nil {
			mmStatus(s, err)
			return
		}
		s.move = e
	}
	s.loaded = true
	if !mmIsTarget(s.pid) {
		return
	}
	if err := mmApply(s); err != nil {
		mmStatus(s, err)
		return
	}
	if err := s.move.Tick(); err != nil {
		mmStatus(s, err)
		return
	}
	if s.track != nil {
		if err := s.track.Tick(); err != nil {
			// Descriptor changed while MapID stayed the same: retire just
			// tracking, keeping desired UI switches for the next loaded tick.
			if stop := s.track.Close(); stop != nil {
				mmStatus(s, fmt.Errorf("tracking tick %v; restoration %w", err, stop))
				return
			}
			s.track = nil
			mmStatus(s, fmt.Errorf("tracking reattachment pending: %w", err))
			return
		}
	}
	if s.lastError != "" {
		s.lastError = ""
		app.status = fmt.Sprintf("WoW PID %d: loaded and settings restored", s.pid)
		mainRedraw()
	}
}
func movementAutoDiscover() {
	if app.main == 0 {
		return
	}
	mmInit()
	oldChoices := append([]clientChoice(nil), app.clients...)
	oldSelection := app.selectedPID
	oldStatus := app.status
	discovered := mvDiscover()
	// Reap confirmed exited PIDs. If discovery temporarily misses a still-
	// running process, retain its handle and ownership until it is known dead.
	for pid, s := range mm.sessions {
		if mvProcessExited(s.proc) {
			s.proc.Close()
			delete(mm.sessions, pid)
			if app.selectedPID == pid {
				app.selectedPID = 0
				mm.manual = false
			}
		}
	}
	for _, item := range discovered {
		if _, err := mmEnsure(item.PID, item.Name); err != nil {
			if app.status != err.Error() {
				app.status = err.Error()
				mainRedraw()
			}
		}
	}
	if app.selectedPID == 0 && len(mm.sessions) != 0 {
		// Prefer a loaded character; never pretend a loading character is
		// already attached. Deterministic low PID fallback while loading.
		pids := mmPIDs()
		app.selectedPID = pids[0]
		for _, pid := range pids {
			if c, err := mmCurrent(mm.sessions[pid]); err == nil && c.Player != 0 {
				app.selectedPID = pid
				break
			}
		}
	}
	for _, pid := range mmPIDs() {
		s := mm.sessions[pid]
		if mmIsTarget(pid) {
			mmPulse(s)
		}
		// Names in the selector are read for ALL clients, not just the current
		// scope. This path never applies gameplay effects to an unselected PID.
		if current, err := mmCurrent(s); err == nil {
			s.name = mmCharacterName(s.proc, current)
		} else {
			s.name = ""
		}
	}
	mmUpdateChoices()
	tpSyncListMap()
	if app.main != 0 && len(mm.sessions) > 0 {
		mvSetTimer.Call(app.main, mvTimerID, 250, 0)
	}
	if app.main != 0 && len(mm.sessions) == 0 {
		mvKillTimer.Call(app.main, mvTimerID)
	}
	if app.toggles["wall"] && app.main != 0 {
		mvSetTimer.Call(app.main, mvWallHoldTimerID, 40, 0)
	}
	if len(mm.sessions) == 1 && !mm.manual {
		if s := mm.sessions[app.selectedPID]; s != nil && s.loaded && mm.lastStatus != "attached" {
			mm.lastStatus = "attached"
			app.status = fmt.Sprintf("Automatically attached to %s (PID %d)", mmDisplayName(s), s.pid)
		}
	}
	changed := len(oldChoices) != len(app.clients) || oldSelection != app.selectedPID || oldStatus != app.status
	if !changed {
		for i := range oldChoices {
			if oldChoices[i] != app.clients[i] {
				changed = true
				break
			}
		}
	}
	if changed {
		mainRedraw()
	}
}
func movementTick() {
	mmInit()
	for _, pid := range mmPIDs() {
		s := mm.sessions[pid]
		if mmIsTarget(pid) {
			mmPulse(s)
		} else if s.track != nil && !s.track.AnyEnabled() {
			// Complete a deselected client's outstanding quest OFF callback.
			// This NEVER reapplies movement/tracking to an unselected client.
			if err := mmFinishQuestOFF(s); err != nil {
				mmStatus(s, err)
			}
		}
	}
	// Corpse monitoring is independent of selected SOLO/ALL scope.
	tpDeathTick()
	tpSyncListMap()
}
func trackingTick() { movementTick() } // dormant compatibility timer, never installed
func mmStopOthers(keep uint32) error {
	for _, pid := range mmPIDs() {
		if pid == keep {
			continue
		}
		s := mm.sessions[pid]
		if mvProcessExited(s.proc) {
			continue
		}
		if err := mmStopEffects(s); err != nil {
			return fmt.Errorf("PID %d: %w", pid, err)
		}
	}
	return nil
}

// Called by the EXISTING Game Selector (no new UI). One shared global config
// is applied to the selected PID; previously affected PIDs are restored.
func movementAttach(pid uint32) error {
	mmInit()
	if pid == 0 {
		return errors.New("no selected WoW PID")
	}
	s := mm.sessions[pid]
	if s == nil {
		var err error
		s, err = mmEnsure(pid, "")
		if err != nil {
			return err
		}
	}
	if err := mmStopOthers(pid); err != nil {
		return fmt.Errorf("cannot switch SOLO until other clients are restored: %w", err)
	}
	app.selectedPID = pid
	app.allClients = false
	mm.manual = true
	mmPulse(s)
	mmUpdateChoices()
	if !s.loaded {
		return fmt.Errorf("PID %d selected; character loading (settings will apply when ready)", pid)
	}
	return nil
}
func mmToggleAll() error {
	mmInit()
	if app.allClients {
		pid := app.selectedPID
		if pid == 0 {
			pids := mmPIDs()
			if len(pids) > 0 {
				pid = pids[0]
			}
		}
		if pid == 0 {
			return errors.New("no WoW client available")
		}
		return movementAttach(pid)
	}
	// Complete discovery immediately before entering ALL. Never silently
	// control only a subset when too many clients are available.
	found := mvDiscover()
	if len(found) == 0 {
		return errors.New("no supported WoW clients running")
	}
	if len(found) > mmMaxSessions {
		return errors.New("Control All supports at most 12 active WoW clients")
	}
	for _, choice := range found {
		if _, err := mmEnsure(choice.PID, choice.Name); err != nil {
			return err
		}
	}
	app.allClients = true
	if app.selectedPID == 0 {
		app.selectedPID = found[0].PID
	}
	movementTick()
	mmUpdateChoices()
	return nil
}
func mmApplyToTargets() error {
	var first error
	for _, pid := range mmPIDs() {
		if !mmIsTarget(pid) {
			continue
		}
		s := mm.sessions[pid]
		mmPulse(s)
		if s.lastError != "" && first == nil {
			first = errors.New(s.lastError)
		}
	}
	return first
}
func movementClick(id string) error {
	known := false
	for _, m := range mmMovementIDs {
		if m == id {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("unknown movement control %s", id)
	}
	if id == "run" || id == "fly" || id == "swim" {
		if !app.toggles[id] {
			if _, err := movement.Target(movement.Mode(id), app.text[id+"_speed"]); err != nil {
				return err
			}
		}
	}
	on := !app.toggles[id]
	app.toggles[id] = on
	if id == "thru" {
		if on {
			app.toggles["fly"] = true
		} else {
			app.toggles["fly"] = false
		}
	}
	if id == "fly" && !on {
		app.toggles["thru"] = false
	}
	if app.toggles["fly"] {
		if _, err := movement.Target(movement.Fly, app.text["fly_speed"]); err != nil {
			return err
		}
	}
	if app.main != 0 {
		if app.toggles["wall"] {
			mvSetTimer.Call(app.main, mvWallHoldTimerID, 40, 0)
		} else {
			mvKillTimer.Call(app.main, mvWallHoldTimerID)
		}
	}
	return mmApplyToTargets()
}
func trackingClick(id string) error {
	if !tracking.Known(id) {
		return fmt.Errorf("unknown tracker %s", id)
	}
	app.toggles[id] = !app.toggles[id]
	return mmApplyToTargets()
}
func movementMultiplierChanged(field string) {
	id := strings.TrimSuffix(field, "_speed")
	if id == field || !app.toggles[id] {
		return
	}
	if _, err := movement.Target(movement.Mode(id), app.text[field]); err != nil {
		app.status = "Speed multiplier: " + err.Error()
		return
	}
	if err := mmApplyToTargets(); err != nil {
		app.status = "Speed multiplier: " + err.Error()
	} else {
		app.status = fmt.Sprintf("Updated %s multiplier to x%s", id, app.text[field])
	}
}
func movementWallHoldTick() {
	if !app.toggles["wall"] {
		if app.main != 0 {
			mvKillTimer.Call(app.main, mvWallHoldTimerID)
		}
		return
	}
	front, _, _ := pGetForegroundWindow.Call()
	var foregroundPID uint32
	if front != 0 {
		pGetWindowThreadProcessID.Call(front, uintptr(unsafe.Pointer(&foregroundPID)))
	}
	input := movement.WallHoldInput{Forward: mvKeyDown(0x57) || (mvKeyDown(0x01) && mvKeyDown(0x02)), Backward: mvKeyDown(0x53), Jump: mvKeyDown(0x20)}
	for _, pid := range mmPIDs() {
		if !mmIsTarget(pid) {
			continue
		}
		s := mm.sessions[pid]
		if s.move == nil || !s.loaded || !s.move.Enabled(movement.Wall) {
			continue
		}
		input.GameFocused = (foregroundPID == pid)
		if err := s.move.WallHoldTick(input); err != nil {
			mmStatus(s, err)
		}
	}
}
func mvKeyDown(vk uintptr) bool {
	r, _, _ := pGetAsyncKeyState.Call(vk)
	return (uint16(r) & 0x8000) != 0
}
func movementStopOnExit() {
	for _, pid := range mmPIDs() {
		s := mm.sessions[pid]
		if !mvProcessExited(s.proc) {
			_ = mmStopEffects(s)
		} // best-effort on exit
		if s.proc != nil {
			s.proc.Close()
		}
	}
	mm.sessions = nil
}
