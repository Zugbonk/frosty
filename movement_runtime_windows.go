//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"github.com/zugbonk/frosty/internal/movement"
)

// Phase 1: a deliberately SINGLE-CLIENT movement session. Full SOLO/ALL
// lifecycle and character names are reserved for the later multi-client phase.
const (
	mvRead             = 0x10
	mvWrite            = 0x20
	mvOp               = 0x8
	mvQuery            = 0x1000
	mvSnap             = 0x2
	mvTimerID          = 9701
	mvWallHoldTimerID  = 9702
	mvDiscoveryTimerID = 9703
)

var (
	mvOpen         = kernel32.NewProc("OpenProcess")
	mvClose        = kernel32.NewProc("CloseHandle")
	mvReadProc     = kernel32.NewProc("ReadProcessMemory")
	mvWriteProc    = kernel32.NewProc("WriteProcessMemory")
	mvQueryPath    = kernel32.NewProc("QueryFullProcessImageNameW")
	mvSnapshot     = kernel32.NewProc("CreateToolhelp32Snapshot")
	mvFirst        = kernel32.NewProc("Process32FirstW")
	mvNext         = kernel32.NewProc("Process32NextW")
	mvProtect      = kernel32.NewProc("VirtualProtectEx")
	mvFlush        = kernel32.NewProc("FlushInstructionCache")
	mvVersion      = syscall.NewLazyDLL("version.dll")
	mvVersionSize  = mvVersion.NewProc("GetFileVersionInfoSizeW")
	mvVersionGet   = mvVersion.NewProc("GetFileVersionInfoW")
	mvVersionQuery = mvVersion.NewProc("VerQueryValueW")
	mvSetTimer     = user32.NewProc("SetTimer")
	mvExitCode     = kernel32.NewProc("GetExitCodeProcess")
	mvKillTimer    = user32.NewProc("KillTimer")
)

type mvProc struct {
	PID    uint32
	handle uintptr
	Path   string
}

func (p *mvProc) Close() {
	if p != nil && p.handle != 0 {
		mvClose.Call(p.handle)
		p.handle = 0
	}
}
func (p *mvProc) Read(addr uint32, dst []byte) error {
	if p == nil || p.handle == 0 {
		return errors.New("WoW is detached")
	}
	if len(dst) == 0 {
		return nil
	}
	var n uintptr
	ok, _, e := mvReadProc.Call(p.handle, uintptr(addr), uintptr(unsafe.Pointer(&dst[0])), uintptr(len(dst)), uintptr(unsafe.Pointer(&n)))
	if ok == 0 || n != uintptr(len(dst)) {
		return fmt.Errorf("read 0x%08x: %v", addr, e)
	}
	return nil
}
func (p *mvProc) Write(addr uint32, src []byte) error {
	if p == nil || p.handle == 0 {
		return errors.New("WoW is detached")
	}
	if len(src) == 0 {
		return nil
	}
	var n uintptr
	ok, _, e := mvWriteProc.Call(p.handle, uintptr(addr), uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)), uintptr(unsafe.Pointer(&n)))
	if ok == 0 || n != uintptr(len(src)) {
		return fmt.Errorf("write 0x%08x: %v", addr, e)
	}
	return nil
}
func (p *mvProc) PatchCode(addr uint32, src []byte) error {
	if p == nil || p.handle == 0 || len(src) == 0 {
		return errors.New("no active process for code patch")
	}
	var prior, ignore uint32
	ok, _, e := mvProtect.Call(p.handle, uintptr(addr), uintptr(len(src)), 0x40, uintptr(unsafe.Pointer(&prior)))
	if ok == 0 {
		return fmt.Errorf("cannot protect code 0x%08x: %v", addr, e)
	}
	writeErr := p.Write(addr, src)
	if writeErr == nil {
		mvFlush.Call(p.handle, uintptr(addr), uintptr(len(src)))
	}
	restored, _, restoreErr := mvProtect.Call(p.handle, uintptr(addr), uintptr(len(src)), uintptr(prior), uintptr(unsafe.Pointer(&ignore)))
	if writeErr != nil {
		return writeErr
	}
	if restored == 0 {
		return fmt.Errorf("failed to restore memory protection: %v", restoreErr)
	}
	return nil
}
func mvPath(h uintptr) (string, error) {
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	ok, _, e := mvQueryPath.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return "", fmt.Errorf("query game path: %v", e)
	}
	return syscall.UTF16ToString(buf[:size]), nil
}

type mvFixed struct {
	Signature, StrucVersion, FileVersionMS, FileVersionLS, ProductVersionMS, ProductVersionLS uint32
	FileFlagsMask, FileFlags, FileOS, FileType, FileSubtype, FileDateMS, FileDateLS           uint32
}

