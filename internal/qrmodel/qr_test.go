package qrmodel

import (
	"strings"
	"testing"
)

func TestMatrix(t *testing.T) {
	for _, s := range []string{"hello", "https://example.org/donate", "wallet:anything", strings.Repeat("x", 318)} {
		m, e := Matrix(s)
		if e != nil {
			t.Fatal(e)
		}
		if len(m) < 29 || len(m) != len(m[0]) {
			t.Fatal("invalid square")
		}
		for _, row := range m {
			for i := 0; i < 4; i++ {
				if row[i] {
					t.Fatal("quiet zone")
				}
			}
		}
	}
}
func TestReject(t *testing.T) {
	if _, e := Matrix(""); e == nil {
		t.Fatal("empty")
	}
	if _, e := Matrix(strings.Repeat("z", 320)); e == nil {
		t.Fatal("long")
	}
}
