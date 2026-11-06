package catalog

import (
	"strings"
	"testing"
)

func TestLegacyCleanRoundTrip(t *testing.T) {
	s := "ID\tLocation\tZone\tMapID\tXPos\tYPos\tZPos\tFaction\tCategory\tMinLevel\tComment\n1\tExample\tDungeon\t619\t1.1\t-2.3\t3\tX\tDungeon\t\tAn exit~FY:e\n"
	rows, e := Read(strings.NewReader(s))
	if e != nil || len(rows) != 1 || rows[0].Subcategory != "Exit" || rows[0].Comment != "An exit" {
		t.Fatalf("legacy: %+v %v", rows, e)
	}
	var b strings.Builder
	if e := Write(&b, rows); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(b.String(), "FY:") {
		t.Fatal("legacy marker written")
	}
	again, e := Read(strings.NewReader(b.String()))
	if e != nil || len(again) != 1 || again[0].Subcategory != "Exit" {
		t.Fatalf("roundtrip: %+v %v", again, e)
	}
}
func TestMapRules(t *testing.T) {
	for _, id := range []int{0, 1, 530, 571} {
		if UseSubcategory(id) {
			t.Fatal(id)
		}
	}
	for _, id := range []int{30, 619, 628, 531} {
		if !UseSubcategory(id) {
			t.Fatal(id)
		}
	}
}
func TestFilter(t *testing.T) {
	r := []Record{{ID: 1, Location: "Ghamoo-ra", Zone: "BFD", Category: "Dungeon"}, {ID: 2, Location: "Stormwind Bank", Zone: "Stormwind", Category: "City"}}
	got := Filter(r, "bank", "", "", "", "zone")
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatal(got)
	}
}
