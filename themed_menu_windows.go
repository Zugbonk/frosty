//go:build windows

package main

// Self-owned, source-maintained popup lists. Native TrackPopupMenu used system
// white menu colours and did not offer a draggable scrollbar for long zone
// lists. All selectable items now live in a lightweight, *themed* Win32 popup.
// This file deliberately does not change the Frosty viewport or its hitboxes.

import (
	"image"
	"unsafe"
)

type menuAppearance uint8

const (
	menuChoice   menuAppearance = iota // themed character/action popup
	menuDropdown                       // legacy-style compact dark dropdown
)

type themedMenu struct {
	entries                                      []menuEntry
	mode                                         menuAppearance
	title                                        string
	width, height                                int
	rowHeight, headerHeight, padding             int
	visibleRows, first, hover, keyboard, pressed int
	dragScroll                                   bool
	dragY, dragFirst                             int
	done, selected, dismissedOutside             bool
	chosen                                       string
}

func (m *themedMenu) rowStart() int    { return m.padding + m.headerHeight }
func (m *themedMenu) scrollable() bool { return len(m.entries) > m.visibleRows }
func (m *themedMenu) maximumFirst() int {
	max := len(m.entries) - m.visibleRows
	if max < 0 {
		return 0
	}
	return max
}
func (m *themedMenu) setFirst(first int) bool {
	fixed := clamp(first, 0, m.maximumFirst())
	if m.first == fixed {
		return false
	}
	m.first = fixed
	return true
}
func (m *themedMenu) scrollTrack() (top, height, thumbHeight, thumbTop int) {
	top = m.rowStart() + 16
	height = m.visibleRows*m.rowHeight - 32
	if height < 10 {
		height = 10
	}
	minThumb := 24
	if height < minThumb {
		minThumb = height
	}
	thumbHeight = clamp(height*m.visibleRows/len(m.entries), minThumb, height)
	free := height - thumbHeight
	if m.maximumFirst() > 0 {
		thumbTop = top + free*m.first/m.maximumFirst()
	} else {
		thumbTop = top
	}
	return
}
func (m *themedMenu) scrollbarHit(x int) bool { return m.scrollable() && x >= m.width-20 }
func (m *themedMenu) rowAt(x, y int) int {
	if x < m.padding || x >= m.width-m.padding || m.scrollbarHit(x) {
		return -1
	}
	rel := y - m.rowStart()
	if rel < 0 || rel >= m.visibleRows*m.rowHeight {
		return -1
	}
	row := m.first + rel/m.rowHeight
	if row >= len(m.entries) {
		return -1
	}
	return row
}
func (m *themedMenu) scrollToKeyboard() {
	if m.keyboard < m.first {
		m.first = m.keyboard
	}
	if m.keyboard >= m.first+m.visibleRows {
		m.first = m.keyboard - m.visibleRows + 1
	}
	m.setFirst(m.first)
}
func (m *themedMenu) nextKeyboard(step int) {
	if len(m.entries) == 0 {
		return
	}
	idx := m.keyboard
	if idx < 0 {
		idx = 0
	} else {
		idx += step
	}
	idx = clamp(idx, 0, len(m.entries)-1)
	for idx >= 0 && idx < len(m.entries) && (m.entries[idx].separator || m.entries[idx].disabled) {
		idx += step
	}
	if idx >= 0 && idx < len(m.entries) {
		m.keyboard = idx
		m.scrollToKeyboard()
	}
}
func (m *themedMenu) setChoice(i int) {
	if i < 0 || i >= len(m.entries) {
		return
	}
	if m.entries[i].disabled || m.entries[i].separator {
		return
	}
	m.chosen = m.entries[i].value
	m.selected = true
	m.done = true
}
func (m *themedMenu) scrollClick(y int) {
	if !m.scrollable() {
		return
	}
	base := m.rowStart()
	bottom := base + m.visibleRows*m.rowHeight
	top, height, thumbHeight, thumbTop := m.scrollTrack()
	if y < base+16 {
		m.setFirst(m.first - 1)
		return
	}
	if y >= bottom-16 {
		m.setFirst(m.first + 1)
		return
	}
	if y >= thumbTop && y < thumbTop+thumbHeight {
		m.dragScroll = true
		m.dragY = y
		m.dragFirst = m.first
		return
	}
	if y < thumbTop {
		m.setFirst(m.first - m.visibleRows)
	} else if y >= top+height {
		m.setFirst(m.first + m.visibleRows)
	} else {
		m.setFirst(m.first + m.visibleRows)
	}
}
func (m *themedMenu) scrollDrag(y int) {
	if !m.dragScroll {
		return
	}
	_, height, thumb, _ := m.scrollTrack()
	free := height - thumb
	if free < 1 || m.maximumFirst() < 1 {
		return
	}
	delta := y - m.dragY
	// Round, rather than truncating, for responsive thumb dragging.
	if delta >= 0 {
		delta += free / (2 * m.maximumFirst())
	} else {
		delta -= free / (2 * m.maximumFirst())
	}
	m.setFirst(m.dragFirst + delta*m.maximumFirst()/free)
}

