// Package addlocation reads the three client-owned WoW 3.3.5a location labels
// used by the Add Location editor. It intentionally exposes only a read-only
// memory interface; it cannot write to the game process.
package addlocation

import (
	"encoding/binary"
	"strings"
	"unicode/utf8"
)

const (
	// These absolute addresses are the verified build-12340 references used by
	// the previous working Frosty Zugbonk Beta 3.01 implementation.
	MinimapTextPointer uint32 = 0x00BD077C
	SubzoneTextPointer uint32 = 0x00BD0784
	ZoneTextPointer    uint32 = 0x00BD0788

	maxTextBytes = 126
)

// Memory is deliberately read-only so callers cannot use this package to
// alter the game process.
type Memory interface {
	Read(address uint32, dst []byte) error
}

// Labels contains the parent zone shown on the world map and the most specific
// place name shown by the minimap (with the legacy subzone/zone fallbacks).
type Labels struct {
	Location string
	Zone     string
}

// ReadLabels follows only the three known string pointers. It performs no
// scanning, injection, hooks, or writes. The minimap label has priority for
// Location, then Subzone, then Zone, matching Beta 3.01.
func ReadLabels(mem Memory) Labels {
	if mem == nil {
		return Labels{}
	}
	zone := readText(mem, ZoneTextPointer)
	subzone := readText(mem, SubzoneTextPointer)
	minimap := readText(mem, MinimapTextPointer)
	location := minimap
	if location == "" {
		location = subzone
	}
	if location == "" {
		location = zone
	}
	return Labels{Location: location, Zone: zone}
}

func readText(mem Memory, pointerAddress uint32) string {
	var rawPointer [4]byte
	if mem.Read(pointerAddress, rawPointer[:]) != nil {
		return ""
	}
	ptr := binary.LittleEndian.Uint32(rawPointer[:])
	// Match the legacy user-mode address bounds and ensure the complete bounded
	// read stays below the upper client address limit. Text pointers need not
	// be DWORD-aligned because these are byte strings, not numeric values.
	if ptr < 0x10000 || ptr > 0x7FFF0000-maxTextBytes {
		return ""
	}
	var sample [maxTextBytes]byte
	if mem.Read(ptr, sample[:]) != nil {
		return ""
	}
	end := -1
	for i, b := range sample {
		if b == 0 {
			end = i
			break
		}
	}
	if end < 2 || end > 110 {
		return ""
	}
	text := string(sample[:end])
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" {
		return ""
	}
	for _, b := range sample[:end] {
		if b < 0x20 || b == 0x7F {
			return ""
		}
	}
	return text
}
