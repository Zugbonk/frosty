package focuspolicy

// ShouldHide returns true only when an application that previously held focus
// has genuinely lost it and is not in an explicitly initiated focus handoff.
// This is a small pure policy so popup-close and external-switch rules can be
// regression tested without a live Windows message loop.
func ShouldHide(everHeldFocus, foregroundOwned, handoffInProgress bool) bool {
	return everHeldFocus && !foregroundOwned && !handoffInProgress
}
