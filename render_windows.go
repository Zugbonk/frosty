//go:build windows

package main

import (
	"fmt"
	"github.com/zugbonk/frosty/internal/teleporttable"
	"image"
	"image/color"
	"image/draw"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/zugbonk/frosty/internal/layout"
)

// Every coordinate in this file is in immutable UI-design space. On resize
// the finished frame is scaled to the client viewport, with no reposition
// loops, hidden legacy controls, timers, or expensive work while idle.

var futureSlots = layout.Reserved // declarative slots; intentionally not painted until activated

type uiItem struct {
	id, label string
	box       rect
}

func mainButtons() []uiItem {
	return []uiItem{
		// Thirty-two pixel top buttons preserve room for the coordinate row.
		{"forward", "TP 1 Yard Forward", R(379, 35, 167, 33)},
		{"corpse", "TP to Corpse", R(556, 35, 137, 33)},
		{"target", "TP Behind Target", R(702, 35, 169, 33)},
		{"coords", "TP to Cords", R(816, 72, 132, 30)},
		{"add", "Add location", R(381, 187, 122, 33)},
		{"edit", "Edit selected", R(514, 187, 127, 33)},
		{"delete", "Delete selected", R(649, 187, 139, 33)},
		{"import", "Import", R(384, 817, 92, 35)},
		{"export", "Export", R(481, 817, 92, 35)},
		{"defaults", "Defaults", R(578, 817, 99, 35)},
		{"teleport", "TELEPORT", R(1166, 817, 121, 35)},
		{"selector", "No WoW", R(1336, 436, 192, 31)},
		{"donations", "Donations", R(1336, 479, 192, 32)},
		{"contacts", "Contacts", R(1336, 513, 192, 32)},
		{"howto", "HowTo", R(1387, 546, 88, 30)},
		{"about", "About", R(1387, 577, 88, 30)},
	}
}

func mainIcons() []uiItem {
	out := []uiItem{}
	objs := []struct {
		k    string
		x, y int
	}{{"herb", 100, 77}, {"mine", 100, 129}, {"treasure", 100, 183}, {"fish", 100, 237}}
	for _, o := range objs {
		out = append(out, uiItem{o.k, o.k, R(o.x, o.y, 58, 53)})
	}
	out = append(out, uiItem{"object_other", "All Other", R(85, 287, 110, 26)})
	movement := []struct {
		k    string
		x, y int
	}{{"run", 213, 88}, {"fly", 213, 146}, {"swim", 213, 201}, {"water", 213, 260}, {"thru", 281, 260}, {"wall", 213, 313}, {"airjump", 281, 313}}
	for _, o := range movement {
		out = append(out, uiItem{o.k, o.k, R(o.x, o.y, 48, 58)})
	}
	names := []string{"repair", "food", "poison", "ammo", "reagents", "innkeeper", "flightmaster", "battlemaster", "trainer", "profession", "auctioneer", "banker", "mailbox", "lowquests"}
	for i, k := range names {
		out = append(out, uiItem{k, k, R(161, 337+i*32, 35, 31)})
	}
	creatures := []struct {
		k    string
		x, y int
	}{{"beasts", 224, 459}, {"dragons", 292, 459}, {"demons", 224, 514}, {"elementals", 292, 514}, {"giants", 224, 570}, {"undead", 292, 570}, {"human", 224, 626}, {"critters", 292, 626}, {"mechanical", 224, 682}, {"totems", 292, 682}, {"pets", 224, 736}}
	for _, o := range creatures {
		out = append(out, uiItem{o.k, o.k, R(o.x, o.y, 43, 56)})
	}
	out = append(out, uiItem{"creature_other", "All Other", R(276, 760, 63, 28)})
	return out
}
func editorShowsSubcategory(values map[string]string) bool {
	return layout.EditorShowsSubcategory(values["Category"])
}
func formFields(values map[string]string) []uiItem {
	fields := []uiItem{
		{"Location", "Location", R(117, 13, 510, 29)}, {"Zone", "Zone", R(117, 57, 510, 27)},
		{"MapID", "MapID", R(117, 100, 96, 26)}, {"X", "X", R(256, 100, 95, 26)}, {"Y", "Y", R(400, 100, 93, 26)}, {"Z", "Z", R(537, 100, 92, 26)},
		{"Category", "Category", R(117, 142, 175, 27)},
		{"MinLevel", "MinLevel", R(117, 184, 101, 26)}, {"Comment", "NOTE", R(117, 223, 510, 27)},
	}
	// Editing the Type instantly switches the second selector, independently
	// of the MapID input. The list view's live map-based column is separate.
	if editorShowsSubcategory(values) {
		fields = append(fields, uiItem{"Subcategory", "Subcategory", R(397, 142, 235, 27)})
	} else {
		// World and City need BOTH fields. Keep Faction at the old
		// position for filtering/color, and place optional Subcategory
		// beside Minimum Level without growing the dialog.
		fields = append(fields, uiItem{"Faction", "Faction", R(397, 142, 235, 27)})
		fields = append(fields, uiItem{"Subcategory", "Subcategory", R(397, 184, 235, 27)})
	}
	return fields
}
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func (w *uiWindow) paint() {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer pEndPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))
	var c rect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&c)))
	aw, ah := int(c.Right), int(c.Bottom)
	if aw < 1 || ah < 1 {
		return
	}
	if w.view == "main" {
		w.prepareOuter(aw, ah)
	}
	if w.dirty || len(w.bgra) == 0 {
		w.rebuildSurface()
	}
	if w.memDC == 0 {
		w.memDC, _, _ = pCreateCompatibleDC.Call(hdc)
		w.memBitmap, _, _ = pCreateCompatibleBitmap.Call(hdc, uintptr(w.width), uintptr(w.height))
		w.oldBitmap, _, _ = pSelectObject.Call(w.memDC, w.memBitmap)
	}
	info := bitmapInfo{Header: bmpInfoHeader{Size: 40, Width: int32(w.width), Height: -int32(w.height), Planes: 1, BitCount: 32}}
	if len(w.bgra) != 0 {
		pStretchDIBits.Call(w.memDC, 0, 0, uintptr(w.width), uintptr(w.height), 0, 0, uintptr(w.width), uintptr(w.height), uintptr(unsafe.Pointer(&w.bgra[0])), uintptr(unsafe.Pointer(&info)), DIB_RGB_COLORS, SRCCOPY)
	}
	pSetBkMode.Call(w.memDC, TRANSPARENT)
	switch w.view {
	case "agreement":
		w.paintAgreement()
	case "theme":
		w.paintChooser()
	case "main":
		w.paintMain()
		w.paintHoverTooltip()
	case "add", "edit":
		w.paintEditor()
	case "bulk":
		w.paintBulk()
	case "delete", "defaults":
		w.paintConfirm()
	case "donations", "contacts", "howto", "about", "qr":
		w.paintInfo()
	case "float":
		// The original logo graphic contains its own details; no overlaid text.
	case "menu":
		w.paintMenuText()
	case "float-tip":
		// Dedicated black tooltip, always readable regardless of theme or UI visibility.
		w.txt("Left-click: Show or hide Zugbonk", 11, 6, floatTipWidth-20, 23, white, 3)
		w.txt("Right-drag: Move the floating icon", 11, 31, floatTipWidth-20, 23, white, 0)
	}
	ox, oy, vw, vh := w.contentViewport(aw, ah)
	if w.view == "main" {
		// Composite both layers into ONE cached, full-client offscreen DC.
		// Exactly one blit touches the visible window (no repaint flash).
		if w.bgDC == 0 {
			w.bgDC, _, _ = pCreateCompatibleDC.Call(hdc)
		}
		if w.bgBitmap == 0 || w.bgBitmapWidth != aw || w.bgBitmapHeight != ah {
			if w.bgBitmap != 0 {
				pSelectObject.Call(w.bgDC, w.bgOld)
				pDeleteObject.Call(w.bgBitmap)
			}
			w.bgBitmap, _, _ = pCreateCompatibleBitmap.Call(hdc, uintptr(aw), uintptr(ah))
			w.bgOld, _, _ = pSelectObject.Call(w.bgDC, w.bgBitmap)
			w.bgBitmapWidth, w.bgBitmapHeight = aw, ah
		}
		if len(w.outerBGRA) != 0 {
			bi := bitmapInfo{Header: bmpInfoHeader{Size: 40, Width: int32(aw), Height: -int32(ah), Planes: 1, BitCount: 32}}
			pStretchDIBits.Call(w.bgDC, 0, 0, uintptr(aw), uintptr(ah), 0, 0, uintptr(aw), uintptr(ah), uintptr(unsafe.Pointer(&w.outerBGRA[0])), uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS, SRCCOPY)
		}
		if vw > 0 && vh > 0 {
			pSetStretchBltMode.Call(w.bgDC, HALFTONE)
			pStretchBlt.Call(w.bgDC, uintptr(ox), uintptr(oy), uintptr(vw), uintptr(vh), w.memDC, 0, 0, uintptr(w.width), uintptr(w.height), SRCCOPY)
		}
		pBitBlt.Call(hdc, 0, 0, uintptr(aw), uintptr(ah), w.bgDC, 0, 0, SRCCOPY)
	} else if vw > 0 && vh > 0 {
		pSetStretchBltMode.Call(hdc, HALFTONE)
		pStretchBlt.Call(hdc, uintptr(ox), uintptr(oy), uintptr(vw), uintptr(vh), w.memDC, 0, 0, uintptr(w.width), uintptr(w.height), SRCCOPY)
	}
}

