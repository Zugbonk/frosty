//go:build windows

package main

// Stage 16 optional public content. The only permitted requests are read-only
// GETs to the four public content endpoints and the approved banner artwork.
// No activation, telemetry, app identity, ad events or background gameplay gate.

import (
	"context"
	"fmt"
	"image"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/zugbonk/frosty/internal/qrmodel"
	"github.com/zugbonk/frosty/internal/remotecontent"
)

const wmRemoteUpdate = 0x8000 + 73
const contentRefreshInterval = 2 * time.Minute

var remoteState struct {
	banner                    remotecontent.Banner
	bannerImage               image.Image
	hasBanner                 bool
	donations, contacts       []remotecontent.Method
	hasDonations, hasContacts bool
	about                     remotecontent.About
	guides                    []remotecontent.Guide
	hasEdition                bool
	inFlight                  bool
	lastAttempt               time.Time
}
var remoteResultCh = make(chan []remotecontent.Partial, 1)
var remoteFetcher = remotecontent.Fetcher{}

func remoteMaybeRefresh() {
	if app.main == 0 || remoteState.inFlight || (!remoteState.lastAttempt.IsZero() && time.Since(remoteState.lastAttempt) < contentRefreshInterval) {
		return
	}
	remoteState.inFlight = true
	remoteState.lastAttempt = time.Now()
	target := app.main
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
		defer cancel()
		results := remoteFetcher.FetchAll(ctx)
		select {
		case remoteResultCh <- results:
		default:
			return
		}
		pPostMessage.Call(target, wmRemoteUpdate, 0, 0)
	}()
}
func remoteApply() {
	select {
	case results := <-remoteResultCh:
		remoteState.inFlight = false
		bannerChanged := false
		for _, r := range results {
			if r.Err != nil {
				continue
			}
			switch r.Kind {
			case "banner":
				if r.Image != nil {
					remoteState.banner = r.Banner
					remoteState.bannerImage = r.Image
					remoteState.hasBanner = true
					bannerChanged = true
				}
			case "donations":
				remoteState.donations = r.Methods
				remoteState.hasDonations = true
			case "contacts":
				remoteState.contacts = r.Methods
				remoteState.hasContacts = true
			case "edition":
				remoteState.about = r.About
				remoteState.guides = r.Guides
				remoteState.hasEdition = true
			}
		}
		if bannerChanged {
			mainRedraw()
		}
		for _, w := range app.windows {
			if isInfoView(w.view) {
				w.infoScroll = clamp(w.infoScroll, 0, infoMaxScroll(w.view))
				redraw(w)
			}
		}
	default:
	}
}
func remoteRows(view string) ([]remotecontent.Method, bool) {
	if view == "donations" {
		return remoteState.donations, remoteState.hasDonations
	}
	if view == "contacts" {
		return remoteState.contacts, remoteState.hasContacts
	}
	return nil, false
}
func remoteQRImage(text string) image.Image {
	matrix, e := qrmodel.Matrix(text)
	if e != nil {
		return nil
	}
	n := len(matrix)
	im := image.NewRGBA(image.Rect(0, 0, n, n))
	for y, row := range matrix {
		for x, b := range row {
			at := im.PixOffset(x, y)
			v := byte(255)
			if b {
				v = 0
			}
			im.Pix[at] = v
			im.Pix[at+1] = v
			im.Pix[at+2] = v
			im.Pix[at+3] = 255
		}
	}
	return im
}
func openQRPopup(owner *uiWindow, row remotecontent.Method) {
	if !row.QR {
		return
	}
	// Unlike generated QR destination images, QR pixels never come from the API.
	img := remoteQRImage(row.QRPayload)
	if img == nil {
		owner.notice = "QR payload is too long"
		redraw(owner)
		return
	}
	w := createWindow("qr", 480, 540, owner.hwnd)
	if w == nil {
		return
	}
	w.returnFocus = owner.hwnd // QR close returns to its originating Donations/Contacts dialog.
	w.qrImage = img
	w.qrLabel = row.Label
	w.qrDestination = row.Destination
	pShowWindow.Call(w.hwnd, SW_SHOW)
	pSetForegroundWindow.Call(w.hwnd)
}

var (
	shell32          = syscall.NewLazyDLL("shell32.dll")
	shellExecuteW    = shell32.NewProc("ShellExecuteW")
	remoteUser32     = syscall.NewLazyDLL("user32.dll")
	openClipboard    = remoteUser32.NewProc("OpenClipboard")
	emptyClipboard   = remoteUser32.NewProc("EmptyClipboard")
	setClipboardData = remoteUser32.NewProc("SetClipboardData")
	closeClipboard   = remoteUser32.NewProc("CloseClipboard")
	remoteKernel32   = syscall.NewLazyDLL("kernel32.dll")
	globalAlloc      = remoteKernel32.NewProc("GlobalAlloc")
	globalLock       = remoteKernel32.NewProc("GlobalLock")
	globalUnlock     = remoteKernel32.NewProc("GlobalUnlock")
	globalFree       = remoteKernel32.NewProc("GlobalFree")
)

func remoteOpenURL(owner uintptr, url string) bool {
	if !remotecontent.HTTPS(url) {
		return false
	}
	verb := u16("open")
	target := u16(url)
	r, _, _ := shellExecuteW.Call(owner, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	return r > 32
}
func remoteCopy(owner uintptr, s string) bool {
	if s == "" || len(s) > 4096 {
		return false
	}
	data, e := syscall.UTF16FromString(s)
	if e != nil {
		return false
	}
	if ok, _, _ := openClipboard.Call(owner); ok == 0 {
		return false
	}
	defer closeClipboard.Call()
	h, _, _ := globalAlloc.Call(2, uintptr(len(data)*2))
	if h == 0 {
		return false
	}
	ptr, _, _ := globalLock.Call(h)
	if ptr == 0 {
		globalFree.Call(h)
		return false
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(data))
	copy(dst, data)
	globalUnlock.Call(h)
	if ok, _, _ := emptyClipboard.Call(); ok == 0 {
		globalFree.Call(h)
		return false
	}
	ok, _, _ := setClipboardData.Call(13, h) // CF_UNICODETEXT, Windows now owns h
	if ok == 0 {
		globalFree.Call(h)
		return false
	}
	return true
}
func remoteActivateMethod(w *uiWindow, row remotecontent.Method) {
	if row.Action == "open" {
		if remoteOpenURL(w.hwnd, row.Destination) {
			w.notice = "Opened in default browser"
		} else {
			w.notice = "Could not open the HTTPS link"
		}
	} else {
		if remoteCopy(w.hwnd, row.Destination) {
			w.notice = "Copied to clipboard"
		} else {
			w.notice = "Could not copy destination"
		}
	}
	redraw(w)
}
func remoteActivateGuide(w *uiWindow, g remotecontent.Guide) {
	if remoteOpenURL(w.hwnd, g.URL) {
		w.notice = "Guide opened"
	} else {
		w.notice = "Could not open guide"
	}
	redraw(w)
}
func remoteInfoTitle(view string) string {
	switch view {
	case "donations":
		return "ZUGBONK / DONATIONS"
	case "contacts":
		return "ZUGBONK / CONTACTS & SOCIAL NETWORKS"
	case "about":
		return "FROSTY ZUGBONK / ABOUT"
	case "howto":
		return "FROSTY ZUGBONK / HOW TO"
	case "qr":
		return "ZUGBONK / RECEIVE QR"
	}
	return strings.ToUpper(fmt.Sprintf("FROSTY ZUGBONK / %s", view))
}