func menuWindowMessage(w *uiWindow, msg uint32, wp, lp uintptr) (bool, uintptr) {
	m := w.menu
	if m == nil {
		return false, 0
	}
	x := int(int16(uint16(lp)))
	y := int(int16(uint16(lp >> 16)))
	switch msg {
	case WM_PAINT, WM_ERASEBKGND, WM_DESTROY:
		if msg == WM_DESTROY {
			m.done = true
		}
		return false, 0 // ordinary paint / destruction handles GDI cleanup
	case WM_LBUTTONDOWN:
		if x < 0 || x >= m.width || y < 0 || y >= m.height {
			m.dismissedOutside = true
			m.done = true
			return true, 0
		}
		m.pressed = -1
		if m.scrollbarHit(x) {
			m.scrollClick(y)
			redraw(w)
		} else {
			m.pressed = m.rowAt(x, y)
		}
		return true, 0
	case WM_LBUTTONUP:
		if m.dragScroll {
			m.dragScroll = false
			redraw(w)
			return true, 0
		}
		if x < 0 || x >= m.width || y < 0 || y >= m.height {
			m.dismissedOutside = true
			m.done = true
			return true, 0
		}
		if m.scrollbarHit(x) {
			return true, 0
		}
		idx := m.rowAt(x, y)
		if idx >= 0 && idx == m.pressed {
			m.setChoice(idx)
		}
		m.pressed = -1
		return true, 0
	case WM_RBUTTONDOWN:
		m.pressed = m.rowAt(x, y)
		return true, 0
	case WM_RBUTTONUP:
		idx := m.rowAt(x, y)
		if idx >= 0 && idx == m.pressed {
			m.setChoice(idx)
		} else {
			if x < 0 || x >= m.width || y < 0 || y >= m.height {
				m.dismissedOutside = true
			}
			m.done = true
		}
		m.pressed = -1
		return true, 0
	case WM_MOUSEMOVE:
		if m.dragScroll {
			m.scrollDrag(y)
			redraw(w)
		} else {
			idx := m.rowAt(x, y)
			if idx != m.hover {
				m.hover = idx
				redraw(w)
			}
		}
		return true, 0
	case WM_MOUSEWHEEL:
		if int(int16(uint16(wp>>16))) > 0 {
			m.setFirst(m.first - 3)
		} else {
			m.setFirst(m.first + 3)
		}
		redraw(w)
		return true, 0
	case WM_KEYDOWN:
		switch wp {
		case 27:
			m.done = true
		case 0x26:
			m.nextKeyboard(-1)
			redraw(w) // up
		case 0x28:
			m.nextKeyboard(1)
			redraw(w) // down
		case 0x21:
			m.nextKeyboard(-m.visibleRows)
			redraw(w)
		case 0x22:
			m.nextKeyboard(m.visibleRows)
			redraw(w)
		case 0x24:
			m.keyboard = -1
			m.nextKeyboard(1)
			m.scrollToKeyboard()
			redraw(w)
		case 0x23:
			m.keyboard = len(m.entries)
			m.nextKeyboard(-1)
			m.scrollToKeyboard()
			redraw(w)
		case 13:
			m.setChoice(m.keyboard)
		}
		return true, 0
	case WM_CHAR:
		return true, 0
	case 0x0008: // WM_KILLFOCUS: clicking another app dismisses the menu.
		m.done = true
		return true, 0
	case 0x0006: // WM_ACTIVATE, low word is WA_INACTIVE.
		if wp&0xffff == 0 {
			m.done = true
		}
		return true, 0
	case WM_CLOSE:
		m.done = true
		return true, 0
	}
	return false, 0
}

func menuTitle(items []menuEntry) string {
	// Context-specific captions are intentionally short and readable.
	if len(items) > 0 {
		switch items[0].label {
		case "Teleport":
			return "LOCATION ACTIONS"
		case "Simple Theme":
			return "SELECT THEME"
		case "Modern Theme":
			return "SELECT THEME"
		}
	}
	for _, it := range items {
		if hasMenuPrefix(it.label, "Capture ") {
			return "SELECT CHARACTER"
		}
		if hasMenuPrefix(it.label, "TP all") || hasMenuPrefix(it.label, "TP each") || hasMenuPrefix(it.label, "Teleport ") {
			return "SELECT DESTINATION"
		}
		if hasMenuPrefix(it.label, "WoW PID ") || hasMenuPrefix(it.label, "Control all active") {
			return "SELECT GAME CLIENT"
		}
	}
	return "SELECT OPTION"
}
func hasMenuPrefix(s, pre string) bool { return len(s) >= len(pre) && s[:len(pre)] == pre }