// Sample the background once per outer-client-size change. The image is
// centered and cropped to fill (not stretched or letterboxed). The inner
// scene samples that SAME background beneath transparent panel artwork.
func (w *uiWindow) prepareOuter(aw, ah int) {
	if w.outerWidth == aw && w.outerHeight == ah && len(w.outerBGRA) != 0 {
		return
	}
	w.outerWidth, w.outerHeight = aw, ah
	w.dirty = true
	bg := image.NewRGBA(image.Rect(0, 0, aw, ah))
	if app.theme == "simple" {
		// Flat/simple outer box: NO scenery image in either layer.
		fillOpaque(bg, 0, 0, aw, ah, 19, 35, 52)
	} else {
		fillOpaque(bg, 0, 0, aw, ah, 8, 18, 37)
	}
	if src := app.images["aurora.png"]; app.theme != "simple" && src != nil {
		sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
		if sw > 0 && sh > 0 {
			// Cover with center cropping, preserving the original artwork ratio.
			sourceW, sourceH := sw, sh
			if int64(sw)*int64(ah) > int64(sh)*int64(aw) {
				sourceW = sh * aw / ah
			} else {
				sourceH = sw * ah / aw
			}
			x0, y0 := (sw-sourceW)/2, (sh-sourceH)/2
			for y := 0; y < ah; y++ {
				for x := 0; x < aw; x++ {
					sx := src.Bounds().Min.X + x0 + x*sourceW/aw
					sy := src.Bounds().Min.Y + y0 + y*sourceH/ah
					r, g, b, _ := src.At(sx, sy).RGBA()
					k := bg.PixOffset(x, y)
					bg.Pix[k], bg.Pix[k+1], bg.Pix[k+2], bg.Pix[k+3] = uint8(r>>8), uint8(g>>8), uint8(b>>8), 255
				}
			}
		}
	}
	w.outerRGBA = bg
	w.outerBGRA = rgbaToBGRA(bg)
}
func rgbaToBGRA(img *image.RGBA) []byte {
	out := make([]byte, len(img.Pix))
	for i := 0; i < len(out); i += 4 {
		out[i], out[i+1], out[i+2], out[i+3] = img.Pix[i+2], img.Pix[i+1], img.Pix[i], 0
	}
	return out
}
func (w *uiWindow) sampleInnerBackground(dst *image.RGBA) {
	if w.outerRGBA == nil {
		return
	}
	ox, oy, vw, vh := w.contentViewport(w.outerWidth, w.outerHeight)
	if vw < 1 || vh < 1 {
		return
	}
	for y := 0; y < w.height; y++ {
		sy := clamp(oy+y*vh/w.height, 0, w.outerHeight-1)
		for x := 0; x < w.width; x++ {
			sx := clamp(ox+x*vw/w.width, 0, w.outerWidth-1)
			i := dst.PixOffset(x, y)
			j := w.outerRGBA.PixOffset(sx, sy)
			copy(dst.Pix[i:i+4], w.outerRGBA.Pix[j:j+4])
		}
	}
}
func initFonts() {
	// Segoe UI for controls and Georgia bold for the original gold fantasy headings.
	gFonts[0] = makeFont(14, 400, "Segoe UI")
	gFonts[1] = makeFont(16, 700, "Georgia")
	gFonts[2] = makeFont(12, 400, "Segoe UI")
	gFonts[3] = makeFont(13, 700, "Segoe UI")
	gFonts[4] = makeFont(20, 700, "Georgia")
	gFonts[5] = makeFont(12, 700, "Georgia")
	gFonts[6] = makeFont(12, 400, "Segoe UI")
	gFonts[7] = makeFont(10, 400, "Segoe UI")
	gFonts[8] = makeFont(13, 600, "Tahoma") // closer to native previous production application ListView legibility
}
func makeFont(size int, weight int, face string) uintptr {
	r, _, _ := pCreateFont.Call(uintptr(int32(-size)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16(face))))
	return r
}
func (w *uiWindow) label(s string, r rect, c color.RGBA, font int, flags uint32) {
	if s == "" {
		return
	}
	pSelectObject.Call(w.memDC, gFonts[font])
	pSetTextColor.Call(w.memDC, rgb(c.R, c.G, c.B))
	str, _ := syscall.UTF16FromString(s)
	if len(str) == 0 {
		return
	}
	pDrawText.Call(w.memDC, uintptr(unsafe.Pointer(&str[0])), uintptr(len(str)-1), uintptr(unsafe.Pointer(&r)), uintptr(flags|DT_NOPREFIX))
}
func (w *uiWindow) txt(s string, x, y, ww, hh int, c color.RGBA, font int) {
	w.label(s, R(x, y, ww, hh), c, font, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}
func (w *uiWindow) center(s string, b rect, c color.RGBA, font int) {
	w.label(s, b, c, font, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

var (
	gold    = color.RGBA{255, 218, 36, 255}
	ice     = color.RGBA{188, 223, 255, 255}
	white   = color.RGBA{243, 246, 253, 255}
	green   = color.RGBA{25, 255, 11, 255}
	magenta = color.RGBA{226, 15, 255, 255}
)

func (w *uiWindow) button(id, text string, box rect) {
	if id == "selector" {
		text = app.clientSummary()
	}
	c := gold
	if id == "capture" && tpXYZDisabled() {
		c = ice
	}
	if app.theme == "simple" && w.view != "agreement" && w.view != "theme" {
		c = white
	}
	// Golden captions belong inside the button artwork, including the tiny TP controls.
	inset := R(int(box.Left)+7, int(box.Top)+5, int(box.Right-box.Left)-14, int(box.Bottom-box.Top)-10)
	if id == "selector" {
		inset.Right -= 17
	}
	w.center(text, inset, c, 5)
	if id == "selector" {
		w.txt("▼", int(box.Right)-20, int(box.Top)+4, 14, int(box.Bottom-box.Top)-8, c, 2)
	}
}

// Measure text using the same Segoe UI font as the multiplier value so the
// blinking caret lands after the actual glyph, not a hard-coded character box.
type textPixelSize struct{ CX, CY int32 }

func (w *uiWindow) speedTextWidth(text string) int {
	if text == "" || w.memDC == 0 {
		return 0
	}
	pSelectObject.Call(w.memDC, gFonts[0])
	chars, _ := syscall.UTF16FromString(text)
	var sz textPixelSize
	ret, _, _ := pGetTextExtentPoint32.Call(w.memDC, uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)-1), uintptr(unsafe.Pointer(&sz)))
	if ret == 0 {
		return len([]rune(text)) * 8
	}
	return int(sz.CX)
}
func (w *uiWindow) speedCaretFromX(key string, x int) int {
	text := []rune(w.getField(key))
	left := int(speedFieldRect(key).Left) + 5
	for i := 0; i < len(text); i++ {
		before := w.speedTextWidth(string(text[:i]))
		after := w.speedTextWidth(string(text[:i+1]))
		if x < left+(before+after)/2 {
			return i
		}
	}
	return len(text)
}
func (w *uiWindow) paintSpeedCaret(key string, field rect) {
	if w.selectAll {
		return
	} // the selected text is highlighted instead
	chars := []rune(w.getField(key))
	at := clamp(w.caretPos, 0, len(chars))
	x := int(field.Left) + 5 + w.speedTextWidth(string(chars[:at]))
	x = clamp(x, int(field.Left)+3, int(field.Right)-3)
	y := int(field.Top) + 5
	line := R(x, y, 1, int(field.Bottom-field.Top)-10)
	brush, _, _ := pCreateSolidBrush.Call(rgb(242, 252, 255))
	if brush != 0 {
		pFillRect.Call(w.memDC, uintptr(unsafe.Pointer(&line)), brush)
		pDeleteObject.Call(brush)
	}
}

// Selection highlighting is shared by every editable field. Draw it into the
// existing offscreen Win32 surface, never as a new native control or layout.
func (w *uiWindow) paintTextSelection(key string, b rect) {
	if w.focus != key || !w.selectAll || w.memDC == 0 {
		return
	}
	value := w.getField(key)
	if value == "" {
		return
	}
	x := int(b.Left) + 5
	width := w.speedTextWidth(value) + 2
	maxWidth := int(b.Right) - x - 5
	if width > maxWidth {
		width = maxWidth
	}
	if width <= 0 {
		return
	}
	selection := R(x, int(b.Top)+3, width, int(b.Bottom-b.Top)-6)
	brush, _, _ := pCreateSolidBrush.Call(rgb(45, 103, 178))
	if brush != 0 {
		pFillRect.Call(w.memDC, uintptr(unsafe.Pointer(&selection)), brush)
		pDeleteObject.Call(brush)
	}
}
func (w *uiWindow) input(key string, b rect) {
	w.paintTextSelection(key, b)
	w.txt(w.getField(key), int(b.Left)+5, int(b.Top), int(b.Right-b.Left)-13, int(b.Bottom-b.Top), white, 0)
	w.paintGenericCaret(key, b)
}
func (w *uiWindow) paintMain() {
	w.center("TRACKING", R(76, 41, 121, 24), gold, 1)
	w.center("OBJECTS", R(76, 63, 121, 22), gold, 1)
	w.center("MOVEMENT HACKS", R(203, 40, 134, 24), gold, 3)
	otherColor := gold
	if app.toggles["object_other"] {
		otherColor = green
	}
	if app.theme != "simple" {
		w.txt("All Other", 92, 292, 115, 20, otherColor, 5)
	}
	// No tiny Herbs/Mining/Treasure/Fishing text beside Modern sprites.
	// The Simple theme paints their names as the whole controls below.
	// Two centered heading lines fit the original panel without cropping.
	w.center("DEFAULT", R(62, 312, 135, 14), gold, 5)
	w.center("TRACKING", R(62, 326, 135, 13), gold, 5)
	w.center("CREATURES", R(215, 417, 125, 20), gold, 1)
	tracks := []struct {
		label string
		id    string
	}{{"Repair", "repair"}, {"Food_Drink", "food"}, {"Poisons", "poison"}, {"Ammunition", "ammo"}, {"Reagents", "reagents"}, {"Innkeeper", "innkeeper"}, {"FlightMaster", "flightmaster"}, {"BattleMaster", "battlemaster"}, {"Class Trainer", "trainer"}, {"Profession Trainers", "profession"}, {"Auctioneer", "auctioneer"}, {"Banker", "banker"}, {"Mailbox", "mailbox"}, {"Low Level Quests", "lowquests"}}
	for i, t := range tracks {
		if app.theme == "simple" {
			continue
		}
		col := gold
		if app.toggles[t.id] {
			col = green
		}
		font := 2
		if len(t.label) > 15 {
			font = 7
		}
		w.txt(t.label, 64, 340+i*32, 106, 25, col, font)
	}
	if app.theme != "simple" {
		for _, it := range []struct {
			s    string
			x, y int
		}{{"Run", 225, 130}, {"Fly", 225, 187}, {"Swim", 225, 243}, {"Walk Water", 201, 299}, {"Fly Thru", 281, 299}, {"Wall Climb", 201, 354}, {"AirJump", 285, 354}, {"Beasts", 223, 502}, {"Dragons", 286, 502}, {"Demons", 218, 557}, {"Elementals", 277, 557}, {"Giants", 224, 613}, {"Undead", 291, 613}, {"Humanoids", 208, 669}, {"Critters", 291, 669}, {"Mechanicals", 207, 725}, {"Totems", 291, 725}, {"N-C Pets", 215, 779}, {"All Other", 281, 779}} {
			c := gold
			key := strings.ToLower(it.s)
			switch key {
			case "walk water":
				key = "water"
			case "fly thru":
				key = "thru"
			case "wall climb":
				key = "wall"
			case "humanoids":
				key = "human"
			case "mechanicals":
				key = "mechanical"
			case "n-c pets":
				key = "pets"
			case "all other":
				key = "creature_other"
			}
			if app.toggles[key] {
				c = green
			}
			w.txt(it.s, it.x, it.y, 67, 17, c, 2)
		}
	} else {
		w.paintSimpleLeftLabels()
	}
	for _, key := range []string{"run_speed", "fly_speed", "swim_speed"} {
		r := speedFieldRect(key)
		y := int(r.Top) + 3
		w.txt("x", speedFieldLabelX(), y, 13, 23, gold, 3)
		w.paintTextSelection(key, r)
		w.txt(app.text[key], int(r.Left)+5, y, int(r.Right-r.Left)-8, 23, white, 0)
		if w.focus == key && w.caretVisible {
			w.paintSpeedCaret(key, r)
		}
	}
	for _, btn := range mainButtons() {
		w.button(btn.id, btn.label, btn.box)
	}
	w.txt("X", 411, 70, 15, 25, gold, 1)
	w.txt("Y", 552, 70, 15, 25, gold, 1)
	w.txt("Z", 694, 70, 15, 25, gold, 1)
	for _, entry := range []struct {
		key string
		r   rect
	}{{"x", R(425, 70, 97, 24)}, {"y", R(569, 70, 97, 24)}, {"z", R(712, 70, 96, 24)}} {
		w.input(entry.key, entry.r)
	}
	// Real themed button, not standalone gold text: uses the same approved
	// Modern green texture or Simple blue flat button as other TP actions.
	w.button("capture", "*", R(379, 71, 31, 28))
	w.txt("Search", 372, 103, 61, 23, gold, 1)
	w.input("search", R(432, 104, 857, 21))
	for _, e := range []struct {
		key, label                string
		labelX, fieldX, y, fieldW int
	}{
		{"zone", "Zone", 373, 411, 132, 274},
		{"faction", "Faction", 688, 741, 132, 274},
		{"type", "Type", 373, 411, 159, 274},
		{"sort", "Sort", 693, 732, 159, 255},
	} {
		w.txt(e.label, e.labelX, e.y, e.fieldX-e.labelX-2, 23, gold, 3)
		w.txt(app.text[e.key], e.fieldX+6, e.y, e.fieldW-25, 22, white, 0)
		w.txt("▼", e.fieldX+e.fieldW-19, e.y, 17, 22, ice, 2)
	}
	countText := strconv.Itoa(len(app.visible)) + " of " + strconv.Itoa(len(app.rows)) + " locations"
	if app.activeMapID >= 0 {
		countText += " | MapID " + strconv.Itoa(app.activeMapID)
	} else if !app.listBlocked {
		countText += " | Offline catalog preview"
	}
	if app.selectedCount() > 1 {
		countText += " | " + strconv.Itoa(app.selectedCount()) + " selected"
	}
	w.txt(countText, 806, 194, 464, 24, ice, 0)
	cols := []struct {
		s     string
		x, ww int
	}{{"Location", 376, 238}, {"Zone", 623, 142}, {"Level", 781, 52}, {"Subcategory", 840, 152}, {"Type", 999, 77}, {"Notes", 1083, 177}}
	for _, c := range cols {
		w.txt(c.s, c.x, 239, c.ww, 19, gold, 3)
	}
	if app.listBlocked {
		w.txt(app.listBlockReason, 393, 276, 865, 30, gold, 3)
	}
	for k := 0; k < 34; k++ {
		index := app.scroll + k
		if index >= len(app.visible) {
			break
		}
		r := app.visible[index]
		y := 258 + k*16
		if y >= 795 {
			break
		}
		c := teleporttable.RowColor(r.Category, r.Faction)
		// The selected row has a dark translucent background in the cached raster.
		w.txt(r.Location, 379, y, 235, 16, c, 8)
		w.txt(r.Zone, 625, y, 143, 16, c, 8)
		w.txt(r.MinLevel, 789, y, 44, 16, c, 8)
		// Faction is deliberately NOT displayed: row color conveys it.
		// The full 12-column record, faction filter and color logic remain.
		w.txt(r.Subcategory, 842, y, 150, 16, c, 8)
		w.txt(r.Category, 1000, y, 76, 16, c, 8)
		w.txt(r.Comment, 1085, y, 176, 16, c, 8)
	}
	if app.status != "" {
		w.txt(app.status, 687, 819, 469, 27, ice, 2)
	}
	w.center("Game Selector", R(1355, 406, 167, 25), gold, 1)
	// Stage16 content is optional; gameplay remains independent of the network.
	// No overlay on top of the footer controls.
}
func (w *uiWindow) tooltipBounds() rect {
	if w.hover == "" || w.view != "main" || hoverHelp(w.hover) == "" {
		return rect{}
	}
	lines := strings.Split(hoverHelp(w.hover), "\n")
	maxLine := 0
	for _, line := range lines {
		if len([]rune(line)) > maxLine {
			maxLine = len([]rune(line))
		}
	}
	width := clamp(maxLine*8+26, 130, 315)
	height := 7 + 23*len(lines)
	x := clamp(int(w.hoverRect.Right)+12, 65, 1590-width-12)
	y := clamp(int(w.hoverRect.Top), 40, 860-height-10)
	return R(x, y, width, height)
}
func (w *uiWindow) paintHoverTooltip() {
	if w.hover == "" {
		return
	}
	b := w.tooltipBounds()
	if b.Right <= b.Left {
		return
	}
	// Paint directly in the final GDI layer. No transparency, no image tint.
	black, _, _ := pCreateSolidBrush.Call(rgb(0, 0, 0))
	if black != 0 {
		pFillRect.Call(w.memDC, uintptr(unsafe.Pointer(&b)), black)
		pDeleteObject.Call(black)
	}
	border, _, _ := pCreateSolidBrush.Call(rgb(185, 215, 244))
	if border != 0 {
		pFrameRect.Call(w.memDC, uintptr(unsafe.Pointer(&b)), border)
		pDeleteObject.Call(border)
	}
	for i, line := range strings.Split(hoverHelp(w.hover), "\n") {
		w.txt(line, int(b.Left)+9, int(b.Top)+3+i*23, int(b.Right-b.Left)-16, 22, white, 0)
	}
}
func (w *uiWindow) paintAgreement() {
	w.txt("FROSTY ZUGBONK", 26, 43, 260, 29, gold, 1)
	w.txt("USER AGREEMENT", 26, 87, 300, 20, gold, 3)
	sections := []struct {
		head  string
		lines []string
		y     int
	}{
		{"ACCOUNT - SERVER RULES", []string{"Zugbonk provides additional features for World of Warcraft 3.3.5a.", "Using them may violate the rules of the server you play on.", "If detected, your account may be suspended or permanently banned,", "depending on the server's rules. You understand and accept this risk."}, 121},
		{"FREE SOFTWARE - OPTIONAL SUPPORT", []string{"Zugbonk is free to use. Donations are welcome but entirely optional.", "No payment is required to use its features."}, 224},
		{"OPTIONAL ONLINE INFORMATION", []string{"One Zugbonk banner may display news about related projects.", "Internet access is optional. All gameplay tools work offline.", "Online access lets Zugbonk fetch About, HowTo and contact info.", "Cloudflare may see standard request metadata when contacted.", "Other sites open only if you click a link or banner."}, 291},
		{"ADDITIONAL INFORMATION", []string{"Zugbonk does not modify installed WoW files. Some features interact", "with the running game and may not always work as expected.", "This is an independent, unofficial companion, not endorsed by", "Blizzard Entertainment or any game server."}, 417},
	}
	for _, p := range sections {
		w.txt(p.head, 26, p.y, 600, 20, gold, 3)
		for i, line := range p.lines {
			w.txt(line, 26, p.y+21+i*19, 621, 20, white, 0)
		}
	}
	w.txt("I AGREE acknowledges these conditions. DISAGREE closes the app.", 26, 513, 600, 20, white, 0)
	w.button("agree", "I AGREE", R(183, 576, 138, 33))
	w.button("disagree", "DISAGREE / EXIT", R(345, 576, 164, 33))
}
func (w *uiWindow) paintChooser() {
	w.txt("CHOOSE YOUR DEFAULT THEME", 24, 43, 480, 29, gold, 1)
	w.txt("You can change the theme later from the small palette button in the main window.", 24, 72, 732, 23, white, 0)
	w.center("Simple", R(132, 340, 145, 40), white, 3)
	w.center("Modern", R(533, 340, 137, 40), gold, 5)
}
func (w *uiWindow) paintEditor() {
	labelColor := gold
	if app.theme == "simple" {
		labelColor = ice
	}
	for _, label := range []struct {
		s        string
		x, y, ww int
	}{{"Location *", 17, 12, 99}, {"Zone *", 17, 56, 99}, {"MapID *", 17, 100, 99}, {"X *", 225, 100, 35}, {"Y *", 366, 100, 35}, {"Z *", 505, 100, 34}, {"Type", 17, 142, 95}, {"Minimum level", 17, 183, 95}, {"NOTE", 17, 222, 95}} {
		w.txt(label.s, label.x, label.y, label.ww, 30, labelColor, 3)
	}
	if editorShowsSubcategory(w.form) {
		w.txt("Subcategory", 308, 142, 85, 26, labelColor, 2)
	} else {
		w.txt("Faction", 315, 142, 78, 26, labelColor, 2)
		w.txt("Subcategory", 308, 184, 85, 26, labelColor, 2)
	}
	for _, f := range formFields(w.form) {
		value := w.form[f.id]
		isCombo := f.id == "Category" || f.id == "Faction" || f.id == "Subcategory"
		if f.id == "Faction" {
			value = factionCaption(value)
		}
		avail := int(f.box.Right-f.box.Left) - 9
		if isCombo {
			avail -= 22
		}
		if !isCombo {
			w.paintTextSelection(f.id, f.box)
		}
		w.txt(value, int(f.box.Left)+5, int(f.box.Top), avail, int(f.box.Bottom-f.box.Top), white, 0)
		if !isCombo {
			w.paintGenericCaret(f.id, f.box)
		}
		if isCombo {
			w.txt("▼", int(f.box.Right)-22, int(f.box.Top)+2, 17, int(f.box.Bottom-f.box.Top)-3, ice, 2)
		}
	}
	if editorShowsSubcategory(w.form) {
		w.txt("Optional: no minimum = blank", 253, 183, 324, 26, gold, 0)
	}
	w.txt("Faction sets World/City colors; Subcategory is optional for every type.", 24, 256, 593, 25, gold, 2)
	w.button("save", "Save location", R(201, 300, 144, 40))
	w.button("cancel", "Cancel", R(355, 300, 104, 40))
}

// Owned bulk edit dialog, independent of the frozen main window layout.
func (w *uiWindow) paintBulk() {
	tone := gold
	if app.theme == "simple" {
		tone = ice
	}
	w.center("BULK EDIT TELEPORT LOCATIONS", R(30, 17, 585, 36), tone, 1)
	w.txt(strconv.Itoa(app.selectedCount())+" selected records; only the chosen field changes.", 45, 61, 560, 25, white, 0)
	w.txt("Field", 65, 109, 110, 30, tone, 3)
	fieldCaption := w.form["Field"]
	if fieldCaption == "Comment" {
		fieldCaption = "Note"
	}
	w.txt(fieldCaption, 185, 109, 345, 31, white, 0)
	w.txt("▼", 511, 109, 22, 31, ice, 3)
	w.txt("New value", 65, 166, 112, 31, tone, 3)
	if w.form["Field"] != "Type" && w.form["Field"] != "Faction" && w.form["Field"] != "Subcategory" {
		w.paintTextSelection("Value", R(180, 166, 355, 34))
	}
	w.txt(w.form["Value"], 185, 166, 340, 31, white, 0)
	if w.form["Field"] == "Type" || w.form["Field"] == "Faction" || w.form["Field"] == "Subcategory" {
		w.txt("▼", 511, 166, 22, 31, ice, 3)
	} else {
		w.paintGenericCaret("Value", R(180, 166, 355, 34))
	}
	w.txt("MapID and X/Y/Z are never bulk-editable.", 64, 219, 500, 24, tone, 0)
	w.button("bulk-save", "Apply to selected", R(190, 277, 142, 40))
	w.button("bulk-cancel", "Cancel", R(352, 277, 142, 40))
}
func (w *uiWindow) paintGenericCaret(key string, b rect) {
	if w.focus != key || !w.caretVisible || w.selectAll {
		return
	}
	value := []rune(w.getField(key))
	n := clamp(w.caretPos, 0, len(value))
	x := int(b.Left) + 5 + w.speedTextWidth(string(value[:n]))
	x = clamp(x, int(b.Left)+4, int(b.Right)-3)
	line := R(x, int(b.Top)+4, 1, int(b.Bottom-b.Top)-8)
	h, _, _ := pCreateSolidBrush.Call(rgb(238, 248, 255))
	if h != 0 {
		pFillRect.Call(w.memDC, uintptr(unsafe.Pointer(&line)), h)
		pDeleteObject.Call(h)
	}
}
func (w *uiWindow) paintConfirm() {
	labelColor := gold
	if app.theme == "simple" {
		labelColor = ice
	}
	if w.view == "delete" {
		w.txt("DELETE SAVED LOCATION?", 33, 21, 530, 28, labelColor, 4)
		loc := "Selected location"
		for _, r := range app.rows {
			if r.ID == app.selectedID {
				loc = r.Location
				break
			}
		}
		if app.selectedCount() > 1 {
			loc = strconv.Itoa(app.selectedCount()) + " selected teleport locations"
		}
		w.txt(loc, 40, 96, 562, 27, labelColor, 1)
		w.txt("This removes the saved location and its MapID / XYZ coordinates.", 40, 137, 582, 23, white, 0)
		w.txt("A readable previous-version TSV backup will be kept.", 40, 168, 580, 23, white, 0)
		w.txt("Cancel is the safe default.", 40, 199, 580, 23, white, 0)
		w.button("delete", "Delete Location", R(181, 240, 142, 30))
	} else {
		w.txt("RESTORE DEFAULT TELEPORTS?", 33, 21, 552, 29, labelColor, 4)
		w.txt(fmt.Sprintf("Restore the %d embedded default locations?", len(app.defaults)), 40, 96, 565, 30, labelColor, 1)
		w.txt("Your current teleport list will be OVERWRITTEN.", 40, 137, 575, 23, white, 0)
		w.txt("Export first to keep your added or edited locations.", 40, 168, 572, 23, white, 0)
		w.txt("Cancel is the safe default.", 40, 199, 580, 23, white, 0)
		w.button("restore", "Restore Defaults", R(181, 240, 142, 30))
	}
	w.button("cancel", "Cancel", R(345, 240, 133, 30))
}

// The alpha-composited background is rasterized only when UI artwork/state
// changes. WM_PAINT draws text into an offscreen native GDI surface and blits
// the finished frame atomically, eliminating multi-stage startup flicker.
func (w *uiWindow) rebuildSurface() {
	ww, hh := w.width, w.height
	canvas := image.NewRGBA(image.Rect(0, 0, ww, hh))
	if w.view == "float" {
		fillOpaque(canvas, 0, 0, ww, hh, 255, 0, 255)
	} else if w.view == "float-tip" {
		fillOpaque(canvas, 0, 0, ww, hh, 0, 0, 0)
		outline(canvas, 0, 0, ww, hh, 165, 205, 240)
	} else if w.view == "main" {
		w.sampleInnerBackground(canvas)
	} else if w.view == "menu" {
		fillOpaque(canvas, 0, 0, ww, hh, 8, 28, 45)
	} else if w.view == "theme" {
		fillOpaque(canvas, 0, 0, ww, hh, 18, 34, 50)
	} else if app.theme == "simple" && w.view != "agreement" {
		// All secondary windows honor Simple; never paint Modern scenery.
		fillOpaque(canvas, 0, 0, ww, hh, 21, 40, 59)
	} else {
		putImage(canvas, 0, 0, ww, hh, app.images["aurora.png"])
	}
	switch w.view {
	case "main":
		w.sceneMain(canvas)
	case "menu":
		w.sceneMenu(canvas)
	case "agreement":
		w.sceneAgreement(canvas)
	case "theme":
		w.sceneChooser(canvas)
	case "add", "edit":
		w.sceneEditor(canvas)
	case "bulk":
		w.sceneBulk(canvas)
	case "delete", "defaults":
		w.sceneConfirm(canvas)
	case "donations", "contacts", "howto", "about", "qr":
		w.sceneInfo(canvas)
	case "float":
		putImage(canvas, 0, 0, ww, hh, app.images["floating_icon.png"])
	}
	w.bgra = make([]byte, ww*hh*4)
	for y := 0; y < hh; y++ {
		for x := 0; x < ww; x++ {
			p := canvas.PixOffset(x, y)
			o := (y*ww + x) * 4
			w.bgra[o] = canvas.Pix[p+2]
			w.bgra[o+1] = canvas.Pix[p+1]
			w.bgra[o+2] = canvas.Pix[p]
			w.bgra[o+3] = 0
		}
	}
	w.dirty = false
}
func fillOpaque(dst *image.RGBA, x, y, ww, hh int, r, g, b uint8) {
	draw.Draw(dst, image.Rect(x, y, x+ww, y+hh), &image.Uniform{C: color.RGBA{r, g, b, 255}}, image.Point{}, draw.Src)
}
func tint(dst *image.RGBA, x, y, ww, hh int, r, g, b, a uint8) {
	bounds := dst.Bounds()
	x0 := clamp(x, 0, bounds.Dx())
	y0 := clamp(y, 0, bounds.Dy())
	x1 := clamp(x+ww, 0, bounds.Dx())
	y1 := clamp(y+hh, 0, bounds.Dy())
	if x1 <= x0 || y1 <= y0 {
		return
	}
	alpha := int(a)
	inv := 255 - alpha
	for yy := y0; yy < y1; yy++ {
		for xx := x0; xx < x1; xx++ {
			i := dst.PixOffset(xx, yy)
			dst.Pix[i] = uint8((int(dst.Pix[i])*inv + int(r)*alpha) / 255)
			dst.Pix[i+1] = uint8((int(dst.Pix[i+1])*inv + int(g)*alpha) / 255)
			dst.Pix[i+2] = uint8((int(dst.Pix[i+2])*inv + int(b)*alpha) / 255)
		}
	}
}
func outline(dst *image.RGBA, x, y, ww, hh int, r, g, b uint8) {
	fillOpaque(dst, x, y, ww, 1, r, g, b)
	fillOpaque(dst, x, y+hh-1, ww, 1, r, g, b)
	fillOpaque(dst, x, y, 1, hh, r, g, b)
	fillOpaque(dst, x+ww-1, y, 1, hh, r, g, b)
}
func putImage(dst *image.RGBA, x, y, ww, hh int, src image.Image) {
	putImageBox(dst, x, y, ww, hh, src, false)
}
func putImageContain(dst *image.RGBA, x, y, ww, hh int, src image.Image) {
	putImageBox(dst, x, y, ww, hh, src, true)
}
func putImageBox(dst *image.RGBA, x, y, ww, hh int, src image.Image, keepAspect bool) {
	if src == nil || ww <= 0 || hh <= 0 {
		return
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw == 0 || sh == 0 {
		return
	}
	dx0, dy0, dww, dhh := x, y, ww, hh
	if keepAspect {
		if sw*hh > sh*ww {
			dhh = ww * sh / sw
			if dhh <= 0 {
				dhh = 1
			}
			dy0 = y + (hh-dhh)/2
		} else {
			dww = hh * sw / sh
			if dww <= 0 {
				dww = 1
			}
			dx0 = x + (ww-dww)/2
		}
	}
	for oy := 0; oy < dhh; oy++ {
		dy := dy0 + oy
		if dy < 0 || dy >= dst.Bounds().Dy() {
			continue
		}
		sy := src.Bounds().Min.Y + oy*sh/dhh
		for ox := 0; ox < dww; ox++ {
			dx := dx0 + ox
			if dx < 0 || dx >= dst.Bounds().Dx() {
				continue
			}
			sx := src.Bounds().Min.X + ox*sw/dww
			rr, gg, bb, aa := src.At(sx, sy).RGBA()
			a := int(aa >> 8)
			if a == 0 {
				continue
			}
			i := dst.PixOffset(dx, dy)
			ia := 255 - a
			dst.Pix[i] = uint8(clamp(int(rr>>8)+int(dst.Pix[i])*ia/255, 0, 255))
			dst.Pix[i+1] = uint8(clamp(int(gg>>8)+int(dst.Pix[i+1])*ia/255, 0, 255))
			dst.Pix[i+2] = uint8(clamp(int(bb>>8)+int(dst.Pix[i+2])*ia/255, 0, 255))
		}
	}
}
func paintSimpleButtonBack(dst *image.RGBA, r rect, pressed bool) {
	x, y, ww, hh := int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top)
	if pressed {
		fillOpaque(dst, x, y, ww, hh, 25, 72, 109)
	} else {
		fillOpaque(dst, x, y, ww, hh, 22, 49, 74)
	}
	outline(dst, x, y, ww, hh, 93, 151, 191)
	outline(dst, x+2, y+2, ww-4, hh-4, 31, 77, 112)
}
func paintThemeButtonBack(dst *image.RGBA, r rect, pressed bool) {
	if app.theme == "simple" {
		paintSimpleButtonBack(dst, r, pressed)
	} else {
		paintButtonBack(dst, r, pressed)
	}
}
func paintButtonBack(dst *image.RGBA, r rect, pressed bool) {
	im := "button.png"
	if pressed {
		im = "button_pressed.png"
	}
	putImage(dst, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), app.images[im])
}
func paintInputBack(dst *image.RGBA, r rect, selected bool) {
	fillOpaque(dst, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 7, 26, 46)
	if selected {
		outline(dst, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 101, 211, 255)
	} else {
		outline(dst, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 51, 79, 91)
	}
}
func paintFrame(dst *image.RGBA, r rect, full bool) {
	x, y := int(r.Left), int(r.Top)
	ww, hh := int(r.Right-r.Left), int(r.Bottom-r.Top)
	outline(dst, x, y, ww, hh, 212, 178, 53)
	outline(dst, x+3, y+3, ww-6, hh-6, 14, 135, 229)
	if full {
		outline(dst, x+7, y+7, ww-14, hh-14, 255, 193, 62)
	}
}
func (w *uiWindow) sceneMain(dst *image.RGBA) {
	simple := app.theme == "simple"
	left := R(59, 34, 286, 823)
	center := R(359, 34, 945, 823)
	right := R(1317, 34, 223, 823)
	// Three matching translucent panels (~80% darkening), still see-through.
	// Apply the tint to both Modern aurora and Simple flat-blue shell.
	for _, r := range []rect{left, center, right} {
		tint(dst, int(r.Left)+4, int(r.Top)+4, int(r.Right-r.Left)-8, int(r.Bottom-r.Top)-8, 3, 9, 20, 204)
	}
	paintFrame(dst, left, false)
	paintFrame(dst, center, false)
	paintFrame(dst, right, false)
	fillOpaque(dst, 198, 39, 1, 812, 152, 139, 98)
	// Modern uses original ON/OFF sprite art; Simple is text-only for
	// all trackers and movement hacks, as expressly requested.
	if !simple {
		for _, ic := range mainIcons() {

			state := "off"
			if app.toggles[ic.id] {
				state = "on"
			}
			asset := app.images[ic.id+"_"+state+".png"]
			if asset == nil {
				continue
			}
			h := int(ic.box.Bottom - ic.box.Top)
			width := int(ic.box.Right - ic.box.Left)
			left := int(ic.box.Left)
			if h > 55 {
				h = 42
				width = 42
				left += (int(ic.box.Right-ic.box.Left) - width) / 2
			}
			putImage(dst, left, int(ic.box.Top), width, h, asset)
		}
	}
	if simple {
		w.paintSimpleToggles(dst)
	}
	// Two future movement icon rectangles are reserved at x218 and x286, y368.
	// None are rendered or instantiated in this build.
	tint(dst, 1319, 41, 219, 280, 9, 14, 25, 175)
	paintFrame(dst, R(1321, 40, 214, 282), false)
	if remoteState.hasBanner && remoteState.bannerImage != nil {
		putImageContain(dst, 1330, 50, 192, 256, remoteState.bannerImage)
	} else {
		putImageContain(dst, 1330, 50, 192, 256, app.images["sidebar_fallback.png"])
	}
	// Original mascot art is a Modern-theme element only. Simple leaves
	// the reserved sidebar area blank without shifting any other controls.
	if !simple {
		putImage(dst, 1352, 686, 184, 168, app.images["mascot_original.png"])
	}
	// Existing capture hitbox; no layout movement.
	paintThemeButtonBack(dst, R(379, 71, 31, 28), false)
	for _, btn := range mainButtons() {
		if btn.id == "selector" {
			putImage(dst, int(btn.box.Left), int(btn.box.Top), int(btn.box.Right-btn.box.Left), int(btn.box.Bottom-btn.box.Top), app.images["button_selector.png"])
		} else if simple {
			paintSimpleButtonBack(dst, btn.box, false)
		} else {
			paintButtonBack(dst, btn.box, false)
		}
	}
	putImage(dst, 1261, 40, 29, 28, app.images["brush.png"])
	for _, r := range []rect{speedFieldRect("run_speed"), speedFieldRect("fly_speed"), speedFieldRect("swim_speed"), R(425, 70, 97, 24), R(568, 70, 97, 24), R(711, 70, 97, 24), R(432, 104, 858, 21), R(411, 133, 274, 22), R(741, 133, 274, 22), R(411, 160, 274, 22), R(732, 160, 255, 22)} {
		selected := w.focus == "search" && r.Top == 104
		if w.focus == "x" && r.Top == 70 && r.Left == 425 {
			selected = true
		}
		if w.focus == "y" && r.Top == 70 && r.Left == 568 {
			selected = true
		}
		if w.focus == "z" && r.Top == 70 && r.Left == 711 {
			selected = true
		}
		if isSpeedField(w.focus) && r == speedFieldRect(w.focus) {
			selected = true
		}
		paintInputBack(dst, r, selected)
	}
	// Three future teleport top-row buttons are allocated inside this clear band.
	// They never require restructuring the list view or moving existing buttons.
	tint(dst, 379, 222, 904, 17, 8, 16, 31, 100)
	fillOpaque(dst, 375, 238, 903, 19, 22, 29, 38)
	fillOpaque(dst, 375, 257, 903, 542, 35, 42, 53)
	for i := 0; i < 34; i++ {
		ix := app.scroll + i
		if ix >= len(app.visible) {
			break
		}
		y := 258 + i*16
		if app.selectedIDs[app.visible[ix].ID] {
			tint(dst, 376, y, 899, 16, 178, 206, 223, 88)
		}
	}
	fillOpaque(dst, teleporttable.TrackLeft, teleporttable.TrackTop, teleporttable.TrackWidth, teleporttable.TrackHeight, 9, 17, 30)
	thumbTop, thumbHeight := teleporttable.ThumbMetrics(app.scroll, len(app.visible))
	fillOpaque(dst, 1278, thumbTop, 7, thumbHeight, 182, 192, 202)
	// Tooltip background is drawn by GDI *after* all native text, so no
	// other label or icon can show through the fully opaque black box.
	// 3 extra right sidebar buttons have reserved ids. Visible stack remains centered.
	// The virtual rectangles are y605, y631, y657, but hidden controls cost zero paint.
}

