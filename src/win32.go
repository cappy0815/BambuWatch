//go:build windows

// Win32/GDI/GDI+/COM-Anbindung ohne Fremdbibliotheken (nur Go-Standardbibliothek).
// Grundgeruest wie EHV-NRW Live-Ticker 3.0 und Claude Limits.
package main

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	gdiplus  = syscall.NewLazyDLL("gdiplus.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	crypt32  = syscall.NewLazyDLL("crypt32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	shlwapi  = syscall.NewLazyDLL("shlwapi.dll")
	winmm    = syscall.NewLazyDLL("winmm.dll")

	pRegisterClassExW      = user32.NewProc("RegisterClassExW")
	pCreateWindowExW       = user32.NewProc("CreateWindowExW")
	pDefWindowProcW        = user32.NewProc("DefWindowProcW")
	pDestroyWindow         = user32.NewProc("DestroyWindow")
	pGetMessageW           = user32.NewProc("GetMessageW")
	pTranslateMessage      = user32.NewProc("TranslateMessage")
	pDispatchMessageW      = user32.NewProc("DispatchMessageW")
	pIsDialogMessageW      = user32.NewProc("IsDialogMessageW")
	pPostQuitMessage       = user32.NewProc("PostQuitMessage")
	pPostMessageW          = user32.NewProc("PostMessageW")
	pShowWindow            = user32.NewProc("ShowWindow")
	pIsWindowVisible       = user32.NewProc("IsWindowVisible")
	pSetWindowTextW        = user32.NewProc("SetWindowTextW")
	pGetWindowTextW        = user32.NewProc("GetWindowTextW")
	pSetTimer              = user32.NewProc("SetTimer")
	pKillTimer             = user32.NewProc("KillTimer")
	pIsIconic              = user32.NewProc("IsIconic")
	pLoadCursorW           = user32.NewProc("LoadCursorW")
	pSetCursor             = user32.NewProc("SetCursor")
	pLoadImageW            = user32.NewProc("LoadImageW")
	pGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	pSendMessageW          = user32.NewProc("SendMessageW")
	pInvalidateRect        = user32.NewProc("InvalidateRect")
	pBeginPaint            = user32.NewProc("BeginPaint")
	pEndPaint              = user32.NewProc("EndPaint")
	pGetClientRect         = user32.NewProc("GetClientRect")
	pGetWindowRect         = user32.NewProc("GetWindowRect")
	pSetWindowPos          = user32.NewProc("SetWindowPos")
	pMoveWindow            = user32.NewProc("MoveWindow")
	pSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	pSetFocus              = user32.NewProc("SetFocus")
	pFillRect              = user32.NewProc("FillRect")
	pDrawTextW             = user32.NewProc("DrawTextW")
	pGetDC                 = user32.NewProc("GetDC")
	pReleaseDC             = user32.NewProc("ReleaseDC")
	pTrackMouseEvent       = user32.NewProc("TrackMouseEvent")
	pSetProcessDPIAware    = user32.NewProc("SetProcessDPIAware")
	pSetProcessDpiAwareCtx = user32.NewProc("SetProcessDpiAwarenessContext")
	pRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	pFindWindowW           = user32.NewProc("FindWindowW")
	pOpenClipboard         = user32.NewProc("OpenClipboard")
	pCloseClipboard        = user32.NewProc("CloseClipboard")
	pGetClipboardData      = user32.NewProc("GetClipboardData")
	pMonitorFromPoint      = user32.NewProc("MonitorFromPoint")
	pCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	pAppendMenuW           = user32.NewProc("AppendMenuW")
	pDestroyMenu           = user32.NewProc("DestroyMenu")
	pTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	pGetCursorPos          = user32.NewProc("GetCursorPos")
	pSetLayeredWindowAttr  = user32.NewProc("SetLayeredWindowAttributes")
	pGetWindowLongPtrW     = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW     = user32.NewProc("SetWindowLongPtrW")
	pSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	pDestroyIcon           = user32.NewProc("DestroyIcon")

	pGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	pGetModuleFileNameW    = kernel32.NewProc("GetModuleFileNameW")
	pGetPrivateProfileStrW = kernel32.NewProc("GetPrivateProfileStringW")
	pWritePrivateProfileSW = kernel32.NewProc("WritePrivateProfileStringW")
	pCreateMutexW          = kernel32.NewProc("CreateMutexW")
	pGlobalLock            = kernel32.NewProc("GlobalLock")
	pGlobalUnlock          = kernel32.NewProc("GlobalUnlock")
	pLocalFree             = kernel32.NewProc("LocalFree")
	pCreateToolhelp32Snap  = kernel32.NewProc("CreateToolhelp32Snapshot")
	pProcess32FirstW       = kernel32.NewProc("Process32FirstW")
	pProcess32NextW        = kernel32.NewProc("Process32NextW")
	pCloseHandle           = kernel32.NewProc("CloseHandle")
	pLoadLibraryW          = kernel32.NewProc("LoadLibraryW")
	pGetProcAddress        = kernel32.NewProc("GetProcAddress")

	pCreateFontW         = gdi32.NewProc("CreateFontW")
	pSelectObject        = gdi32.NewProc("SelectObject")
	pDeleteObject        = gdi32.NewProc("DeleteObject")
	pDeleteDC            = gdi32.NewProc("DeleteDC")
	pCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
	pBitBlt              = gdi32.NewProc("BitBlt")
	pSetTextColor        = gdi32.NewProc("SetTextColor")
	pSetBkMode           = gdi32.NewProc("SetBkMode")
	pSetBkColor          = gdi32.NewProc("SetBkColor")
	pCreateSolidBrush    = gdi32.NewProc("CreateSolidBrush")
	pGetDeviceCaps       = gdi32.NewProc("GetDeviceCaps")

	pGdiplusStartup         = gdiplus.NewProc("GdiplusStartup")
	pGdipCreateFromHDC      = gdiplus.NewProc("GdipCreateFromHDC")
	pGdipDeleteGraphics     = gdiplus.NewProc("GdipDeleteGraphics")
	pGdipSetSmoothingMode   = gdiplus.NewProc("GdipSetSmoothingMode")
	pGdipSetTextRendering   = gdiplus.NewProc("GdipSetTextRenderingHint")
	pGdipSetInterpolation   = gdiplus.NewProc("GdipSetInterpolationMode")
	pGdipSetPixelOffset     = gdiplus.NewProc("GdipSetPixelOffsetMode")
	pGdipScaleWorld         = gdiplus.NewProc("GdipScaleWorldTransform")
	pGdipTranslateWorld     = gdiplus.NewProc("GdipTranslateWorldTransform")
	pGdipSetClipRect        = gdiplus.NewProc("GdipSetClipRect")
	pGdipCreateSolidFill    = gdiplus.NewProc("GdipCreateSolidFill")
	pGdipDeleteBrush        = gdiplus.NewProc("GdipDeleteBrush")
	pGdipCreatePath         = gdiplus.NewProc("GdipCreatePath")
	pGdipDeletePath         = gdiplus.NewProc("GdipDeletePath")
	pGdipAddPathArc         = gdiplus.NewProc("GdipAddPathArc")
	pGdipClosePathFigure    = gdiplus.NewProc("GdipClosePathFigure")
	pGdipFillPath           = gdiplus.NewProc("GdipFillPath")
	pGdipFillRectangle      = gdiplus.NewProc("GdipFillRectangle")
	pGdipFillEllipse        = gdiplus.NewProc("GdipFillEllipse")
	pGdipDrawEllipse        = gdiplus.NewProc("GdipDrawEllipse")
	pGdipCreatePen1         = gdiplus.NewProc("GdipCreatePen1")
	pGdipDeletePen          = gdiplus.NewProc("GdipDeletePen")
	pGdipCreateBmpStream    = gdiplus.NewProc("GdipCreateBitmapFromStream")
	pGdipGetImageWidth      = gdiplus.NewProc("GdipGetImageWidth")
	pGdipGetImageHeight     = gdiplus.NewProc("GdipGetImageHeight")
	pGdipDrawImageRect      = gdiplus.NewProc("GdipDrawImageRect")
	pGdipDisposeImage       = gdiplus.NewProc("GdipDisposeImage")
	pGdipCreateFontFamily   = gdiplus.NewProc("GdipCreateFontFamilyFromName")
	pGdipDeleteFontFamily   = gdiplus.NewProc("GdipDeleteFontFamily")
	pGdipCreateFont         = gdiplus.NewProc("GdipCreateFont")
	pGdipDeleteFont         = gdiplus.NewProc("GdipDeleteFont")
	pGdipCreateStringFormat = gdiplus.NewProc("GdipCreateStringFormat")
	pGdipDeleteStringFormat = gdiplus.NewProc("GdipDeleteStringFormat")
	pGdipSetStringAlign     = gdiplus.NewProc("GdipSetStringFormatAlign")
	pGdipSetStringLineAlign = gdiplus.NewProc("GdipSetStringFormatLineAlign")
	pGdipSetStringTrimming  = gdiplus.NewProc("GdipSetStringFormatTrimming")
	pGdipDrawString         = gdiplus.NewProc("GdipDrawString")
	pGdipMeasureString      = gdiplus.NewProc("GdipMeasureString")

	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

	pCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")

	pCryptProtectData   = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")

	pShellExecute    = shell32.NewProc("ShellExecuteW")
	pShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")

	pSHCreateMemStream = shlwapi.NewProc("SHCreateMemStream")

	pPlaySoundW = winmm.NewProc("PlaySoundW")
)

