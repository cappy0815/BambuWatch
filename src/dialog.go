//go:build windows

// Dunkle Dialoge im Stil von 1.7: alles selbst gezeichnet (Abschnitte, Hinweise, Haekchen,
// Umschalt-Felder, Pillen-Knoepfe, Reiter), nur die Eingabefelder sind echte EDIT-Steuerelemente.
// Freie Flaechen und Beschriftungen sind Greifflaechen zum Verschieben.
package main

import (
	"math"
	"strings"
	"unsafe"
)

const (
	wmDlgEdit = WM_APP + 20
	dlgClass  = "BambuWatchDlg"
)

type widget struct {
	id         string // Aktion beim Klick ("" = keine)
	x, y, w, h float64
	page       int  // -1 = immer sichtbar, sonst Reiter-Seite
	hide       bool // zusaetzlich ausgeblendet (z. B. Code-Zeile)
	text       string
	col        uint32
	px         float64
	style      int
	align      int
	on         bool    // Haekchen
	fill       uint32  // Pille
	edit       uintptr // Eingabefeld
	pass       bool
	draw       func(c *canvas, wd *widget, hot bool)
}

type dialog struct {
	hwnd    uintptr
	title   string
	w, h    float64
	ws      []*widget
	hover   string
	page    int
	nTabs   int
	tabY    float64
	onClick func(id string)
	onOK    func() // Enter
	onClose func() // X, Esc, Schliessen
	// Rollen (Changelog): Bausteine ab scrollFrom verschieben sich um scroll
	scrollFrom, scroll, content float64
}

var (
	dialogs   = map[uintptr]*dialog{}
	editBrush uintptr
	editFont  uintptr
	editFontA float64 = -1
)

func dlgInit() {
	registerClass(dlgClass, dlgProc, appIcon)
	editBrush, _, _ = pCreateSolidBrush.Call(cref(cPanel))
}

func newDialog(title string, w float64) *dialog {
	return &dialog{title: title, w: w, page: 0}
}

// Fenster anlegen und zeigen (Hoehe aus dem Aufbau); zentriert ueber dem Hauptfenster
func (d *dialog) show(h float64, owner uintptr) {
	wa0 := workArea()
	if maxH := L(wa0.bottom-wa0.top) - 40; h > maxH && d.scrollFrom > 0 {
		d.content, h = h, maxH
	}
	d.h = h
	ex := uint32(WS_EX_TOOLWIN)
	if cfg.onTop {
		ex |= WS_EX_TOPMOST
	}
	mr := windowRect(hMain)
	x := mr.left + (mr.right-mr.left-S(d.w))/2
	y := mr.top + S(40)
	wa := workArea()
	if x+S(d.w) > wa.right {
		x = wa.right - S(d.w)
	}
	if x < wa.left {
		x = wa.left
	}
	if y+S(h) > wa.bottom {
		y = wa.bottom - S(h) - S(10)
	}
	if y < wa.top {
		y = wa.top
	}
	d.hwnd = createPopupOwned(dlgClass, d.title+" – "+appName, ex, x, y, S(d.w), S(h), owner)
	dialogs[d.hwnd] = d
	d.fontCheck()
	// Eingabefelder anlegen
	for i, wd := range d.ws {
		if wd.id == "\x00edit" {
			wd.edit = createEdit(d.hwnd, 1000+i, wd.pass)
			sendMsg(wd.edit, WM_SETFONT, editFont, 0)
			if wd.pass {
				sendMsg(wd.edit, EM_SETPASSWORDCHAR, 0x25CF, 0)
			}
			setText(wd.edit, wd.text)
			d.placeEdit(wd)
		}
	}
	d.applyPage()
	pShowWindow.Call(d.hwnd, SW_SHOW)
	pSetForegroundWindow.Call(d.hwnd)
}

func (d *dialog) fontCheck() {
	if editFont != 0 && editFontA == fontAdd {
		return
	}
	if editFont != 0 {
		pDeleteObject.Call(editFont)
	}
	h := -S(40.0/3 + fontAdd) // 10 pt
	editFont, _, _ = pCreateFontW.Call(uintptr(h), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, up(uiFn))
	editFontA = fontAdd
}

func (d *dialog) placeEdit(wd *widget) {
	eh := (40.0/3 + fontAdd) * 1.36
	if eh > wd.h-2 {
		eh = wd.h - 2
	}
	y := wd.y + (wd.h-eh)/2
	pMoveWindow.Call(wd.edit, uintptr(S(wd.x+6)), uintptr(S(y)), uintptr(S(wd.w-12)), uintptr(S(eh)), 1)
}

