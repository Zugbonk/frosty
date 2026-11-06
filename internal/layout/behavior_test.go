package layout

import "testing"

func TestEditorSelectorFollowsTypeImmediately(t *testing.T) {
	for _, tc := range []struct {
		category string
		want     bool
	}{
		{"World", false}, {"City", false}, {"Dungeon", true},
		{"Raid", true}, {"Battleground", true}, {"", false},
	} {
		if got := EditorShowsSubcategory(tc.category); got != tc.want {
			t.Errorf("type %q: subcategory = %v, want %v", tc.category, got, tc.want)
		}
	}
}

func TestSpeedFieldAlignmentByTheme(t *testing.T) {
	for _, tc := range []struct {
		theme, key string
		want       int
	}{
		{"simple", "run_speed", 122}, {"simple", "fly_speed", 179},
		{"simple", "swim_speed", 235}, {"modern", "run_speed", 96},
		{"modern", "fly_speed", 152}, {"modern", "swim_speed", 207},
	} {
		if got := SpeedFieldTop(tc.theme, tc.key); got != tc.want {
			t.Errorf("%s/%s: top = %d, want %d", tc.theme, tc.key, got, tc.want)
		}
	}
}