// Simple theme: text-only tracker and movement controls. The same positions
// are used by simpleLeftUIHit; both captions and borders reflect ON/OFF.
func (w *uiWindow) paintSimpleToggles(dst *image.RGBA) {
	for _, ent := range simpleLeftEntries() {
		r := ent.box
		col := uint8(48)
		if app.toggles[ent.id] {
			col = 94
		}
		tint(dst, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 18, col, 68, 170)
		outline(dst, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 59, 103, 136)
	}
}
func simpleLeftEntries() []uiItem {
	out := make([]uiItem, 0, 37)
	for i, id := range []string{"herb", "mine", "treasure", "fish"} {
		out = append(out, uiItem{id, id, R(75, 88+i*53, 111, 27)})
	}
	out = append(out, uiItem{"object_other", "All Other", R(75, 287, 111, 27)})
	for i, id := range []string{"repair", "food", "poison", "ammo", "reagents", "innkeeper", "flightmaster", "battlemaster", "trainer", "profession", "auctioneer", "banker", "mailbox", "lowquests"} {
		out = append(out, uiItem{id, id, R(65, 336+i*32, 127, 28)})
	}
	for _, a := range []struct {
		id       string
		x, y, ww int
	}{
		{"run", 210, 122, 70}, {"fly", 210, 179, 70}, {"swim", 210, 235, 70},
		{"water", 202, 293, 72}, {"thru", 276, 293, 67}, {"wall", 202, 348, 72}, {"airjump", 276, 348, 67},
		{"beasts", 207, 493, 70}, {"dragons", 277, 493, 66}, {"demons", 207, 549, 70}, {"elementals", 277, 549, 66},
		{"giants", 207, 605, 70}, {"undead", 277, 605, 66}, {"human", 207, 661, 70}, {"critters", 277, 661, 66},
		{"mechanical", 207, 717, 70}, {"totems", 277, 717, 66}, {"pets", 207, 773, 70}, {"creature_other", 277, 773, 66},
	} {
		out = append(out, uiItem{a.id, a.id, R(a.x, a.y, a.ww, 27)})
	}
	return out
}
func (w *uiWindow) paintSimpleLeftLabels() {
	labels := map[string]string{
		"herb": "Herbs", "mine": "Mining", "treasure": "Treasure", "fish": "Fishing", "object_other": "All Other",
		"repair": "Repair", "food": "Food & Drink", "poison": "Poisons", "ammo": "Ammunition", "reagents": "Reagents",
		"innkeeper": "Innkeeper", "flightmaster": "FlightMaster", "battlemaster": "BattleMaster", "trainer": "Class Trainer",
		"profession": "Profession", "auctioneer": "Auctioneer", "banker": "Banker", "mailbox": "Mailbox", "lowquests": "Low Level Quests",
		"run": "Run", "fly": "Fly", "swim": "Swim", "water": "Walk Water", "thru": "Fly Thru", "wall": "Wall Climb", "airjump": "AirJump",
		"beasts": "Beasts", "dragons": "Dragons", "demons": "Demons", "elementals": "Elementals", "giants": "Giants", "undead": "Undead",
		"human": "Humanoids", "critters": "Critters", "mechanical": "Mechanicals", "totems": "Totems", "pets": "N-C Pets", "creature_other": "All Other",
	}
	for _, ent := range simpleLeftEntries() {
		c := gold
		if app.toggles[ent.id] {
			c = green
		}
		font := 2
		if len(labels[ent.id]) > 14 {
			font = 7
		}
		w.center(labels[ent.id], ent.box, c, font)
	}
}