func (d *dialog) close() {
	if d == nil || d.hwnd == 0 {
		return
	}
	h := d.hwnd
	d.hwnd = 0
	delete(dialogs, h)
	pDestroyWindow.Call(h)
}

func (d *dialog) alive() bool { return d != nil && d.hwnd != 0 }

func (d *dialog) visible(wd *widget) bool {
	return !wd.hide && (wd.page < 0 || wd.page == d.page)
}

func (d *dialog) applyPage() {
	if d == nil {
		return
	}
	for _, wd := range d.ws {
		if wd.edit != 0 {
			sw := SW_HIDE
			if d.visible(wd) {
				sw = SW_SHOW
			}
			pShowWindow.Call(wd.edit, uintptr(sw))
		}
	}
	invalidate(d.hwnd)
}

func (d *dialog) setPage(p int) {
	if d == nil || d.page == p {
		return
	}
	d.page = p
	d.applyPage()
}

func (d *dialog) redraw() {
	if d != nil {
		invalidate(d.hwnd)
	}
}

func (d *dialog) byID(id string) *widget {
	for _, wd := range d.ws {
		if wd.id == id {
			return wd
		}
	}
	return nil
}

// ---------------------------------------------------------------- Bausteine
var curPage = -1 // Seite fuer neu angelegte Bausteine

func (d *dialog) add(wd *widget) *widget {
	wd.page = curPage
	d.ws = append(d.ws, wd)
	return wd
}

func (d *dialog) label(s string, x, y, w, h float64, col uint32, px float64, style, align int) *widget {
	return d.add(&widget{text: s, x: x, y: y, w: w, h: h, col: col, px: px, style: style, align: align,
		draw: func(c *canvas, wd *widget, hot bool) {
			c.text(wd.text, wd.px, wd.style, wd.col, wd.x, wd.y, wd.w, wd.h, wd.align, 1)
		}})
}

// umbrochener Text, Hoehe nach Laenge
func (d *dialog) wrapLbl(s string, x, y, w float64, col uint32, px float64, style, align int) *widget {
	h := textH(s, px, style, w) + 2
	return d.add(&widget{text: s, x: x, y: y, w: w, h: h, col: col, px: px, style: style, align: align,
		draw: func(c *canvas, wd *widget, hot bool) {
			c.wrap(wd.text, wd.px, wd.style, wd.col, wd.x, wd.y, wd.w, wd.h+40, wd.align)
		}})
}

// Kopf mit Titel, X und gruener Linie; liefert die naechste y-Position
func (d *dialog) head(title string) float64 {
	d.label(title, 20, 12, d.w-80, 30, cTxt, 18.7, 1, 0)
	d.closeX()
	d.add(&widget{x: 20, y: 48, w: d.w - 40, h: 2, draw: func(c *canvas, wd *widget, hot bool) { c.fill(wd.x, wd.y, wd.w, wd.h, cGreen) }})
	return 54
}

func (d *dialog) closeX() {
	d.add(&widget{id: "close", x: d.w - 44, y: 0, w: 44, h: 36, draw: func(c *canvas, wd *widget, hot bool) {
		col := uint32(cTxt2)
		if hot {
			c.fill(wd.x, wd.y, wd.w, wd.h, cRed)
			col = cWhite
		}
		c.glyph(0xE8BB, 12, col, wd.x, wd.y, wd.w, wd.h)
	}})
}

func (d *dialog) section(title string, y float64) float64 {
	y += 14
	d.label(strings.ToUpper(title), 20, y, d.w-40, 16, cGreen, 10.7, 1, 0)
	return y + 22
}

func (d *dialog) hint(s string, x, y, w float64) float64 {
	wd := d.wrapLbl(s, x, y, w, cTxt2, 10.7, 0, 0)
	return y + wd.h + 2
}

// Eingabefeld mit Beschriftung darueber
func (d *dialog) field(lbl, val string, x, y, w float64, pass bool) *widget {
	d.label(lbl, x, y, w, 16, cTxt2, 10.7, 0, 0)
	return d.input(val, x, y+18, w, pass)
}

func (d *dialog) input(val string, x, y, w float64, pass bool) *widget {
	return d.add(&widget{id: "\x00edit", text: val, x: x, y: y, w: w, h: 24, pass: pass,
		draw: func(c *canvas, wd *widget, hot bool) {
			c.fill(wd.x, wd.y, wd.w, wd.h, cPanel)
			c.frame(wd.x, wd.y, wd.w, wd.h, cLine)
		}})
}

