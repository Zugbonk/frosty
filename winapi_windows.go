//go:build windows

package main

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	user32                      = syscall.NewLazyDLL("user32.dll")
	gdi32                       = syscall.NewLazyDLL("gdi32.dll")
	comdlg32                    = syscall.NewLazyDLL("comdlg32.dll")
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	pRegisterClassEx            = user32.NewProc("RegisterClassExW")
	pCreateWindowEx             = user32.NewProc("CreateWindowExW")
	pDefWindowProc              = user32.NewProc("DefWindowProcW")
	pShowWindow                 = user32.NewProc("ShowWindow")
	pUpdateWindow               = user32.NewProc("UpdateWindow")
	pGetMessage                 = user32.NewProc("GetMessageW")
	pTranslateMessage           = user32.NewProc("TranslateMessage")
	pDispatchMessage            = user32.NewProc("DispatchMessageW")
	pPostQuitMessage            = user32.NewProc("PostQuitMessage")
	pDestroyWindow              = user32.NewProc("DestroyWindow")
	pBeginPaint                 = user32.NewProc("BeginPaint")
	pEndPaint                   = user32.NewProc("EndPaint")
	pInvalidateRect             = user32.NewProc("InvalidateRect")
	pGetClientRect              = user32.NewProc("GetClientRect")
	pGetWindowRect              = user32.NewProc("GetWindowRect")
	pSetWindowPos               = user32.NewProc("SetWindowPos")
	pSetWindowText              = user32.NewProc("SetWindowTextW")
	pLoadCursor                 = user32.NewProc("LoadCursorW")
	pLoadImage                  = user32.NewProc("LoadImageW")
	pSendMessage                = user32.NewProc("SendMessageW")
	pSetProcessDPIAware         = user32.NewProc("SetProcessDPIAware")
	pMessageBox                 = user32.NewProc("MessageBoxW")
	pSetFocus                   = user32.NewProc("SetFocus")
	pGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	pCreatePopupMenu            = user32.NewProc("CreatePopupMenu")
	pSetMenuInfo                = user32.NewProc("SetMenuInfo")
	pDestroyMenu                = user32.NewProc("DestroyMenu")
	pAppendMenu                 = user32.NewProc("AppendMenuW")
	pTrackPopupMenu             = user32.NewProc("TrackPopupMenu")
	pClientToScreen             = user32.NewProc("ClientToScreen")
	pScreenToClient             = user32.NewProc("ScreenToClient")
	pSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	pGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	pGetWindowThreadProcessID   = user32.NewProc("GetWindowThreadProcessId")
	pGetAsyncKeyState           = user32.NewProc("GetAsyncKeyState")
	pOpenClipboard              = user32.NewProc("OpenClipboard")
	pCloseClipboard             = user32.NewProc("CloseClipboard")
	pEmptyClipboard             = user32.NewProc("EmptyClipboard")
	pGetClipboardData           = user32.NewProc("GetClipboardData")
	pSetClipboardData           = user32.NewProc("SetClipboardData")
	pGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	pGlobalLock                 = kernel32.NewProc("GlobalLock")
	pGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	pGlobalSize                 = kernel32.NewProc("GlobalSize")
	pGlobalFree                 = kernel32.NewProc("GlobalFree")
	pShowOwnedPopups            = user32.NewProc("ShowOwnedPopups")
	pIsWindow                   = user32.NewProc("IsWindow")
	pGetCurrentProcessID        = kernel32.NewProc("GetCurrentProcessId")
	pPostMessage                = user32.NewProc("PostMessageW")
	pRegisterHotKey             = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey           = user32.NewProc("UnregisterHotKey")
	pIsWindowVisible            = user32.NewProc("IsWindowVisible")
	pSetCapture                 = user32.NewProc("SetCapture")
	pReleaseCapture             = user32.NewProc("ReleaseCapture")
	pGetCursorPos               = user32.NewProc("GetCursorPos")
	pTrackMouseEvent            = user32.NewProc("TrackMouseEvent")
	pSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	pGetModuleHandle            = kernel32.NewProc("GetModuleHandleW")
	pCreateCompatibleDC         = gdi32.NewProc("CreateCompatibleDC")
	pDeleteDC                   = gdi32.NewProc("DeleteDC")
	pCreateCompatibleBitmap     = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject               = gdi32.NewProc("SelectObject")
	pDeleteObject               = gdi32.NewProc("DeleteObject")
	pStretchDIBits              = gdi32.NewProc("StretchDIBits")
	pStretchBlt                 = gdi32.NewProc("StretchBlt")
	pBitBlt                     = gdi32.NewProc("BitBlt")
	pSetStretchBltMode          = gdi32.NewProc("SetStretchBltMode")
	pSetTextColor               = gdi32.NewProc("SetTextColor")
	pSetBkMode                  = gdi32.NewProc("SetBkMode")
	pCreateFont                 = gdi32.NewProc("CreateFontW")
	pGetTextExtentPoint32       = gdi32.NewProc("GetTextExtentPoint32W")
	pDrawText                   = user32.NewProc("DrawTextW")
	pCreateSolidBrush           = gdi32.NewProc("CreateSolidBrush")
	pFillRect                   = user32.NewProc("FillRect")
	pFrameRect                  = user32.NewProc("FrameRect")
	pGetOpenFileName            = comdlg32.NewProc("GetOpenFileNameW")
	pGetSaveFileName            = comdlg32.NewProc("GetSaveFileNameW")
)

