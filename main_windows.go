//go:build windows

package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/zugbonk/frosty/internal/catalog"
	"github.com/zugbonk/frosty/internal/focuspolicy"
	"github.com/zugbonk/frosty/internal/infoscroll"
	"github.com/zugbonk/frosty/internal/layout"
	"github.com/zugbonk/frosty/internal/settingsio"
	"github.com/zugbonk/frosty/internal/teleporttable"
)

//go:embed assets/* data/teleports.tsv
var embedded embed.FS

const baseWidth = 1590
const baseHeight = 860

type settings struct {
	Agreed      bool   `json:"agreed"`
	Theme       string `json:"theme"`
	ThemeChosen bool   `json:"theme_chosen"`
}
type uiWindow struct {
	hwnd                          uintptr
	returnFocus                   uintptr // last foreground Zugbonk window before this owned popup opened
	view                          string
	width, height                 int
	bgra                          []byte
	dirty                         bool
	memDC, memBitmap, oldBitmap   uintptr
	hover, focus                  string
	tableFocused                  bool
	caretPos                      int  // rune offset within active speed multiplier
	caretVisible                  bool // blinking line only while a speed field has focus
	caretTicks                    uint8
	hoverRect                     rect
	selectAll                     bool
	infoScroll                    int
	infoScrollPress               bool
	infoThumbDragging             bool
	infoThumbGrabY                int
	notice                        string
	qrLabel, qrDestination        string
	qrImage                       image.Image
	menu                          *themedMenu // transient custom themed selector
	form                          map[string]string
	selectedType                  int
	listThumbDragging             bool // teleport table scrollbar only
	listThumbGrabY                int  // pointer-to-thumb offset, in virtual pixels
	listScrollPress               bool // consume scrollbar up; never select underlying row
	dragging                      bool
	dragOffsetX, dragOffsetY      int
	outerWidth, outerHeight       int
	outerRGBA                     *image.RGBA
	outerBGRA                     []byte
	bgDC, bgBitmap, bgOld         uintptr
	bgBitmapWidth, bgBitmapHeight int
}
type clientChoice struct {
	Name string
	PID  uint32
}

type appState struct {
	main                          uintptr
	windows                       map[uintptr]*uiWindow
	images                        map[string]image.Image
	rows, defaults, visible       []catalog.Record
	selectedID                    int
	selectedIDs                   map[int]bool
	selectionAnchor               int
	tableFocused                  bool
	listBlocked                   bool
	listBlockReason               string
	scroll                        int
	text                          map[string]string
	toggles                       map[string]bool
	theme                         string
	agreed                        bool
	themeChosen                   bool
	appDir                        string
	status                        string
	zones, types, factions, sorts []string
	listColumnMode                bool
	activeMapID                   int            // -1 while no WoW connected
	floating                      uintptr        // permanent always-on-top icon; never dismissed by Escape
	floatingTooltip               uintptr        // separate, non-activating solid-black hover window
	clients                       []clientChoice // supplied by a future runtime discovery module
	selectedPID                   uint32
	allClients                    bool
	uiEverForeground              bool      // only auto-hide once this UI has genuinely held focus
	focusHandoffUntil             time.Time // brief Win32 activation gap after an owned dialog closes
	hiddenPopups                  []uintptr // restore owned themed popups after Alt+W
}

var app appState
var className *uint16
var gFonts [9]uintptr
var winInstance uintptr

func initState() error {
	app.windows = make(map[uintptr]*uiWindow)
	app.images = make(map[string]image.Image)
	app.text = map[string]string{"search": "", "x": "", "y": "", "z": "", "zone": "All zones", "type": "All types", "faction": "All factions", "sort": "Zone (A-Z)", "run_speed": "1", "fly_speed": "1", "swim_speed": "1"}
	app.toggles = make(map[string]bool)
	app.selectedIDs = make(map[int]bool)
	app.theme = "modern"
	app.activeMapID = -1
	app.types = []string{"All types", "World", "City", "Dungeon", "Raid", "Battleground"}
	app.factions = []string{"All factions", "Alliance", "Horde", "Neutral", "Other"}
	app.sorts = []string{"Zone (A-Z)", "Location (A-Z)", "Type (A-Z)", "Level"}
	for _, name := range []string{"aurora.png", "mascot_original.png", "sidebar_fallback.png", "qr_button_icon.png", "button.png", "button_pressed.png", "button_selector.png", "preview_modern.png", "preview_simple.png", "brush.png", "floating_icon.png"} {
		loadImage(name)
	}
	for _, key := range []string{"herb", "mine", "treasure", "fish", "run", "fly", "swim", "water", "thru", "wall", "airjump", "beasts", "dragons", "demons", "elementals", "giants", "undead", "human", "critters", "mechanical", "totems", "pets", "repair", "food", "poison", "ammo", "reagents", "innkeeper", "flightmaster", "battlemaster", "trainer", "profession", "auctioneer", "banker", "mailbox", "lowquests"} {
		loadImage(key + "_off.png")
		loadImage(key + "_on.png")
	}
	data, e := embedded.ReadFile("data/teleports.tsv")
	if e != nil {
		return e
	}
	app.defaults, e = catalog.Read(bytes.NewReader(data))
	if e != nil {
		return e
	}
	app.rows = catalog.Clone(app.defaults)
	dir, e := os.UserConfigDir()
	if e != nil {
		dir = os.TempDir()
	}
	// Frosty coexists with legacy Frosty encrypted teleports.dat without
	// touching or migrating any old data. Only its own plaintext files are used.
	app.appDir = filepath.Join(dir, "Zugbonk", "Frosty")
	if b, e := settingsio.Load(app.appDir); e == nil && len(b) > 0 {
		var cfg settings
		if json.Unmarshal(b, &cfg) == nil {
			app.agreed = cfg.Agreed
			app.themeChosen = cfg.ThemeChosen
			if cfg.Theme == "simple" {
				app.theme = "simple"
			}
		}
	}
	if b, e := os.ReadFile(filepath.Join(app.appDir, "teleports.tsv")); e == nil {
		rows, err := catalog.Read(bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("existing working teleports.tsv cannot be loaded (not overwritten): %w", err)
		}
		app.rows = rows
	} else if os.IsNotExist(e) {
		// First run: write only the editable working copy, never unpack/replace
		// the embedded default. Do not create any old ZTP2 file or migration.
		if err := app.saveCatalog(); err != nil {
			return err
		}
	} else {
		return e
	}
	app.zones = append([]string{"All zones"}, catalog.UniqueValues(app.rows, "zone")...)
	app.refreshList()
	return nil
}
func loadImage(name string) {
	b, e := embedded.ReadFile("assets/" + name)
	if e != nil {
		return
	}
	im, e := png.Decode(bytes.NewReader(b))
	if e == nil {
		app.images[name] = im
	}
}

// Faction captions are UI text, but the TSV stores compact legacy codes.
// X is the existing Other/unrestricted catalog value; blank means Empty.
// Preserve all existing saved records without rewriting their codes.
var factionLabels = []string{"Alliance", "Horde", "Neutral", "Other", "Empty"}

func factionCaption(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "A":
		return "Alliance"
	case "H":
		return "Horde"
	case "N":
		return "Neutral"
	case "O", "X":
		return "Other"
	default:
		return "Empty"
	}
}
func factionCode(label string) string {
	switch label {
	case "Alliance":
		return "A"
	case "Horde":
		return "H"
	case "Neutral":
		return "N"
	case "Other":
		return "X" // historical catalog "Other" encoding
	default:
		return "" // explicit Empty
	}
}

