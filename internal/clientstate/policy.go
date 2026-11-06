// Package clientstate owns the platform-independent SOLO / ALL selection
// policy. It contains no game memory code, no Windows calls, and no UI.
package clientstate

import "sort"

const MaxClients = 12

// Scope chooses affected processes. SOLO is the selected process only; ALL
// includes every verified process (even if one is temporarily loading).
// Caller is responsible for enforcing MaxClients and verifying each PID.
type Scope struct {
	Selected uint32
	All      bool
}

func (s Scope) Targets(verified []uint32) []uint32 {
	seen := map[uint32]bool{}
	out := make([]uint32, 0, len(verified))
	for _, pid := range verified {
		if pid == 0 || seen[pid] || (!s.All && s.Selected != pid) {
			continue
		}
		seen[pid] = true
		out = append(out, pid)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func (s Scope) Wants(pid uint32) bool { return pid != 0 && (s.All || s.Selected == pid) }
func SamePlayer(oldPID, newPID uint32, oldPtr, newPtr uint32, oldGUID, newGUID [8]byte) bool {
	return oldPID != 0 && oldPID == newPID && oldPtr != 0 && oldPtr == newPtr && oldGUID == newGUID && oldGUID != ([8]byte{})
}

// StableObserved is an attachment gate, not a proof of actual in-game behavior.
// Require consecutive matching loaded states after loading, relog, teleport or
// a character change before modifying a new player object's memory.
type StableObserved struct {
	PID, Ptr uint32
	GUID     [8]byte
	Map      uint32
	Count    int
}

func (o *StableObserved) Observe(pid, ptr uint32, guid [8]byte, mapID uint32, loaded bool) bool {
	if !loaded || pid == 0 || ptr == 0 || guid == ([8]byte{}) {
		*o = StableObserved{}
		return false
	}
	if o.PID != pid || o.Ptr != ptr || o.GUID != guid || o.Map != mapID {
		*o = StableObserved{PID: pid, Ptr: ptr, GUID: guid, Map: mapID, Count: 1}
		return false
	}
	if o.Count < 3 {
		o.Count++
	}
	return o.Count >= 2
}
