package catalog

import (
	"bytes"
	"testing"
)

// World/City retain independent faction and optional subcategory values in
// the same 12-column record; editing one must never clear the other.
func TestWorldCitySubcategoriesStayIndependentOfFaction(t *testing.T) {
	records := []Record{
		{ID: 7, Location: "Portal", Zone: "Test", MapID: 0, X: "1", Y: "2", Z: "3", Faction: "A", Category: "World", Subcategory: "Door / Passage / Other Point of Interest"},
		{ID: 8, Location: "Bank", Zone: "Test", MapID: 1, X: "4", Y: "5", Z: "6", Faction: "H", Category: "City", Subcategory: "Vendor/Repair"},
	}
	var b bytes.Buffer
	if e := Write(&b, records); e != nil {
		t.Fatal(e)
	}
	decoded, e := Read(&b)
	if e != nil {
		t.Fatal(e)
	}
	for i := range records {
		if decoded[i] != records[i] {
			t.Fatalf("record %d changed: got %+v", i, decoded[i])
		}
	}
	chosen := map[int]bool{7: true, 8: true}
	updated, e := BulkSet(records, chosen, "Subcategory", "Quest")
	if e != nil {
		t.Fatal(e)
	}
	for i := range updated {
		if updated[i].Subcategory != "Quest" || updated[i].Faction != records[i].Faction || updated[i].MapID != records[i].MapID || updated[i].X != records[i].X {
			t.Fatalf("bulk subcategory must not change faction/map/xyz: %+v", updated[i])
		}
	}
	onlyAlliance := Filter(updated, "", "", "A", "", "zone")
	if len(onlyAlliance) != 1 || onlyAlliance[0].ID != 7 {
		t.Fatal("underlying faction filter changed", onlyAlliance)
	}
}