func (a *appState) refreshList() {
	mapID, valid, reason := tpDisplayMap()
	a.listBlocked = !valid && len(mm.sessions) > 0
	a.listBlockReason = reason
	a.activeMapID = mapID
	a.listColumnMode = valid && catalog.UseSubcategory(mapID)
	base := a.rows
	if a.listBlocked {
		base = nil
	} else if valid {
		filtered := make([]catalog.Record, 0, len(a.rows))
		for _, r := range a.rows {
			if r.MapID == mapID {
				filtered = append(filtered, r)
			}
		}
		base = filtered
	}
	a.zones = append([]string{"All zones"}, catalog.UniqueValues(base, "zone")...)
	if a.listColumnMode {
		a.factions = []string{"All subcategories"}
		seen := map[string]bool{}
		for _, r := range base {
			if r.Subcategory != "" {
				seen[r.Subcategory] = true
			}
		}
		vals := make([]string, 0, len(seen))
		for v := range seen {
			vals = append(vals, v)
		}
		sort.Strings(vals)
		a.factions = append(a.factions, vals...)
		if a.text["faction"] == "All factions" {
			a.text["faction"] = "All subcategories"
		}
	} else {
		a.factions = []string{"All factions", "Alliance", "Horde", "Neutral", "Other", "Empty"}
		if a.text["faction"] == "All subcategories" || a.text["faction"] == "" {
			a.text["faction"] = "All factions"
		}
	}
	zone, cat, fac := "", "", ""
	if a.text["zone"] != "All zones" {
		zone = a.text["zone"]
	}
	if a.text["type"] != "All types" {
		cat = a.text["type"]
	}
	switch a.text["faction"] {
	case "Alliance":
		fac = "A"
	case "Horde":
		fac = "H"
	case "Neutral":
		fac = "N"
	case "Other":
		fac = "X"
	case "Empty":
		fac = "_EMPTY_"
	}
	if a.listColumnMode && a.text["faction"] != "All subcategories" {
		tmp := make([]catalog.Record, 0, len(base))
		for _, r := range base {
			if r.Subcategory == a.text["faction"] {
				tmp = append(tmp, r)
			}
		}
		base = tmp
		fac = ""
	}
	if fac == "_EMPTY_" {
		tmp := make([]catalog.Record, 0, len(base))
		for _, r := range base {
			if r.Faction == "" {
				tmp = append(tmp, r)
			}
		}
		base = tmp
		fac = ""
	}
	sorting := "zone"
	switch a.text["sort"] {
	case "Location (A-Z)":
		sorting = "location"
	case "Type (A-Z)":
		sorting = "type"
	case "Level":
		sorting = "level"
	}
	a.visible = catalog.Filter(base, a.text["search"], zone, fac, cat, sorting)
	for id := range a.selectedIDs {
		found := false
		for _, r := range a.visible {
			if r.ID == id {
				found = true
				break
			}
		}
		if !found {
			delete(a.selectedIDs, id)
		}
	}
	if !a.selectedIDs[a.selectedID] {
		a.selectedID = 0
	}
	if a.scroll >= len(a.visible) {
		a.scroll = 0
	}
	if a.scroll < 0 {
		a.scroll = 0
	}
}
func (a *appState) clearSelection() {
	a.selectedID = 0
	a.selectedIDs = map[int]bool{}
	a.selectionAnchor = -1
}
func (a *appState) selectedCount() int { return len(a.selectedIDs) }
func (a *appState) selectIndex(index int, ctrl, shift bool) {
	if index < 0 || index >= len(a.visible) {
		return
	}
	if a.selectedIDs == nil {
		a.selectedIDs = map[int]bool{}
	}
	id := a.visible[index].ID
	if shift && a.selectionAnchor >= 0 && a.selectionAnchor < len(a.visible) {
		if !ctrl {
			a.selectedIDs = map[int]bool{}
		}
		from, to := a.selectionAnchor, index
		if from > to {
			from, to = to, from
		}
		for i := from; i <= to; i++ {
			a.selectedIDs[a.visible[i].ID] = true
		}
	} else if ctrl {
		if a.selectedIDs[id] {
			delete(a.selectedIDs, id)
		} else {
			a.selectedIDs[id] = true
		}
		a.selectionAnchor = index
	} else {
		a.selectedIDs = map[int]bool{id: true}
		a.selectionAnchor = index
	}
	a.selectedID = id
	if !a.selectedIDs[id] {
		a.selectedID = 0
	}
}
func (a *appState) saveSettings() {
	os.MkdirAll(a.appDir, 0700)
	data, _ := json.MarshalIndent(settings{Agreed: a.agreed, Theme: a.theme, ThemeChosen: a.themeChosen}, "", "  ")
	_ = settingsio.Save(a.appDir, data)
}
func (a *appState) saveCatalog() error { return a.saveCatalogRows(a.rows) }

// Persist the *proposed* catalog before publishing it to the UI or game. A
// failed save must not leave the table showing changes absent from AppData.
func (a *appState) saveCatalogRows(proposed []catalog.Record) error {
	if e := os.MkdirAll(a.appDir, 0700); e != nil {
		return e
	}
	var b bytes.Buffer
	if e := catalog.Write(&b, proposed); e != nil {
		return e
	}
	target := filepath.Join(a.appDir, "teleports.tsv")
	// Keep a readable pre-change backup and avoid overwriting it on failed write.
	tmp := target + ".new"
	if e := os.WriteFile(tmp, b.Bytes(), 0600); e != nil {
		return e
	}
	if _, e := os.Stat(target); e == nil {
		bak := target + ".previous.tsv"
		_ = os.Remove(bak)
		if e = os.Rename(target, bak); e != nil {
			_ = os.Remove(tmp)
			return e
		}
		if e = os.Rename(tmp, target); e != nil {
			_ = os.Rename(bak, target)
			return e
		}
		return nil
	}
	return os.Rename(tmp, target)
}
func (a *appState) commitCatalog(proposed []catalog.Record) error {
	if e := a.saveCatalogRows(proposed); e != nil {
		return e
	}
	a.rows = proposed
	a.refreshList()
	return nil
}
func main() {
	runtime.LockOSThread() // Win32 windows/messages belong to one OS thread.
	defer runtime.UnlockOSThread()
	if err := initState(); err != nil {
		messageBox(0, err.Error(), "Frosty Zugbonk - Startup Error", MB_ICONWARNING)
		return
	}
	pSetProcessDPIAware.Call()
	h, _, _ := pGetModuleHandle.Call(0)
	winInstance = h
	className = u16("FrostyZugbonkWindow")
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), Style: CS_DBLCLKS, WndProc: syscall.NewCallback(windowProc), Instance: h, Cursor: IDC_ARROW, ClassName: className}
	// MAKEINTRESOURCE for IDC_ARROW (integer resource, not a string address).
	wc.Cursor, _, _ = pLoadCursor.Call(0, uintptr(IDC_ARROW))
	if ok, _, _ := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); ok == 0 {
		messageBox(0, "Window class registration failed", "Frosty Zugbonk", MB_ICONWARNING)
		return
	}
	initFonts()
	view := "agreement"
	w, hgt := 674, 670
	if app.agreed {
		if app.themeChosen {
			view = "main"
			w = 1590
			hgt = 860
		} else {
			view = "theme"
			w, hgt = 782, 393
		}
	}
	mainWin := createWindow(view, w, hgt, 0)
	if mainWin == nil {
		return
	}
	app.main = mainWin.hwnd
	pShowWindow.Call(app.main, SW_SHOW)
	pUpdateWindow.Call(app.main)
	if view == "main" {
		remoteMaybeRefresh()
		createFloatingIcon()
		app.uiEverForeground = true // startup main is intentionally shown/activated
		startRuntimeTimers()        // auto-attach / focus watcher, not a UI redraw timer
	}
	// ALT+W is a global hotkey, so it also restores a hidden window.
	pRegisterHotKey.Call(0, 9201, MOD_ALT|MOD_NOREPEAT, uintptr('W'))
	defer pUnregisterHotKey.Call(0, 9201)
	var msg winMsg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if msg.Message == WM_HOTKEY && msg.WParam == 9201 {
			toggleVisibility()
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	for _, f := range gFonts {
		if f != 0 {
			pDeleteObject.Call(f)
		}
	}
}
func createWindow(view string, clientW, clientH int, owner uintptr) *uiWindow {
	title := "Frosty Zugbonk - Your Everyday WoW Companion"
	style := uintptr(WS_OVERLAPPEDWINDOW)
	if view != "main" {
		style = WS_CAPTION | WS_SYSMENU
		if isInfoView(view) && view != "qr" {
			style = WS_OVERLAPPEDWINDOW
		}
	}
	if view == "float" || view == "float-tip" {
		style = WS_POPUP
	}
	switch view {
	case "agreement":
		title = "Frosty Zugbonk - First Run Agreement"
	case "theme":
		title = "Frosty Zugbonk - Select Theme"
	case "add":
		title = "Add teleport location"
	case "edit":
		title = "Edit teleport location"
	case "delete":
		title = "Frosty Zugbonk - Confirm Deletion"
	case "defaults":
		title = "Frosty Zugbonk - Restore Defaults"
	case "donations":
		title = "Frosty Zugbonk - Donations"
	case "contacts":
		title = "Frosty Zugbonk - Contacts"
	case "howto":
		title = "Frosty Zugbonk - HowTo"
	case "about":
		title = "Frosty Zugbonk - About"
	case "qr":
		title = "Zugbonk - QR"
	case "float":
		title = "Frosty Zugbonk - Show Window"
	case "float-tip":
		title = "Floating Zugbonk icon help"
	}
	// Extra outer size for caption and borders. UI always coordinates its client area.
	width, height := clientW+16, clientH+39
	if view == "float" || view == "float-tip" {
		width, height = clientW, clientH
	}
	sw, _, _ := pGetSystemMetrics.Call(0)
	sh, _, _ := pGetSystemMetrics.Call(1)
	if int(sw) > 160 && width > int(sw)-40 {
		width = int(sw) - 40
	}
	if int(sh) > 160 && height > int(sh)-80 {
		height = int(sh) - 80
	}
	x, y := 80, 55
	if sw > 0 {
		x = (int(sw) - width) / 2
	}
	if sh > 0 {
		y = (int(sh) - height) / 2
	}
	if view == "float" {
		// Every launch starts at the primary screen center. Never persist a
		// potentially off-screen icon coordinate from an earlier session.
		x, y = (int(sw)-width)/2, (int(sh)-height)/2
	}
	exstyle := uintptr(0)
	if view == "float" {
		exstyle = WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE | WS_EX_LAYERED
	} else if view == "float-tip" {
		// The help popup cannot take focus or intercept icon clicks.
		exstyle = WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE | WS_EX_LAYERED | WS_EX_TRANSPARENT
	}
	hwnd, _, _ := pCreateWindowEx.Call(exstyle, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(u16(title))), style, uintptr(x), uintptr(y), uintptr(width), uintptr(height), owner, 0, winInstance, 0)
	if hwnd == 0 {
		return nil
	}
	win := &uiWindow{hwnd: hwnd, view: view, width: clientW, height: clientH, dirty: true, form: map[string]string{}}
	app.windows[hwnd] = win
	if view == "float" {
		// Magenta-keyed transparent popup: original embedded icon without a square.
		pSetLayeredWindowAttributes.Call(hwnd, rgb(255, 0, 255), 255, LWA_COLORKEY)
	} else if view == "float-tip" {
		// Fully opaque black tooltip, layered only so mouse clicks pass through.
		pSetLayeredWindowAttributes.Call(hwnd, 0, 255, LWA_ALPHA)
	}
	if view != "float-tip" {
		applyNativeIcon(hwnd)
	}
	return win
}
func isInfoView(view string) bool {
	switch view {
	case "donations", "contacts", "howto", "about", "qr":
		return true
	}
	return false
}