const (
	WS_POPUP       = 0x80000000
	WS_CHILD       = 0x40000000
	WS_VISIBLE     = 0x10000000
	WS_TABSTOP     = 0x00010000
	WS_SYSMENU     = 0x00080000
	WS_MINIMIZEBOX = 0x00020000
	WS_EX_TOPMOST  = 0x00000008
	WS_EX_TOOLWIN  = 0x00000080
	WS_EX_LAYERED  = 0x00080000
	WS_EX_NOACTIV  = 0x08000000

	ES_PASSWORD    = 0x0020
	ES_AUTOHSCROLL = 0x0080

	CS_DBLCLKS    = 0x0008
	CS_DROPSHADOW = 0x00020000

	WM_DESTROY       = 0x0002
	WM_SIZE          = 0x0005
	WM_SETFOCUS      = 0x0007
	WM_PAINT         = 0x000F
	WM_CLOSE         = 0x0010
	WM_ERASEBKGND    = 0x0014
	WM_SETCURSOR     = 0x0020
	WM_GETMINMAXINFO = 0x0024
	WM_SETFONT       = 0x0030
	WM_SETICON       = 0x0080
	WM_CONTEXTMENU   = 0x007B
	WM_NCHITTEST     = 0x0084
	WM_KEYDOWN       = 0x0100
	WM_COMMAND       = 0x0111
	WM_TIMER         = 0x0113
	WM_CTLCOLOREDIT  = 0x0133
	WM_MOUSEMOVE     = 0x0200
	WM_LBUTTONDOWN   = 0x0201
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONUP     = 0x0205
	WM_SIZING        = 0x0214
	WM_EXITSIZEMOVE  = 0x0232
	WM_MOUSELEAVE    = 0x02A3
	WM_APP           = 0x8000

	EM_SETPASSWORDCHAR = 0x00CC
	EM_SETMARGINS      = 0x00D3
	EM_SETSEL          = 0x00B1

	HTCLIENT  = 1
	HTCAPTION = 2

	SW_HIDE            = 0
	SW_SHOWNORMAL      = 1
	SW_SHOWNOACTIVATE  = 4
	SW_SHOW            = 5
	SW_MINIMIZE        = 6
	SW_SHOWMINNOACTIVE = 7
	SW_RESTORE         = 9

	DT_LEFT         = 0x0
	DT_CENTER       = 0x1
	DT_RIGHT        = 0x2
	DT_VCENTER      = 0x4
	DT_WORDBREAK    = 0x10
	DT_SINGLELINE   = 0x20
	DT_CALCRECT     = 0x400
	DT_NOPREFIX     = 0x800
	DT_END_ELLIPSIS = 0x8000

	SWP_NOSIZE     = 0x1
	SWP_NOMOVE     = 0x2
	SWP_NOZORDER   = 0x4
	SWP_NOACTIVATE = 0x10

	MF_STRING    = 0x0
	MF_CHECKED   = 0x8
	MF_GRAYED    = 0x1
	MF_POPUP     = 0x10
	MF_SEPARATOR = 0x800
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct{ x, y int32 }

type windowPos struct {
	hwnd, insertAfter uintptr
	x, y, cx, cy      int32
	flags             uint32
}

type rect struct{ left, top, right, bottom int32 }

type msgT struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type paintStruct struct {
	hdc         uintptr
	fErase      int32
	rcPaint     rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type trackMouseEventT struct {
	cbSize    uint32
	dwFlags   uint32
	hwnd      uintptr
	hoverTime uint32
}

type minMaxInfo struct {
	reserved, maxSize, maxPos, minTrack, maxTrack point
}

func u16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	return p
}

func up(s string) uintptr { return uintptr(unsafe.Pointer(u16(s))) }

func sendMsg(h uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(h, uintptr(m), w, l)
	return r
}

func setText(h uintptr, s string) { pSetWindowTextW.Call(h, up(s)) }

func getText(h uintptr) string {
	buf := make([]uint16, 4096)
	n, _, _ := pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func invalidate(h uintptr) {
	if h != 0 {
		pInvalidateRect.Call(h, 0, 0)
	}
}

func hInstance() uintptr { h, _, _ := pGetModuleHandleW.Call(0); return h }

func registerClass(name string, proc func(hwnd, msg, wParam, lParam uintptr) uintptr, icon uintptr) {
	cur, _, _ := pLoadCursorW.Call(0, 32512) // IDC_ARROW
	wc := wndClassExW{style: CS_DBLCLKS | CS_DROPSHADOW, lpfnWndProc: syscall.NewCallback(proc),
		hInstance: hInstance(), hCursor: cur, lpszClassName: u16(name), hIcon: icon, hIconSm: icon}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
}

func createPopup(class, title string, exStyle uint32, x, y, w, h int32) uintptr {
	return createPopupOwned(class, title, exStyle, x, y, w, h, 0)
}

// Fenster ohne Windows-Rahmen; owner = Besitzer (bleibt darueber, keine eigene Taskleisten-Schaltflaeche)
func createPopupOwned(class, title string, exStyle uint32, x, y, w, h int32, owner uintptr) uintptr {
	r, _, _ := pCreateWindowExW.Call(uintptr(exStyle), up(class), up(title),
		uintptr(WS_POPUP|WS_SYSMENU|WS_MINIMIZEBOX),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), owner, 0, hInstance(), 0)
	pref := uint32(2) // Windows 11: runde Ecken
	pDwmSetWindowAttribute.Call(r, 33, uintptr(unsafe.Pointer(&pref)), 4)
	dark := uint32(1) // dunkler Fensterrahmen
	pDwmSetWindowAttribute.Call(r, 20, uintptr(unsafe.Pointer(&dark)), 4)
	border := uint32(0x00423A36) // feiner Rahmen in Linienfarbe (BGR)
	pDwmSetWindowAttribute.Call(r, 34, uintptr(unsafe.Pointer(&border)), 4)
	return r
}

// Eingabefeld (Kind-Fenster)
func createEdit(parent uintptr, id int, password bool) uintptr {
	st := uintptr(WS_CHILD | WS_TABSTOP | ES_AUTOHSCROLL)
	if password {
		st |= ES_PASSWORD
	}
	h, _, _ := pCreateWindowExW.Call(0, up("EDIT"), up(""), st, 0, 0, 10, 10, parent, uintptr(id), hInstance(), 0)
	return h
}

func windowRect(h uintptr) rect {
	var r rect
	pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}

func clientRect(h uintptr) rect {
	var r rect
	pGetClientRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}

func screenW() int32 { v, _, _ := pGetSystemMetrics.Call(0); return int32(v) }
func screenH() int32 { v, _, _ := pGetSystemMetrics.Call(1); return int32(v) }

// Arbeitsbereich (ohne Taskleiste)
func workArea() rect {
	var r rect
	pSystemParametersInfoW.Call(0x0030, 0, uintptr(unsafe.Pointer(&r)), 0) // SPI_GETWORKAREA
	if r.right <= 0 {
		r = rect{0, 0, screenW(), screenH()}
	}
	return r
}

// Liegt der Punkt auf irgendeinem Monitor? (MONITOR_DEFAULTTONULL)
func onScreen(x, y int32) bool {
	r, _, _ := pMonitorFromPoint.Call(uintptr(uint32(x))|uintptr(uint32(y))<<32, 0)
	return r != 0
}

func mouseXY(lParam uintptr) (int32, int32) {
	return int32(int16(lParam & 0xFFFF)), int32(int16((lParam >> 16) & 0xFFFF))
}

func cursorPos() point {
	var p point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func trackLeave(h uintptr) {
	t := trackMouseEventT{dwFlags: 0x2, hwnd: h} // TME_LEAVE
	t.cbSize = uint32(unsafe.Sizeof(t))
	pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&t)))
}

