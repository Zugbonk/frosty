//go:build windows

package main

import (
	"github.com/zugbonk/frosty/internal/infoscroll"
	"image"
	"strings"
	"unicode/utf8"
)

func remoteAboutLines() []string {
	if remoteState.hasEdition && (remoteState.about.Top != "" || remoteState.about.Bottom != "") {
		a := wrapLines(remoteState.about.Top, 85)
		if remoteState.about.Bottom != "" {
			a = append(a, "", "")
			a = append(a, wrapLines(remoteState.about.Bottom, 85)...)
		}
		return a
	}
	return []string{"Frosty Zugbonk - Your Everyday WoW Companion", "Application Version: 1.0", "Made by Zugbonk | https://www.zugbonk.com", "", "Independent free and offline-first companion for WoW 3.3.5a.", "Movement and tracking support selected or all running game clients.", "", "No registration, activation, analytics, or internet connection is required.", "Cloud-managed content is optional and may be unavailable while offline.", "", "Unofficial project, not affiliated with Blizzard Entertainment.", "Copyright © 2026 Zugbonk"}
}
func wrapLines(s string, max int) []string {
	var lines []string
	for _, para := range strings.Split(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"), "\n") {
		if strings.TrimSpace(para) == "" {
			lines = append(lines, "")
			continue
		}
		var line string
		for _, word := range strings.Fields(para) {
			if line == "" {
				line = word
				continue
			}
			if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > max {
				lines = append(lines, line)
				line = word
			} else {
				line += " " + word
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
func remoteListGeometry(view string) (startY, stride int) {
	if view == "contacts" {
		return 104, 65
	}
	return 119, 54 // original previous production application row geometry
}
func infoMaxScroll(view string) int {
	height, content, visible := 0, 0, 0
	switch view {
	case "donations":
		height = 660
		top, stride := remoteListGeometry(view)
		content = len(remoteState.donations)*stride + 8
		visible = height - 68 - top
	case "contacts":
		height = 450
		top, stride := remoteListGeometry(view)
		content = len(remoteState.contacts)*stride + 8
		visible = height - 68 - top
	case "howto":
		height = 395
		content = len(remoteState.guides)*34 + 25
		visible = height - 190
	case "about":
		height = 500
		content = len(remoteAboutLines())*25 + 20
		visible = height - 190
	default:
		return 0
	}
	for _, w := range app.windows {
		if w.view == view {
			height = w.height
			break
		}
	}
	if view == "contacts" || view == "donations" {
		top, _ := remoteListGeometry(view)
		visible = height - 68 - top
	} else {
		visible = height - 190
	}
	max := content - visible
	if max < 0 {
		return 0
	}
	return max
}

// The scrollbar is drawn only when the current popup's content exceeds its
// visible height. A narrow dedicated strip keeps QR and row hit targets intact.
func infoScrollTrack(w *uiWindow) (rect, bool) {
	if w == nil || (w.view != "about" && w.view != "howto" && w.view != "donations" && w.view != "contacts") || infoMaxScroll(w.view) <= 0 {
		return rect{}, false
	}
	top := 94
	if w.view == "howto" {
		top = 136
	}
	if w.view == "donations" {
		top = 119
	}
	if w.view == "contacts" {
		top = 104
	}
	bottom := w.height - 83
	if w.view == "donations" || w.view == "contacts" {
		bottom = w.height - 70
	}
	if bottom-top < 36 {
		return rect{}, false
	}
	return R(w.width-35, top, 10, bottom-top), true
}
func infoScrollThumb(w *uiWindow) (rect, bool) {
	track, ok := infoScrollTrack(w)
	if !ok {
		return rect{}, false
	}
	offset, h := infoscroll.Thumb(int(track.Bottom-track.Top), infoMaxScroll(w.view), w.infoScroll)
	if h == 0 {
		return rect{}, false
	}
	return R(int(track.Left)+1, int(track.Top)+offset, 8, h), true
}

func (w *uiWindow) sceneInfo(dst *image.RGBA) {
	ww, hh := w.width, w.height
	paintDialogFrame(dst, R(7, 7, ww-14, hh-14))
	if app.theme != "simple" {
		tint(dst, 20, 75, ww-40, hh-140, 4, 12, 30, 218)
	} else {
		fillOpaque(dst, 20, 75, ww-40, hh-140, 20, 43, 64)
	}
	outline(dst, 20, 75, ww-40, hh-140, 66, 105, 141)
	fillOpaque(dst, 23, 66, ww-46, 2, 212, 178, 53)
	// Close buttons remain at bottom-right, never shifting main UI controls.
	paintThemeButtonBack(dst, R(ww-148, hh-52, 124, 35), false)
	if w.view == "donations" || w.view == "contacts" {
		rows, _ := remoteRows(w.view)
		top, stride := remoteListGeometry(w.view)
		for i := range rows {
			y := top + i*stride - w.infoScroll
			if y < top || y+stride > hh-67 {
				continue
			}
			if i%2 == 0 {
				tint(dst, 32, y, ww-66, stride-3, 22, 39, 57, 80)
			}
			fillOpaque(dst, 40, y+stride-4, ww-84, 1, 83, 73, 51)
			if rows[i].QR {
				fillOpaque(dst, ww-76, y+7, 36, 34, 111, 91, 58)
				tint(dst, ww-75, y+8, 34, 32, 37, 26, 15, 230)
				putImage(dst, ww-72, y+10, 28, 28, app.images["qr_button_icon.png"])
			}
		}
	}
	if w.view == "qr" && w.qrImage != nil {
		putImage(dst, 61, 92, 355, 355, w.qrImage)
		fillOpaque(dst, 48, 455, ww-95, 1, 99, 116, 130)
	}
	if track, ok := infoScrollTrack(w); ok {
		fillOpaque(dst, int(track.Left), int(track.Top), int(track.Right-track.Left), int(track.Bottom-track.Top), 8, 19, 32)
		outline(dst, int(track.Left), int(track.Top), int(track.Right-track.Left), int(track.Bottom-track.Top), 64, 106, 137)
		if thumb, shown := infoScrollThumb(w); shown {
			if app.theme == "simple" {
				fillOpaque(dst, int(thumb.Left), int(thumb.Top), int(thumb.Right-thumb.Left), int(thumb.Bottom-thumb.Top), 113, 173, 217)
			} else {
				fillOpaque(dst, int(thumb.Left), int(thumb.Top), int(thumb.Right-thumb.Left), int(thumb.Bottom-thumb.Top), 191, 209, 227)
			}
		}
	}
}
func (w *uiWindow) paintInfo() {
	ww, hh := w.width, w.height
	heading := remoteInfoTitle(w.view)
	label := gold
	if app.theme == "simple" {
		label = ice
	}
	w.txt(heading, 29, 22, ww-62, 31, label, 4)
	switch w.view {
	case "donations", "contacts":
		rows, loaded := remoteRows(w.view)
		if !loaded {
			message := "Online content unavailable. Offline functions continue."
			if remoteState.inFlight {
				message = "Fetching optional online content..."
			}
			w.txt(message, 43, 139, ww-87, 26, ice, 0)
		}
		if loaded && len(rows) == 0 {
			w.txt("No published methods are currently configured.", 43, 139, ww-87, 26, white, 0)
		}
		help := "Optional support. Click to copy or open; select QR to scan."
		if w.view == "contacts" {
			help = "Click a row to copy or open; select QR to scan on your phone."
		}
		w.txt(help, 38, 82, ww-78, 25, ice, 2)
		top, stride := remoteListGeometry(w.view)
		for i, row := range rows {
			y := top + i*stride - w.infoScroll
			if y < top || y+stride > hh-67 {
				continue
			}
			w.txt(strings.ToUpper(row.Label), 40, y+6, 145, 27, label, 3)
			w.txt(row.Destination, 193, y+6, ww-287, 25, white, 0)
			if w.view == "contacts" {
				action := "Click to copy"
				if row.Action == "open" {
					action = "Click to open in browser"
				}
				w.txt(action, 193, y+29, ww-287, 22, ice, 2)
			}
			if row.QR && app.images["qr_button_icon.png"] == nil {
				w.txt("QR", ww-74, y+12, 34, 22, label, 2)
			}
		}
		if infoMaxScroll(w.view) > 0 {
			w.txt("Mouse wheel to scroll", 37, hh-56, 220, 21, ice, 2)
		}
	case "about":
		lines := remoteAboutLines()
		for i, line := range lines {
			y := 100 + i*25 - w.infoScroll
			if y < 92 || y > hh-85 {
				continue
			}
			w.txt(line, 42, y, ww-82, 23, white, 0)
		}
		if infoMaxScroll(w.view) > 0 {
			w.txt("Mouse wheel to scroll", 37, hh-56, 240, 20, ice, 2)
		}
	case "howto":
		w.txt("FROSTY ZUGBONK GUIDES", 43, 92, ww-90, 26, label, 3)
		if len(remoteState.guides) == 0 {
			w.txt("Online guides are unavailable or have not been configured.", 43, 146, ww-82, 25, white, 0)
		}
		for i, g := range remoteState.guides {
			y := 140 + i*34 - w.infoScroll
			if y < 135 || y > hh-84 {
				continue
			}
			w.txt(g.Text, 46, y, ww-92, 25, ice, 1)
		}
		if infoMaxScroll(w.view) > 0 {
			w.txt("Mouse wheel to scroll", 37, hh-56, 240, 21, ice, 2)
		}
	case "qr":
		w.txt(w.qrLabel, 35, 461, ww-60, 23, label, 3)
		w.txt(w.qrDestination, 35, 485, ww-60, 25, white, 0)
	}
	if w.notice != "" {
		w.txt(w.notice, 32, hh-60, ww-192, 22, ice, 2)
	}
	w.button("close", "Close", R(ww-148, hh-52, 124, 35))
}
func (w *uiWindow) clickInfo(x, y int) {
	ww, hh := w.width, w.height
	if hit(x, y, R(ww-148, hh-52, 124, 35)) {
		closePopup(w.hwnd)
		return
	}
	switch w.view {
	case "donations", "contacts":
		rows, _ := remoteRows(w.view)
		top, stride := remoteListGeometry(w.view)
		if x < 35 || x > ww-38 || y < top || y >= hh-67 {
			return
		}
		i := (y - top + w.infoScroll) / stride
		if i < 0 || i >= len(rows) {
			return
		}
		row := rows[i]
		at := top + i*stride - w.infoScroll
		if y < at || y >= at+stride {
			return
		}
		if row.QR && x >= ww-76 && x < ww-40 && y >= at+6 && y < at+41 {
			openQRPopup(w, row)
			return
		}
		remoteActivateMethod(w, row)
	case "howto":
		if x < 40 || x > ww-40 || y < 136 || y > hh-84 {
			return
		}
		i := (y - 140 + w.infoScroll) / 34
		if i >= 0 && i < len(remoteState.guides) {
			remoteActivateGuide(w, remoteState.guides[i])
		}
	}
}