func popup(view string) {
	for _, w := range app.windows {
		if w.view == view && w.hwnd != app.main {
			pShowWindow.Call(w.hwnd, SW_SHOW)
			pSetForegroundWindow.Call(w.hwnd)
			return
		}
	}
	width, height := 642, 362
	if view == "delete" || view == "defaults" {
		width = 650
		height = 287
	}
	if isInfoView(view) {
		switch view {
		case "donations":
			width, height = 900, 660
		case "contacts":
			width, height = 760, 450
		case "about":
			width, height = 745, 500
		case "howto":
			width, height = 715, 395
		case "qr":
			width, height = 480, 540
		}
		remoteMaybeRefresh()
	}
	// Preserve the last active app dialog, not only the main window. For
	// example, closing a QR window should return to Donations/Contacts.
	returnTo := app.main
	if zugbonkForegroundIsOurs() {
		front, _, _ := pGetForegroundWindow.Call()
		if front != app.floating && front != app.floatingTooltip {
			if prev := app.windows[front]; prev != nil && prev.view != "menu" {
				returnTo = front
			}
		}
	}
	w := createWindow(view, width, height, app.main)
	if w == nil {
		return
	}
	w.returnFocus = returnTo
	if view == "bulk" {
		w.form = map[string]string{"Field": "Zone", "Value": ""}
	}
	if view == "edit" {
		for _, r := range app.rows {
			if r.ID == app.selectedID {
				w.form = map[string]string{"Location": r.Location, "Zone": r.Zone, "MapID": strconv.Itoa(r.MapID), "X": r.X, "Y": r.Y, "Z": r.Z, "Category": r.Category, "Faction": r.Faction, "MinLevel": r.MinLevel, "Comment": r.Comment, "Subcategory": r.Subcategory}
				break
			}
		}
	}
	if view == "add" {
		w.form = map[string]string{"Category": "World", "Faction": "X", "Subcategory": ""}
	}
	pShowWindow.Call(w.hwnd, SW_SHOW)
	pSetForegroundWindow.Call(w.hwnd)
	pUpdateWindow.Call(w.hwnd)
}
func changeMainStage(view string) {
	old := app.main
	ww, hh := 1590, 860
	if view == "theme" {
		ww, hh = 782, 393
	}
	next := createWindow(view, ww, hh, 0)
	if next == nil {
		messageBox(old, "Could not open the next application window.", "Frosty Zugbonk", MB_ICONWARNING)
		return
	}
	app.main = next.hwnd
	pShowWindow.Call(next.hwnd, SW_SHOW)
	pUpdateWindow.Call(next.hwnd)
	if view == "main" {
		remoteMaybeRefresh()
		createFloatingIcon()
		app.uiEverForeground = true
		startRuntimeTimers()
	}
	if old != 0 {
		pDestroyWindow.Call(old)
	}
}

func redraw(w *uiWindow) { w.dirty = true; invalidate(w.hwnd) }
func mainRedraw() {
	if w := app.windows[app.main]; w != nil {
		redraw(w)
	}
}

