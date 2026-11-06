// Package catalog is the independent, plaintext Frosty teleport catalog.
// It has no WoW, Windows, UI, or encryption dependency.
package catalog

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

var Columns = []string{"ID", "Location", "Zone", "MapID", "XPos", "YPos", "ZPos", "Faction", "Category", "MinLevel", "Comment", "Subcategory"}
var CategoryOptions = []string{"World", "City", "Dungeon", "Raid", "Battleground"}
var SubcategoryOptions = []string{"", "Boss", "Boss (Must Summon)", "Boss (Roams Around)", "Rare", "Door / Passage / Other Point of Interest", "Quest", "Vendor/Repair", "Exit"}
var subcategoryCode = map[string]string{"b": "Boss", "s": "Boss (Must Summon)", "r": "Boss (Roams Around)", "a": "Rare", "d": "Door / Passage / Other Point of Interest", "q": "Quest", "v": "Vendor/Repair", "e": "Exit"}

type Record struct {
	ID                                                int
	Location, Zone                                    string
	MapID                                             int
	X, Y, Z                                           string // original decimal precision is retained
	Faction, Category, MinLevel, Comment, Subcategory string
}

func (r Record) Row() []string {
	return []string{strconv.Itoa(r.ID), r.Location, r.Zone, strconv.Itoa(r.MapID), r.X, r.Y, r.Z, r.Faction, r.Category, r.MinLevel, r.Comment, r.Subcategory}
}

func WorldMap(id int) bool       { return id == 0 || id == 1 || id == 530 || id == 571 }
func UseSubcategory(id int) bool { return !WorldMap(id) }

func stripLegacyMarker(s string) (string, string) {
	if len(s) >= 4 && s[len(s)-4:len(s)-1] == "FY:" {
		if sub, ok := subcategoryCode[s[len(s)-1:]]; ok {
			s = s[:len(s)-4]
			s = strings.TrimSuffix(s, "~")
			return strings.TrimRight(s, " "), sub
		}
	}
	return s, ""
}

// Read accepts the final 12-column source-owned format and 11-column legacy TSV.
// The historic ~FY: marker is decoded rather than lost, and is never written back.
func Read(r io.Reader) ([]Record, error) {
	cr := csv.NewReader(r)
	cr.Comma = '\t'
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty teleport collection")
	}
	if len(rows[0]) < 11 || rows[0][0] != "ID" {
		return nil, fmt.Errorf("unknown teleport TSV header")
	}
	ret := make([]Record, 0, len(rows)-1)
	ids := make(map[int]bool)
	for ix, row := range rows[1:] {
		if len(row) == 1 && row[0] == "" {
			continue
		}
		if len(row) != 11 && len(row) != 12 {
			return nil, fmt.Errorf("line %d: expected 11 or 12 fields, received %d", ix+2, len(row))
		}
		id, e1 := strconv.Atoi(row[0])
		mid, e2 := strconv.Atoi(row[3])
		if e1 != nil || e2 != nil || id <= 0 || mid < 0 || ids[id] {
			return nil, fmt.Errorf("line %d: invalid ID/MapID or duplicate ID", ix+2)
		}
		ids[id] = true
		for _, n := range row[4:7] {
			value, err := strconv.ParseFloat(n, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("line %d: invalid coordinate %q", ix+2, n)
			}
		}
		comment, sub := stripLegacyMarker(row[10])
		if len(row) == 12 && strings.TrimSpace(row[11]) != "" {
			sub = row[11]
		}
		ret = append(ret, Record{id, row[1], row[2], mid, row[4], row[5], row[6], row[7], row[8], row[9], comment, sub})
	}
	return ret, nil
}

