//go:build windows

// BambuWatch 2.0 – Live-Bild und Druckstatus von Bambu-Lab-Druckern in einem kleinen Fenster.
// Start, Einstellungsdatei, Verbindung zwischen Steuerung (engine.go) und Oberflaeche.
package main

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const mainClass = "BambuWatchMain"

var (
	comdlg32           = syscall.NewLazyDLL("comdlg32.dll")
	pGetOpenFileNameW  = comdlg32.NewProc("GetOpenFileNameW")
	pSHBrowseForFolder = shell32.NewProc("SHBrowseForFolderW")
	pSHGetPathFromIDL  = shell32.NewProc("SHGetPathFromIDListW")
	pSHGetFolderPathW  = shell32.NewProc("SHGetFolderPathW")
	pCoTaskMemFree     = ole32.NewProc("CoTaskMemFree")
	pOpenMutexW        = kernel32.NewProc("OpenMutexW")
	pMessageBoxW       = user32.NewProc("MessageBoxW")
)

func init() { runtime.LockOSThread() } // Fenster gehoeren dem Haupt-Thread

func main() {
	// nur eine Instanz: zweiter Start holt das vorhandene Fenster nach vorn
	_, _, e := pCreateMutexW.Call(0, 0, up(`Local\BambuWatchMutex`))
	if e == syscall.Errno(183) { // ERROR_ALREADY_EXISTS
		if h, _, _ := pFindWindowW.Call(up(mainClass), 0); h != 0 {
			pShowWindow.Call(h, SW_SHOW)
			pShowWindow.Call(h, SW_RESTORE)
			pSetForegroundWindow.Call(h)
		}
		return
	}
	// alte AutoIt-Version (1.x) laeuft noch? Beide gleichzeitig wuerden sich die Drucker teilen.
	if h, _, _ := pOpenMutexW.Call(0x00100000, 0, up("BambuWatch_Micha_SingleInstance")); h != 0 {
		pCloseHandle.Call(h)
		pMessageBoxW.Call(0, up("Die alte BambuWatch-Version (1.x) läuft noch.\n\nBitte zuerst beenden (Infobereich › Beenden), dann BambuWatch 2.0 erneut starten."),
			up(appName+" "+appVersion), 0x40)
		return
	}

	initGfx()
	darkMenus()
	iniFile = iniPath()
	ini = winIni{iniFile}
	dpapiProtect = func(b []byte) []byte { return dpapi(b, true) }
	dpapiUnprotect = func(b []byte) []byte { return dpapi(b, false) }
	cfg = loadSettings(ini, docsDir())
	fontAdd = float64(cfg.fontAdd)
	eng = newEngine(cfg)

	uiToast = toastShow
	uiNotify = notify
	uiDirty = func() { invalidate(hMain) }
	uiStatusChanged = titleUpdate
	uiSave = func() { cfg.savePrinters(ini) }
	uiTrayTip = trayBalloon
	studioRunning = func() bool { return processRunning("bambu-studio.exe") }
	ffmpegFind = findFfmpeg

	hInst := hInstance()
	bx, _, _ := pGetSystemMetrics.Call(11)                          // SM_CXICON
	sx, _, _ := pGetSystemMetrics.Call(49)                          // SM_CXSMICON
	appIcon, _, _ = pLoadImageW.Call(hInst, 1, 1, bx, bx, 0x8000)   // Ressource 1, LR_SHARED
	appIconSm, _, _ = pLoadImageW.Call(hInst, 1, 1, sx, sx, 0x8000) // klein
	registerClass(mainClass, mainProc, appIcon)
	dlgInit()
	popupInit()
	msgTBC, _, _ = pRegisterWindowMessage.Call(up("TaskbarButtonCreated"))
	msgTaskbarNew, _, _ = pRegisterWindowMessage.Call(up("TaskbarCreated"))

	// Groesse und Position (logische Einheiten in der INI)
	w, h := float64(cfg.w), float64(cfg.h)
	if w < minW {
		w = 560
	}
	if h < minH {
		h = math.Round((w-2*pad)/aspect) + barH + pad
	}
	x, y := float64(cfg.x), float64(cfg.y)
	vx, _, _ := pGetSystemMetrics.Call(76) // virtueller Bildschirm
	vy, _, _ := pGetSystemMetrics.Call(77)
	vw, _, _ := pGetSystemMetrics.Call(78)
	vh, _, _ := pGetSystemMetrics.Call(79)
	VX, VY, VW, VH := L(int32(vx)), L(int32(vy)), L(int32(vw)), L(int32(vh))
	if cfg.x == -1 && cfg.y == -1 || x < VX-w+80 || x > VX+VW-80 || y < VY || y > VY+VH-60 {
		wa := workArea()
		x = L(wa.left) + (L(wa.right-wa.left)-w)/2
		y = L(wa.top) + (L(wa.bottom-wa.top)-h)/2
	}
	ex := uint32(0)
	if cfg.onTop {
		ex = WS_EX_TOPMOST
	}
	hMain = createPopup(mainClass, appName, ex, S(x), S(y), S(w), S(h))
	sendMsg(hMain, WM_SETICON, 1, appIcon)
	sendMsg(hMain, WM_SETICON, 0, appIconSm)
	setAlpha(hMain, cfg.alpha)
	trayIcon(0, hMain, appIconSm, appName, "", "")
	wake = func() { pPostMessageW.Call(hMain, wmEvent, 0, 0) }
	wake()
	pShowWindow.Call(hMain, SW_SHOW)
	titleUpdate()
	pSetTimer.Call(hMain, 1, 1000, 0) // Sekundentakt
	pSetTimer.Call(hMain, 2, 100, 0)  // Tooltips, Hinweise

	eng.studioPause = cfg.studio && studioRunning()
	if !eng.canLan(false) && cfg.token == "" && !(cfg.haConfigured() && eng.p().haCam != "") {
		settingsShow(0)
	} else {
		eng.helpersStart()
	}

	var m msgT
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if m.message == WM_TIMER && m.hwnd == hMain && m.wParam == 1 {
			settingsTick()
		}
		if dlgMessage(&m) {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// Einstellungsdatei: wie 1.7 neben der EXE (dann findet 2.0 alle Drucker sofort),
// sonst – z. B. unter "Programme" – im AppData-Ordner.
func iniPath() string {
	dir := filepath.Dir(exePath())
	local := filepath.Join(dir, "BambuWatch.ini")
	appDir := filepath.Join(os.Getenv("APPDATA"), "BambuWatch")
	roam := filepath.Join(appDir, "BambuWatch.ini")
	switch {
	case fileExists(local) && dirWritable(dir):
		return local
	case fileExists(roam):
		return roam
	case dirWritable(dir):
		return local
	}
	os.MkdirAll(appDir, 0755)
	if fileExists(local) { // nur lesbar -> Kopie uebernehmen
		if b, err := os.ReadFile(local); err == nil {
			os.WriteFile(roam, b, 0644)
		}
	}
	return roam
}

func fileExists(f string) bool { st, err := os.Stat(f); return err == nil && !st.IsDir() }

func dirWritable(dir string) bool {
	f := filepath.Join(dir, "~bw_write_test.tmp")
	if os.WriteFile(f, nil, 0644) != nil {
		return false
	}
	os.Remove(f)
	return true
}

func docsDir() string {
	buf := make([]uint16, 520)
	if r, _, _ := pSHGetFolderPathW.Call(0, 5, 0, 0, uintptr(unsafe.Pointer(&buf[0]))); r == 0 { // CSIDL_PERSONAL
		if s := syscall.UTF16ToString(buf); s != "" {
			return s
		}
	}
	return filepath.Join(os.Getenv("USERPROFILE"), "Documents")
}

// ffmpeg.exe: Einstellung, neben der EXE oder im PATH
func findFfmpeg(setting string) string {
	if setting != "" && fileExists(setting) {
		return setting
	}
	if f := filepath.Join(filepath.Dir(exePath()), "ffmpeg.exe"); fileExists(f) {
		return f
	}
	for _, d := range strings.Split(os.Getenv("PATH"), ";") {
		if d = strings.TrimSpace(d); d != "" {
			if f := filepath.Join(d, "ffmpeg.exe"); fileExists(f) {
				return f
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------- Datei- und Ordnerauswahl
func u16z(s string) []uint16 { return append(utf16.Encode([]rune(s)), 0) }

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

// filter: "Name\x00Muster\x00" (wird doppelt abgeschlossen)
func openFileDlg(owner uintptr, title, filter string) string {
	f := u16z(filter)
	file := make([]uint16, 1024)
	t := u16z(title)
	o := openFileName{hwndOwner: owner, lpstrFilter: &f[0], lpstrFile: &file[0], nMaxFile: uint32(len(file)),
		lpstrTitle: &t[0], flags: 0x1000 | 0x800 | 0x8 | 0x80000} // FILEMUSTEXIST|PATHMUSTEXIST|NOCHANGEDIR|EXPLORER
	o.lStructSize = uint32(unsafe.Sizeof(o))
	if r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&o))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(file)
}

type browseInfo struct {
	hwndOwner      uintptr
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

func folderDlg(owner uintptr, title string) string {
	name := make([]uint16, 260)
	t := u16z(title)
	bi := browseInfo{hwndOwner: owner, pszDisplayName: &name[0], lpszTitle: &t[0], ulFlags: 0x1 | 0x40} // RETURNONLYFSDIRS|NEWDIALOGSTYLE
	pidl, _, _ := pSHBrowseForFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer pCoTaskMemFree.Call(pidl)
	buf := make([]uint16, 1024)
	if r, _, _ := pSHGetPathFromIDL.Call(pidl, uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