// Closing one of our own dialogs is NOT a request to hide Zugbonk. Windows
// sometimes activates the WoW window behind a destroyed owned popup before
// reactivating the owner; the auto-hide timer must not interpret that brief
// handoff as a deliberate application switch.
func closePopup(hwnd uintptr) {
	if hwnd == 0 || hwnd == app.main {
		return
	}
	w := app.windows[hwnd]
	if w == nil {
		return
	}
	wasOurs := zugbonkForegroundIsOurs()
	target := app.main
	if w.returnFocus != 0 && w.returnFocus != hwnd && zugbonkVisibleWindow(w.returnFocus) {
		target = w.returnFocus
	}
	if wasOurs && zugbonkVisibleWindow(app.main) {
		// Activate a surviving Zugbonk window BEFORE destroying the currently
		// foreground dialog, while Windows still permits this process to
		// activate its own windows. Reassert after destruction as well.
		zugbonkReturnFocus(target)
	}
	pDestroyWindow.Call(hwnd)
	if wasOurs && zugbonkVisibleWindow(app.main) {
		zugbonkReturnFocus(target)
	}
}
func windowProc(hwnd uintptr, m uint32, wp, lp uintptr) uintptr {
	w := app.windows[hwnd]
	if w == nil {
		return defWindowProc(hwnd, m, wp, lp)
	}
	if w.view == "menu" {
		if handled, result := menuWindowMessage(w, m, wp, lp); handled {
			return result
		}
	}
	switch m {
	case wmRemoteUpdate:
		if hwnd == app.main {
			remoteApply()
		}
		return 0
	case 0x0113: // WM_TIMER (all timers are targeted, no background UI repaint)
		if hwnd == app.main {
			switch wp {
			case mvTimerID:
				movementTick()
			case trackTimerID:
				trackingTick()
			case mvWallHoldTimerID:
				movementWallHoldTick()
			case mvDiscoveryTimerID:
				movementAutoDiscover()
				remoteMaybeRefresh()
			case uiFocusTimerID:
				observeUIFocus()
				tickSpeedCaret()
			default:
				return defWindowProc(hwnd, m, wp, lp)
			}
			return 0
		}
	case WM_ERASEBKGND:
		return 1
	case WM_PAINT:
		w.paint()
		return 0
	case WM_SYSCOMMAND:
		if (w.view == "float" || w.view == "float-tip") && (wp&0xFFF0 == SC_MINIMIZE || wp&0xFFF0 == SC_CLOSE) {
			return 0 // the icon is never a minimizable/closable dialog
		}
		if hwnd == app.main && w.view == "main" && wp&0xFFF0 == SC_MINIMIZE {
			// The titlebar - means hide, never minimize to the taskbar.
			hideMain()
			return 0
		}
	case WM_CLOSE:
		// Only normal application/dialog windows can close. The permanent
		// floating control must survive Escape, focus changes and close messages.
		if w.view == "float" || w.view == "float-tip" {
			return 0
		}
		if hwnd == app.main {
			pDestroyWindow.Call(hwnd)
		} else {
			closePopup(hwnd)
		}
		return 0
	case WM_DESTROY:
		if w.memBitmap != 0 {
			pSelectObject.Call(w.memDC, w.oldBitmap)
			pDeleteObject.Call(w.memBitmap)
			pDeleteDC.Call(w.memDC)
		}
		if w.bgBitmap != 0 {
			pSelectObject.Call(w.bgDC, w.bgOld)
			pDeleteObject.Call(w.bgBitmap)
			pDeleteDC.Call(w.bgDC)
		}
		delete(app.windows, hwnd)
		if hwnd == app.floatingTooltip {
			app.floatingTooltip = 0
		}
		if hwnd == app.main {
			movementStopOnExit()
			if app.floatingTooltip != 0 {
				pDestroyWindow.Call(app.floatingTooltip)
				app.floatingTooltip = 0
			}
			if app.floating != 0 {
				pDestroyWindow.Call(app.floating)
				app.floating = 0
			}
			pPostQuitMessage.Call(0)
		}
		return 0
	case WM_DISPLAYCHANGE:
		if w.view == "float" {
			// A monitor can be disconnected while the app is running. Clamp
			// the floating icon into the remaining virtual desktop bounds.
			var pos rect
			if ok, _, _ := pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&pos))); ok != 0 {
				moveFloatingTo(int(pos.Left), int(pos.Top))
			}
		}
		return 0
	case WM_SIZE:
		if isInfoView(w.view) && w.view != "qr" {
			var client rect
			if ok, _, _ := pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client))); ok != 0 {
				nw, nh := int(client.Right), int(client.Bottom)
				if nw >= 280 && nh >= 180 && (w.width != nw || w.height != nh) {
					w.width, w.height = nw, nh
					if w.memBitmap != 0 {
						pSelectObject.Call(w.memDC, w.oldBitmap)
						pDeleteObject.Call(w.memBitmap)
						pDeleteDC.Call(w.memDC)
						w.memDC = 0
						w.memBitmap = 0
						w.oldBitmap = 0
					}
					w.dirty = true
					w.infoScroll = clamp(w.infoScroll, 0, infoMaxScroll(w.view))
				}
			}
		}
		if w.view == "main" {
			w.dirty = true // viewport changes; resample background beneath UI
		}
		invalidate(hwnd)
		return 0
	case WM_LBUTTONDOWN:
		if w.view != "float" {
			pSetFocus.Call(hwnd)
		}
		if r, ok := infoScrollTrack(w); ok {
			x, y := int(int16(uint16(lp))), int(int16(uint16(lp>>16)))
			if hit(x, y, r) {
				w.infoScrollPress = true
				thumb, _ := infoScrollThumb(w)
				if y >= int(thumb.Top) && y < int(thumb.Bottom) {
					w.infoThumbDragging = true
					w.infoThumbGrabY = y - int(thumb.Top)
					pSetCapture.Call(hwnd)
				} else {
					page := int(r.Bottom - r.Top)
					if y < int(thumb.Top) {
						page = -page
					}
					w.infoScroll = clamp(w.infoScroll+page, 0, infoMaxScroll(w.view))
				}
				redraw(w)
				return 0
			}
		}
		if w.view == "main" {
			x, y := w.pointToVirtual(int(int16(uint16(lp))), int(int16(uint16(lp>>16))))
			if x >= teleporttable.TrackLeft && x < teleporttable.TrackLeft+teleporttable.TrackWidth &&
				y >= teleporttable.TrackTop && y < teleporttable.TrackTop+teleporttable.TrackHeight {
				w.listScrollPress = true
				w.focus = ""
				w.selectAll = false
				w.tableFocus(true)
				top, height := teleporttable.ThumbMetrics(app.scroll, len(app.visible))
				if y >= top && y < top+height && teleporttable.MaxScroll(len(app.visible)) > 0 {
					w.listThumbDragging = true
					w.listThumbGrabY = y - top
					pSetCapture.Call(hwnd)
				} else {
					app.scroll = teleporttable.TrackPageClick(y, app.scroll, len(app.visible))
				}
				redraw(w)
				return 0
			}
		}
		return 0
	case WM_LBUTTONUP:
		if w.infoScrollPress {
			if w.infoThumbDragging {
				if track, ok := infoScrollTrack(w); ok {
					w.infoScroll = infoscroll.Drag(int(track.Bottom-track.Top), infoMaxScroll(w.view), int(int16(uint16(lp>>16)))-int(track.Top), w.infoThumbGrabY)
				}
				w.infoThumbDragging = false
				pReleaseCapture.Call()
			}
			w.infoScrollPress = false
			redraw(w)
			return 0
		}
		if w.view == "main" && w.listScrollPress {
			if w.listThumbDragging {
				y := w.virtualYFromClient(int(int16(uint16(lp >> 16))))
				app.scroll = teleporttable.ScrollForThumbTop(y-w.listThumbGrabY, len(app.visible))
				w.listThumbDragging = false
				pReleaseCapture.Call()
			}
			w.listScrollPress = false
			redraw(w)
			return 0
		}
		if w.view == "float" {
			toggleVisibility()
			return 0
		}
		x, y := w.pointToVirtual(int(int16(uint16(lp))), int(int16(uint16(lp>>16))))
		w.click(x, y)
		return 0
	case WM_LBUTTONDBLCLK:
		if w.view == "main" {
			x, y := w.pointToVirtual(int(int16(uint16(lp))), int(int16(uint16(lp>>16))))
			if hit(x, y, R(377, 258, 899, 539)) && !app.listBlocked {
				ix := (y-258)/16 + app.scroll
				if ix >= 0 && ix < len(app.visible) {
					app.selectIndex(ix, false, false)
					tpRun(w, "teleport")
					return 0
				}
			}
		}
	case WM_RBUTTONDOWN:
		if w.view == "float" {
			hideFloatingTooltip()
			w.dragOffsetX = int(int16(uint16(lp)))
			w.dragOffsetY = int(int16(uint16(lp >> 16)))
			w.dragging = true
			pSetCapture.Call(hwnd)
			return 0
		}
	case WM_RBUTTONUP:
		if w.view == "float" {
			w.dragging = false
			pReleaseCapture.Call()
			// The pointer remains over the dragged icon once movement ends.
			showFloatingTooltip()
			return 0
		}
	case WM_CONTEXTMENU:
		if w.view == "main" {
			// Windows provides screen coordinates in lParam. Convert to
			// our immutable Frosty logical viewport using ScreenToClient.
			pt := point{X: int32(int16(uint16(lp))), Y: int32(int16(uint16(lp >> 16)))}
			pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
			x, y := w.pointToVirtual(int(pt.X), int(pt.Y))
			if hit(x, y, R(377, 258, 899, 539)) && !app.listBlocked {
				ix := (y-258)/16 + app.scroll
				if ix >= 0 && ix < len(app.visible) {
					id := app.visible[ix].ID
					if !app.selectedIDs[id] {
						app.selectIndex(ix, false, false)
					}
					w.tableFocused = true
					selection, ok := w.popupMenu(R(x, y, 3, 3), []menuEntry{{label: "Teleport", value: "teleport"}, {label: "Edit", value: "edit"}, {label: "Delete", value: "delete"}})
					if ok {
						switch selection {
						case "teleport":
							tpRun(w, "teleport")
						case "edit":
							if app.selectedCount() > 1 {
								popup("bulk")
							} else {
								popup("edit")
							}
						case "delete":
							popup("delete")
						}
					}
					redraw(w)
					return 0
				}
			}
		}
	case WM_CAPTURECHANGED:
		w.infoThumbDragging = false
		w.infoScrollPress = false
		if w.view == "float" {
			w.dragging = false
		}
		if w.view == "main" {
			w.listThumbDragging = false
			w.listScrollPress = false
		}
	case WM_MOUSELEAVE:
		if w.view == "float" {
			hideFloatingTooltip()
			return 0
		}
		if w.view == "main" && w.hover != "" {
			w.hover = ""
			redraw(w)
		}
	case WM_MOUSEMOVE:
		if w.infoThumbDragging {
			if track, ok := infoScrollTrack(w); ok {
				w.infoScroll = infoscroll.Drag(int(track.Bottom-track.Top), infoMaxScroll(w.view), int(int16(uint16(lp>>16)))-int(track.Top), w.infoThumbGrabY)
				redraw(w)
			}
			return 0
		}
		if w.view == "main" {
			if w.listThumbDragging {
				y := w.virtualYFromClient(int(int16(uint16(lp >> 16))))
				app.scroll = teleporttable.ScrollForThumbTop(y-w.listThumbGrabY, len(app.visible))
				redraw(w)
				return 0
			}
			p := trackMouseEvent{Size: uint32(unsafe.Sizeof(trackMouseEvent{})), Flags: TME_LEAVE, HWND: hwnd}
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&p)))
			x, y := w.pointToVirtual(int(int16(uint16(lp))), int(int16(uint16(lp>>16))))
			id, rr := leftUIHit(x, y)
			if w.hover != id {
				w.hover, w.hoverRect = id, rr
				redraw(w)
			}
		}
		if w.view == "float" {
			if w.dragging {
				hideFloatingTooltip()
				var pos point
				pGetCursorPos.Call(uintptr(unsafe.Pointer(&pos)))
				moveFloatingTo(int(pos.X)-w.dragOffsetX, int(pos.Y)-w.dragOffsetY)
			} else {
				// Track actual pointer exit instead of hoping a native tooltip
				// subclass receives our layered/no-activate window messages.
				tr := trackMouseEvent{Size: uint32(unsafe.Sizeof(trackMouseEvent{})), Flags: TME_LEAVE, HWND: hwnd}
				pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tr)))
				showFloatingTooltip()
			}
			return 0
		}
	case WM_CHAR:
		if w.focus != "" {
			key := rune(wp)
			if key == 1 {
				w.selectAll = true
				w.caretPos = len([]rune(w.getField(w.focus)))
				w.caretVisible = true
				redraw(w)
				return 0
			}
			if key == 3 {
				if w.selectAll {
					clipWrite(w.getField(w.focus))
				}
				return 0
			}
			if key == 24 {
				if w.selectAll {
					clipWrite(w.getField(w.focus))
					w.setField(w.focus, "")
					w.caretPos = 0
					w.selectAll = false
					if w.view == "main" {
						movementMultiplierChanged(w.focus)
						if w.focus == "search" {
							app.refreshList()
						}
					}
					redraw(w)
				}
				return 0
			}
			if key == 22 {
				if val := clipRead(); val != "" {
					w.typeText(val)
				}
				return 0
			}
			if key == 13 || key == 27 || key == 9 {
				return 0
			}
			text := w.getField(w.focus)
			text, w.caretPos = textEdit(text, w.caretPos, w.selectAll, key)
			w.selectAll = false
			w.caretVisible = true
			w.caretTicks = 0
			w.setField(w.focus, text)
			if w.view == "main" {
				movementMultiplierChanged(w.focus)
				if w.focus == "search" {
					app.refreshList()
				}
			}
			redraw(w)
			return 0
		}
	case WM_KEYDOWN:
		if w.view == "float" || w.view == "float-tip" {
			// Critical fix: the generic Escape handler previously destroyed the
			// icon as though it were an ordinary popup. Ignore all icon keypresses.
			// Alt+W remains the only global show/hide keyboard shortcut.
			return 0
		}
		if w.focus != "" {
			if wp == 0x41 && keyPressed(0x11) { // Ctrl+A selects only focused textbox
				w.selectAll = true
				w.caretPos = len([]rune(w.getField(w.focus)))
				w.caretVisible, w.caretTicks = true, 0
				redraw(w)
				return 0
			}
			if wp == 0x2E && w.selectAll { // Delete clears highlighted text
				w.setField(w.focus, "")
				w.selectAll = false
				w.caretPos = 0
				w.caretVisible, w.caretTicks = true, 0
				if w.view == "main" {
					movementMultiplierChanged(w.focus)
					if w.focus == "search" {
						app.refreshList()
					}
				}
				redraw(w)
				return 0
			}
			text := w.getField(w.focus)
			if moved, pos, updated := speedCaretKey(text, w.caretPos, wp); updated {
				w.caretPos = pos
				w.caretVisible, w.caretTicks = true, 0
				w.selectAll = false
				if moved != text {
					w.setField(w.focus, moved)
					movementMultiplierChanged(w.focus)
					if w.focus == "search" {
						app.refreshList()
					}
				}
				redraw(w)
				return 0
			}
		}
		if wp == 65 && w.view == "main" && w.tableFocused && keyPressed(0x11) {
			app.selectedIDs = map[int]bool{}
			for _, r := range app.visible {
				app.selectedIDs[r.ID] = true
			}
			if len(app.visible) > 0 {
				app.selectedID = app.visible[0].ID
			}
			redraw(w)
			return 0
		}
		if wp == 27 {
			if w.view != "main" && w.view != "agreement" && w.view != "theme" {
				closePopup(hwnd)
			}
			w.focus = ""
			redraw(w)
			return 0
		}
		if wp == 46 && w.view == "main" && w.tableFocused && app.selectedCount() > 0 {
			popup("delete")
			return 0
		}
		if wp == 13 && (w.view == "add" || w.view == "edit") {
			w.commitForm()
			return 0
		}
	case WM_MOUSEWHEEL:
		if isInfoView(w.view) {
			step := 70
			if int(int16(uint16(wp>>16))) > 0 {
				step = -70
			}
			w.infoScroll = clamp(w.infoScroll+step, 0, infoMaxScroll(w.view))
			redraw(w)
			return 0
		}
		if w.view == "main" {
			delta := int(int16(uint16(wp >> 16)))
			if delta > 0 {
				app.scroll -= 3
			} else {
				app.scroll += 3
			}
			if app.scroll < 0 {
				app.scroll = 0
			}
			app.scroll = teleporttable.ClampScroll(app.scroll, len(app.visible))
			redraw(w)
			return 0
		}
	}
	return defWindowProc(hwnd, m, wp, lp)
}