// Haekchen im Fluent-Stil, Text daneben ebenfalls klickbar
func (d *dialog) toggle(s string, on bool, x, y, w float64, id string) *widget {
	return d.add(&widget{id: id, text: s, on: on, x: x, y: y, w: w, h: 22,
		draw: func(c *canvas, wd *widget, hot bool) {
			g, col := rune(0xE739), uint32(cTxt2)
			if wd.on {
				g, col = 0xE73A, cGreen
			}
			c.glyph(g, 14.7, col, wd.x, wd.y, 20, wd.h)
			tc := uint32(cTxt)
			if hot {
				tc = cWhite
			}
			c.text(wd.text, 12, 0, tc, wd.x+26, wd.y, wd.w-26, wd.h, 0, 1)
		}})
}

// Auswahl, die sich per Klick weiterschaltet ("Text  ›")
func (d *dialog) cycle(s string, x, y, w float64, id string) *widget {
	return d.add(&widget{id: id, text: s, x: x, y: y, w: w, h: 22,
		draw: func(c *canvas, wd *widget, hot bool) {
			bg := uint32(cPanel)
			if hot {
				bg = cHover
			}
			c.rrect(wd.x, wd.y, wd.w, wd.h, 4, bg)
			c.text(wd.text+"  ›", 12, 1, cTxt, wd.x+4, wd.y, wd.w-8, wd.h, 1, 1)
		}})
}

func (d *dialog) pill(s string, x, y, w, h float64, fill uint32, id string) *widget {
	return d.add(&widget{id: id, text: s, x: x, y: y, w: w, h: h, fill: fill,
		draw: func(c *canvas, wd *widget, hot bool) {
			c.pill(wd.x, wd.y, wd.w, wd.h, wd.text, wd.fill, hot)
		}})
}

// unterstrichener Link
func (d *dialog) link(s string, x, y, w, h float64, align int, id string) *widget {
	return d.add(&widget{id: id, text: s, x: x, y: y, w: w, h: h, align: align,
		draw: func(c *canvas, wd *widget, hot bool) {
			col := uint32(cBlue)
			if hot {
				col = lighter(lighter(cBlue))
			}
			c.text(wd.text, 11.3, 4, col, wd.x, wd.y, wd.w, wd.h, wd.align, 1)
		}})
}

func (d *dialog) hline(x, y, w float64, col uint32) {
	d.add(&widget{x: x, y: y, w: w, h: 1, col: col, draw: func(c *canvas, wd *widget, hot bool) { c.fill(wd.x, wd.y, wd.w, wd.h, wd.col) }})
}

// Reiter; Klick liefert "tab:<i>"
func (d *dialog) tabs(names []string, xs, ws []float64, y float64) {
	d.nTabs = len(names)
	d.tabY = y + 26
	save := curPage
	curPage = -1
	for i, n := range names {
		i := i
		d.add(&widget{id: "tab:" + itoa(i), text: n, x: xs[i], y: y, w: ws[i], h: 26,
			draw: func(c *canvas, wd *widget, hot bool) {
				col := uint32(cTxt2)
				if d.page == i || hot {
					col = cTxt
				}
				c.text(wd.text, 12, 1, col, wd.x, wd.y, wd.w, wd.h, 1, 1)
				if d.page == i {
					c.fill(wd.x, d.tabY, wd.w, 2, cGreen)
				}
			}})
	}
	curPage = save
}

// Text eines Eingabefelds
func (wd *widget) val() string {
	if wd == nil || wd.edit == 0 {
		if wd != nil {
			return wd.text
		}
		return ""
	}
	return getText(wd.edit)
}

func (wd *widget) set(s string) {
	wd.text = s
	if wd.edit != 0 {
		setText(wd.edit, s)
	}
}

// ---------------------------------------------------------------- Fensterprozedur
func (d *dialog) scrolled(wd *widget) bool { return d.content > 0 && wd.y >= d.scrollFrom }

func (d *dialog) hit(x, y float64) *widget {
	for i := len(d.ws) - 1; i >= 0; i-- {
		wd := d.ws[i]
		if wd.id == "" || wd.id == "\x00edit" || !d.visible(wd) {
			continue
		}
		yy := y
		if d.scrolled(wd) {
			if y < d.scrollFrom {
				continue
			}
			yy += d.scroll
		}
		if x >= wd.x && x < wd.x+wd.w && yy >= wd.y && yy < wd.y+wd.h {
			return wd
		}
	}
	return nil
}

