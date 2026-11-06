package settingsio

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrate(t *testing.T) {
	dir := t.TempDir()
	old := []byte(`{"agreed":true,"theme":"simple","theme_chosen":true}`)
	if err := os.WriteFile(filepath.Join(dir, "stage2_settings.json"), old, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || !bytes.Equal(got, old) {
		t.Fatalf("migration read: %s %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatal("new preferences missing", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stage2_settings.json")); !os.IsNotExist(err) {
		t.Fatal("old preferences weren't renamed")
	}
}
func TestPreferCurrentAndSave(t *testing.T) {
	dir := t.TempDir()
	old := []byte(`{"theme":"modern"}`)
	fresh := []byte(`{"theme":"simple"}`)
	_ = os.WriteFile(filepath.Join(dir, "stage2_settings.json"), old, 0600)
	_ = os.WriteFile(filepath.Join(dir, FileName), fresh, 0600)
	got, err := Load(dir)
	if err != nil || !bytes.Equal(got, fresh) {
		t.Fatalf("existing current settings not preferred: %s %v", got, err)
	}
	change := []byte(`{"theme":"modern","agreed":true}`)
	if err := Save(dir, change); err != nil {
		t.Fatal(err)
	}
	got, err = Load(dir)
	if err != nil || !bytes.Equal(got, change) {
		t.Fatalf("save failed: %s %v", got, err)
	}
}
func TestNoSettings(t *testing.T) {
	b, err := Load(t.TempDir())
	if err != nil || b != nil {
		t.Fatalf("expected no config, got %q %v", b, err)
	}
}
