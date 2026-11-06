//go:build windows

package main

import (
	"github.com/zugbonk/frosty/internal/catalog"
	"strings"
	"syscall"
	"unsafe"
)

func keyPressed(vk uintptr) bool { r, _, _ := pGetAsyncKeyState.Call(vk); return (r & 0x8000) != 0 }
func textEdit(s string, pos int, selected bool, key rune) (string, int) {
	r := []rune(s)
	pos = clamp(pos, 0, len(r))
	if selected {
		r = nil
		pos = 0
	}
	switch {
	case key == 8:
		if pos > 0 {
			r = append(r[:pos-1], r[pos:]...)
			pos--
		}
	case key >= 32 && len(r) < 260:
		r = append(r[:pos], append([]rune{key}, r[pos:]...)...)
		pos++
	}
	return string(r), pos
}
func (w *uiWindow) caretAt(b rect, value string, x int) int {
	r := []rune(value)
	left := int(b.Left) + 5
	for i := 0; i < len(r); i++ {
		before := w.speedTextWidth(string(r[:i]))
		after := w.speedTextWidth(string(r[:i+1]))
		if x < left+(before+after)/2 {
			return i
		}
	}
	return len(r)
}
func (w *uiWindow) typeText(value string) {
	for _, key := range []rune(value) {
		if key < 32 && key != '\t' {
			continue
		}
		old := w.getField(w.focus)
		newer, next := textEdit(old, w.caretPos, w.selectAll, key)
		w.selectAll = false
		w.caretPos = next
		w.setField(w.focus, newer)
	}
	w.caretVisible = true
	w.caretTicks = 0
	if w.view == "main" {
		movementMultiplierChanged(w.focus)
		if w.focus == "search" {
			app.refreshList()
		}
	}
	redraw(w)
}
func (w *uiWindow) tableFocus(on bool) { w.tableFocused = on }

func clipRead() string {
	ok, _, _ := pOpenClipboard.Call(0)
	if ok == 0 {
		return ""
	}
	defer pCloseClipboard.Call()
	handle, _, _ := pGetClipboardData.Call(13)
	if handle == 0 {
		return ""
	}
	count, _, _ := pGlobalSize.Call(handle)
	if count == 0 {
		return ""
	}
	ptr, _, _ := pGlobalLock.Call(handle)
	if ptr == 0 {
		return ""
	}
	defer pGlobalUnlock.Call(handle)
	max := int(count / 2)
	if max > 260 {
		max = 260
	}
	chars := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), max)
	end := 0
	for end < len(chars) && chars[end] != 0 {
		end++
	}
	return strings.ReplaceAll(syscall.UTF16ToString(chars[:end]), "\r\n", "\n")
}
func clipWrite(value string) {
	chars, e := syscall.UTF16FromString(value)
	if e != nil {
		return
	}
	ok, _, _ := pOpenClipboard.Call(app.main)
	if ok == 0 {
		return
	}
	defer pCloseClipboard.Call()
	size := len(chars) * 2
	h, _, _ := pGlobalAlloc.Call(0x0002, uintptr(size))
	if h == 0 {
		return
	}
	ptr, _, _ := pGlobalLock.Call(h)
	if ptr == 0 {
		pGlobalFree.Call(h)
		return
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(chars)), chars)
	pGlobalUnlock.Call(h)
	pEmptyClipboard.Call()
	if set, _, _ := pSetClipboardData.Call(13, h); set == 0 {
		pGlobalFree.Call(h)
	}
}

func (w *uiWindow) clickBulk(x, y int) {
	if hit(x, y, R(180, 108, 355, 34)) {
		opts := []string{"Location", "Zone", "Minimum Level", "Type", "Note"}
		if bulkSelectionKind() == "world" {
			opts = append(opts, "Faction")
		}
		// Subcategory is allowed for ALL types, including World/City
		// and mixed selections. MapID and XYZ remain excluded.
		opts = append(opts, "Subcategory")
		if v, ok := w.chooseValue(R(180, 108, 355, 34), opts, bulkFieldCaption(w.form["Field"])); ok {
			if v == "Note" {
				v = "Comment"
			} // display name only; TSV stays compatible
			w.form["Field"] = v
			w.form["Value"] = ""
			w.focus = ""
		}
		redraw(w)
		return
	}
	if hit(x, y, R(180, 166, 355, 34)) {
		switch w.form["Field"] {
		case "Type":
			if v, ok := w.chooseValue(R(180, 166, 355, 34), catalogCategoryOptions(), w.form["Value"]); ok {
				w.form["Value"] = v
			}
			w.focus = ""
		case "Faction":
			if v, ok := w.chooseValue(R(180, 166, 355, 34), factionLabels, w.form["Value"]); ok {
				w.form["Value"] = v
			}
			w.focus = ""
		case "Subcategory":
			if v, ok := w.chooseValue(R(180, 166, 355, 34), subcategoryOptions(), w.form["Value"]); ok {
				w.form["Value"] = v
			}
			w.focus = ""
		default:
			w.focus = "Value"
			w.selectAll = false
			w.caretVisible = true
			w.caretPos = w.caretAt(R(180, 166, 355, 34), w.form["Value"], x)
		}
		redraw(w)
		return
	}
	if hit(x, y, R(190, 277, 142, 40)) {
		bulkCommit(w)
		return
	}
	if hit(x, y, R(352, 277, 142, 40)) {
		closePopup(w.hwnd)
	}
}
func bulkFieldCaption(field string) string {
	if field == "Comment" {
		return "Note"
	}
	return field
}
func catalogCategoryOptions() []string {
	return []string{"World", "City", "Dungeon", "Raid", "Battleground"}
}
func subcategoryOptions() []string {
	return []string{"", "Boss", "Boss (Must Summon)", "Boss (Roams Around)", "Rare", "Door / Passage / Other Point of Interest", "Quest", "Vendor/Repair", "Exit"}
}
func bulkSelectionKind() string {
	world, instance := false, false
	for _, r := range app.rows {
		if !app.selectedIDs[r.ID] {
			continue
		}
		if catalog.WorldMap(r.MapID) {
			world = true
		} else {
			instance = true
		}
	}
	if world && !instance {
		return "world"
	}
	if instance && !world {
		return "instance"
	}
	return "mixed"
}
func bulkCommit(w *uiWindow) {
	field := w.form["Field"]
	switch field {
	case "Minimum Level":
		field = "MinLevel"
	case "Type":
		field = "Category"
	}
	value := w.form["Value"]
	if field == "Faction" {
		value = factionCode(value)
	}
	if field == "Category" && value == "" {
		messageBox(w.hwnd, "Choose a Type first", "Bulk edit", MB_ICONWARNING)
		return
	}
	proposed, e := catalogBulkSet(field, value)
	if e != nil {
		messageBox(w.hwnd, e.Error(), "Bulk edit", MB_ICONWARNING)
		return
	}
	if e = app.commitCatalog(proposed); e != nil {
		messageBox(w.hwnd, e.Error(), "Bulk edit persistence failed", MB_ICONWARNING)
		return
	}
	app.status = "Bulk edit saved to working teleports.tsv"
	closePopup(w.hwnd)
	mainRedraw()
}

func catalogBulkSet(field, value string) ([]catalog.Record, error) {
	return catalog.BulkSet(app.rows, app.selectedIDs, field, value)
}
