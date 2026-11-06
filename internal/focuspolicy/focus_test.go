package focuspolicy

import "testing"

func TestShouldHide(t *testing.T) {
	tests := []struct {
		name     string
		ever     bool
		owned    bool
		handoff  bool
		expected bool
	}{
		{"main foreground", true, true, false, false},
		{"edit or add popup foreground", true, true, false, false},
		{"native picker foreground", true, true, false, false},
		{"themed menu foreground", true, true, false, false},
		{"popup close transient foreground gap", true, false, true, false},
		{"woW foreground after handoff expired", true, false, false, true},
		{"explorer foreground", true, false, false, true},
		{"never foreground on startup", false, false, false, false},
		{"transient startup gap", false, false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldHide(tt.ever, tt.owned, tt.handoff); got != tt.expected {
				t.Fatalf("ShouldHide(%v,%v,%v)=%v; want %v", tt.ever, tt.owned, tt.handoff, got, tt.expected)
			}
		})
	}
}
