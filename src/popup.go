//go:build windows

// Meldungsfenster unten rechts (Druckende, Pause, Abbruch) und eigene dunkle Tooltips.
package main

import (
	"os"
	"strings"
	"time"
)

const (
	popClass = "BambuWatchPop"
	tipClass = "BambuWatchTip"
	popW     = 380.0
	popH     = 112.0
)

var (
	hPop                       uintptr
	popAt                      time.Time
	popTitle, popText, popTime string
	popCol                     uint32
)

func popupInit() {
	registerClass(popClass, popProc, appIcon)
	registerClass(tipClass, tipProc, 0)
}

func notify(title, text string, col uint32) {
	if !cfg.notify {
		return
	}
	popupShow(title, text, col)
	if cfg.sound {
		win := os.Getenv("WINDIR")
		wav := win + `\Media\Windows Notify System Generic.wav`
		if _, err := os.Stat(wav); err != nil {
			wav = win + `\Media\notify.wav`
		}
		if _, err := os.Stat(wav); err == nil {
			playSound(wav)
		}
	}
	toastShow(title)
}

func popupShow(title, text string, col uint32) {
	popupClose()
	popTitle, popText, popCol = title, text, col
	popTime = time.Now().Format("15:04")
	wa := workArea()
	x, y := wa.right-S(popW)-S(16), wa.bottom-S(popH)-S(16)
	hPop = createPopup(popClass, title+" – "+appName, WS_EX_TOOLWIN|WS_EX_TOPMOST|WS_EX_NOACTIV, x, y, S(popW), S(popH))
	pShowWindow.Call(hPop, SW_SHOWNOACTIVATE)
	popAt = time.Now()
}

func popupClose() {
	if hPop != 0 {
		h := hPop
		hPop = 0
		pDestroyWindow.Call(h)
	}
}

func paintPop(c *canvas, w, h float64) {
	c.clear(cBG)
	c.fill(0, 0, 5, h, popCol)
	syncImages()
	xt := 18.0
	img := coverImg
	if img == nil {
		img = frameImg
	}
	if img != nil {
		c.fill(14, 12, 88, 88, 0xFF15171B)
		c.imageFit(img, 14, 12, 88, 88, 0)
		xt = 116
	}
	c.text(popTitle, 15, 1, cWhite, xt, 14, w-xt-14, 24, 0, 1)
	c.text(popText, 12, 0, cTxt, xt, 42, w-xt-14, 22, 0, 1)
	c.text(eng.p().name+"  ·  "+popTime+"  ·  Klick öffnet BambuWatch", 10.5, 0, cTxt2, xt, h-34, w-xt-14, 20, 0, 1)
}

func popProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case WM_PAINT:
		paintWindow(hwnd, paintPop)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_SETCURSOR:
		setHandCursor()
		return 1
	case WM_LBUTTONDOWN:
		popupClose()
		mainShow()
		return 0
	case 0x0021: // WM_MOUSEACTIVATE: nicht aktivieren
		return 3 // MA_NOACTIVATE
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// ---------------------------------------------------------------- Tooltips (Leiste, Statuspunkte)
var (
	hTip               uintptr
	tipTarget, tipText string
	tipSince           time.Time
)

func tipTrack(target string) {
	if target == tipTarget {
		return
	}
	tipTarget, tipSince = target, time.Now()
	tipHide()
}

func tipFor(target string) string {
	switch target {
	case "dot0", "dot1":
		_, _, cam, mq := dotInfo()
		if target == "dot0" {
			return cam
		}
		return mq
	case "title":
		return "Drucker wechseln"
	}
	for _, b := range barBtns {
		if b.id == target {
			return b.tip
		}
	}
	return ""
}

// alle 100 ms: nach kurzer Pause zeigen, Inhalt aktuell halten (Statuspunkte aendern sich)
func tipTimer() {
	if tipTarget == "" {
		return
	}
	if hTip == 0 && time.Since(tipSince) < 600*time.Millisecond {
		return
	}
	s := tipFor(tipTarget)
	if s == "" {
		tipHide()
		return
	}
	if hTip != 0 && s == tipText {
		return
	}
	tipText = s
	lines := strings.Split(s, "\n")
	w := 0.0
	for _, l := range lines {
		if tw := textW(l, 11.3, 0); tw > w {
			w = tw
		}
	}
	w += 20
	h := float64(len(lines))*(15.5+fontAdd) + 10
	p := cursorPos()
	x, y := p.x+S(12), p.y+S(20)
	wa := workArea()
	if x+S(w) > wa.right {
		x = wa.right - S(w)
	}
	if y+S(h) > wa.bottom {
		y = p.y - S(h) - S(6)
	}
	if hTip == 0 {
		hTip = createPopup(tipClass, "", WS_EX_TOOLWIN|WS_EX_TOPMOST|WS_EX_NOACTIV|0x20, x, y, S(w), S(h)) // 0x20 WS_EX_TRANSPARENT
		pShowWindow.Call(hTip, SW_SHOWNOACTIVATE)
	} else {
		pSetWindowPos.Call(hTip, 0, uintptr(x), uintptr(y), uintptr(S(w)), uintptr(S(h)), SWP_NOZORDER|SWP_NOACTIVATE)
		invalidate(hTip)
	}
}

func tipHide() {
	if hTip != 0 {
		h := hTip
		hTip = 0
		pDestroyWindow.Call(h)
	}
	tipText = ""
}

func tipProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case WM_PAINT:
		paintWindow(hwnd, func(c *canvas, w, h float64) {
			c.clear(cPanel)
			for i, l := range strings.Split(tipText, "\n") {
				c.text(l, 11.3, 0, cTxt, 10, 5+float64(i)*(15.5+fontAdd), w-14, 15.5+fontAdd, 0, 1)
			}
		})
		return 0
	case WM_ERASEBKGND:
		return 1
	case 0x0021:
		return 3
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}
