//go:build windows

package platform

import (
	"syscall"
	"unsafe"

	"fyne.io/fyne/v2"
)

const (
	tbpNoProgress    = 0x0
	tbpIndeterminate = 0x1
	tbpNormal        = 0x2
	tbpError         = 0x4
	tbpPaused        = 0x8

	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidTaskbarList = guid{0x56FDF344, 0xFD6D, 0x11d0, [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iidITaskbarList3 = guid{0xEA1AFB91, 0x9E28, 0x4B86, [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF}}

	ole32                = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	user32          = syscall.NewLazyDLL("user32.dll")
	procFindWindowW = user32.NewProc("FindWindowW")
)

type iTaskbarList3Vtbl struct {
	QueryInterface        uintptr
	AddRef                uintptr
	Release               uintptr
	HrInit                uintptr
	AddTab                uintptr
	DeleteTab             uintptr
	ActivateTab           uintptr
	SetActiveAlt          uintptr
	MarkFullscreenWindow  uintptr
	SetProgressValue      uintptr
	SetProgressState      uintptr
	RegisterTab           uintptr
	UnregisterTab         uintptr
	SetTabOrder           uintptr
	SetTabActive          uintptr
	ThumbBarAddButtons    uintptr
	ThumbBarUpdateButtons uintptr
	ThumbBarSetImageList  uintptr
	SetOverlayIcon        uintptr
	SetThumbnailTooltip   uintptr
	SetThumbnailClip      uintptr
}

type iTaskbarList3 struct {
	vtbl *iTaskbarList3Vtbl
}

var (
	tbInstance *iTaskbarList3
	tbInited   bool
)

func ensureTaskbar() *iTaskbarList3 {
	if tbInited {
		return tbInstance
	}
	tbInited = true

	_, _, _ = procCoInitializeEx.Call(0, coinitApartmentThreaded)

	var ptr uintptr
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidTaskbarList)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidITaskbarList3)),
		uintptr(unsafe.Pointer(&ptr)),
	)
	if hr != 0 || ptr == 0 {
		return nil
	}

	tb := (*iTaskbarList3)(unsafe.Pointer(ptr))
	_, _, _ = syscall.SyscallN(tb.vtbl.HrInit, uintptr(unsafe.Pointer(tb)))
	tbInstance = tb
	return tbInstance
}

func findMainHWND(w fyne.Window) uintptr {
	title := "tg-dlp GUI (Fyne)"
	if w != nil && w.Title() != "" {
		title = w.Title()
	}
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	return hwnd
}

func withHWND(w fyne.Window, fn func(hwnd uintptr, tb *iTaskbarList3)) {
	hwnd := findMainHWND(w)
	if hwnd == 0 {
		return
	}
	tb := ensureTaskbar()
	if tb == nil {
		return
	}
	fn(hwnd, tb)
}

func SetTaskbarIndeterminate(w fyne.Window) {
	withHWND(w, func(hwnd uintptr, tb *iTaskbarList3) {
		_, _, _ = syscall.SyscallN(tb.vtbl.SetProgressState, uintptr(unsafe.Pointer(tb)), hwnd, tbpIndeterminate)
	})
}

func SetTaskbarProgress(w fyne.Window, progress float64) {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	completed := uint64(progress * 1000)
	total := uint64(1000)

	withHWND(w, func(hwnd uintptr, tb *iTaskbarList3) {
		_, _, _ = syscall.SyscallN(tb.vtbl.SetProgressState, uintptr(unsafe.Pointer(tb)), hwnd, tbpNormal)
		_, _, _ = syscall.SyscallN(tb.vtbl.SetProgressValue, uintptr(unsafe.Pointer(tb)), hwnd, uintptr(completed), uintptr(total))
	})
}

func SetTaskbarError(w fyne.Window) {
	withHWND(w, func(hwnd uintptr, tb *iTaskbarList3) {
		_, _, _ = syscall.SyscallN(tb.vtbl.SetProgressState, uintptr(unsafe.Pointer(tb)), hwnd, tbpError)
	})
}

func ClearTaskbarProgress(w fyne.Window) {
	withHWND(w, func(hwnd uintptr, tb *iTaskbarList3) {
		_, _, _ = syscall.SyscallN(tb.vtbl.SetProgressState, uintptr(unsafe.Pointer(tb)), hwnd, tbpNoProgress)
	})
}
