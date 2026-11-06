package tracking

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
)

type mock struct {
	b         map[uint32]byte
	next      uint32
	patches   map[uint32]int
	failPatch uint32
}

func newMock() *mock { return &mock{b: map[uint32]byte{}, next: 0x3000000, patches: map[uint32]int{}} }
func (m *mock) Read(a uint32, b []byte) error {
	for i := range b {
		b[i] = m.b[a+uint32(i)]
	}
	return nil
}
func (m *mock) Write(a uint32, b []byte) error {
	for i, v := range b {
		m.b[a+uint32(i)] = v
	}
	return nil
}
func (m *mock) PatchCode(a uint32, b []byte) error {
	if a == m.failPatch {
		return errors.New("simulated patch failure")
	}
	m.patches[a]++
	return m.Write(a, b)
}
func (m *mock) FlushCode(addr, n uint32) error          { return nil }
func (m *mock) Alloc(size, prot uint32) (uint32, error) { p := m.next; m.next += size; return p, nil }
func (m *mock) setU(a, v uint32) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	_ = m.Write(a, b)
}
func (m *mock) u(a uint32) uint32        { v, _ := getU(m, a); return v }
func (m *mock) setF(a uint32, v float32) { m.setU(a, math.Float32bits(v)) }
func fixture() *mock {
	m := newMock()
	const p = 0x220000
	const d = 0x260000
	m.setU(0x00BD088C, 571)
	m.setU(0x00CD87A8, 0x200000)
	m.setU(0x200034, 0x210000)
	m.setU(0x210024, p)
	m.setF(p+0x798, 100)
	m.setF(p+0x79c, 200)
	m.setF(p+0x7a0, 300)
	m.setF(p+0x81c, 7)
	guid := []byte{0x99, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77}
	_ = m.Write(p+0x30, guid)
	m.setU(p+8, d)
	_ = m.Write(d, guid)
	m.setU(d+0x60, 100)
	m.setU(d+0x80, 100)
	m.setU(d+resourceOffset, 0x100)
	m.setU(d+creatureOffset, 0x20)
	_ = m.Write(npcPredicate, repairSignature)
	m.setU(gameRepairDefinition, 1)
	m.setU(gameRepairDefinition+4, 0x1000)
	m.setU(gameRepairDefinition+8, 0x00A11C1C)
	m.setU(gameRepairDefinition+12, 0x00A11C14)
	m.setU(gameTrackPointer, gameRepairDefinition)
	_ = m.Write(mailboxPredicate, append(append([]byte{}, originalFive...), mailboxTail...))
	_ = m.Write(questPredicate, append(append([]byte{}, originalFive...), questTail...))
	return m
}
func TestAllThreeFamiliesExist(t *testing.T) {
	if len(options) != 31 {
		t.Fatalf("missing tracking controls: %d", len(options))
	}
	seen := map[string]bool{}
	n := map[Family]int{}
	for _, o := range options {
		if seen[o.ID] {
			t.Fatalf("duplicate %q", o.ID)
		}
		seen[o.ID] = true
		n[o.Family]++
	}
	if n[Resources] != 5 || n[Creatures] != 12 || n[Services] != 14 {
		t.Fatal(n)
	}
}
func TestObjectAndCreatureMasksIndependentAndRestore(t *testing.T) {
	m := fixture()
	e, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Set("herb", true); err != nil {
		t.Fatal(err)
	}
	if m.u(0x260000+resourceOffset) != (1 << 1) {
		t.Fatal("herb mask")
	}
	if m.u(0x260000+creatureOffset) != 0x20 {
		t.Fatal("creature changed")
	}
	if err = e.Set("beasts", true); err != nil {
		t.Fatal(err)
	}
	if err = e.Set("dragons", true); err != nil {
		t.Fatal(err)
	}
	if m.u(0x260000+creatureOffset) != 3 {
		t.Fatal("independent creatures")
	}
	if err = e.Set("beasts", false); err != nil {
		t.Fatal(err)
	}
	if m.u(0x260000+creatureOffset) != 2 {
		t.Fatal("creature disable")
	}
	// A genuinely unrelated game-modified bit survives restoration.
	m.setU(0x260000+resourceOffset, 0x1002)
	if err = e.Set("herb", false); err != nil {
		t.Fatal(err)
	}
	if m.u(0x260000+resourceOffset) != 0x1100 {
		t.Fatalf("masked restoration: %08x", m.u(0x260000+resourceOffset))
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if m.u(0x260000+creatureOffset) != 0x20 {
		t.Fatal("original creature bits not restored")
	}
}
func TestAllOtherExcludesNamed(t *testing.T) {
	m := fixture()
	e, _ := New(m)
	if err := e.Set("object_other", true); err != nil {
		t.Fatal(err)
	}
	if m.u(0x260000+resourceOffset)&resourceNamed != 0 {
		t.Fatal("named resources included")
	}
	if err := e.Set("creature_other", true); err != nil {
		t.Fatal(err)
	}
	if m.u(0x260000+creatureOffset)&creatureNamed != 0 {
		t.Fatal("named creatures included")
	}
}
func TestCharacterChangeRefusesWrites(t *testing.T) {
	m := fixture()
	e, _ := New(m)
	m.setU(0x220000+0x30, 0xdeadbeef)
	if err := e.Set("herb", true); err == nil {
		t.Fatal("character change accepted")
	}
	if got := m.u(0x260000 + resourceOffset); got != 0x100 {
		t.Fatal("wrote to another character", got)
	}
}
func TestNativeServicesDescriptorApplyRestoreAndExternalSelection(t *testing.T) {
	m := fixture()
	e, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Set("repair", true); err != nil {
		t.Fatal(err)
	}
	if e.nativeDesc == 0 || m.u(gameTrackPointer) != e.nativeDesc {
		t.Fatal("not selected")
	}
	if m.u(e.nativeDesc+4) != 0x1000 {
		t.Fatal("wrong service mask")
	}
	if err = e.Set("food", true); err != nil {
		t.Fatal(err)
	}
	if m.u(e.nativeDesc+4) != 0x1200 {
		t.Fatal("combined service mask")
	}
	// WoW's own dropdown changed underneath us; remember its most recent choice.
	m.setU(gameTrackPointer, 0xA11C60)
	if err = e.Tick(); err != nil {
		t.Fatal(err)
	}
	if e.originalNative != 0xA11C60 {
		t.Fatal("native choice not observed")
	}
	if err = e.Set("food", false); err != nil {
		t.Fatal(err)
	}
	if err = e.Set("repair", false); err != nil {
		t.Fatal(err)
	}
	if m.u(gameTrackPointer) != 0xA11C60 {
		t.Fatalf("native selection not restored %08x", m.u(gameTrackPointer))
	}
}
func TestSpecialBridgesAreGuardedReversibleAndIndependent(t *testing.T) {
	m := fixture()
	e, _ := New(m)
	if err := e.Set("mailbox", true); err != nil {
		t.Fatal(err)
	}
	if e.hookPage == 0 || e.hookMask != 3 {
		t.Fatalf("hooks not prepared: %x", e.hookMask)
	}
	for _, x := range []struct{ at, off uint32 }{{mailboxPredicate, mailboxStubOffset}, {questPredicate, questStubOffset}} {
		b, _ := read(m, x.at, 5)
		if !bytes.Equal(b, jump(x.at, e.hookPage+x.off)) {
			t.Fatalf("missing bridge at %08x", x.at)
		}
	}
	if m.u(e.hookPage) != 1 {
		t.Fatal("mailbox bit missing")
	}
	if err := e.Set("lowquests", true); err != nil {
		t.Fatal(err)
	}
	if m.u(e.hookPage) != 3 {
		t.Fatal("quest bit missing")
	}
	if err := e.Set("mailbox", false); err != nil {
		t.Fatal(err)
	}
	if m.u(e.hookPage) != 2 {
		t.Fatal("mailbox off disabled quest")
	}
	if err := e.Set("lowquests", false); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	for _, addr := range []uint32{mailboxPredicate, questPredicate} {
		b, _ := read(m, addr, 5)
		if !bytes.Equal(b, originalFive) {
			t.Fatalf("not restored %08x", addr)
		}
	}
}
func TestSpecialSignatureGuard(t *testing.T) {
	m := fixture()
	m.b[questPredicate+6] = 0
	e, _ := New(m)
	if err := e.Set("lowquests", true); err == nil {
		t.Fatal("bad quest signature accepted")
	}
	if len(m.patches) != 0 {
		t.Fatal("patched despite bad signature")
	}
}
func TestLegacyTrampolineByteLengthsAndOffsets(t *testing.T) {
	mail, quest := makeStubs(0x3000000)
	if len(mail) != 37 || len(quest) != 72 {
		t.Fatalf("bad bridge lengths %d %d", len(mail), len(quest))
	}
	if binary.LittleEndian.Uint32(mail[2:]) != 0x3000000 || binary.LittleEndian.Uint32(quest[43:]) != 0x3000008 {
		t.Fatal("bad flag/count fixups")
	}
	if !bytes.Equal(quest[25:29], []byte{0x20, 0xe0, 0x57, 0x00}) {
		t.Fatal("native overhead marker callback missing")
	}
}
func TestBadNativeDescriptorRejects(t *testing.T) {
	m := fixture()
	m.setU(0x260000+0x80, 0)
	if _, err := New(m); err == nil || !strings.Contains(err.Error(), "health") {
		t.Fatalf("bad descriptor not refused: %v", err)
	}
}

func TestSecondBridgePatchFailureRestoresFirst(t *testing.T) {
	m := fixture()
	m.failPatch = questPredicate
	e, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Set("mailbox", true); err == nil {
		t.Fatal("expected simulated second-patch failure")
	}
	got, _ := read(m, mailboxPredicate, 5)
	if !bytes.Equal(got, originalFive) {
		t.Fatalf("first hook not removed on failure: %x", got)
	}
	if e.Enabled("mailbox") {
		t.Fatal("UI state accepted a failed operation")
	}
	if e.hookMask != 0 {
		t.Fatal("unreleased hook bits", e.hookMask)
	}
}

func TestNeverOverwriteUnknownServiceCodeChange(t *testing.T) {
	m := fixture()
	e, _ := New(m)
	if err := e.Set("mailbox", true); err != nil {
		t.Fatal(err)
	}
	m.b[questPredicate] = 0x90 // another component changed original instructions
	err := e.Close()
	if err == nil {
		t.Fatal("foreign code change not reported")
	}
	if m.b[questPredicate] != 0x90 {
		t.Fatal("overwrote foreign patch")
	}
}

func TestRestoringNativePointerDoesNotOverwriteExternalChoice(t *testing.T) {
	m := fixture()
	e, _ := New(m)
	if err := e.Set("repair", true); err != nil {
		t.Fatal(err)
	}
	m.setU(gameTrackPointer, 0xA11C60)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if m.u(gameTrackPointer) != 0xA11C60 {
		t.Fatal("overwrote player's native selection")
	}
}

// This reproduces earlier development build's overhead-marker OFF failure: the old manager
// removed both installed predicates immediately after the last tracker went
// OFF, so the native renderer could not acknowledge its queued quest refresh.
func TestQuestOFFWaitsForNativeOverheadRefresh(t *testing.T) {
	m := fixture()
	e, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Set("lowquests", true); err != nil {
		t.Fatal(err)
	}
	page := e.hookPage
	// Simulate WoW completing the ON refresh on its own renderer thread.
	m.setU(page+4, 2)
	m.setU(page+8, 7)
	if err := e.Set("lowquests", false); err != nil {
		t.Fatal(err)
	}
	if got := m.u(page); got != 0 {
		t.Fatalf("quest flag still ON: %d", got)
	}
	if got := e.hookMask; got != 3 {
		t.Fatalf("native hooks retired before OFF refresh: %d", got)
	}
	pending, err := e.QuestOFFPending()
	if err != nil || !pending {
		t.Fatalf("OFF refresh should be pending; pending=%v err=%v", pending, err)
	}
	// An extra ON-side callback must NOT count as OFF acknowledgement.
	m.setU(page+8, 8)
	pending, err = e.QuestOFFPending()
	if err != nil || !pending {
		t.Fatal("ON-side callback mistakenly accepted as OFF")
	}
	// Now the game's bridge has recorded OFF and invoked the refresh callback.
	m.setU(page+4, 0)
	m.setU(page+8, 9)
	pending, err = e.QuestOFFPending()
	if err != nil || pending {
		t.Fatalf("OFF acknowledgement not observed; pending=%v err=%v", pending, err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	for _, addr := range []uint32{mailboxPredicate, questPredicate} {
		got, _ := read(m, addr, 5)
		if !bytes.Equal(got, originalFive) {
			t.Fatalf("bridge not restored after OFF acknowledgement at %08x", addr)
		}
	}
}

func TestQuestOFFCanBeCancelledByRetoggleON(t *testing.T) {
	m := fixture()
	e, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Set("lowquests", true); err != nil {
		t.Fatal(err)
	}
	if err = e.Set("lowquests", false); err != nil {
		t.Fatal(err)
	}
	if err = e.Set("lowquests", true); err != nil {
		t.Fatal(err)
	}
	pending, err := e.QuestOFFPending()
	if err != nil || pending {
		t.Fatalf("retoggle ON did not cancel stale OFF request; pending=%v err=%v", pending, err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
}