// Write emits unencrypted, human-readable TSV, without old FY comment markers.
func Write(w io.Writer, rec []Record) error {
	cw := csv.NewWriter(w)
	cw.Comma = '\t'
	cw.UseCRLF = false
	if err := cw.Write(Columns); err != nil {
		return err
	}
	for _, r := range rec {
		if err := cw.Write(r.Row()); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func NextID(rows []Record) int {
	max := 0
	for _, r := range rows {
		if r.ID > max {
			max = r.ID
		}
	}
	return max + 1
}
func Clone(rows []Record) []Record { return append([]Record(nil), rows...) }
func UniqueValues(rows []Record, field string) []string {
	seen := make(map[string]bool)
	for _, r := range rows {
		v := ""
		switch field {
		case "zone":
			v = r.Zone
		case "faction":
			v = r.Faction
		case "category":
			v = r.Category
		}
		if v != "" {
			seen[v] = true
		}
	}
	vals := make([]string, 0, len(seen))
	for k := range seen {
		vals = append(vals, k)
	}
	sort.Strings(vals)
	return vals
}

// Filter works on a snapshot of records; all preview filtering is offline.
func Filter(rows []Record, query, zone, faction, category, sortBy string) []Record {
	query = strings.ToLower(strings.TrimSpace(query))
	ret := make([]Record, 0, len(rows))
	for _, r := range rows {
		if zone != "" && r.Zone != zone {
			continue
		}
		if faction != "" && r.Faction != faction {
			continue
		}
		if category != "" && r.Category != category {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(r.Location+" "+r.Zone+" "+r.Comment+" "+r.Subcategory), query) {
			continue
		}
		ret = append(ret, r)
	}
	s := strings.ToLower(sortBy)
	sort.SliceStable(ret, func(i, j int) bool {
		a, b := ret[i], ret[j]
		switch s {
		case "location":
			if a.Location != b.Location {
				return strings.ToLower(a.Location) < strings.ToLower(b.Location)
			}
		case "type":
			if a.Category != b.Category {
				return a.Category < b.Category
			}
		case "level":
			av, _ := strconv.Atoi(a.MinLevel)
			bv, _ := strconv.Atoi(b.MinLevel)
			if av != bv {
				return av < bv
			}
		default:
			if a.Zone != b.Zone {
				return strings.ToLower(a.Zone) < strings.ToLower(b.Zone)
			}
		}
		if a.Zone != b.Zone && s == "zone" {
			return strings.ToLower(a.Zone) < strings.ToLower(b.Zone)
		}
		if a.Location != b.Location {
			return strings.ToLower(a.Location) < strings.ToLower(b.Location)
		}
		return a.ID < b.ID
	})
	return ret
}

// ContentKey intentionally excludes the editable record ID. Import deduplication
// is exact across every content field, including optional Subcategory.
func ContentKey(r Record) string {
	fields := r.Row()[1:]
	return strings.Join(fields, "\x00")
}

// MergeExact retains existing records and adds only records whose complete
// content differs. Newly added records receive collision-free local IDs.
func MergeExact(existing, incoming []Record) ([]Record, int) {
	result := Clone(existing)
	seen := make(map[string]bool, len(existing)+len(incoming))
	for _, r := range existing {
		seen[ContentKey(r)] = true
	}
	next := NextID(existing)
	added := 0
	for _, r := range incoming {
		key := ContentKey(r)
		if seen[key] {
			continue
		}
		seen[key] = true
		r.ID = next
		next++
		result = append(result, r)
		added++
	}
	return result, added
}

// RemoveIDs is stable even if a UI view is sorted or filtered.
func RemoveIDs(rows []Record, selected map[int]bool) []Record {
	result := make([]Record, 0, len(rows))
	for _, r := range rows {
		if !selected[r.ID] {
			result = append(result, r)
		}
	}
	return result
}

// BulkSet changes one and only one editable content field. MapID and XYZ
// intentionally have no bulk-edit cases.
func BulkSet(rows []Record, chosen map[int]bool, field, value string) ([]Record, error) {
	allowed := map[string]bool{"Location": true, "Zone": true, "MinLevel": true, "Category": true, "Faction": true, "Subcategory": true, "Comment": true}
	if !allowed[field] {
		return nil, fmt.Errorf("field %q cannot be bulk edited", field)
	}
	if (field == "Location" || field == "Zone") && strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("%s cannot be blank", field)
	}
	if field == "MinLevel" && strings.TrimSpace(value) != "" {
		n, e := strconv.Atoi(value)
		if e != nil || n < 0 || n > 255 {
			return nil, fmt.Errorf("invalid minimum level")
		}
	}
	result := Clone(rows)
	for i := range result {
		if !chosen[result[i].ID] {
			continue
		}
		switch field {
		case "Location":
			result[i].Location = value
		case "Zone":
			result[i].Zone = value
		case "MinLevel":
			result[i].MinLevel = value
		case "Category":
			result[i].Category = value
		case "Faction":
			result[i].Faction = value
		case "Subcategory":
			result[i].Subcategory = value
		case "Comment":
			result[i].Comment = value
		}
	}
	return result, nil
}
