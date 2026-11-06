package movement

import "testing"

func holdGame() WallHoldInput { return WallHoldInput{GameFocused: true} }
func holdWalk() WallHoldInput { return WallHoldInput{GameFocused: true, Forward: true} }
func armWallHold(t *testing.T) (*fake, *Engine) {
	t.Helper()
	f := newFake()
	e, err := New(f, 123)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.SetWall(true); err != nil {
		t.Fatal(err)
	}
	if err = e.WallHoldTick(holdWalk()); err != nil {
		t.Fatal(err)
	}
	f.setF(0x40000+pos, -8930.70)
	f.setF(0x40000+pos+8, 100.35)
	if err = e.WallHoldTick(holdWalk()); err != nil {
		t.Fatal(err)
	}
	f.setU(0x40000+flags, 0x10|0x1|0x3000|0x10000)
	if err = e.WallHoldTick(holdGame()); err != nil {
		t.Fatal(err)
	}
	if !e.wallHold.active {
		t.Fatal("hold did not arm after actual uphill forward movement")
	}
	if readFakeU(f, 0x40000+flags)&gravityDisabled == 0 {
		t.Fatal("hold gravity protection absent")
	}
	return f, e
}
func TestWallHoldAnchorsReachedPositionAndReleases(t *testing.T) {
	f, e := armWallHold(t)
	expectedX := readFakeF(f, 0x40000+pos)
	expectedZ := readFakeF(f, 0x40000+pos+8)
	flagsNow := readFakeU(f, 0x40000+flags)
	if flagsNow&0x10 == 0 || flagsNow&holdClear != 0 {
		t.Fatalf("invalid hold motion flags: %08x", flagsNow)
	}
	f.setF(0x40000+pos, expectedX+0.05)
	f.setF(0x40000+pos+8, expectedZ-0.04)
	if err := e.WallHoldTick(holdGame()); err != nil {
		t.Fatal(err)
	}
	if readFakeF(f, 0x40000+pos) != expectedX || readFakeF(f, 0x40000+pos+8) != expectedZ {
		t.Fatal("hold did not restore actual reached position")
	}
	if err := e.WallHoldTick(holdWalk()); err != nil {
		t.Fatal(err)
	}
	if !e.wallHold.resuming || e.wallHold.active == true {
		t.Fatal("W did not begin controlled resume")
	}
	f.setF(0x40000+pos, expectedX+0.13)
	f.setF(0x40000+pos+8, expectedZ+0.2)
	if err := e.WallHoldTick(holdWalk()); err != nil {
		t.Fatal(err)
	}
	if e.wallHold.active || e.wallHold.resuming || (readFakeU(f, 0x40000+flags)&gravityDisabled) != 0 {
		t.Fatal("credible uphill progress did not release gravity hold")
	}
	if err := e.SetWall(false); err != nil {
		t.Fatal(err)
	}
	if readFakeF(f, 0x40000+climbParam) != 1 {
		t.Fatal("climb parameter wasn't restored")
	}
}
func TestWallHoldQuickForwardTapReholds(t *testing.T) {
	f, e := armWallHold(t)
	if err := e.WallHoldTick(holdWalk()); err != nil {
		t.Fatal(err)
	}
	if err := e.WallHoldTick(holdGame()); err != nil {
		t.Fatal(err)
	}
	if !e.wallHold.active || readFakeU(f, 0x40000+flags)&gravityDisabled == 0 {
		t.Fatal("brief W tap should rehold instead of dropping")
	}
}
func TestWallHoldBackwardJumpAndFocusRelease(t *testing.T) {
	for _, variant := range []string{"back", "jump", "unfocused"} {
		t.Run(variant, func(t *testing.T) {
			f, e := armWallHold(t)
			var inp WallHoldInput
			switch variant {
			case "back":
				inp = WallHoldInput{GameFocused: true, Backward: true}
			case "jump":
				inp = WallHoldInput{GameFocused: true, Jump: true}
			default:
				inp = WallHoldInput{}
			}
			rounds := 1
			if variant == "unfocused" {
				rounds = 11
			}
			for i := 0; i < rounds; i++ {
				if err := e.WallHoldTick(inp); err != nil {
					t.Fatal(err)
				}
			}
			if e.wallHold.active || readFakeU(f, 0x40000+flags)&gravityDisabled != 0 {
				t.Fatal("hold did not release")
			}
		})
	}
}
func TestWallHoldLargeCorrectionAndIdentityGuard(t *testing.T) {
	f, e := armWallHold(t)
	before := f.writes
	f.setF(0x40000+pos+8, readFakeF(f, 0x40000+pos+8)+3)
	if err := e.WallHoldTick(holdGame()); err != nil {
		t.Fatal(err)
	}
	if e.wallHold.active || readFakeU(f, 0x40000+flags)&gravityDisabled != 0 {
		t.Fatal("large correction should cancel hold")
	}
	// Character changed => NEVER touch the newly loaded character.
	f, e = armWallHold(t)
	before = f.writes
	f.b[0x40000+guidOffset] = 77
	if err := e.WallHoldTick(holdGame()); err == nil {
		t.Fatal("changed character accepted")
	}
	if f.writes != before {
		t.Fatal("wrote to different character")
	}
	_ = e.SetWall(false)
}
func TestWallHoldDoesNotArmOnFlatOrTransport(t *testing.T) {
	for _, isTransport := range []bool{false, true} {
		f := newFake()
		e, _ := New(f, 123)
		_ = e.SetWall(true)
		_ = e.WallHoldTick(holdWalk())
		if isTransport {
			f.setF(0x40000+pos, -8930.1)
			f.setF(0x40000+pos+8, 100.1)
			f.setU(0x40000+flags, 0x10|onTransport)
		}
		_ = e.WallHoldTick(holdWalk())
		_ = e.WallHoldTick(holdGame())
		if e.wallHold.active || readFakeU(f, 0x40000+flags)&gravityDisabled != 0 {
			t.Fatal("flat ground / transport incorrectly armed hold")
		}
	}
}
