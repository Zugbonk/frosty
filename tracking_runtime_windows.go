//go:build windows

package main

import (
	"errors"
	"fmt"
)

// Tracking memory adapters are per-process; the earlier development build multi-client
// manager constructs a distinct tracking.Engine for each live PID.
const trackTimerID = 9710

var mvAllocRemote = kernel32.NewProc("VirtualAllocEx")

func (p *mvProc) FlushCode(addr, n uint32) error {
	if p == nil || p.handle == 0 {
		return errors.New("no active process for native instruction flush")
	}
	result, _, err := mvFlush.Call(p.handle, uintptr(addr), uintptr(n))
	if result == 0 {
		return fmt.Errorf("native bridge instruction cache flush failed: %v", err)
	}
	return nil
}
func (p *mvProc) Alloc(size uint32, protection uint32) (uint32, error) {
	if p == nil || p.handle == 0 {
		return 0, errors.New("no attached WoW process")
	}
	addr, _, err := mvAllocRemote.Call(p.handle, 0, uintptr(size), 0x3000, uintptr(protection))
	if addr < 0x10000 || addr >= 0x7fe00000 {
		return 0, fmt.Errorf("remote native tracking allocation failed: %v", err)
	}
	return uint32(addr), nil
}

// Protect memory writes with the same process-bound adapter already used by
// movement. Guard code hook writes with a separate read-back verification.
type trackingMemory struct{ *mvProc }

func (p trackingMemory) PatchCode(addr uint32, bytes []byte) error {
	if err := p.mvProc.PatchCode(addr, bytes); err != nil {
		return err
	}
	check := make([]byte, len(bytes))
	if err := p.Read(addr, check); err != nil {
		return err
	}
	for i := range bytes {
		if check[i] != bytes[i] {
			return fmt.Errorf("code patch verification failed at 0x%08x", addr)
		}
	}
	return nil
}

var trackingIDs = []string{
	"herb", "mine", "treasure", "fish", "object_other",
	"beasts", "dragons", "demons", "elementals", "mechanical", "totems",
	"giants", "undead", "human", "critters", "pets", "creature_other",
	"repair", "food", "poison", "ammo", "reagents", "innkeeper", "flightmaster",
	"battlemaster", "trainer", "profession", "auctioneer", "banker", "mailbox", "lowquests",
}