func (w *uiWindow) themedPopupMenu(anchor rect, items []menuEntry, mode menuAppearance) (string, bool) {
	if len(items) == 0 {
		return "", false
	}
	maxChars := 0
	for _, it := range items {
		if n := len([]rune(it.label)); n > maxChars {
			maxChars = n
		}
	}
	rowH, header, maxRows, pad := 24, 0, 11, 3
	width := int(anchor.Right - anchor.Left)
	if mode == menuChoice {
		rowH, header, maxRows, pad = 37, 33, 9, 5
		width = clamp(maxChars*8+68, 230, 520)
	} else {
		width = clamp(width, 160, 430)
		if width < maxChars*7+42 {
			width = clamp(maxChars*7+42, width, 430)
		}
	}
	visible := len(items)
	if visible > maxRows {
		visible = maxRows
	}
	width = clamp(width, 160, 580)
	height := header + pad*2 + visible*rowH
	m := &themedMenu{entries: append([]menuEntry(nil), items...), mode: mode, title: menuTitle(items), width: width, height: height, rowHeight: rowH, headerHeight: header, padding: pad, visibleRows: visible, hover: -1, keyboard: -1, pressed: -1}
	for i, it := range items {
		if it.checked {
			m.keyboard = i
			break
		}
	}
	if m.keyboard < 0 {
		for i, it := range items {
			if !it.separator && !it.disabled {
				m.keyboard = i
				break
			}
		}
	}
	if mode == menuDropdown && m.keyboard >= 0 {
		m.first = clamp(m.keyboard-2, 0, m.maximumFirst())
	}
	var owner rect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&owner)))
	aw, ah := int(owner.Right), int(owner.Bottom)
	ox, oy, vw, vh := w.contentViewport(aw, ah)
	if vw < 1 || vh < 1 {
		return "", false
	}
	xx := ox + int(anchor.Left)*vw/w.width
	yy := oy + int(anchor.Bottom)*vh/w.height
	pt := point{X: int32(xx), Y: int32(yy)}
	pClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&pt)))
	x, y := int(pt.X), int(pt.Y)
	vx, _, _ := pGetSystemMetrics.Call(SM_XVIRTUALSCREEN)
	vy, _, _ := pGetSystemMetrics.Call(SM_YVIRTUALSCREEN)
	cw, _, _ := pGetSystemMetrics.Call(SM_CXVIRTUALSCREEN)
	ch, _, _ := pGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)
	screenX, screenY, screenW, screenH := int(int32(vx)), int(int32(vy)), int(int32(cw)), int(int32(ch))
	if screenW <= 0 {
		screenX = 0
		v, _, _ := pGetSystemMetrics.Call(0)
		screenW = int(v)
	}
	if screenH <= 0 {
		screenY = 0
		v, _, _ := pGetSystemMetrics.Call(1)
		screenH = int(v)
	}
	// Keep a long dropdown usable on a small screen; a scroll thumb remains.
	if height > screenH-60 {
		visible = clamp((screenH-60-header-pad*2)/rowH, 1, len(items))
		m.visibleRows = visible
		m.height = header + pad*2 + visible*rowH
		height = m.height
		m.setFirst(m.first)
	}
	if y+height > screenY+screenH-8 {
		aboveY := oy + int(anchor.Top)*vh/w.height
		above := point{X: int32(xx), Y: int32(aboveY)}
		pClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&above)))
		if int(above.Y)-height >= screenY+8 {
			y = int(above.Y) - height
		} else {
			y = screenY + screenH - height - 8
		}
	}
	x = clamp(x, screenX+4, screenX+screenW-width-4)
	y = clamp(y, screenY+4, screenY+screenH-height-4)
	hwnd, _, _ := pCreateWindowEx.Call(WS_EX_TOOLWINDOW, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(u16("Zugbonk Selection"))), WS_POPUP, uintptr(int32(x)), uintptr(int32(y)), uintptr(width), uintptr(height), w.hwnd, 0, winInstance, 0)
	if hwnd == 0 {
		return "", false
	}
	mw := &uiWindow{hwnd: hwnd, view: "menu", width: width, height: height, dirty: true, form: map[string]string{}, menu: m}
	app.windows[hwnd] = mw
	pShowWindow.Call(hwnd, SW_SHOW)
	pUpdateWindow.Call(hwnd)
	pSetForegroundWindow.Call(hwnd)
	pSetFocus.Call(hwnd)
	pSetCapture.Call(hwnd)
	var msg winMsg
	for !m.done {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			m.done = true
			if r == 0 {
				pPostQuitMessage.Call(0)
			}
			break
		}
		if msg.Message == WM_HOTKEY && msg.WParam == 9201 {
			m.done = true
			toggleVisibility()
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	// Reclaim our parent before destroying this foreground popup. Without
	// this, the auto-hide timer can see WoW for a frame after a menu choice.
	// A click outside Zugbonk (or Alt+W hide) must NOT steal focus back.
	restoreParent := !m.dismissedOutside && zugbonkForegroundIsOurs() && zugbonkVisibleWindow(w.hwnd)
	if restoreParent {
		zugbonkReturnFocus(w.hwnd)
	}
	pReleaseCapture.Call()
	pDestroyWindow.Call(hwnd)
	if restoreParent {
		zugbonkReturnFocus(w.hwnd)
	}
	return m.chosen, m.selected
}

