//go:build windows

package main

import (
	"strconv"
	"strings"

	"github.com/zugbonk/frosty/internal/addlocation"
	"github.com/zugbonk/frosty/internal/teleport"
)

// addLocationMultiClientBlocked limits Add Location to SOLO scope when ALL
// control is active for two or more live WoW clients.
func addLocationMultiClientBlocked() bool {
	if !app.allClients {
		return false
	}
	active := 0
	for _, s := range mm.sessions {
		if s != nil && s.proc != nil && !mvProcessExited(s.proc) {
			active++
			if active >= 2 {
				return true
			}
		}
	}
	return false
}

// fillAddLocationFromActiveCharacter uses the existing verified selected
// client handle and the established, read-only 3.3.5a teleport snapshot. Only
// the six live-character fields are populated, and only while still blank.
// Category, faction, subcategory, level and comment remain user-controlled.
func fillAddLocationFromActiveCharacter(form map[string]string) {
	if form == nil || addLocationMultiClientBlocked() || app.selectedPID == 0 {
		return
	}
	s := mm.sessions[app.selectedPID]
	if s == nil || s.proc == nil || mvProcessExited(s.proc) {
		return
	}
	state, err := teleport.Snapshot(s.proc)
	if err != nil {
		return // Keep the editor usable for manual entry during loading/unavailable states.
	}
	setBlank := func(key, value string) {
		if strings.TrimSpace(form[key]) == "" && strings.TrimSpace(value) != "" {
			form[key] = value
		}
	}
	labels := addlocation.ReadLabels(s.proc)
	setBlank("Location", labels.Location)
	setBlank("Zone", labels.Zone)
	setBlank("MapID", strconv.FormatUint(uint64(state.MapID), 10))
	setBlank("X", strconv.FormatFloat(float64(state.X), 'f', -1, 32))
	setBlank("Y", strconv.FormatFloat(float64(state.Y), 'f', -1, 32))
	setBlank("Z", strconv.FormatFloat(float64(state.Z), 'f', -1, 32))
}