const (
	WM_CREATE           = 0x0001
	WM_SYSCOMMAND       = 0x0112
	SC_MINIMIZE         = 0xF020
	SC_CLOSE            = 0xF060
	WM_DISPLAYCHANGE    = 0x007E
	WM_RBUTTONDOWN      = 0x0204
	WM_RBUTTONUP        = 0x0205
	WM_CAPTURECHANGED   = 0x0215
	WM_MOUSELEAVE       = 0x02A3
	TME_LEAVE           = 0x00000002
	SM_XVIRTUALSCREEN   = 76
	SM_YVIRTUALSCREEN   = 77
	SM_CXVIRTUALSCREEN  = 78
	SM_CYVIRTUALSCREEN  = 79
	HWND_TOPMOST        = ^uintptr(0)
	WS_EX_NOACTIVATE    = 0x08000000
	WS_EX_TRANSPARENT   = 0x00000020
	WS_EX_LAYERED       = 0x00080000
	LWA_COLORKEY        = 0x00000001
	LWA_ALPHA           = 0x00000002
	SW_SHOWNOACTIVATE   = 4
	SWP_NOSIZE          = 0x0001
	SWP_NOACTIVATE      = 0x0010
	WM_NULL             = 0x0000
	WM_HOTKEY           = 0x0312
	MOD_ALT             = 0x0001
	MOD_NOREPEAT        = 0x4000
	TPM_RETURNCMD       = 0x0100
	TPM_RIGHTBUTTON     = 0x0002
	MF_STRING           = 0x0000
	MF_GRAYED           = 0x0001
	MF_CHECKED          = 0x0008
	MF_SEPARATOR        = 0x0800
	WS_POPUP            = 0x80000000
	WS_EX_TOPMOST       = 0x00000008
	WS_EX_TOOLWINDOW    = 0x00000080
	SW_RESTORE          = 9
	LR_DEFAULTSIZE      = 0x0040
	WM_DESTROY          = 0x0002
	WM_SIZE             = 0x0005
	WM_PAINT            = 0x000F
	WM_CLOSE            = 0x0010
	WM_ERASEBKGND       = 0x0014
	WM_KEYDOWN          = 0x0100
	WM_CHAR             = 0x0102
	WM_MOUSEMOVE        = 0x0200
	WM_LBUTTONDOWN      = 0x0201
	WM_LBUTTONUP        = 0x0202
	WM_LBUTTONDBLCLK    = 0x0203
	WM_CONTEXTMENU      = 0x007B
	CS_DBLCLKS          = 0x0008
	WM_MOUSEWHEEL       = 0x020A
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_MINIMIZEBOX      = 0x00020000
	WS_VISIBLE          = 0x10000000
	CS_HREDRAW          = 0x0002
	CS_VREDRAW          = 0x0001
	IDC_ARROW           = 32512
	SW_SHOW             = 5
	SW_HIDE             = 0
	SWP_NOMOVE          = 0x0002
	SWP_NOZORDER        = 0x0004
	DT_LEFT             = 0x0000
	DT_CENTER           = 0x0001
	DT_RIGHT            = 0x0002
	DT_VCENTER          = 0x0004
	DT_SINGLELINE       = 0x0020
	DT_END_ELLIPSIS     = 0x8000
	DT_NOPREFIX         = 0x800
	DIB_RGB_COLORS      = 0
	SRCCOPY             = 0x00CC0020
	HALFTONE            = 4
	TRANSPARENT         = 1
	OFN_PATHMUSTEXIST   = 0x00000800
	OFN_FILEMUSTEXIST   = 0x00001000
	OFN_OVERWRITEPROMPT = 0x00000002
	OFN_EXPLORER        = 0x00080000
	OFN_NOCHANGEDIR     = 0x00000008
	MB_OK               = 0
	MB_ICONINFORMATION  = 0x40
	MB_ICONWARNING      = 0x30
	MB_YESNOCANCEL      = 0x0003
	IMAGE_ICON          = 1
	LR_LOADFROMFILE     = 0x10
	WM_SETICON          = 0x80
)