// The outer window is free-form; only its content layer is locked to 16:9.
// There is no black letterboxing: the aurora covers the entire outer window.
func (w *uiWindow) contentViewport(aw, ah int) (ox, oy, vw, vh int) {
	if w.view != "main" {
		scale := float64(aw) / float64(w.width)
		if float64(ah)/float64(w.height) < scale {
			scale = float64(ah) / float64(w.height)
		}
		vw, vh = int(float64(w.width)*scale), int(float64(w.height)*scale)
		return (aw - vw) / 2, (ah - vh) / 2, vw, vh
	}
	vw, vh = aw, (aw*9)/16
	if vh > ah {
		vh, vw = ah, (ah*16)/9
	}
	return (aw - vw) / 2, (ah - vh) / 2, vw, vh
}
func (w *uiWindow) pointToVirtual(x, y int) (int, int) {
	var c rect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&c)))
	aw, ah := int(c.Right), int(c.Bottom)
	if aw < 1 || ah < 1 || w.width <= 0 || w.height <= 0 {
		return -1, -1
	}
	ox, oy, vw, vh := w.contentViewport(aw, ah)
	if vw == 0 || vh == 0 || x < ox || y < oy || x >= ox+vw || y >= oy+vh {
		return -1, -1
	}
	return (x - ox) * w.width / vw, (y - oy) * w.height / vh
}

// When a scrollbar thumb is captured, the mouse can leave the viewport.
// Convert the Y coordinate without rejecting it, then let the scroll model
// clamp above/below the track to the first/last row.
func (w *uiWindow) virtualYFromClient(y int) int {
	var c rect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&c)))
	_, oy, _, vh := w.contentViewport(int(c.Right), int(c.Bottom))
	if vh <= 0 {
		return teleporttable.TrackTop
	}
	return (y - oy) * w.height / vh
}
func (w *uiWindow) getField(key string) string {
	if w.view == "main" {
		return app.text[key]
	}
	return w.form[key]
}
func (w *uiWindow) setField(key, value string) {
	if w.view == "main" {
		app.text[key] = value
	} else {
		w.form[key] = value
	}
}
func inside(x, y int, b rect) bool {
	return x >= int(b.Left) && x < int(b.Right) && y >= int(b.Top) && y < int(b.Bottom)
}
func R(x, y, w, h int) rect      { return rect{int32(x), int32(y), int32(x + w), int32(y + h)} }
func hit(x, y int, rr rect) bool { return inside(x, y, rr) }

// Menu items are native Win32 popup-menu entries, not cycle-through buttons.
type winMenuInfo struct {
	Size, Mask, Style, MaxHeight uint32
	Back                         uintptr
	ContextHelp, MenuData        uint32
}
type menuEntry struct {
	label, value                 string
	checked, disabled, separator bool
}

func (w *uiWindow) popupMenu(anchor rect, items []menuEntry) (string, bool) {
	// Character/target/corpse/right-click choices follow the active Modern or
	// Simple theme. No generic white Windows menu is ever displayed.
	return w.themedPopupMenu(anchor, items, menuChoice)
}
func (w *uiWindow) chooseValue(anchor rect, options []string, current string) (string, bool) {
	items := make([]menuEntry, 0, len(options))
	for _, value := range options {
		items = append(items, menuEntry{label: value, value: value, checked: value == current})
	}
	// previous production application compact dark dropdown styling, with an actual draggable
	// custom scrollbar; alphabetical contents remain Frosty's existing ones.
	return w.themedPopupMenu(anchor, items, menuDropdown)
}
func (w *uiWindow) themeDropdown() {
	v, ok := w.popupMenu(R(1260, 36, 34, 35), []menuEntry{
		{label: themeCaption("simple"), value: "simple", checked: app.theme == "simple"},
		{label: themeCaption("modern"), value: "modern", checked: app.theme == "modern"},
	})
	if ok && v != app.theme {
		app.theme = v
		app.saveSettings()
		for _, active := range app.windows {
			active.outerWidth, active.outerHeight = 0, 0
			redraw(active)
		}
	}
}
func themeCaption(id string) string {
	label := "Modern Theme"
	if id == "simple" {
		label = "Simple Theme"
	}
	if app.theme == id {
		return label + "  [Currently Active]"
	}
	return label
}
func (a *appState) clientSummary() string {
	if a.allClients && len(a.clients) > 0 {
		return fmt.Sprintf("All (%d)", len(a.clients))
	}
	for _, c := range a.clients {
		if c.PID == a.selectedPID {
			if c.Name != "" {
				return c.Name
			}
			return fmt.Sprintf("WoW PID %d", c.PID)
		}
	}
	return "No WoW"
}
func (w *uiWindow) clientDropdown() {
	// Native menu uses verified PID sessions with validated character names.
	movementAutoDiscover()
	// Only current live client records are displayed.
	// Never synthesize a character name or claim that an unconnected game is live.
	items := []menuEntry{}
	for _, c := range app.clients {
		label := fmt.Sprintf("WoW PID %d", c.PID)
		if c.Name != "" {
			label = fmt.Sprintf("%s (PID %d)", c.Name, c.PID)
		}
		items = append(items, menuEntry{label: label, value: fmt.Sprintf("pid:%d", c.PID), checked: !app.allClients && c.PID == app.selectedPID})
	}
	if len(app.clients) == 0 {
		items = append(items, menuEntry{label: "No compatible WoW clients", disabled: true})
	}
	items = append(items, menuEntry{separator: true}, menuEntry{label: "Control all active WoW clients", value: "all", checked: app.allClients, disabled: len(app.clients) == 0})
	v, ok := w.popupMenu(R(1336, 436, 192, 31), items)
	if !ok {
		return
	}
	if v == "all" {
		if err := mmToggleAll(); err != nil {
			app.status = "Client manager: " + err.Error()
		} else {
			app.status = "Game Selector scope updated"
		}
	} else if strings.HasPrefix(v, "pid:") {
		num, e := strconv.ParseUint(strings.TrimPrefix(v, "pid:"), 10, 32)
		if e == nil {
			if err := movementAttach(uint32(num)); err != nil {
				app.status = "WoW attachment failed: " + err.Error()
			} else {
				app.selectedPID = uint32(num)
				app.status = fmt.Sprintf("Controlling selected WoW PID %d", app.selectedPID)
			}
		}
	}
	app.refreshList()
	redraw(w)
}

// The floating icon is ALWAYS visible while the main UI exists. Its position
// is intentionally not saved and is reset to primary-screen center per launch.
func createFloatingIcon() {
	if app.floating != 0 {
		return
	}
	fl := createWindow("float", 52, 52, 0)
	if fl == nil {
		return
	}
	app.floating = fl.hwnd
	pShowWindow.Call(fl.hwnd, SW_SHOWNOACTIVATE)
	pUpdateWindow.Call(fl.hwnd)
	createFloatingTooltip(fl.hwnd)
}

// The icon help is a dedicated, non-activating Win32 hover surface. earlier development build
// used the common-controls TTF_SUBCLASS tooltip, which did not appear in
// Windows testing. Here WM_MOUSEMOVE directly shows the help window and
// WM_MOUSELEAVE hides it; this works while the main UI is visible or hidden.
// It is never part of the application's dismissible dialogue flow.
const floatTipWidth = 268
const floatTipHeight = 60

func createFloatingTooltip(iconHWND uintptr) {
	if iconHWND == 0 || app.floatingTooltip != 0 {
		return
	}
	tip := createWindow("float-tip", floatTipWidth, floatTipHeight, 0)
	if tip != nil {
		app.floatingTooltip = tip.hwnd
		// Intentionally not visible until the icon receives a mouse hover.
	}
}

