package catalog

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestEmbeddedCatalogRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("../../data/teleports.tsv")
	if err != nil {
		t.Fatal(err)
	}
	recs, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1042 {
		t.Fatalf("expected 1042 records in user final catalog, got %d", len(recs))
	}
	var b bytes.Buffer
	if err := Write(&b, recs); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&b)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(recs) {
		t.Fatalf("round trip lost %d rows", len(recs)-len(out))
	}
	for i, r := range recs {
		if r != out[i] {
			t.Fatalf("round trip changed row %d: %+v vs %+v", i, r, out[i])
		}
	}
}
func TestExactMatchingIndependentOfID(t *testing.T) {
	existing := []Record{{ID: 1, Location: "Entry", Zone: "A", MapID: 0, X: "1", Y: "2", Z: "3", Faction: "A", Category: "World", Comment: "same"}}
	duplicate := existing[0]
	duplicate.ID = 222
	differing := duplicate
	differing.Comment = "changed"
	differing.ID = 333
	result, n := MergeExact(existing, []Record{duplicate, differing, differing})
	if n != 1 || len(result) != 2 || result[1].ID != 2 || result[1].Comment != "changed" {
		t.Fatalf("merge %#v, %d", result, n)
	}
}
func TestSearchCannotEscapeFilteredZone(t *testing.T) {
	rows := []Record{{ID: 1, Location: "Town Square", Zone: "Town", MapID: 0}, {ID: 2, Location: "Town Square", Zone: "Dungeon", MapID: 571}}
	res := Filter(rows, "square", "Town", "", "", "zone")
	if len(res) != 1 || res[0].ID != 1 {
		t.Fatalf("search crossed zone: %+v", res)
	}
}
func TestBulkMapAndXYZNeverEditable(t *testing.T) {
	rows := []Record{{ID: 9, Location: "Keep", Zone: "Zone", MapID: 571, X: "1", Y: "2", Z: "3"}}
	for _, f := range []string{"MapID", "X", "Y", "Z"} {
		if _, e := BulkSet(rows, map[int]bool{9: true}, f, "42"); e == nil {
			t.Errorf("bulk allowed %s", f)
		}
	}
	out, e := BulkSet(rows, map[int]bool{9: true}, "Comment", "notes")
	if e != nil || out[0].MapID != 571 || out[0].X != "1" || out[0].Comment != "notes" {
		t.Fatalf("bulk %+v %v", out, e)
	}
}
func TestRejectNonFiniteTSVCoordinates(t *testing.T) {
	s := strings.Join(Columns, "\t") + "\n1\tHere\tHere\t0\tNaN\t2\t3\tA\tWorld\t\t\t\n"
	if _, e := Read(strings.NewReader(s)); e == nil {
		t.Fatal("accepted NaN")
	}
}