func setHandCursor() {
	c, _, _ := pLoadCursorW.Call(0, 32649) // IDC_HAND
	pSetCursor.Call(c)
}

func isVisible(h uintptr) bool { r, _, _ := pIsWindowVisible.Call(h); return r != 0 }
func isIconic(h uintptr) bool  { r, _, _ := pIsIconic.Call(h); return r != 0 }

func setTopmost(h uintptr, on bool) {
	z := ^uintptr(1) // HWND_NOTOPMOST (-2)
	if on {
		z = ^uintptr(0) // HWND_TOPMOST (-1)
	}
	pSetWindowPos.Call(h, z, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
}

// Deckkraft 100..255 (255 = undurchsichtig, Layered-Stil nur wenn noetig)
func setAlpha(h uintptr, a int) {
	ex, _, _ := pGetWindowLongPtrW.Call(h, ^uintptr(19)) // GWL_EXSTYLE (-20)
	if a >= 255 {
		pSetWindowLongPtrW.Call(h, ^uintptr(19), ex&^WS_EX_LAYERED)
		return
	}
	pSetWindowLongPtrW.Call(h, ^uintptr(19), ex|WS_EX_LAYERED)
	pSetLayeredWindowAttr.Call(h, 0, uintptr(a), 2) // LWA_ALPHA
}

// ---------------------------------------------------------------- INI (Windows)
type winIni struct{ file string }

func (w winIni) get(sec, key, def string) string {
	buf := make([]uint16, 8192)
	pGetPrivateProfileStrW.Call(up(sec), up(key), up(def), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), up(w.file))
	return syscall.UTF16ToString(buf)
}