func (w *uiWindow) sceneMenu(dst *image.RGBA) {
	m := w.menu
	if m == nil {
		return
	}
	if m.mode == menuDropdown {
		fillOpaque(dst, 0, 0, m.width, m.height, 8, 28, 45)
		outline(dst, 0, 0, m.width, m.height, 114, 146, 166)
	} else if app.theme == "simple" {
		fillOpaque(dst, 0, 0, m.width, m.height, 20, 38, 58)
		outline(dst, 0, 0, m.width, m.height, 110, 160, 195)
		tint(dst, 3, 3, m.width-6, 28, 26, 79, 116, 110)
	} else {
		putImage(dst, 0, 0, m.width, m.height, app.images["aurora.png"])
		tint(dst, 0, 0, m.width, m.height, 3, 9, 22, 218)
		outline(dst, 0, 0, m.width, m.height, 226, 183, 64)
		tint(dst, 4, 4, m.width-8, 28, 5, 23, 40, 215)
		outline(dst, 3, 3, m.width-6, m.height-6, 23, 130, 200)
	}
	for j := 0; j < m.visibleRows; j++ {
		i := m.first + j
		if i >= len(m.entries) {
			break
		}
		it := m.entries[i]
		top := m.rowStart() + j*m.rowHeight
		if it.separator {
			tint(dst, m.padding+12, top+m.rowHeight/2, m.width-40, 1, 127, 151, 180, 220)
			continue
		}
		active := i == m.hover || (m.hover < 0 && i == m.keyboard)
		rr := R(m.padding+2, top+2, m.width-m.padding*2-8, m.rowHeight-4)
		if m.scrollable() {
			rr.Right -= 16
		}
		if m.mode == menuChoice {
			paintThemeButtonBack(dst, rr, active)
		} else if active {
			fillOpaque(dst, int(rr.Left), int(rr.Top), int(rr.Right-rr.Left), int(rr.Bottom-rr.Top), 41, 83, 120)
		}
	}
	if m.scrollable() {
		x := m.width - 18
		base := m.rowStart()
		bottom := base + m.visibleRows*m.rowHeight
		fillOpaque(dst, x, base, 15, bottom-base, 18, 39, 58)
		outline(dst, x, base, 15, bottom-base, 88, 122, 150)
		fillOpaque(dst, x+1, base+1, 13, 15, 36, 68, 95)
		fillOpaque(dst, x+1, bottom-16, 13, 15, 36, 68, 95)
		_, _, thumb, thumbTop := m.scrollTrack()
		fillOpaque(dst, x+3, thumbTop, 9, thumb, 163, 183, 200)
		outline(dst, x+3, thumbTop, 9, thumb, 213, 223, 237)
	}
}
func (w *uiWindow) paintMenuText() {
	m := w.menu
	if m == nil {
		return
	}
	if m.mode == menuChoice {
		col := gold
		if app.theme == "simple" {
			col = ice
		}
		w.center(m.title, R(10, 5, m.width-20, 27), col, 3)
	}
	for j := 0; j < m.visibleRows; j++ {
		i := m.first + j
		if i >= len(m.entries) {
			break
		}
		it := m.entries[i]
		if it.separator {
			continue
		}
		y := m.rowStart() + j*m.rowHeight
		label := it.label
		x := m.padding + 27
		ww := m.width - x - m.padding - 8
		if m.scrollable() {
			ww -= 16
		}
		textColor := white
		if it.disabled {
			textColor = ice
		}
		if m.mode == menuChoice {
			if app.theme == "modern" {
				textColor = gold
			}
			w.txt(label, x, y+2, ww, m.rowHeight-4, textColor, 3)
		} else {
			w.txt(label, x, y, ww, m.rowHeight, textColor, 0)
		}
		if it.checked {
			w.txt("✓", m.padding+6, y, 17, m.rowHeight, ice, 3)
		}
	}
	if m.scrollable() {
		x := m.width - 17
		base := m.rowStart()
		bottom := base + m.visibleRows*m.rowHeight
		w.center("▲", R(x, base, 13, 15), ice, 7)
		w.center("▼", R(x, bottom-16, 13, 15), ice, 7)
	}
}
