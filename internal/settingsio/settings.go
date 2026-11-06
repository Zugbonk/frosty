// Package settingsio handles the single persistent preferences file and
// transparently migrates preferences created by earlier application builds.
package settingsio

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const FileName = "settings.json"

func Load(dir string) ([]byte, error) {
	current := filepath.Join(dir, FileName)
	b, err := os.ReadFile(current)
	if err == nil {
		// The old file is obsolete only after the replacement exists and can
		// be read. Never delete legacy preferences if the new file is invalid.
		if json.Valid(b) {
			_ = os.Remove(filepath.Join(dir, "stage2_settings.json"))
		}
		return b, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	previous := filepath.Join(dir, "stage2_settings.json")
	b, err = os.ReadFile(previous)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !json.Valid(b) {
		// Keep the original intact if invalid; the caller will use defaults.
		return b, nil
	}
	if err = os.Rename(previous, current); err != nil {
		// If the atomic rename isn't permitted, retain the original and
		// continue reading it. A later successful Save will migrate it.
		return b, nil
	}
	return b, nil
}

func Save(dir string, data []byte) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file := filepath.Join(dir, FileName)
	tmp := file + ".new"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		// Windows Rename cannot replace an existing file, unlike POSIX.
		// Try direct overwrite as fallback, retaining the existing file on
		// errors when possible.
		if err2 := os.WriteFile(file, data, 0600); err2 != nil {
			_ = os.Remove(tmp)
			return err2
		}
		_ = os.Remove(tmp)
	}
	_ = os.Remove(filepath.Join(dir, "stage2_settings.json"))
	return nil
}
