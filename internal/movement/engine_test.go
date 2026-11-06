package movement

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
)

type fake struct {
	b               map[uint32]byte
	writes, patches int
	failAt          uint32
}

func newFake() *fake {
	f := &fake{b: map[uint32]byte{}}
	f.setU(mapIDAddr, 0)
	f.setU(root, 0x20000)
	f.setU(0x20000+0x34, 0x30000)
	f.setU(0x30000+0x24, 0x40000)
	f.setF(0x40000+pos, -8931.19)
	f.setF(0x40000+pos+4, 615.38)
	f.setF(0x40000+pos+8, 99.52)
	f.setF(0x40000+0x81C, 7)
	f.setF(0x40000+0x820, 4.5)
	f.setF(0x40000+0x82C, 7)
	f.setF(0x40000+0x830, 4.5)
	f.setF(0x40000+0x824, 4.722222)
	f.setF(0x40000+0x828, 2.25)
	f.setF(0x40000+climbParam, 1.0)
	f.setF(0x40000+clipWidth, 0.6)
	f.setU(0x40000+flags, 0x10)
	for i := 0; i < 8; i++ {
		f.b[0x40000+guidOffset+uint32(i)] = byte(i + 1)
	}
	for _, s := range textureSites {
		f.setB(s.at-uint32(len(s.before)), s.before)
		f.setB(s.at, s.old)
	}
	f.setB(jumpSite-uint32(len(jumpPrefix)), jumpPrefix)
	f.setB(jumpSite, jumpOriginal)
	f.setB(jumpSite+2, jumpSuffix)
	return f
}
func (f *fake) setU(a, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	f.setB(a, b[:])
}
func (f *fake) setF(a uint32, v float32) { f.setU(a, math.Float32bits(v)) }
func (f *fake) setB(a uint32, b []byte) {
	for i, v := range b {
		f.b[a+uint32(i)] = v
	}
}
func (f *fake) Read(a uint32, dst []byte) error {
	for i := range dst {
		v, ok := f.b[a+uint32(i)]
		if !ok {
			return errors.New("unmapped byte")
		}
		dst[i] = v
	}
	return nil
}
func (f *fake) Write(a uint32, src []byte) error {
	if a == f.failAt {
		return errors.New("forced failure")
	}
	f.writes++
	f.setB(a, src)
	return nil
}
func (f *fake) PatchCode(a uint32, src []byte) error { f.patches++; return f.Write(a, src) }
func readFakeF(f *fake, a uint32) float32            { v, _ := f32(f, a); return v }
func readFakeU(f *fake, a uint32) uint32             { v, _ := u32(f, a); return v }
func TestBaseSpeedMultiplier(t *testing.T) {
	for _, tc := range []struct {
		m    Mode
		str  string
		want float32
	}{{Run, "1", 7}, {Fly, "2", 14}, {Swim, "2", float32(4.722222 * 2)}, {Run, "0.5", 3.5}} {
		got, e := Target(tc.m, tc.str)
		if e != nil || math.Abs(float64(got-tc.want)) > 0.00001 {
			t.Fatalf("%v x%s => %v/%v expected %v", tc.m, tc.str, got, e, tc.want)
		}
	}
	for _, v := range []string{"", "0", "-2", "NaN", "Inf", "2x", "1e3", "1,5", "999999999999999999999999999999999999999999999999999999999999999"} {
		if _, e := Target(Run, v); e == nil {
			t.Fatalf("accepted bad multiplier %q", v)
		}
	}
}
func TestSpeedForwardBackwardIndependentRestore(t *testing.T) {
	f := newFake()
	e, er := New(f, 123)
	if er != nil {
		t.Fatal(er)
	}
	if err := e.SetSpeed(Run, "2", true); err != nil {
		t.Fatal(err)
	}
	if readFakeF(f, 0x40000+0x81C) != 14 || readFakeF(f, 0x40000+0x820) != 9 {
		t.Fatal("run/backward not doubled")
	}
	if err := e.SetSpeed(Swim, "3", true); err != nil {
		t.Fatal(err)
	}
	if readFakeF(f, 0x40000+0x824) < 14.16 || readFakeF(f, 0x40000+0x828) != 6.75 {
		t.Fatal("swim/backward not independent")
	}
	if err := e.SetSpeed(Run, "4", true); err != nil {
		t.Fatal(err)
	}
	if readFakeF(f, 0x40000+0x820) != 18 {
		t.Fatal("backward compounded or wrong original")
	}
	if err := e.StopAll(); err != nil {
		t.Fatal(err)
	}
	if readFakeF(f, 0x40000+0x81C) != 7 || readFakeF(f, 0x40000+0x820) != 4.5 || readFakeF(f, 0x40000+0x828) != 2.25 {
		t.Fatal("original speeds not restored")
	}
}
func TestFlyFlagsOwnedAndGroundCompatible(t *testing.T) {
	f := newFake()
	e, _ := New(f, 123)
	if er := e.SetSpeed(Fly, "1", true); er != nil {
		t.Fatal(er)
	}
	flagsNow := readFakeU(f, 0x40000+flags)
	if flagsNow&canFly == 0 || flagsNow&flying != 0 {
		t.Fatalf("flying was incorrectly forced: %x", flagsNow)
	}
	f.setU(0x40000+flags, flagsNow|flying|0x10000)
	if er := e.StopSpeed(Fly); er != nil {
		t.Fatal(er)
	}
	after := readFakeU(f, 0x40000+flags)
	if after&canFly != 0 || after&flying != 0 || after&0x10000 == 0 {
		t.Fatalf("owned bits not restored: %x", after)
	}
}
func TestWaterWallApplyAndRestore(t *testing.T) {
	f := newFake()
	e, _ := New(f, 123)
	if er := e.SetWater(true); er != nil {
		t.Fatal(er)
	}
	if readFakeU(f, 0x40000+flags)&waterWalk == 0 {
		t.Fatal("waterwalk missing")
	}
	if er := e.SetWall(true); er != nil {
		t.Fatal(er)
	}
	if readFakeF(f, 0x40000+climbParam) != 200 {
		t.Fatal("wall slope not set")
	}
	if er := e.StopAll(); er != nil {
		t.Fatal(er)
	}
	if readFakeU(f, 0x40000+flags)&waterWalk != 0 || readFakeF(f, 0x40000+climbParam) != 1 {
		t.Fatal("original wall/water not restored")
	}
}
func TestTickAndIdentityProtection(t *testing.T) {
	f := newFake()
	e, _ := New(f, 123)
	if er := e.SetSpeed(Run, "2", true); er != nil {
		t.Fatal(er)
	}
	f.setF(0x40000+0x81C, 7)
	if er := e.Tick(); er != nil || readFakeF(f, 0x40000+0x81C) != 14 {
		t.Fatalf("speed not reapplied: %v", er)
	}
	before := f.writes
	f.b[0x40000+guidOffset] = 55
	if er := e.Tick(); er == nil || !strings.Contains(er.Error(), "changed") {
		t.Fatalf("character change not detected: %v", er)
	}
	if f.writes != before {
		t.Fatal("wrote after character change")
	}
}
func TestCodePatchesReversibleAndGuarded(t *testing.T) {
	f := newFake()
	e, _ := New(f, 123)
	if er := e.SetAirJump(true); er != nil {
		t.Fatal(er)
	}

	if er := e.SetAirJump(false); er != nil {
		t.Fatal(er)
	}
	b, _ := read(f, jumpSite, 2)
	if b[0] != jumpOriginal[0] || b[1] != jumpOriginal[1] {
		t.Fatal("AirJump not restored")
	}
	if er := e.SetSpeed(Fly, "1", true); er != nil {
		t.Fatal(er)
	}
	if er := e.SetThru(true); er != nil {
		t.Fatal(er)
	}
	if readFakeF(f, 0x40000+clipWidth) != 0 {
		t.Fatal("collision width not zero")
	}
	if er := e.SetThru(false); er != nil {
		t.Fatal(er)
	}
	if readFakeF(f, 0x40000+clipWidth) != 0.6 {
		t.Fatal("collision width not restored")
	}
	for _, s := range textureSites {
		b, _ := read(f, s.at, len(s.old))
		for i := range b {
			if b[i] != s.old[i] {
				t.Fatal("texture code not restored")
			}
		}
	}
	f.b[textureSites[0].at] = 0xCC
	before := f.patches
	if er := e.SetThru(true); er == nil {
		t.Fatal("unexpected code patch accepted")
	}
	if f.patches != before {
		t.Fatal("modified code after bad signature")
	}
}