func showFloatingTooltip() {
	if app.floating == 0 || app.floatingTooltip == 0 {
		return
	}
	var icon rect
	if ok, _, _ := pGetWindowRect.Call(app.floating, uintptr(unsafe.Pointer(&icon))); ok == 0 {
		return
	}
	minX := int(int32(metric(SM_XVIRTUALSCREEN)))
	minY := int(int32(metric(SM_YVIRTUALSCREEN)))
	maxX := minX + int(metric(SM_CXVIRTUALSCREEN))
	maxY := minY + int(metric(SM_CYVIRTUALSCREEN))
	// Prefer right of icon. Fall back left, then beneath if neither side fits.
	x, y := int(icon.Right)+10, int(icon.Top)+1
	if x+floatTipWidth > maxX {
		x = int(icon.Left) - floatTipWidth - 10
	}
	if x < minX {
		x = clamp(int(icon.Left), minX, maxX-floatTipWidth)
		y = int(icon.Bottom) + 8
	}
	x = clamp(x, minX, maxX-floatTipWidth)
	y = clamp(y, minY, maxY-floatTipHeight)
	pSetWindowPos.Call(app.floatingTooltip, HWND_TOPMOST, uintptr(int32(x)), uintptr(int32(y)), 0, 0, SWP_NOSIZE|SWP_NOACTIVATE)
	pShowWindow.Call(app.floatingTooltip, SW_SHOWNOACTIVATE)
}
func hideFloatingTooltip() {
	if app.floatingTooltip != 0 {
		pShowWindow.Call(app.floatingTooltip, SW_HIDE)
	}
}

// Each theme keeps its own movement rows. Sharing the same rectangle for
// drawing, mouse focus and text eliminates the old Simple offset mismatch.
func speedFieldRect(key string) rect {
	// Use the spare space to the left of x, without touching any icon or
	// panel. The text-only Simple toggle extends to x280, so it has its
	// own slightly narrower safe inset within the same frozen left panel.
	if app.theme == "simple" {
		return R(295, layout.SpeedFieldTop(app.theme, key), 43, 26)
	}
	return R(286, layout.SpeedFieldTop(app.theme, key), 49, 26)
}
func speedFieldLabelX() int {
	if app.theme == "simple" {
		return 281
	}
	return 271
}
func isSpeedField(key string) bool {
	return key == "run_speed" || key == "fly_speed" || key == "swim_speed"
}
func (w *uiWindow) speedFocus(key string, mouseX int) {
	w.focus = key
	w.selectAll = false
	w.caretVisible = true
	w.caretTicks = 0
	w.caretPos = w.speedCaretFromX(key, mouseX)
}
func tickSpeedCaret() {
	front, _, _ := pGetForegroundWindow.Call()
	w := app.windows[front]
	if w == nil || w.focus == "" {
		return
	}
	w.caretTicks++
	if w.caretTicks >= 3 {
		w.caretTicks = 0
		w.caretVisible = !w.caretVisible
		invalidate(w.hwnd)
	}
}

func moveFloatingTo(x, y int) {
	if app.floating == 0 {
		return
	}
	minX := int(int32(metric(SM_XVIRTUALSCREEN)))
	minY := int(int32(metric(SM_YVIRTUALSCREEN)))
	maxX := minX + int(metric(SM_CXVIRTUALSCREEN)) - 52
	maxY := minY + int(metric(SM_CYVIRTUALSCREEN)) - 52
	if maxX < minX {
		maxX = minX
	}
	if maxY < minY {
		maxY = minY
	}
	x = clamp(x, minX, maxX)
	y = clamp(y, minY, maxY)
	pSetWindowPos.Call(app.floating, HWND_TOPMOST, uintptr(int32(x)), uintptr(int32(y)), 0, 0, SWP_NOSIZE|SWP_NOACTIVATE)
}

// Track actual foreground ownership instead of relying only on main-window
// visibility. Native common file dialogs and all themed owned dialogs belong
// to this process; the permanent floating icon is NOACTIVATE and isn't a dialog.
const uiFocusTimerID = 9704

// Only deliberate in-app handoffs receive a short transition allowance.
// Clicking WoW, Explorer, or another application still causes auto-hide.
func zugbonkMarkFocusHandoff() {
	app.focusHandoffUntil = time.Now().Add(400 * time.Millisecond)
}
func zugbonkVisibleWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	exists, _, _ := pIsWindow.Call(hwnd)
	shown, _, _ := pIsWindowVisible.Call(hwnd)
	return exists != 0 && shown != 0
}
func zugbonkReturnFocus(hwnd uintptr) {
	if !zugbonkVisibleWindow(app.main) {
		return
	}
	if !zugbonkVisibleWindow(hwnd) {
		hwnd = app.main
	}
	zugbonkMarkFocusHandoff()
	pSetForegroundWindow.Call(hwnd)
	pSetFocus.Call(hwnd)
}

func zugbonkForegroundIsOurs() bool {
	front, _, _ := pGetForegroundWindow.Call()
	if front == 0 {
		return false
	}
	var pid uint32
	pGetWindowThreadProcessID.Call(front, uintptr(unsafe.Pointer(&pid)))
	ours, _, _ := pGetCurrentProcessID.Call()
	return pid == uint32(ours)
}
func startRuntimeTimers() {
	if app.main == 0 {
		return
	}
	mvSetTimer.Call(app.main, uiFocusTimerID, 175, 0)
	mvSetTimer.Call(app.main, mvDiscoveryTimerID, 1600, 0)
	// First scan after the shell has appeared, with no dummy client names.
	movementAutoDiscover()
}
func observeUIFocus() {
	if app.main == 0 || app.windows[app.main] == nil || app.windows[app.main].view != "main" {
		return
	}
	if v, _, _ := pIsWindowVisible.Call(app.main); v == 0 {
		return
	}
	front, _, _ := pGetForegroundWindow.Call()
	if front == 0 {
		return
	} // Ignore transient Windows activation gaps.
	owned := zugbonkForegroundIsOurs()
	if owned {
		app.uiEverForeground = true
		app.focusHandoffUntil = time.Time{}
	} else if focuspolicy.ShouldHide(app.uiEverForeground, owned, time.Now().Before(app.focusHandoffUntil)) {
		// A real switch to WoW, Explorer, etc. still hides the application.
		// Only a bounded, explicitly initiated in-app handoff is exempt.
		hideMain()
	}
}
func hideMain() {
	if app.main == 0 {
		return
	}
	app.hiddenPopups = app.hiddenPopups[:0]
	for h, w := range app.windows {
		if h == app.main || w.view == "float" || w.view == "float-tip" {
			continue
		}
		if shown, _, _ := pIsWindowVisible.Call(h); shown != 0 {
			app.hiddenPopups = append(app.hiddenPopups, h)
			pShowWindow.Call(h, SW_HIDE)
		}
	}
	// Also hides standard Windows-owned popup windows (Import/Export).
	pShowOwnedPopups.Call(app.main, 0)
	pShowWindow.Call(app.main, SW_HIDE)
	app.uiEverForeground = false
	app.focusHandoffUntil = time.Time{}
}
func showMain() {
	if app.main == 0 {
		return
	}
	pShowWindow.Call(app.main, SW_SHOW)
	pShowOwnedPopups.Call(app.main, 1)
	foreground := app.main
	for _, h := range app.hiddenPopups {
		if isWindow, _, _ := pIsWindow.Call(h); isWindow != 0 {
			pShowWindow.Call(h, SW_SHOW)
			foreground = h
		}
	}
	app.hiddenPopups = nil
	pSetForegroundWindow.Call(foreground)
	// Even a very fast switch back to WoW should auto-hide this window.
	// An unsuccessful foreground activation still leaves the next Alt+W
	// behaving as SHOW (because toggleVisibility checks the actual owner).
	app.uiEverForeground = true
}
func toggleVisibility() {
	w := app.windows[app.main]
	if w == nil || w.view != "main" {
		return
	}
	visible, _, _ := pIsWindowVisible.Call(app.main)
	if visible != 0 && zugbonkForegroundIsOurs() {
		hideMain()
	} else {
		// Visible-but-behind-WoW should RAISE on the FIRST press, not hide.
		showMain()
	}
}
func (w *uiWindow) click(x, y int) {
	if x < 0 || y < 0 {
		return
	}
	switch w.view {
	case "agreement":
		if hit(x, y, R(183, 576, 138, 33)) {
			app.agreed = true
			app.saveSettings()
			changeMainStage("theme")
		} else if hit(x, y, R(345, 576, 164, 33)) {
			pDestroyWindow.Call(w.hwnd)
		}
	case "theme":
		if hit(x, y, R(130, 341, 155, 41)) || hit(x, y, R(29, 108, 356, 225)) {
			app.theme = "simple"
			app.themeChosen = true
			app.saveSettings()
			changeMainStage("main")
		}
		if hit(x, y, R(533, 341, 136, 41)) || hit(x, y, R(414, 108, 363, 225)) {
			app.theme = "modern"
			app.themeChosen = true
			app.saveSettings()
			changeMainStage("main")
		}
	case "main":
		w.clickMain(x, y)
	case "add", "edit":
		w.clickForm(x, y)
	case "bulk":
		w.clickBulk(x, y)
	case "donations", "contacts", "howto", "about", "qr":
		w.clickInfo(x, y)
		return
	case "delete":
		if hit(x, y, R(181, 240, 142, 30)) {
			if app.selectedCount() > 0 {
				proposed := catalog.RemoveIDs(app.rows, app.selectedIDs)
				if e := app.commitCatalog(proposed); e != nil {
					app.status = "Delete failed: " + e.Error()
					messageBox(w.hwnd, e.Error(), "Frosty - Save failed", MB_ICONWARNING)
					return
				}
				app.clearSelection()
				app.status = "Location deleted (plain TSV saved)"
			}
			closePopup(w.hwnd)
			mainRedraw()
		} else if hit(x, y, R(345, 240, 133, 30)) {
			closePopup(w.hwnd)
		}
	case "defaults":
		if hit(x, y, R(181, 240, 142, 30)) {
			if e := app.commitCatalog(catalog.Clone(app.defaults)); e != nil {
				app.status = "Restore failed: " + e.Error()
				messageBox(w.hwnd, e.Error(), "Frosty - Save failed", MB_ICONWARNING)
				return
			}
			app.clearSelection()
			app.status = fmt.Sprintf("Original %d embedded default teleport locations restored", len(app.defaults))
			closePopup(w.hwnd)
			mainRedraw()
		}
		if hit(x, y, R(345, 240, 133, 30)) {
			closePopup(w.hwnd)
		}
	}
	redraw(w)
}