func mvIsWow12340(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	w := u16(path)
	if w == nil {
		return false
	}
	n, _, _ := mvVersionSize.Call(uintptr(unsafe.Pointer(w)), 0)
	if n < 32 || n > 1<<20 {
		return false
	}
	data := make([]byte, n)
	ok, _, _ := mvVersionGet.Call(uintptr(unsafe.Pointer(w)), 0, n, uintptr(unsafe.Pointer(&data[0])))
	if ok == 0 {
		return false
	}
	root := u16("\\")
	var at uintptr
	var size uint32
	ok, _, _ = mvVersionQuery.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&at)), uintptr(unsafe.Pointer(&size)))
	if ok == 0 || at == 0 || size < uint32(unsafe.Sizeof(mvFixed{})) {
		return false
	}
	v := (*mvFixed)(unsafe.Pointer(at))
	return v.Signature == 0xFEEF04BD && v.FileVersionMS == ((3<<16)|3) && v.FileVersionLS == ((5<<16)|12340)
}

type mvProcessEntry struct {
	Size, Usage, PID          uint32
	DefaultHeap               uintptr
	ModuleID, Threads, Parent uint32
	Priority                  int32
	Flags                     uint32
	ExeFile                   [260]uint16
}

func mvDiscover() []clientChoice {
	h, _, _ := mvSnapshot.Call(mvSnap, 0)
	if h == 0 || h == ^uintptr(0) {
		return nil
	}
	defer mvClose.Call(h)
	out := []clientChoice{}
	var p mvProcessEntry
	p.Size = uint32(unsafe.Sizeof(p))
	ok, _, _ := mvFirst.Call(h, uintptr(unsafe.Pointer(&p)))
	for ok != 0 {
		proc, _, _ := mvOpen.Call(mvQuery, 0, uintptr(p.PID))
		if proc != 0 {
			path, err := mvPath(proc)
			mvClose.Call(proc)
			if err == nil {
				verified := false
				// Reuse the verification from our still-live original handle.
				// Avoid re-reading VERSIONINFO from disk every 1.6s for
				// clients that we already verified on initial attachment.
				if mm.sessions != nil {
					if known := mm.sessions[p.PID]; known != nil && known.proc != nil && !mvProcessExited(known.proc) && strings.EqualFold(known.proc.Path, path) {
						verified = true
					}
				}
				if verified || mvIsWow12340(path) {
					out = append(out, clientChoice{PID: p.PID, Name: filepath.Base(path)})
				}
			}
		}
		p.Size = uint32(unsafe.Sizeof(p))
		ok, _, _ = mvNext.Call(h, uintptr(unsafe.Pointer(&p)))
	}
	// Deterministic default selection: lowest (oldest) PID wins.
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}
func mvAttach(pid uint32) (*mvProc, *movement.Engine, error) {
	h, _, e := mvOpen.Call(mvRead|mvWrite|mvOp|mvQuery, 0, uintptr(pid))
	if h == 0 {
		return nil, nil, fmt.Errorf("cannot attach to PID %d: %v", pid, e)
	}
	p := &mvProc{PID: pid, handle: h}
	path, err := mvPath(h)
	if err != nil {
		p.Close()
		return nil, nil, err
	}
	if !mvIsWow12340(path) {
		p.Close()
		return nil, nil, errors.New("not a verified WoW 3.3.5a build 12340 process")
	}
	p.Path = path
	var mz [2]byte
	if p.Read(0x00400000, mz[:]) != nil || mz != ([2]byte{'M', 'Z'}) {
		p.Close()
		return nil, nil, errors.New("unsupported WoW image base")
	}
	engine, err := movement.New(p, pid)
	if err != nil {
		p.Close()
		return nil, nil, fmt.Errorf("WoW found, but no loaded character: %w", err)
	}
	return p, engine, nil
}

// Common verified process and native-memory primitives used by the
// independent per-PID Phase 3 manager in multi_runtime_windows.go.
func mvProcessExited(p *mvProc) bool {
	if p == nil || p.handle == 0 {
		return true
	}
	var code uint32 = 259
	ok, _, _ := mvExitCode.Call(p.handle, uintptr(unsafe.Pointer(&code)))
	return ok != 0 && code != 259
}

// A process may exist while its character is still loading; keep an opened,
// verified process handle without requiring a live player object just yet.
func mvOpenVerified(pid uint32) (*mvProc, error) {
	h, _, e := mvOpen.Call(mvRead|mvWrite|mvOp|mvQuery, 0, uintptr(pid))
	if h == 0 {
		return nil, fmt.Errorf("cannot open PID %d: %v", pid, e)
	}
	p := &mvProc{PID: pid, handle: h}
	path, err := mvPath(h)
	if err != nil {
		p.Close()
		return nil, err
	}
	if !mvIsWow12340(path) {
		p.Close()
		return nil, errors.New("not a verified WoW 3.3.5a build 12340 process")
	}
	p.Path = path
	var mz [2]byte
	if p.Read(0x00400000, mz[:]) != nil || mz != ([2]byte{'M', 'Z'}) {
		p.Close()
		return nil, errors.New("unsupported WoW image base")
	}
	return p, nil
}
