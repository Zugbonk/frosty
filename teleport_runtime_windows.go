//go:build windows

package main

// Phase 4: source-owned teleport integration. Never uses the compiled earlier development build
// PE core, ZTP2, the legacy ListBox, or any injected teleport DLL.
import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/zugbonk/frosty/internal/catalog"
	"github.com/zugbonk/frosty/internal/teleport"
)

const tpGhostFlag = 0x10

var tpMenuCancelled = errors.New("teleport popup cancelled")

type tpDeath struct {
	guid                [8]byte
	lastAliveMap        uint32
	deathMap            uint32
	haveLast, haveDeath bool
	missing, healthy    int
}

var tpDeaths = map[uint32]*tpDeath{}

// Snapshot additionally validates a stable per-process player identity; this
// is checked again before every write and on any possible rollback.
type tpParticipant struct {
	pid      uint32
	name     string
	proc     *mvProc
	state    teleport.State
	guid     [8]byte
	original [3]float32
}

func tpRawU(m teleport.Memory, addr uint32) (uint32, error) {
	var b [4]byte
	if err := m.Read(addr, b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b[:]), nil
}
func tpFloat(m teleport.Memory, addr uint32) (float32, error) {
	v, e := tpRawU(m, addr)
	return math.Float32frombits(v), e
}
func tpValid(x, y, z float32) bool {
	return !math.IsNaN(float64(x)) && !math.IsNaN(float64(y)) && !math.IsNaN(float64(z)) && math.Abs(float64(x)) < 100000 && math.Abs(float64(y)) < 100000 && z > -50000 && z < 50000 && (x != 0 || y != 0 || z != 0)
}
func tpGuard(s *mmSession) (tpParticipant, error) {
	if s == nil || s.proc == nil || !s.loaded {
		return tpParticipant{}, errors.New("character loading or unavailable")
	}
	c, e := mmCurrent(s)
	if e != nil {
		return tpParticipant{}, e
	}
	t, e := teleport.Snapshot(s.proc)
	if e != nil {
		return tpParticipant{}, e
	}
	if c.Player != t.Player || c.MapID != t.MapID {
		return tpParticipant{}, errors.New("player identity/map transition in progress")
	}
	return tpParticipant{pid: s.pid, name: mmDisplayName(s), proc: s.proc, state: t, guid: c.GUID, original: [3]float32{t.X, t.Y, t.Z}}, nil
}
func tpSameIdentity(p tpParticipant) bool {
	s := mm.sessions[p.pid]
	if s == nil || s.proc != p.proc || !s.loaded {
		return false
	}
	c, e := mmCurrent(s)
	if e != nil || c.Player != p.state.Player || c.GUID != p.guid || c.MapID != p.state.MapID {
		return false
	}
	t, e := teleport.Snapshot(p.proc)
	return e == nil && t.Player == p.state.Player && t.MapID == p.state.MapID
}
func tpSubjects() ([]tpParticipant, error) {
	mmInit()
	ids := []uint32{}
	if app.allClients {
		ids = mmPIDs()
	} else if app.selectedPID != 0 {
		ids = []uint32{app.selectedPID}
	}
	if len(ids) == 0 {
		return nil, errors.New("no WoW client selected")
	}
	out := make([]tpParticipant, 0, len(ids))
	for _, id := range ids {
		p, e := tpGuard(mm.sessions[id])
		if e != nil {
			return nil, fmt.Errorf("PID %d: %w", id, e)
		}
		out = append(out, p)
	}
	return out, nil
}
func tpShared() ([]tpParticipant, uint32, error) {
	list, e := tpSubjects()
	if e != nil {
		return nil, 0, e
	}
	m := list[0].state.MapID
	for _, p := range list {
		if p.state.MapID != m {
			return nil, 0, errors.New("move all characters to the same MapID, or choose SOLO")
		}
	}
	return list, m, nil
}
func tpDisplayMap() (int, bool, string) {
	ids := mmPIDs()
	if !app.allClients {
		ids = []uint32{app.selectedPID}
	}
	if len(ids) == 0 || ids[0] == 0 {
		return -1, false, "Connect to a loaded WoW character to display this map's locations"
	}
	mapid := -1
	for _, id := range ids {
		p, e := tpGuard(mm.sessions[id])
		if e != nil {
			return -1, false, "Character loading — teleport list temporarily unavailable"
		}
		if mapid < 0 {
			mapid = int(p.state.MapID)
		} else if mapid != int(p.state.MapID) {
			return -1, false, "Move all characters to the same map or select a single client"
		}
	}
	return mapid, true, ""
}
func tpCapture(w *uiWindow) error {
	list, _, e := tpShared()
	if e != nil {
		return e
	}
	chosen := list[0]
	if app.allClients && len(list) > 1 {
		items := make([]menuEntry, 0, len(list))
		for _, p := range list {
			items = append(items, menuEntry{label: fmt.Sprintf("%s (PID %d)", p.name, p.pid), value: fmt.Sprint(p.pid)})
		}
		v, ok := w.popupMenu(R(389, 71, 23, 27), items)
		if !ok {
			return tpMenuCancelled
		}
		pid, _ := strconv.ParseUint(v, 10, 32)
		for _, p := range list {
			if p.pid == uint32(pid) {
				chosen = p
				break
			}
		}
	}
	if !tpSameIdentity(chosen) {
		return errors.New("selected character changed while capturing XYZ")
	}
	snap, e := teleport.Snapshot(chosen.proc)
	if e != nil {
		return e
	}
	app.text["x"] = strconv.FormatFloat(float64(snap.X), 'f', 4, 32)
	app.text["y"] = strconv.FormatFloat(float64(snap.Y), 'f', 4, 32)
	app.text["z"] = strconv.FormatFloat(float64(snap.Z), 'f', 4, 32)
	app.status = "Captured live XYZ from " + chosen.name
	return nil
}
func tpEntered() ([3]float32, error) {
	var a [3]float32
	for i, k := range []string{"x", "y", "z"} {
		v, e := strconv.ParseFloat(strings.TrimSpace(app.text[k]), 32)
		if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return a, fmt.Errorf("invalid %s coordinate", strings.ToUpper(k))
		}
		a[i] = float32(v)
	}
	if !tpValid(a[0], a[1], a[2]) {
		return a, errors.New("unsafe or invalid XYZ coordinates")
	}
	return a, nil
}
func tpSaved() ([3]float32, int, error) {
	if app.selectedCount() != 1 {
		return [3]float32{}, 0, errors.New("select exactly one location to teleport")
	}
	var coords [3]float32
	for _, r := range app.visible {
		if r.ID != app.selectedID {
			continue
		}
		for i, v := range []string{r.X, r.Y, r.Z} {
			f, e := strconv.ParseFloat(v, 32)
			if e != nil {
				return coords, 0, e
			}
			coords[i] = float32(f)
		}
		if !tpValid(coords[0], coords[1], coords[2]) {
			return coords, 0, errors.New("invalid saved coordinates")
		}
		return coords, r.MapID, nil
	}
	return coords, 0, errors.New("select a location in the current filtered list")
}