// Plain Simple theme has no tracking/movement sprites: the visible text
// rows themselves are the complete click and hover targets.
func simpleLeftUIHit(x, y int) (string, rect) {
	for i, id := range []string{"herb", "mine", "treasure", "fish"} {
		rr := R(75, 88+i*53, 111, 27)
		if hit(x, y, rr) {
			return id, rr
		}
	}
	if rr := R(75, 287, 111, 27); hit(x, y, rr) {
		return "object_other", rr
	}
	for i, id := range []string{"repair", "food", "poison", "ammo", "reagents", "innkeeper", "flightmaster", "battlemaster", "trainer", "profession", "auctioneer", "banker", "mailbox", "lowquests"} {
		rr := R(65, 336+i*32, 127, 28)
		if hit(x, y, rr) {
			return id, rr
		}
	}
	for _, e := range []struct {
		id      string
		x, y, w int
	}{
		{"run", 210, 122, 70}, {"fly", 210, 179, 70}, {"swim", 210, 235, 70},
		{"water", 202, 293, 72}, {"thru", 276, 293, 67},
		{"wall", 202, 348, 72}, {"airjump", 276, 348, 67},
		{"beasts", 207, 493, 70}, {"dragons", 277, 493, 66},
		{"demons", 207, 549, 70}, {"elementals", 277, 549, 66},
		{"giants", 207, 605, 70}, {"undead", 277, 605, 66},
		{"human", 207, 661, 70}, {"critters", 277, 661, 66},
		{"mechanical", 207, 717, 70}, {"totems", 277, 717, 66},
		{"pets", 207, 773, 70}, {"creature_other", 277, 773, 66},
	} {
		rr := R(e.x, e.y, e.w, 27)
		if hit(x, y, rr) {
			return e.id, rr
		}
	}
	return "", rect{}
}