func (d *dialog) paint(c *canvas, w, h float64) {
	c.clear(cBG)
	for _, wd := range d.ws {
		if d.visible(wd) && wd.draw != nil && !d.scrolled(wd) {
			wd.draw(c, wd, wd.id != "" && wd.id == d.hover)
		}
	}
	if d.content == 0 {
		return
	}
	pGdipSetClipRect.Call(c.g, f32(0), f32(d.scrollFrom), f32(w), f32(h-d.scrollFrom), 0)
	pGdipTranslateWorld.Call(c.g, f32(0), f32(-d.scroll), 0)
	for _, wd := range d.ws {
		if d.visible(wd) && wd.draw != nil && d.scrolled(wd) && wd.y+wd.h > d.scroll+d.scrollFrom && wd.y < d.scroll+h {
			wd.draw(c, wd, wd.id != "" && wd.id == d.hover)
		}
	}
	pGdipTranslateWorld.Call(c.g, f32(0), f32(d.scroll), 0)
	pGdipResetClip.Call(c.g)
	// Rollbalken
	view := h - d.scrollFrom
	total := d.content - d.scrollFrom
	bh := math.Max(30, view*view/total)
	by := d.scrollFrom + (view-bh)*d.scroll/(d.content-h)
	c.rrect(w-7, by+2, 4, bh-4, 2, cLine)
}

func (d *dialog) wheel(delta int) {
	if d.content == 0 {
		return
	}
	d.scroll -= float64(delta) / 120 * 60
	d.scroll = math.Max(0, math.Min(d.scroll, d.content-d.h))
	invalidate(d.hwnd)
}

func dlgProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	d := dialogs[hwnd]
	if d == nil {
		r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	}
	switch uint32(msg) {
	case WM_PAINT:
		paintWindow(hwnd, d.paint)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_CTLCOLOREDIT:
		pSetTextColor.Call(wParam, cref(cTxt))
		pSetBkColor.Call(wParam, cref(cPanel))
		return editBrush
	case WM_MOUSEMOVE:
		x, y := mouseXY(lParam)
		id := ""
		if wd := d.hit(L(x), L(y)); wd != nil {
			id = wd.id
		}
		if id != d.hover {
			d.hover = id
			invalidate(hwnd)
		}
		trackLeave(hwnd)
		return 0
	case WM_SETCURSOR:
		if d.hover != "" && int(lParam&0xFFFF) == HTCLIENT {
			setHandCursor()
			return 1
		}
	case WM_MOUSELEAVE:
		if d.hover != "" {
			d.hover = ""
			invalidate(hwnd)
		}
		return 0
	case WM_LBUTTONDOWN, WM_LBUTTONDBLCLK:
		x, y := mouseXY(lParam)
		wd := d.hit(L(x), L(y))
		if wd == nil {
			dragWindow(hwnd)
			return 0
		}
		switch {
		case wd.id == "close":
			if d.onClose != nil {
				d.onClose()
			} else {
				d.close()
			}
			return 0
		case strings.HasPrefix(wd.id, "tab:"):
			d.setPage(atoiDef(wd.id[4:], 0))
		case strings.HasPrefix(wd.id, "tog:"):
			wd.on = !wd.on
		}
		if d.onClick != nil {
			d.onClick(wd.id)
		}
		if d.alive() {
			invalidate(hwnd)
		}
		return 0
	case 0x020A: // WM_MOUSEWHEEL
		d.wheel(int(int16(wParam >> 16)))
		return 0
	case WM_COMMAND:
		switch wParam & 0xFFFF {
		case 1: // IDOK (Enter)
			if d.onOK != nil {
				d.onOK()
			}
			return 0
		case 2: // IDCANCEL (Esc)
			if d.onClose != nil {
				d.onClose()
			} else {
				d.close()
			}
			return 0
		}
	case WM_KEYDOWN:
		switch wParam {
		case 0x1B:
			if d.onClose != nil {
				d.onClose()
			} else {
				d.close()
			}
		case 0x0D:
			if d.onOK != nil {
				d.onOK()
			}
		}
		return 0
	case WM_CLOSE:
		if d.onClose != nil {
			d.onClose()
		} else {
			d.close()
		}
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// Fenster an einer beliebigen Stelle ziehen (wie _DragAny in 1.7)
func dragWindow(hwnd uintptr) {
	pReleaseCapture.Call()
	sendMsg(hwnd, 0x00A1, 2, 0) // WM_NCLBUTTONDOWN, HTCAPTION
}

var pReleaseCapture = user32.NewProc("ReleaseCapture")

// Tastatur fuer Dialoge (Tab, Enter, Esc in Eingabefeldern)
func dlgMessage(m *msgT) bool {
	root, _, _ := pGetAncestor.Call(m.hwnd, 2) // GA_ROOT
	d := dialogs[root]
	if d == nil {
		return false
	}
	r, _, _ := pIsDialogMessageW.Call(root, uintptr(unsafe.Pointer(m)))
	return r != 0
}

var pGetAncestor = user32.NewProc("GetAncestor")

func itoa(i int) string {
	if i < 0 {
		return "-" + itoa(-i)
	}
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}