type tpPlan struct {
	subject tpParticipant
	dest    [3]float32
}

func tpExecute(plans []tpPlan) error {
	if len(plans) == 0 {
		return errors.New("no eligible characters")
	}
	// Two-phase preflight. Readback does NOT prove server acceptance.
	for _, p := range plans {
		if !tpSameIdentity(p.subject) {
			return fmt.Errorf("%s changed identity or MapID", p.subject.name)
		}
		if !tpValid(p.dest[0], p.dest[1], p.dest[2]) {
			return errors.New("invalid destination")
		}
	}
	moved := []tpPlan{}
	for _, p := range plans {
		if !tpSameIdentity(p.subject) {
			return tpRollback(moved, errors.New("client changed during group teleport"))
		}
		if e := teleport.Teleport(p.subject.proc, p.subject.state, p.dest[0], p.dest[1], p.dest[2]); e != nil {
			return tpRollback(moved, fmt.Errorf("%s: %w", p.subject.name, e))
		}
		moved = append(moved, p)
	}
	for _, p := range plans {
		if !tpSameIdentity(p.subject) {
			return tpRollback(moved, errors.New("client changed after group teleport"))
		}
	}
	return nil
}
func tpRollback(moved []tpPlan, reason error) error {
	restored := 0
	for i := len(moved) - 1; i >= 0; i-- {
		p := moved[i]
		if tpSameIdentity(p.subject) {
			now, e := teleport.Snapshot(p.subject.proc)
			if e == nil {
				e = teleport.Teleport(p.subject.proc, now, p.subject.original[0], p.subject.original[1], p.subject.original[2])
				if e == nil {
					restored++
				}
			}
		}
	}
	return fmt.Errorf("%w (attempted safe position restoration on %d/%d clients; check in-game positions)", reason, restored, len(moved))
}
func tpPlansCommon(list []tpParticipant, dest [3]float32) []tpPlan {
	out := make([]tpPlan, 0, len(list))
	for _, p := range list {
		out = append(out, tpPlan{p, dest})
	}
	return out
}
func tpSavedAction() error {
	list, m, e := tpShared()
	if e != nil {
		return e
	}
	coord, recordMap, e := tpSaved()
	if e != nil {
		return e
	}
	if int(m) != recordMap {
		return errors.New("selected location belongs to a different MapID")
	}
	return tpExecute(tpPlansCommon(list, coord))
}
func tpXYZAction(w *uiWindow) error {
	list, _, e := tpShared()
	if e != nil {
		return e
	}
	xyz, e := tpEntered()
	if e != nil {
		return e
	}
	if len(list) == 1 || !app.allClients {
		return tpExecute(tpPlansCommon(list, xyz))
	}
	items := []menuEntry{}
	for _, p := range list {
		items = append(items, menuEntry{label: fmt.Sprintf("Teleport %s (PID %d)", p.name, p.pid), value: fmt.Sprint(p.pid)})
	}
	items = append(items, menuEntry{separator: true}, menuEntry{label: "Teleport ALL characters", value: "all"})
	v, ok := w.popupMenu(R(816, 72, 132, 30), items)
	if !ok {
		return tpMenuCancelled
	}
	if v != "all" {
		pid, _ := strconv.ParseUint(v, 10, 32)
		for _, p := range list {
			if p.pid == uint32(pid) {
				return tpExecute(tpPlansCommon([]tpParticipant{p}, xyz))
			}
		}
		return errors.New("chosen character is no longer available")
	}
	return tpExecute(tpPlansCommon(list, xyz))
}
func tpForwardAction() error {
	// Unlike a saved-location group teleport, each player's current position
	// and facing are independent; there is deliberately NO shared-map gate.
	list, e := tpSubjects()
	if e != nil {
		return e
	}
	plans := make([]tpPlan, 0, len(list))
	for _, p := range list {
		x := p.state.X + float32(math.Cos(float64(p.state.Facing)))
		y := p.state.Y + float32(math.Sin(float64(p.state.Facing)))
		plans = append(plans, tpPlan{p, [3]float32{x, y, p.state.Z}})
	}
	return tpExecute(plans)
}
func tpTarget(p tpParticipant) ([3]float32, error) {
	var coord [3]float32
	var b [8]byte
	if e := p.proc.Read(0x00BD07B0, b[:]); e != nil {
		return coord, e
	}
	if b == ([8]byte{}) {
		return coord, errors.New("no target selected")
	}
	conn, e := tpRawU(p.proc, 0x00C79CE0)
	if e != nil || conn < 0x10000 || conn > 0x7ff00000 {
		return coord, errors.New("target object manager missing")
	}
	mgr, e := tpRawU(p.proc, conn+0x2ED0)
	if e != nil || mgr < 0x10000 || mgr > 0x7ff00000 {
		return coord, errors.New("target manager unavailable")
	}
	obj, e := tpRawU(p.proc, mgr+0xAC)
	if e != nil {
		return coord, e
	}
	visited := make(map[uint32]bool)
	for i := 0; i < 7000 && obj >= 0x10000 && obj <= 0x7ff00000-0x800; i++ {
		if visited[obj] {
			break
		}
		visited[obj] = true
		var g [8]byte
		if e := p.proc.Read(obj+0x30, g[:]); e != nil {
			break
		}
		if g == b {
			kind, e := tpRawU(p.proc, obj+0x14)
			if e != nil || kind != 3 && kind != 4 {
				return coord, errors.New("target is not a player or NPC")
			}
			for j := 0; j < 3; j++ {
				coord[j], e = tpFloat(p.proc, obj+0x798+uint32(j*4))
				if e != nil {
					return coord, e
				}
			}
			facing, e := tpFloat(p.proc, obj+0x7A8)
			if e != nil || facing < -7 || facing > 7 {
				return coord, errors.New("target has invalid facing")
			}
			coord[0] -= 2 * float32(math.Cos(float64(facing)))
			coord[1] -= 2 * float32(math.Sin(float64(facing)))
			if !tpValid(coord[0], coord[1], coord[2]) {
				return coord, errors.New("target position invalid")
			}
			return coord, nil
		}
		obj, e = tpRawU(p.proc, obj+0x3C)
		if e != nil {
			break
		}
	}
	return coord, errors.New("target not loaded in this map (party portrait alone cannot supply coordinates)")
}
func tpVitals(p tpParticipant) (uint32, uint32, error) {
	desc, e := tpRawU(p.proc, p.state.Player+0x08)
	if e != nil || desc < 0x10000 || desc > 0x7ff00000 {
		return 0, 0, errors.New("player descriptor unavailable")
	}
	health, e := tpRawU(p.proc, desc+0x60)
	if e != nil {
		return 0, 0, e
	}
	max, e := tpRawU(p.proc, desc+0x80)
	if e != nil || max == 0 || max > 1000000000 || health > max {
		return 0, 0, errors.New("invalid health information")
	}
	flags, e := tpRawU(p.proc, desc+0x258)
	if e != nil {
		return 0, 0, e
	}
	return health, flags, nil
}
func tpPassive(s *mmSession) (tpParticipant, error) {
	if s == nil || s.proc == nil || mvProcessExited(s.proc) {
		return tpParticipant{}, errors.New("process unavailable")
	}
	c, e := mmCurrent(s)
	if e != nil {
		return tpParticipant{}, e
	}
	t, e := teleport.Snapshot(s.proc)
	if e != nil {
		return tpParticipant{}, e
	}
	if t.Player != c.Player || t.MapID != c.MapID {
		return tpParticipant{}, errors.New("player transition")
	}
	return tpParticipant{pid: s.pid, name: mmDisplayName(s), proc: s.proc, state: t, guid: c.GUID}, nil
}
func tpDeathTick() {
	for _, pid := range mmPIDs() {
		s := mm.sessions[pid]
		p, e := tpPassive(s)
		if e != nil {
			if d := tpDeaths[pid]; d != nil {
				d.missing++
				if d.missing > 300 {
					delete(tpDeaths, pid)
				}
			}
			continue
		}
		d := tpDeaths[pid]
		if d == nil || d.guid != p.guid {
			d = &tpDeath{guid: p.guid}
			tpDeaths[pid] = d
		}
		d.missing = 0
		hp, flags, e := tpVitals(p)
		if e != nil {
			continue
		}
		if hp == 0 || (flags&tpGhostFlag) != 0 {
			d.healthy = 0
			if !d.haveDeath && d.haveLast {
				d.deathMap = d.lastAliveMap
				d.haveDeath = true
			}
		} else if d.haveDeath {
			d.healthy++
			if d.healthy >= 4 {
				d.haveDeath = false
				d.lastAliveMap = p.state.MapID
				d.haveLast = true
				d.healthy = 0
			}
		} else {
			d.lastAliveMap = p.state.MapID
			d.haveLast = true
			d.healthy = 0
		}
	}
	for pid := range tpDeaths {
		if mm.sessions[pid] == nil {
			delete(tpDeaths, pid)
		}
	}
}
func tpCorpse(p tpParticipant) ([3]float32, error) {
	var dest [3]float32
	d := tpDeaths[p.pid]
	if d == nil || !d.haveDeath || d.guid != p.guid {
		return dest, errors.New("no death map recorded for this character")
	}
	if d.deathMap != p.state.MapID {
		return dest, fmt.Errorf("corpse MapID %d differs from current MapID %d", d.deathMap, p.state.MapID)
	}
	health, flags, e := tpVitals(p)
	if e != nil {
		return dest, e
	}
	if health > 0 && (flags&tpGhostFlag) == 0 {
		return dest, errors.New("character is alive")
	}
	for i := 0; i < 3; i++ {
		dest[i], e = tpFloat(p.proc, 0x00BD0A58+uint32(i*4))
		if e != nil {
			return dest, e
		}
	}
	dest[2] += 0.3
	if !tpValid(dest[0], dest[1], dest[2]) {
		return dest, errors.New("corpse coordinates invalid")
	}
	return dest, nil
}
func tpSpecialAction(w *uiWindow, kind string) error {
	list, e := tpSubjects()
	if e != nil {
		return e
	}
	get := tpTarget
	label := "target"
	if kind == "corpse" {
		get = tpCorpse
		label = "corpse"
	}
	if len(list) == 1 || !app.allClients {
		dest, e := get(list[0])
		if e != nil {
			return e
		}
		return tpExecute([]tpPlan{{list[0], dest}})
	}
	options := []menuEntry{}
	valid := map[uint32][3]float32{}
	for _, p := range list {
		dest, e := get(p)
		if e == nil {
			valid[p.pid] = dest
			options = append(options, menuEntry{label: "TP all to " + p.name + "'s " + label, value: fmt.Sprint(p.pid)})
		}
	}
	if len(valid) == 0 {
		return errors.New("no valid " + label + " destinations among controlled characters")
	}
	options = append(options, menuEntry{separator: true}, menuEntry{label: "TP each to their own " + label, value: "each"})
	v, ok := w.popupMenu(R(556, 35, 315, 33), options)
	if !ok {
		return tpMenuCancelled
	}
	plans := []tpPlan{}
	if v == "each" {
		for _, p := range list {
			if dest, ok := valid[p.pid]; ok {
				plans = append(plans, tpPlan{p, dest})
			}
		}
	} else {
		pid, _ := strconv.ParseUint(v, 10, 32)
		src := uint32(pid)
		dest, ok := valid[src]
		if !ok {
			return errors.New("chosen destination disappeared")
		}
		var srcmap uint32
		for _, p := range list {
			if p.pid == src {
				srcmap = p.state.MapID
				break
			}
		}
		for _, p := range list {
			if p.state.MapID == srcmap {
				plans = append(plans, tpPlan{p, dest})
			}
		}
	}
	return tpExecute(plans)
}
func tpRun(w *uiWindow, command string) {
	var err error
	switch command {
	case "capture":
		err = tpCapture(w)
	case "forward":
		err = tpForwardAction()
	case "coords":
		err = tpXYZAction(w)
	case "target", "corpse":
		err = tpSpecialAction(w, command)
	case "teleport":
		err = tpSavedAction()
	}
	if errors.Is(err, tpMenuCancelled) {
		return
	}
	if err != nil {
		app.status = "Teleport: " + err.Error()
	} else if command != "capture" {
		app.status = "Teleport operation completed (verify in-game position)"
	}
	mainRedraw()
}
func tpSortedChoices() []string {
	list := []string{}
	for _, p := range mmPIDs() {
		s := mm.sessions[p]
		if s != nil {
			list = append(list, fmt.Sprintf("%s (%d)", mmDisplayName(s), p))
		}
	}
	sort.Strings(list)
	return list
}