func (w *uiWindow) sceneAgreement(dst *image.RGBA) {
	tint(dst, 0, 0, w.width, w.height, 0, 4, 25, 55)
	paintFrame(dst, R(5, 5, 664, 657), true)
	tint(dst, 20, 109, 638, 443, 16, 22, 31, 249)
	fillOpaque(dst, 19, 81, 638, 3, 5, 127, 239)
	fillOpaque(dst, 14, 551, 645, 2, 212, 168, 56)
	putImage(dst, 550, 447, 114, 113, app.images["mascot_original.png"])
	paintButtonBack(dst, R(183, 576, 138, 33), false)
	paintButtonBack(dst, R(345, 576, 164, 33), false)
}
func (w *uiWindow) sceneChooser(dst *image.RGBA) {
	fillOpaque(dst, 0, 0, w.width, w.height, 19, 34, 51)
	putImage(dst, 31, 108, 349, 218, app.images["preview_simple.png"])
	putImage(dst, 421, 108, 350, 218, app.images["preview_modern.png"])
	outline(dst, 30, 107, 351, 219, 108, 151, 179)
	outline(dst, 420, 107, 352, 219, 108, 151, 179)
	paintSimpleButtonBack(dst, R(132, 340, 145, 40), app.theme == "simple")
	paintButtonBack(dst, R(533, 340, 137, 40), app.theme != "simple")
	putImage(dst, 717, 49, 34, 34, app.images["brush.png"])
}
func paintDialogFrame(dst *image.RGBA, r rect) {
	if app.theme == "simple" {
		x, y, ww, hh := int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top)
		outline(dst, x, y, ww, hh, 93, 151, 191)
		outline(dst, x+3, y+3, ww-6, hh-6, 31, 77, 112)
	} else {
		paintFrame(dst, r, false)
	}
}
func (w *uiWindow) sceneEditor(dst *image.RGBA) {
	paintDialogFrame(dst, R(4, 4, 634, 349))
	if app.theme == "modern" {
		tint(dst, 9, 9, 623, 340, 2, 11, 29, 105)
	}
	for _, field := range formFields(w.form) {
		paintInputBack(dst, field.box, w.focus == field.id)
		if field.id == "Category" || field.id == "Faction" || field.id == "Subcategory" {
			x, y, hh := int(field.box.Right)-27, int(field.box.Top)+1, int(field.box.Bottom-field.box.Top)-2
			tint(dst, x, y, 25, hh, 23, 60, 92, 188)
			fillOpaque(dst, x, y, 1, hh, 70, 116, 146)
		}
	}
	paintThemeButtonBack(dst, R(201, 300, 144, 40), false)
	paintThemeButtonBack(dst, R(355, 300, 104, 40), false)
	if app.theme != "simple" {
		putImage(dst, 570, 285, 67, 66, app.images["mascot_original.png"])
	}
}
func (w *uiWindow) sceneBulk(dst *image.RGBA) {
	paintDialogFrame(dst, R(4, 4, 634, 349))
	if app.theme == "modern" {
		tint(dst, 9, 9, 623, 340, 2, 11, 29, 140)
	}
	paintInputBack(dst, R(180, 108, 355, 34), false)
	paintInputBack(dst, R(180, 166, 355, 34), w.focus == "Value")
	paintThemeButtonBack(dst, R(190, 277, 142, 40), false)
	paintThemeButtonBack(dst, R(352, 277, 142, 40), false)
}
func (w *uiWindow) sceneConfirm(dst *image.RGBA) {
	paintDialogFrame(dst, R(5, 5, 640, 277))
	if app.theme != "simple" {
		tint(dst, 23, 75, 602, 149, 16, 22, 31, 230)
	}
	fillOpaque(dst, 21, 61, 607, 2, 10, 122, 180)
	paintThemeButtonBack(dst, R(181, 240, 142, 30), false)
	paintThemeButtonBack(dst, R(345, 240, 133, 30), false)
}