func (w winIni) set(sec, key, val string) {
	pWritePrivateProfileSW.Call(up(sec), up(key), up(val), up(w.file))
}

func (w winIni) del(sec, key string) {
	if key == "" {
		pWritePrivateProfileSW.Call(up(sec), 0, 0, up(w.file))
		return
	}
	pWritePrivateProfileSW.Call(up(sec), up(key), 0, up(w.file))
}

func exePath() string {
	buf := make([]uint16, 1024)
	n, _, _ := pGetModuleFileNameW.Call(0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func shellOpen(target string) {
	pShellExecute.Call(0, up("open"), up(target), 0, 0, SW_SHOWNORMAL)
}

func clipboardText() string {
	if r, _, _ := pOpenClipboard.Call(0); r == 0 {
		return ""
	}
	defer pCloseClipboard.Call()
	h, _, _ := pGetClipboardData.Call(13) // CF_UNICODETEXT
	if h == 0 {
		return ""
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer pGlobalUnlock.Call(h)
	var s []uint16
	for i := 0; i < 16384; i++ {
		ch := *(*uint16)(unsafe.Pointer(p + uintptr(i*2)))
		if ch == 0 {
			break
		}
		s = append(s, ch)
	}
	return syscall.UTF16ToString(s)
}

// ---------------------------------------------------------------- DPAPI
type dataBlob struct {
	cb uint32
	pb *byte
}

// Beschreibung "BambuWatch" wie in 1.7 (fuer das Entschluesseln egal)
func dpapi(in []byte, protect bool) []byte {
	if len(in) == 0 {
		return nil
	}
	src := dataBlob{uint32(len(in)), &in[0]}
	var dst dataBlob
	var r uintptr
	if protect {
		r, _, _ = pCryptProtectData.Call(uintptr(unsafe.Pointer(&src)), up("BambuWatch"), 0, 0, 0, 1, uintptr(unsafe.Pointer(&dst)))
	} else {
		r, _, _ = pCryptUnprotectData.Call(uintptr(unsafe.Pointer(&src)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&dst)))
	}
	if r == 0 || dst.pb == nil {
		return nil
	}
	out := make([]byte, dst.cb)
	copy(out, unsafe.Slice(dst.pb, dst.cb))
	pLocalFree.Call(uintptr(unsafe.Pointer(dst.pb)))
	return out
}

// ---------------------------------------------------------------- Taskleisten-Fortschritt (ITaskbarList3)
type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

var (
	clsidTaskbarList = guid{0x56FDF344, 0xFD6D, 0x11D0, [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iidTaskbarList3  = guid{0xEA1AFB91, 0x9E28, 0x4B86, [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF}}
	taskbar          uintptr
)

const (
	TBPF_NOPROGRESS    = 0
	TBPF_INDETERMINATE = 0x1
	TBPF_NORMAL        = 0x2
	TBPF_ERROR         = 0x4
	TBPF_PAUSED        = 0x8
)

func taskbarInit() {
	if taskbar != 0 {
		return
	}
	pCoInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED
	var p uintptr
	r, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidTaskbarList)), 0, 1,
		uintptr(unsafe.Pointer(&iidTaskbarList3)), uintptr(unsafe.Pointer(&p)))
	if r != 0 || p == 0 {
		return
	}
	if comCall(p, 3) != 0 { // HrInit
		return
	}
	taskbar = p
}

func comCall(obj uintptr, idx int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func taskbarProgress(hwnd uintptr, state int, value, total uint64) {
	if taskbar == 0 {
		return
	}
	comCall(taskbar, 10, hwnd, uintptr(state)) // SetProgressState
	if state == TBPF_NORMAL || state == TBPF_ERROR || state == TBPF_PAUSED {
		comCall(taskbar, 9, hwnd, uintptr(value), uintptr(total)) // SetProgressValue
	}
}

// ---------------------------------------------------------------- Infobereich (Tray)
type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         guid
	hBalloonIcon     uintptr
}

func copyU16(dst []uint16, s string) {
	p, _ := syscall.UTF16FromString(s)
	if len(p) > len(dst) {
		p = p[:len(dst)]
		p[len(p)-1] = 0
	}
	copy(dst, p)
}

// msg: 0 hinzufuegen, 1 aendern, 2 entfernen
func trayIcon(msg uintptr, hwnd uintptr, icon uintptr, tip, infoTitle, info string) {
	var d notifyIconData
	d.cbSize = uint32(unsafe.Sizeof(d))
	d.hWnd, d.uID = hwnd, 1
	d.uFlags = 0x1 | 0x2 | 0x4 // NIF_MESSAGE | NIF_ICON | NIF_TIP
	d.uCallbackMessage = wmTray
	d.hIcon = icon
	copyU16(d.szTip[:], tip)
	if info != "" {
		d.uFlags |= 0x10 // NIF_INFO
		copyU16(d.szInfo[:], info)
		copyU16(d.szInfoTitle[:], infoTitle)
		d.dwInfoFlags = 1 // NIIF_INFO
	}
	pShellNotifyIcon.Call(msg, uintptr(unsafe.Pointer(&d)))
}

// ---------------------------------------------------------------- Prozesse (Bambu Studio laeuft?)
type processEntry32 struct {
	dwSize              uint32
	cntUsage            uint32
	th32ProcessID       uint32
	th32DefaultHeapID   uintptr
	th32ModuleID        uint32
	cntThreads          uint32
	th32ParentProcessID uint32
	pcPriClassBase      int32
	dwFlags             uint32
	szExeFile           [260]uint16
}

func processRunning(exe string) bool {
	snap, _, _ := pCreateToolhelp32Snap.Call(0x2, 0) // TH32CS_SNAPPROCESS
	if snap == 0 || snap == ^uintptr(0) {
		return false
	}
	defer pCloseHandle.Call(snap)
	var pe processEntry32
	pe.dwSize = uint32(unsafe.Sizeof(pe))
	r, _, _ := pProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	for r != 0 {
		if strings.EqualFold(syscall.UTF16ToString(pe.szExeFile[:]), exe) {
			return true
		}
		r, _, _ = pProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	}
	return false
}

// ---------------------------------------------------------------- Ton und dunkle Menues
func playSound(file string) {
	pPlaySoundW.Call(up(file), 0, 0x00020000|0x0001|0x0002) // SND_FILENAME | SND_ASYNC | SND_NODEFAULT
}

// dunkle Kontextmenues (undokumentiert: uxtheme Ordinal 135 SetPreferredAppMode, 136 FlushMenuThemes)
func darkMenus() {
	h, _, _ := pLoadLibraryW.Call(up("uxtheme.dll"))
	if h == 0 {
		return
	}
	if f, _, _ := pGetProcAddress.Call(h, 135); f != 0 {
		syscall.SyscallN(f, 2) // AllowDark=1 / ForceDark=2
	}
	if f, _, _ := pGetProcAddress.Call(h, 136); f != 0 {
		syscall.SyscallN(f)
	}
}

// Menue an der Mausposition (oder x/y) zeigen; Rueckgabe: gewaehlte ID (0 = nichts)
type menuItem struct {
	id      int
	text    string
	checked bool
	sub     []menuItem
}

func buildMenu(items []menuItem) uintptr {
	m, _, _ := pCreatePopupMenu.Call()
	for _, it := range items {
		switch {
		case it.text == "":
			pAppendMenuW.Call(m, MF_SEPARATOR, 0, 0)
		case it.sub != nil:
			pAppendMenuW.Call(m, MF_POPUP, buildMenu(it.sub), up(it.text))
		default:
			f := uintptr(MF_STRING)
			if it.checked {
				f |= MF_CHECKED
			}
			pAppendMenuW.Call(m, f, uintptr(it.id), up(it.text))
		}
	}
	return m
}

func popupMenu(owner uintptr, items []menuItem, x, y int32) int {
	m := buildMenu(items)
	pSetForegroundWindow.Call(owner)
	r, _, _ := pTrackPopupMenu.Call(m, 0x0100|0x0080, uintptr(x), uintptr(y), 0, owner, 0) // TPM_RETURNCMD | TPM_NONOTIFY
	pDestroyMenu.Call(m)
	return int(r)
}