// A filtered list is purely a view; teleport never reads a hidden row.
func tpCurrentVisibleRecord(id int) (catalog.Record, bool) {
	for _, r := range app.visible {
		if r.ID == id {
			return r, true
		}
	}
	return catalog.Record{}, false
}

// Refresh only when the scope's map/loading compatibility changes. This avoids
// rebuilding the 1,043-row view 4 times per second and also corrects the
// standalone harness's UI-thread freeze pattern.
func tpSyncListMap() {
	if app.main == 0 {
		return
	}
	m, ok, why := tpDisplayMap()
	blocked := !ok && len(mm.sessions) > 0
	if m == app.activeMapID && blocked == app.listBlocked && why == app.listBlockReason {
		return
	}
	oldMap := app.activeMapID
	if oldMap != m || blocked != app.listBlocked {
		app.text["zone"] = "All zones"
		if ok && catalog.UseSubcategory(m) {
			app.text["faction"] = "All subcategories"
		} else {
			app.text["faction"] = "All factions"
		}
		app.scroll = 0
		app.clearSelection()
	}
	app.refreshList()
	mainRedraw()
}
func tpXYZDisabled() bool {
	if !app.allClients || len(mm.sessions) < 2 {
		return false
	}
	if _, _, e := tpShared(); e == nil {
		return false
	}
	// Disable while the ALL scope has incompatible or loading characters;
	// forward / target / corpse have their own separate rules.
	return true
}
