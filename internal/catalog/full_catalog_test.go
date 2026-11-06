package catalog

import (
	"os"
	"strings"
	"testing"
)

func TestRealCleanCatalog(t *testing.T) {
	b, e := os.ReadFile("../../data/teleports.tsv")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "~FY:") || strings.Contains(string(b), "FY:b") {
		t.Fatal("legacy metadata leaked into clean TSV")
	}
	rows, e := Read(strings.NewReader(string(b)))
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) == 0 {
		t.Fatal("embedded catalog has no records")
	}
	sub := 0
	world := 0
	for _, r := range rows {
		if r.Subcategory != "" {
			sub++
		}
		if WorldMap(r.MapID) {
			world++
		}
	}
	if sub == 0 {
		t.Fatal("missing subcategories in embedded catalog")
	}
	if world == 0 {
		t.Fatal("missing world-map entries")
	}
	var out strings.Builder
	if e := Write(&out, rows); e != nil {
		t.Fatal(e)
	}
	check, e := Read(strings.NewReader(out.String()))
	if e != nil || len(check) != len(rows) {
		t.Fatalf("round trip %d %v", len(check), e)
	}
	for i := range rows {
		if rows[i] != check[i] {
			t.Fatalf("row %d changed on round trip", i)
		}
	}
}