// Unified interaction for icon/label pairs and on-hover explanations.
func leftUIHit(x, y int) (string, rect) {
	if x < 0 || y < 0 {
		return "", rect{}
	}
	if app.theme == "simple" {
		return simpleLeftUIHit(x, y)
	}
	for i, id := range []string{"herb", "mine", "treasure", "fish"} {
		r := R(62, 77+i*53, 48, 51)
		if hit(x, y, r) {
			return id, r
		}
	}
	for i, id := range []string{"repair", "food", "poison", "ammo", "reagents", "innkeeper", "flightmaster", "battlemaster", "trainer", "profession", "auctioneer", "banker", "mailbox", "lowquests"} {
		r := R(62, 337+i*32, 106, 31)
		if hit(x, y, r) {
			return id, r
		}
	}
	for _, z := range []struct {
		id           string
		x, y, ww, hh int
	}{
		{"run", 225, 130, 67, 17}, {"fly", 225, 187, 67, 17}, {"swim", 225, 243, 67, 17},
		{"water", 201, 299, 78, 17}, {"thru", 281, 299, 68, 17},
		{"wall", 201, 354, 79, 17}, {"airjump", 285, 354, 65, 17},
		{"beasts", 223, 502, 67, 17}, {"dragons", 286, 502, 67, 17},
		{"demons", 218, 557, 67, 17}, {"elementals", 277, 557, 77, 17},
		{"giants", 224, 613, 67, 17}, {"undead", 291, 613, 67, 17},
		{"human", 208, 669, 70, 17}, {"critters", 291, 669, 67, 17},
		{"mechanical", 207, 725, 79, 17}, {"totems", 291, 725, 67, 17},
		{"pets", 215, 779, 67, 17}, {"creature_other", 281, 779, 70, 17},
	} {
		r := R(z.x, z.y, z.ww, z.hh)
		if hit(x, y, r) {
			return z.id, r
		}
	}
	if r := R(92, 292, 115, 20); hit(x, y, r) {
		return "object_other", r
	}
	for _, ic := range mainIcons() {
		if hit(x, y, ic.box) {
			return ic.id, ic.box
		}
	}
	return "", rect{}
}
func hoverHelp(id string) string {
	help := map[string]string{
		"herb": "Herb tracking", "mine": "Mineral tracking", "treasure": "Treasure tracking", "fish": "Fishing spot tracking", "object_other": "Track other game objects",
		"run": "Run speed", "fly": "Flight speed", "swim": "Swim speed", "water": "Walk on water", "thru": "Fly through textures\nAlso enables Fly", "wall": "Wall climb", "airjump": "AirJump",
		"repair": "Repair services", "food": "Food and drink vendors", "poison": "Poison vendors", "ammo": "Ammunition vendors", "reagents": "Reagent vendors",
		"innkeeper": "Innkeepers", "flightmaster": "Flight masters", "battlemaster": "Battle masters", "trainer": "Class trainers", "profession": "Profession trainers",
		"auctioneer": "Auctioneers", "banker": "Bankers", "mailbox": "Mailboxes", "lowquests": "Low level quests",
		"beasts": "Beast creatures", "dragons": "Dragonkin", "demons": "Demons", "elementals": "Elementals", "giants": "Giants", "undead": "Undead",
		"human": "Humanoids", "critters": "Critters", "mechanical": "Mechanical creatures", "totems": "Totems", "pets": "Non-combat pets", "creature_other": "Other creature types",
	}
	description := help[id]
	if description == "" {
		return ""
	}
	state := "OFF"
	if app.toggles[id] {
		state = "ON"
	}
	// Append status to the final line; captions on the actual icons do not change.
	return description + " — " + state
}
func (w *uiWindow) clickMain(x, y int) {
	if hit(x, y, R(1330, 50, 192, 256)) && remoteState.hasBanner && remoteState.banner.Website != "" {
		if remoteOpenURL(w.hwnd, remoteState.banner.Website) {
			app.status = "Zugbonk project link opened"
		} else {
			app.status = "Could not open Zugbonk project link"
		}
		redraw(w)
		return
	}
	if hit(x, y, R(1260, 36, 34, 35)) {
		w.themeDropdown()
		return
	}
	if hit(x, y, R(1336, 436, 192, 31)) {
		w.clientDropdown()
		return
	}
	// Search, coordinates and editable selectors. No native game code exists in the earlier development build
	for _, h := range []struct {
		name string
		r    rect
	}{{"search", R(432, 104, 859, 21)}, {"x", R(424, 70, 97, 25)}, {"y", R(568, 70, 97, 25)}, {"z", R(709, 70, 97, 25)}, {"run_speed", speedFieldRect("run_speed")}, {"fly_speed", speedFieldRect("fly_speed")}, {"swim_speed", speedFieldRect("swim_speed")}} {
		if hit(x, y, h.r) {
			if (h.name == "x" || h.name == "y" || h.name == "z") && tpXYZDisabled() {
				return
			}
			if isSpeedField(h.name) {
				w.speedFocus(h.name, x)
				w.tableFocused = false
			} else {
				w.focus = h.name
				w.tableFocused = false
				w.selectAll = false
				w.caretVisible = true
				w.caretPos = w.caretAt(h.r, w.getField(h.name), x)
			}
			redraw(w)
			return
		}
	}
	if w.focus != "" {
		w.focus = ""
		w.caretVisible = false
	}
	filters := []struct {
		name string
		r    rect
		opts []string
	}{{"zone", R(411, 133, 274, 23), app.zones}, {"faction", R(740, 133, 274, 23), app.factions}, {"type", R(411, 160, 274, 23), app.types}, {"sort", R(731, 160, 256, 23), app.sorts}}
	for _, h := range filters {
		if hit(x, y, h.r) {
			if v, ok := w.chooseValue(h.r, h.opts, app.text[h.name]); ok {
				app.text[h.name] = v
				app.scroll = 0
				app.clearSelection()
				app.refreshList()
				redraw(w)
			}
			return
		}
	}
	// Keep earlier development build hit targets/drawing, replace only movement triggers.
	if id, _ := leftUIHit(x, y); id != "" {
		switch id {
		case "run", "fly", "swim", "water", "wall", "thru", "airjump":
			if err := movementClick(id); err != nil {
				app.status = "Movement: " + err.Error()
			} else {
				app.status = "Movement " + id + " desired for " + app.clientSummary()
			}
		default:
			if err := trackingClick(id); err != nil {
				app.status = "Tracking: " + err.Error()
			} else {
				app.status = "Tracking " + id + " desired for " + app.clientSummary()
			}
		}
		redraw(w)
		return
	}
	// Eight expansion slots are reserved in internal/layout but remain invisible.
	for _, btn := range mainButtons() {
		if !hit(x, y, btn.box) {
			continue
		}
		switch btn.id {
		case "add":
			popup("add")
		case "edit":
			if app.selectedCount() > 1 {
				popup("bulk")
			} else if app.selectedCount() == 1 {
				popup("edit")
			} else {
				app.status = "Select a location to edit"
			}
		case "delete":
			if app.selectedCount() > 0 {
				popup("delete")
			} else {
				app.status = "Select a location to delete"
			}
		case "defaults":
			popup("defaults")
		case "import":
			app.importCatalog(w.hwnd)
		case "export":
			app.exportCatalog(w.hwnd)
		case "theme":
			w.themeDropdown()
		case "selector":
			w.clientDropdown()
		case "donations", "contacts", "howto", "about":
			popup(btn.id)
		case "forward", "coords", "target", "corpse", "teleport":
			tpRun(w, btn.id)
		default:
			app.status = btn.label + " is reserved for a future Frosty module"
		}
		redraw(w)
		return
	}
	if hit(x, y, R(379, 70, 31, 28)) {
		if tpXYZDisabled() {
			return
		}
		tpRun(w, "capture")
		return
	}
	if hit(x, y, R(377, 258, 899, 539)) && !app.listBlocked {
		ix := (y-258)/16 + app.scroll
		if ix >= 0 && ix < len(app.visible) {
			ctrl := keyPressed(0x11)
			shift := keyPressed(0x10)
			app.selectIndex(ix, ctrl, shift)
			w.focus = ""
			w.tableFocus(true)
			redraw(w)
		}
	}
}
func (w *uiWindow) clickForm(x, y int) {
	if hit(x, y, R(201, 300, 144, 40)) {
		w.commitForm()
		return
	}
	if hit(x, y, R(355, 300, 104, 40)) {
		closePopup(w.hwnd)
		return
	}
	for _, entry := range formFields(w.form) {
		if hit(x, y, entry.box) {
			if entry.id == "Category" {
				if v, ok := w.chooseValue(entry.box, catalog.CategoryOptions, w.form[entry.id]); ok {
					w.form[entry.id] = v
					// The right-hand combobox is now decided by the selected
					// Type immediately, even before a MapID is entered.
				}
				w.focus = ""
			} else if entry.id == "Faction" {
				if v, ok := w.chooseValue(entry.box, factionLabels, factionCaption(w.form[entry.id])); ok {
					w.form[entry.id] = factionCode(v)
				}
				w.focus = ""
			} else if entry.id == "Subcategory" {
				if v, ok := w.chooseValue(entry.box, catalog.SubcategoryOptions, w.form[entry.id]); ok {
					w.form[entry.id] = v
				}
				w.focus = ""
			} else {
				w.focus = entry.id
				w.selectAll = false
				w.caretPos = w.caretAt(entry.box, w.getField(entry.id), x)
				w.caretVisible = true
			}
			redraw(w)
			return
		}
	}
}
func (w *uiWindow) commitForm() {
	x := strings.TrimSpace(w.form["X"])
	y := strings.TrimSpace(w.form["Y"])
	z := strings.TrimSpace(w.form["Z"])
	mapID, me := strconv.Atoi(strings.TrimSpace(w.form["MapID"]))
	if strings.TrimSpace(w.form["Location"]) == "" || strings.TrimSpace(w.form["Zone"]) == "" || me != nil || mapID < 0 || mapID >= 2000 || x == "" || y == "" || z == "" {
		messageBox(w.hwnd, "Location, Zone, MapID, X, Y and Z are required.", "Frosty - Incomplete location", MB_ICONWARNING)
		return
	}
	for _, num := range []string{x, y, z} {
		v, e := strconv.ParseFloat(num, 32)
		if e != nil || v != v || v > 100000 || v < -100000 || strings.ContainsAny(strings.ToLower(num), "infna") {
			messageBox(w.hwnd, "Coordinates must be finite decimal numbers.", "Frosty - Invalid coordinates", MB_ICONWARNING)
			return
		}
	}
	if level := strings.TrimSpace(w.form["MinLevel"]); level != "" {
		n, e := strconv.Atoi(level)
		if e != nil || n < 0 || n > 255 {
			messageBox(w.hwnd, "Minimum level must be blank or a number from 0 to 255.", "Frosty - Invalid level", MB_ICONWARNING)
			return
		}
	}
	r := catalog.Record{ID: catalog.NextID(app.rows), Location: w.form["Location"], Zone: w.form["Zone"], MapID: mapID, X: x, Y: y, Z: z, Category: w.form["Category"], Faction: factionCode(factionCaption(w.form["Faction"])), MinLevel: w.form["MinLevel"], Comment: w.form["Comment"], Subcategory: w.form["Subcategory"]}
	if r.Category == "" {
		r.Category = "World"
	}

	proposed := catalog.Clone(app.rows)
	if w.view == "edit" {
		found := false
		for i := range proposed {
			if proposed[i].ID == app.selectedID {
				r.ID = app.selectedID
				proposed[i] = r
				found = true
				break
			}
		}
		if !found {
			messageBox(w.hwnd, "Selected record no longer exists", "Frosty - Edit failed", MB_ICONWARNING)
			return
		}
	} else {
		proposed = append(proposed, r)
	}
	if e := app.commitCatalog(proposed); e != nil {
		messageBox(w.hwnd, e.Error(), "Frosty - Save failed", MB_ICONWARNING)
		return // Keep the editor open and preserve the working list.
	}
	if w.view == "add" {
		app.selectedID = r.ID
		app.selectedIDs = map[int]bool{r.ID: true}
	}
	app.status = "Teleport catalog saved as plaintext TSV"
	closePopup(w.hwnd)
	mainRedraw()
}
func (a *appState) importCatalog(hwnd uintptr) {
	path, ok := showFilePicker(hwnd, false)
	if !ok {
		return
	}
	b, e := os.ReadFile(path)
	if e != nil {
		messageBox(hwnd, e.Error(), "Import failed", MB_ICONWARNING)
		return
	}
	rec, e := catalog.Read(bytes.NewReader(b))
	if e != nil {
		messageBox(hwnd, e.Error(), "Import failed", MB_ICONWARNING)
		return
	}
	// Use the themed selection popup for Import mode too. Native file
	// pickers stay native Windows dialogs, as they were in the accepted UI.
	owner := app.windows[hwnd]
	if owner == nil {
		return
	}
	choice, selected := owner.popupMenu(R(384, 817, 92, 35), []menuEntry{
		{label: "OVERWRITE current locations (replaces all)", value: "overwrite"},
		{label: "MERGE new locations (skip exact duplicates)", value: "merge"},
		{separator: true},
		{label: "Cancel import", value: "cancel"},
	})
	if !selected || choice == "cancel" {
		return
	}
	var proposed []catalog.Record
	status := ""
	if choice == "overwrite" {
		proposed = rec
		status = fmt.Sprintf("Overwrote with %d imported locations", len(rec))
	} else if choice == "merge" {
		var added int
		proposed, added = catalog.MergeExact(a.rows, rec)
		status = fmt.Sprintf("Merged %d new locations (%d duplicates skipped)", added, len(rec)-added)
	} else {
		return
	}
	if e = a.commitCatalog(proposed); e != nil {
		messageBox(hwnd, e.Error(), "Import save failed", MB_ICONWARNING)
		return
	}
	a.clearSelection()
	a.scroll = 0
	a.text["zone"] = "All zones"
	a.text["faction"] = "All factions"
	a.text["type"] = "All types"
	a.refreshList()
	a.status = status
	mainRedraw()
}
func (a *appState) exportCatalog(hwnd uintptr) {
	path, ok := showFilePicker(hwnd, true)
	if !ok {
		return
	}
	if strings.ToLower(filepath.Ext(path)) != ".tsv" {
		path += ".tsv"
	}
	var b bytes.Buffer
	if e := catalog.Write(&b, a.rows); e != nil {
		messageBox(hwnd, e.Error(), "Export failed", MB_ICONWARNING)
		return
	}
	if e := os.WriteFile(path, b.Bytes(), 0600); e != nil {
		messageBox(hwnd, e.Error(), "Export failed", MB_ICONWARNING)
		return
	}
	a.status = fmt.Sprintf("Exported %d locations to plain TSV", len(a.rows))
	mainRedraw()
}

// Use the original Frosty application icon for the titlebar and task switcher.
// It remains a separate embedded asset; no binary patching of old executables.
var sharedAppIcon uintptr

func applyNativeIcon(hwnd uintptr) {
	// The application icon is an actual Win32 GROUP_ICON resource (ID 1),
	// linked into the portable EXE; it is never written to the filesystem.
	if sharedAppIcon == 0 {
		sharedAppIcon, _, _ = pLoadImage.Call(winInstance, 1, IMAGE_ICON, 32, 32, LR_DEFAULTSIZE)
	}
	if sharedAppIcon != 0 {
		pSendMessage.Call(hwnd, WM_SETICON, 1, sharedAppIcon)
		pSendMessage.Call(hwnd, WM_SETICON, 0, sharedAppIcon)
	}
}
