package catalog

import "testing"

func TestMergeExactContentAndStableIDs(t *testing.T) {
	base := []Record{{ID: 1, Location: "Place", Zone: "Zone", MapID: 0, X: "1", Y: "2", Z: "3", Comment: "note"}}
	dup := base[0]
	dup.ID = 22
	slightly := dup
	slightly.Comment = "other"
	slightly.ID = 23
	out, n := MergeExact(base, []Record{dup, slightly, slightly})
	if n != 1 || len(out) != 2 || out[1].ID != 2 || out[1].Comment != "other" {
		t.Fatalf("unexpected merge %d %#v", n, out)
	}
}
func TestBulkDoesNotChangeCoordinates(t *testing.T) {
	base := []Record{{ID: 1, Location: "A", Zone: "A", MapID: 571, X: "2", Y: "3", Z: "4"}}
	out, e := BulkSet(base, map[int]bool{1: true}, "Zone", "B")
	if e != nil || out[0].Zone != "B" || out[0].MapID != 571 || out[0].X != "2" {
		t.Fatalf("bulk %#v: %v", out, e)
	}
	if _, e = BulkSet(base, map[int]bool{1: true}, "MapID", "0"); e == nil {
		t.Fatal("MapID bulk allowed")
	}
}
func TestZoneSortSecondaryLocation(t *testing.T) {
	rows := []Record{{ID: 1, Zone: "City", Location: "Zeta"}, {ID: 2, Zone: "City", Location: "Alpha"}, {ID: 3, Zone: "B", Location: "Moon"}}
	out := Filter(rows, "", "", "", "", "zone")
	if out[0].ID != 3 || out[1].ID != 2 || out[2].ID != 1 {
		t.Fatalf("sort %#v", out)
	}
}
