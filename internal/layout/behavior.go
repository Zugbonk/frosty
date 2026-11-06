// Package layout contains source-owned layout and editor behavior shared by
// the Windows UI and portable regression tests. It never accesses WoW.
package layout

// EditorShowsSubcategory switches the second combobox as soon as the user
// changes the Type. The live in-game teleport list uses MapID separately.
func EditorShowsSubcategory(category string) bool {
	switch category {
	case "Dungeon", "Raid", "Battleground":
		return true
	default: // World, City (and an unset type) use Faction.
		return false
	}
}

// SpeedFieldTop puts text input boxes on the same vertical row as their
// associated movement buttons, without changing the Modern icon layout.
func SpeedFieldTop(theme, key string) int {
	if theme == "simple" {
		switch key {
		case "run_speed":
			return 122
		case "fly_speed":
			return 179
		case "swim_speed":
			return 235
		}
	}
	switch key {
	case "fly_speed":
		return 152
	case "swim_speed":
		return 207
	default:
		return 96
	}
}
