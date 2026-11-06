//go:build windows

package main

import "github.com/zugbonk/frosty/internal/speedfield"

// Input editing is testable without Windows or a real WoW process.
func speedTextEdit(old string, caret int, selected bool, key rune) (string, int) {
	return speedfield.Edit(old, caret, selected, key)
}
func speedCaretKey(old string, caret int, key uintptr) (string, int, bool) {
	return speedfield.Navigate(old, caret, key)
}