type trackMouseEvent struct {
	Size, Flags uint32
	HWND        uintptr
	HoverTime   uint32
}
type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type wndClassEx struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}
type winMsg struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
}
type paintStruct struct {
	Hdc                uintptr
	Erase              int32
	Rect               rect
	Restore, IncUpdate int32
	Reserved           [32]byte
}
type bmpInfoHeader struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}
type bitmapInfo struct {
	Header bmpInfoHeader
	Colors [4]byte
}

// OPENFILENAMEW, Win32 layout (the app targets 32-bit native Windows).
type openFileName struct {
	Size                         uint32
	Owner, Instance              uintptr
	Filter, CustomFilter         *uint16
	MaxCustomFilter, FilterIndex uint32
	File                         *uint16
	MaxFile                      uint32
	FileTitle                    *uint16
	MaxFileTitle                 uint32
	InitialDir, Title            *uint16
	Flags                        uint32
	FileOffset, FileExtension    uint16
	DefaultExt                   *uint16
	CustData, Hook               uintptr
	TemplateName                 *uint16
	Reserved                     uintptr
	Reserved2, FlagsEx           uint32
}

func u16(s string) *uint16    { p, _ := syscall.UTF16PtrFromString(s); return p }
func invalidate(hwnd uintptr) { pInvalidateRect.Call(hwnd, 0, 0) }
func defWindowProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	ret, _, _ := pDefWindowProc.Call(hwnd, uintptr(m), w, l)
	return ret
}
func setWindowTitle(hwnd uintptr, title string) {
	pSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(u16(title))))
}
func messageBox(hwnd uintptr, msg, title string, flags uintptr) uintptr {
	// System warning/confirmation boxes are also owned Zugbonk dialogs.
	wasOurs := zugbonkForegroundIsOurs()
	result, _, _ := pMessageBox.Call(hwnd, uintptr(unsafe.Pointer(u16(msg))), uintptr(unsafe.Pointer(u16(title))), flags)
	if wasOurs && zugbonkVisibleWindow(app.main) {
		zugbonkReturnFocus(hwnd)
	}
	return result
}
func showFilePicker(hwnd uintptr, save bool) (string, bool) {
	// A native Explorer dialog belongs to Zugbonk. Returning from Open,
	// Save, or Cancel must not briefly activate WoW and auto-hide the app.
	wasOurs := zugbonkForegroundIsOurs()
	if wasOurs {
		zugbonkMarkFocusHandoff()
	}
	buf := make([]uint16, 1024)
	title := "Import teleport collection (plain TSV)"
	if save {
		title = "Export teleport collection (plain TSV)"
	}
	filter := []uint16{}
	for _, part := range []string{"Frosty teleport catalog (*.tsv)", "*.tsv", "All files (*.*)", "*.*", ""} {
		filter = append(filter, utf16.Encode([]rune(part))...)
		filter = append(filter, 0)
	}
	of := openFileName{Size: uint32(unsafe.Sizeof(openFileName{})), Owner: hwnd, Filter: &filter[0], FilterIndex: 1, File: &buf[0], MaxFile: uint32(len(buf)), Title: u16(title), Flags: OFN_EXPLORER | OFN_PATHMUSTEXIST | OFN_NOCHANGEDIR, DefaultExt: u16("tsv")}
	if save {
		of.Flags |= OFN_OVERWRITEPROMPT
	} else {
		of.Flags |= OFN_FILEMUSTEXIST
	}
	proc := pGetOpenFileName
	if save {
		proc = pGetSaveFileName
	}
	// Explorer handles a file double-click and the Open button through the
	// same native GetOpenFileNameW success path. Keep foreground ownership
	// during the modal dialog handoff and return before running Import.
	v, _, _ := proc.Call(uintptr(unsafe.Pointer(&of)))
	if wasOurs && zugbonkVisibleWindow(app.main) {
		zugbonkReturnFocus(hwnd)
	}
	if v == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}
func metric(i int) uintptr      { v, _, _ := pGetSystemMetrics.Call(uintptr(i)); return v }
func rgb(r, g, b uint8) uintptr { return uintptr(uint32(b)<<16 | uint32(g)<<8 | uint32(r)) }
