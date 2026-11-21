//go:build windows

// Hauptfenster: Leiste (Statuspunkte, Druckername, Symbol-Knoepfe) und Bildflaeche mit Overlay.
// Aufbau, Farben, Texte und Verhalten wie BambuWatch 1.7.
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

const (
	barH = 32.0 // Leiste
	pad  = 6.0  // Rahmen um das Bild (= Greifzone zum Groesse aendern)
	minW = 430.0
	minH = 260.0
	btnW = 30.0

	wmEvent = WM_APP + 1
	wmTray  = WM_APP + 2
)

var (
	cfg     *settings
	eng     *engine
	ini     iniStore
	iniFile string

	hMain, appIcon, appIconSm uintptr
	msgTBC, msgTaskbarNew     uintptr

	hover    string
	frameImg *gimg
	frameSeq = -1
	coverImg *gimg
	coverSeq = -1
	aspect   = 16.0 / 9

	toast   string
	toastAt time.Time

	tbState, tbVal = -1, -1
	firstTrayHint  = true
	lastTrayTip    string
)

// ---------------------------------------------------------------- Knoepfe in der Leiste
type barBtn struct {
	id    string
	glyph rune
	tip   string
}

var barBtns = []barBtn{ // von rechts nach links
	{"close", 0xE8BB, "Beenden"},
	{"min", 0xE921, "Minimieren"},
	{"help", 0xE897, "Hilfe (F1)"},
	{"set", 0xE713, "Einstellungen (F2)"},
	{"pin", 0xE718, "Immer im Vordergrund (T)"},
	{"snap", 0xE722, "Snapshot speichern (S)"},
	{"ovl", 0xE81E, "Status-Overlay umschalten (O)"},
	{"light", 0xE82F, "Druckerlicht an/aus (L)"},
	{"view", 0xE8B9, "Kamerabild / Druckobjekt umschalten"},
}

func btnX(w float64, i int) float64 { // i 0 = ganz rechts
	x := w - float64(i+1)*btnW
	if i > 0 {
		x -= 4
	}
	return x
}

func titleLabel() string { return eng.p().name + "  ▾" }

func titleW(w float64) float64 {
	max := btnX(w, len(barBtns)-1) - 42 - 8
	tw := textW(titleLabel(), 12.7, 1) + 6
	if tw > max {
		tw = max
	}
	return tw
}

// Treffer in Client-Koordinaten (logisch)
func mainHit(x, y float64) string {
	w := L(clientRect(hMain).right)
	if y < barH {
		for i, b := range barBtns {
			if bx := btnX(w, i); x >= bx && x < bx+btnW {
				return b.id
			}
		}
		switch {
		case x >= 10 && x < 23:
			return "dot0"
		case x >= 23 && x < 37:
			return "dot1"
		case x >= 42 && x < 42+titleW(w):
			return "title"
		}
	}
	return ""
}

// ---------------------------------------------------------------- Zeichnen
func paintMain(c *canvas, w, h float64) {
	c.clear(cBG)
	camCol, mqCol, _, _ := dotInfo()
	c.ellipse(13, 11.5, 9, 9, camCol)
	c.ellipse(26, 11.5, 9, 9, mqCol)
	tc := uint32(cTxt)
	if hover == "title" {
		tc = cWhite
	}
	c.text(titleLabel(), 12.7, 1, tc, 42, 0, titleW(w), barH, 0, 1)
	for i, b := range barBtns {
		bx := btnX(w, i)
		col := uint32(cTxt2)
		g := b.glyph
		switch {
		case b.id == "pin":
			if cfg.onTop {
				col = cGreen
			} else {
				g = 0xE77A
			}
		case b.id == "ovl" && cfg.overlay > 0:
			col = cGreen
		case b.id == "light" && eng.stv("light") == "on":
			col = cAmber
		case b.id == "view" && eng.forceCover:
			col = cGreen
		}
		if hover == b.id {
			bg := uint32(cHover)
			if b.id == "close" {
				bg = cRed
			}
			c.fill(bx, 0, btnW, barH, bg)
			if !(b.id == "pin" && cfg.onTop) {
				col = cWhite
			}
		}
		c.glyph(g, 12.7, col, bx, 0, btnW, barH)
	}
	// Bildflaeche
	iw, ih := w-2*pad, h-barH-pad
	if iw < 20 || ih < 20 {
		return
	}
	pGdipTranslateWorld.Call(c.g, f32(pad), f32(barH), 0)
	pGdipSetClipRect.Call(c.g, f32(0), f32(0), f32(iw), f32(ih), 0)
	renderImage(c, iw, ih)
	pGdipResetClip.Call(c.g)
	pGdipTranslateWorld.Call(c.g, f32(-pad), f32(-barH), 0)
}

func fScale(w float64) float64 { return math.Max(0.8, math.Min(1.4, w/560)) }

// gelieferte Bilder dekodieren (nur wenn sich etwas geaendert hat)
func syncImages() {
	if eng.frameSeq != frameSeq {
		frameSeq = eng.frameSeq
		if eng.frame == nil {
			frameImg.free()
			frameImg = nil
		} else if img := loadImage(eng.frame); img != nil {
			frameImg.free()
			frameImg = img
			aspect = float64(img.w) / float64(img.hh)
		}
	}
	if eng.coverSeq != coverSeq {
		coverImg.free()
		coverImg = nil
		if eng.cover != nil {
			coverImg = loadImage(eng.cover)
			if coverImg == nil {
				eng.coverBad()
			}
		}
		coverSeq = eng.coverSeq
	}
}

func renderImage(c *canvas, w, h float64) {
	syncImages()
	c.fill(0, 0, w, h, cImgBg)
	fS := fScale(w)
	p := eng.p()
	cover := eng.coverShown() && coverImg != nil
	frameOld := false
	switch {
	case cover:
		drawCover(c, w, h, fS)
	case frameImg != nil && p.camOn:
		c.imageFit(frameImg, 0, 0, w, h, 0)
		age := since(eng.frameAt)
		if age >= 10*time.Second {
			frameOld = true
		}
		if frameOld && !eng.studioPause {
			c.fill(0, 0, w, h, 0x99000000)
			st, msg, via := eng.camEff()
			why := "verbinde neu …"
			if st == 4 && msg != "" {
				why = msg
			} else if eng.away() && via == "Drucker" {
				why = "unterwegs – Bild nur im Heimnetz"
			}
			badge(c, w, "Kein neues Bild seit "+ageText(age)+" · "+why, w/2, 14*fS, fS, 0xE6303338, cTxt, 1)
		}
	default:
		drawPlaceholder(c, w, h, fS)
	}
	if cfg.overlay > 0 && p.stOn {
		drawOverlay(c, w, h, fS)
	}
	// Herkunft oben links
	if cfg.overlay > 0 || cover {
		orig := ""
		if cover {
			orig = eng.coverLabel() + "  ·  " + eng.noCamWhy()
		} else if frameImg != nil && p.camOn && eng.imgVia == "HA" {
			orig = "Bild: Home Assistant"
		}
		if cfg.overlay > 0 && p.stOn && eng.away() {
			src := "Cloud"
			if eng.src == "HA" {
				src = "Home Assistant"
			}
			if orig != "" {
				orig += "  ·  "
			}
			orig += "Status: " + src
		}
		if orig != "" {
			badge(c, w, orig, 8*fS, 8*fS, fS, 0xD0303338, cTxt2, 0)
		}
	}
	if eng.studioPause && (frameImg != nil || cover) {
		badge(c, w, "Pausiert – Bambu Studio läuft", w/2, 40*fS, fS, 0xE6303338, cAmber, 1)
	}
	if toast != "" {
		if time.Since(toastAt) < 1800*time.Millisecond {
			y := 14.0
			if cover || (frameImg != nil && frameOld) {
				y = 44
			}
			badge(c, w, toast, w/2, y*fS, fS, 0xE600AE42, cWhite, 1)
		} else {
			toast = ""
		}
	}
}

// abgerundete Plakette; anchor 0 = x links, 1 = Mitte, 2 = rechts. Liefert die Breite.
func badge(c *canvas, areaW float64, s string, x, y, fS float64, bg, fg uint32, anchor int) float64 {
	fPx, fH, fP := 11.5*fS, 20*fS, 9*fS
	bw := textW(s, fPx, 1) + 2*fP
	if max := areaW - 16; bw > max {
		bw = max
	}
	x0 := x
	if anchor == 1 {
		x0 = x - bw/2
	} else if anchor == 2 {
		x0 = x - bw
	}
	c.rrect(x0, y, bw, fH, fH/2, bg)
	c.text(s, fPx, 1, fg, x0+fP/2, y, bw-fP, fH, 1, 1)
	return bw
}

func drawPlaceholder(c *canvas, w, h, fS float64) {
	p := eng.p()
	head, sub, col := "", "", uint32(cOff)
	haCam := cfg.haConfigured() && p.haCam != "" && p.imgSrc != 1
	switch {
	case eng.studioPause:
		head, sub, col = "Pausiert – Bambu Studio läuft", "BambuWatch verbindet sich wieder, sobald Bambu Studio beendet ist", cAmber
	case !eng.canLan(false) && cfg.token == "" && !haCam:
		head, sub, col = "Noch nicht eingerichtet", "Einstellungen öffnen: Zahnrad oben oder F2", cGreen
	case !p.camOn:
		head, sub = "Kamera ausgeschaltet", "für diesen Drucker – Einstellungen › Drucker"
	case eng.wHa != nil || p.imgSrc == 2:
		switch {
		case p.imgSrc == 2 && !haCam:
			head, sub, col = "Home Assistant nicht eingerichtet", "Adresse, Token und Kamera in den Einstellungen eintragen", cAmber
		case eng.iHa == 4:
			head, sub, col = "Kein Bild von Home Assistant", eng.haMsg, cRed
		default:
			head, sub, col = "Hole Bild über Home Assistant …", p.haCam, cAmber
		}
	case eng.stv("cam") == "off":
		head, sub, col = "LAN Mode Liveview ist aus", "Am Drucker einschalten, dann kommt das Bild", cAmber
	case eng.camStop:
		head, sub, col = "Kein Kamerabild", eng.camMsg, cAmber
	case !eng.canLan(false):
		head, sub, col = "Noch kein Kamerabild", "Access Code fehlt (Einstellungen)", cAmber
		if p.ip == "" {
			sub = "Warte auf die Drucker-IP aus der Bambu-Cloud …"
		}
	case eng.away():
		head, sub, col = "Unterwegs – kein Kamerabild", "Bild von unterwegs: Home Assistant in den Einstellungen eintragen", cBlue
		if haCam {
			sub = "Home Assistant liefert gerade kein Bild"
		}
	case eng.iCam == 4:
		head, sub, col = "Keine Verbindung zur Kamera", eng.camMsg+" · neuer Versuch läuft", cRed
	case eng.iCam == 2:
		head, sub, col = "Verbunden – warte auf Bild …", "Der Drucker liefert nicht immer sofort ein Bild", cAmber
		if eng.camTypeNow == "rtsp" {
			sub = "Videostream startet (ffmpeg) …"
		}
	default:
		head, col = "Verbinde mit "+p.ip+" …", cAmber
	}
	fG := 54 * fS
	y0 := h/2 - fG*0.9
	if cfg.overlay > 0 && eng.haveSt {
		y0 -= 24 * fS
	}
	if y0 < 4 {
		y0 = 4
	}
	c.textFam(string(rune(0xE714)), glyphFn, fG, 0, col, 0, y0, w, fG*1.25, 1, 1)
	c.text(head, 15*fS, 1, cTxt, 10, y0+fG*1.3, w-20, 24*fS, 1, 1)
	if sub != "" {
		c.text(sub, 11.5*fS, 0, cTxt2, 10, y0+fG*1.3+24*fS, w-20, 20*fS, 1, 1)
	}
}

// Bild des Druckobjekts; oben Platz fuer die Herkunfts-Plakette, unten fuers Overlay
func drawCover(c *canvas, w, h, fS float64) {
	c.fill(0, 0, w, h, 0xFF15171B)
	top, bot := 36*fS, 12*fS
	if cfg.overlay > 0 && eng.p().stOn && eng.haveSt {
		switch {
		case cfg.overlay == 1:
			bot = 34 * fS
		case eng.stv("amsl") != "":
			bot = 99 * fS
		default:
			bot = 79 * fS
		}
	}
	aw, ah := w-24*fS, h-top-bot
	if ah < 60*fS { // Fenster sehr flach: Bild darf unters Overlay
		top, ah = 8*fS, h-16*fS
	}
	if aw < 20 || ah < 20 {
		return
	}
	c.imageFit(coverImg, (w-aw)/2, top, aw, ah, 3) // kleine Vorschaubilder nicht ins Unscharfe ziehen
}

// ---------------------------------------------------------------- Overlay
func stateInfo(s string) (string, uint32) {
	switch s {
	case "RUNNING":
		return "Druckt", cGreen
	case "PAUSE":
		return "Pausiert", cAmber
	case "PREPARE":
		return "Vorbereitung", cAmber
	case "SLICING":
		return "Slicing", cAmber
	case "FINISH":
		return "Fertig", cBlue
	case "FAILED":
		return "Abgebrochen", cRed
	case "INIT":
		return "Startet", cAmber
	case "OFFLINE":
		return "Offline", cOff
	case "IDLE":
		return "Bereit", cTxt2
	case "":
		return "–", cTxt2
	}
	return s, cTxt2
}

func speedText(s string) string {
	switch s {
	case "1":
		return "Leise"
	case "2":
		return "Standard"
	case "3":
		return "Sport"
	case "4":
		return "Ludicrous"
	}
	return ""
}

var reHex6 = regexp.MustCompile(`^[0-9A-Fa-f]{6}`)

// Bambu liefert RRGGBBAA -> ARGB (Alpha voll); 0 = keine Farbe
func colorFromBambu(s string) uint32 {
	if !reHex6.MatchString(s) {
		return 0
	}
	v, _ := strconv.ParseUint(s[:6], 16, 32)
	return 0xFF000000 | uint32(v)
}

func durText(m int) string {
	if m < 60 {
		return strconv.Itoa(m) + " min"
	}
	return fmt.Sprintf("%d h %02d min", m/60, m%60)
}

// Fertig-Uhrzeit: "14:27", "morgen 06:10" oder "24.10. 09:00"
func etaText(m int) string {
	t := time.Now().Add(time.Duration(m) * time.Minute)
	n := time.Now()
	if t.YearDay() == n.YearDay() && t.Year() == n.Year() {
		return t.Format("15:04")
	}
	tm := n.AddDate(0, 0, 1)
	if t.YearDay() == tm.YearDay() && t.Year() == tm.Year() {
		return "morgen " + t.Format("15:04")
	}
	return t.Format("02.01. 15:04")
}

func progressBar(c *canvas, x, y, w, h float64, pct int, col uint32) {
	c.fill(x, y, w, h, 0x40FFFFFF)
	if pct > 100 {
		pct = 100
	}
	if pct > 0 {
		c.fill(x, y, w*float64(pct)/100, h, col)
	}
}

func drawOverlay(c *canvas, w, h, fS float64) {
	st := eng.stv
	if !eng.haveSt {
		if s, msg, _ := eng.statusEff(); s == 4 && msg != "" {
			badge(c, w, "Status: "+msg, w/2, h-30*fS, fS, 0xD9303338, cRed, 1)
		}
		return
	}
	state := st("st")
	stTxt, stCol := stateInfo(state)
	active := isActive(state)
	pct, _ := strconv.Atoi(st("pct"))
	rem, _ := strconv.Atoi(st("rem"))
	job := st("job")
	old := since(eng.stAt) > 2*time.Minute
	if old {
		stCol = cOff
	}
	fF, fPad := 12*fS, 10*fS

	prog := ""
	switch {
	case active:
		prog = strconv.Itoa(pct) + " %"
		if state == "RUNNING" && st("rem") != "" {
			prog += "   ·   Rest " + durText(rem) + "   ·   fertig " + etaText(rem)
		}
	case cfg.overlay == 1:
		prog = job
		if prog == "" {
			prog = "Kein Druck aktiv"
		}
	case state == "FINISH":
		prog = "Druck beendet"
	case state == "FAILED":
		prog = "Druck abgebrochen"
	default:
		prog = "Kein Druck aktiv"
	}
	if e := st("err"); e != "" && e != "0" {
		n, _ := strconv.ParseInt(e, 10, 64)
		prog += fmt.Sprintf("   ·   Fehlercode 0x%08X", uint32(n))
	}
	if old {
		prog += "   ·   (veraltet)"
	}
	barPct := 0
	if active || state == "FINISH" {
		barPct = pct
	}

	if cfg.overlay == 1 {
		hB := 26 * fS
		c.fill(0, h-hB, w, hB, 0xB4101214)
		c.text(prog, fF, 0, cTxt, fPad, h-hB, w-2*fPad-90*fS, hB-3*fS, 0, 1)
		c.text(stTxt, fF, 1, stCol, w-fPad-100*fS, h-hB, 100*fS, hB-3*fS, 2, 1)
		progressBar(c, 0, h-3*fS, w, 3*fS, barPct, stCol)
		return
	}

	// ausfuehrlich
	ams := st("amsl")
	hL := 20 * fS
	lines := 3.0
	if ams != "" {
		lines = 4
	}
	hP := 3*fS + 8*fS + lines*hL
	y := h - hP
	c.fill(0, y, w, hP, 0xC4101214)
	y += 6 * fS
	// Zeile 1: Auftrag + Zustand
	bw := badge(c, w, stTxt, w-fPad, y+hL/2-9*fS, fS, stCol, cWhite, 2)
	if job == "" {
		job = "–"
	}
	c.text(job, fF+1, 1, cWhite, fPad, y, w-2*fPad-bw-8*fS, hL, 0, 1)
	y += hL
	// Zeile 2: Fortschritt, Schicht, Restzeit
	l2 := prog
	if active && st("tlay") != "" && st("tlay") != "0" {
		l2 = strconv.Itoa(pct) + " %   ·   Schicht " + st("lay") + "/" + st("tlay") + strings.TrimPrefix(prog, strconv.Itoa(pct)+" %")
	}
	c.text(l2, fF, 0, cTxt, fPad, y, w-2*fPad, hL, 0, 1)
	y += hL
	// Zeile 3: Temperaturen + Filament
	var t string
	if st("dual") == "1" {
		t = "Düse L " + st("nozL") + "/" + st("noztL") + " · R " + st("nozR") + "/" + st("noztR") + " °C     Bett " + st("bed") + "/" + st("bedt") + " °C"
	} else {
		t = "Düse " + st("noz") + "/" + st("nozt") + " °C     Bett " + st("bed") + "/" + st("bedt") + " °C"
		if sp := speedText(st("spd")); sp != "" {
			t += "     " + sp
		}
	}
	c.text(t, fF-1, 0, cTxt2, fPad, y, w-2*fPad-140*fS, hL, 0, 1)
	if st("fil") != "" || st("col") != "" {
		slot := " · Slot " + st("slot")
		if a := st("ams"); a != "" && a != "1" {
			slot = " · AMS " + a + "/" + st("slot")
		}
		fil := st("fil")
		if st("slot") != "" {
			if st("slot") == "Ext" {
				fil += " · extern"
			} else {
				fil += slot
			}
		}
		c.text(fil, fF-1, 0, cTxt2, w-fPad-120*fS, y, 120*fS, hL, 2, 1)
		if col := colorFromBambu(st("col")); col != 0 {
			fD := 10 * fS
			tw := math.Min(textW(fil, fF-1, 0), 120*fS)
			xd := w - fPad - tw - fD - 6*fS
			c.ellipse(xd, y+(hL-fD)/2, fD, fD, col)
			c.ellipseLine(xd, y+(hL-fD)/2, fD, fD, 0x66FFFFFF, 1)
		}
	}
	if ams != "" {
		drawAmsRow(c, ams, fPad, y+hL, w-2*fPad, hL, fF-1, fS)
	}
	progressBar(c, 0, h-3*fS, w, 3*fS, barPct, stCol)
}

var reMultiAms = regexp.MustCompile(`^([2-9]|HT)`)
var reUnit = regexp.MustCompile(`^\d+\.`)

// AMS-Uebersicht: "Einheit.Slot,Typ,RRGGBB,aktiv;..." -> Farbpunkt + Slot + Material
func drawAmsRow(c *canvas, ams string, x, y, wMax, hL, fPx, fS float64) {
	parts := strings.Split(ams, ";")
	multi := false
	for _, p := range parts {
		if reMultiAms.MatchString(p) {
			multi = true
		}
	}
	fD, xc := 9*fS, x
	for _, it := range parts {
		p := strings.Split(it, ",")
		if len(p) < 4 {
			continue
		}
		act := p[3] == "1"
		name := p[0]
		switch {
		case name == "E":
			name = "Ext"
		case !multi:
			name = reUnit.ReplaceAllString(name, "")
		}
		typ := p[1]
		if typ == "" {
			typ = "–"
		}
		lbl := name + " " + typ
		style := 0
		if act {
			style = 1
		}
		tw := textW(lbl, fPx, style)
		if xc+fD+4*fS+tw > x+wMax {
			break
		}
		yd := y + (hL-fD)/2
		if col := colorFromBambu(p[2]); p[1] != "" && col != 0 {
			c.ellipse(xc, yd, fD, fD, col)
		}
		if act {
			c.ellipseLine(xc, yd, fD, fD, cGreen, 2)
		} else {
			c.ellipseLine(xc, yd, fD, fD, 0x66FFFFFF, 1)
		}
		tc := uint32(cTxt2)
		if act {
			tc = cWhite
		}
		c.text(lbl, fPx, style, tc, xc+fD+4*fS, y, tw+8, hL, 0, 1)
		xc += fD + 4*fS + tw + 14*fS
	}
}

// ---------------------------------------------------------------- Statuspunkte
func stateColor(i int) uint32 {
	switch i {
	case 1, 2:
		return cAmber
	case 3:
		return cGreen
	case 4:
		return cRed
	}
	return cOff
}

func stateWord(i int, on bool) string {
	if !on {
		return "ausgeschaltet"
	}
	switch i {
	case 1:
		return "verbinde …"
	case 2:
		return "verbunden, warte auf Daten"
	case 3:
		return "läuft"
	case 4:
		return "Fehler"
	}
	return "nicht verbunden"
}

// Farben und Tooltips der Punkte: grau aus · gelb verbindet · gruen laeuft · rot Fehler
func dotInfo() (camCol, mqCol uint32, camTip, mqTip string) {
	if eng.studioPause {
		return cOff, cOff, "Pausiert – Bambu Studio läuft", "Pausiert – Bambu Studio läuft"
	}
	p := eng.p()
	iCam, camMsg, camVia := 0, "", ""
	if p.camOn {
		iCam, camMsg, camVia = eng.camEff()
	}
	iMq, mqMsg, via := 0, "", ""
	if p.stOn {
		iMq, mqMsg, via = eng.statusEff()
	}
	away := p.camOn && camVia == "Drucker" && eng.away() && iCam != 3
	if away {
		iCam = 0
	}
	if iCam == 3 && !eng.frameAt.IsZero() && since(eng.frameAt) > 10*time.Second {
		iCam = 1
	}
	camTip = "Kamera"
	if p.camOn {
		camTip += " über " + camVia
	}
	camTip += ": "
	if away {
		camTip += "nur im Heimnetz (gerade unterwegs)"
	} else {
		camTip += stateWord(iCam, p.camOn)
		if camMsg != "" {
			camTip += "\n" + camMsg
		}
	}
	if eng.coverShown() {
		src := "Bambu-Cloud"
		if eng.covVia == "HA" {
			src = "Home Assistant"
		}
		camTip += "\nErsatzbild: Druckobjekt über " + src
	} else if p.coverOn && eng.covMsg != "" && !eng.liveFrameOk() {
		camTip += "\nDruckobjekt: " + eng.covMsg
	}
	mqTip = "Druckstatus"
	if p.stOn {
		mqTip += " über " + via
	}
	mqTip += ": " + stateWord(iMq, p.stOn)
	if mqMsg != "" {
		mqTip += "\n" + mqMsg
	}
	return stateColor(iCam), stateColor(iMq), camTip, mqTip
}

// ---------------------------------------------------------------- Titel, Infobereich, Taskleiste
func titleUpdate() {
	if hMain == 0 {
		return
	}
	p := eng.p()
	title, tip := appName+" – "+p.name, appName+" · "+p.name
	if eng.haveSt {
		state := eng.stv("st")
		txt, _ := stateInfo(state)
		if isActive(state) {
			title += " – " + eng.stv("pct") + " %"
			tip += "\n" + txt + " " + eng.stv("pct") + " %"
			if r := eng.stv("rem"); r != "" && state == "RUNNING" {
				m, _ := strconv.Atoi(r)
				tip += "\nfertig " + etaText(m)
			}
		} else {
			tip += "\n" + txt
		}
	}
	if getText(hMain) != title {
		setText(hMain, title)
	}
	if tip != lastTrayTip {
		lastTrayTip = tip
		trayIcon(1, hMain, appIconSm, tip, "", "")
	}
	taskbarUpdate()
	invalidate(hMain)
}

func taskbarUpdate() {
	if taskbar == 0 {
		return
	}
	state, val := 0, 0
	if eng.haveSt && since(eng.stAt) < 2*time.Minute {
		val = clampI(atoiDef(eng.stv("pct"), 0), 0, 100)
		switch eng.stv("st") {
		case "RUNNING":
			state = TBPF_NORMAL
		case "PAUSE":
			state = TBPF_PAUSED
		case "FAILED":
			state = TBPF_ERROR
		case "PREPARE", "SLICING", "INIT":
			state = TBPF_INDETERMINATE
		}
	}
	if state == tbState && val == tbVal {
		return
	}
	taskbarProgress(hMain, state, uint64(val), 100)
	tbState, tbVal = state, val
}

func trayBalloon(title, text string) {
	trayIcon(1, hMain, appIconSm, lastTrayTip, title, text)
}

// ---------------------------------------------------------------- Aktionen
func toastShow(s string) {
	toast, toastAt = s, time.Now()
	invalidate(hMain)
}

func cycleOverlay() {
	cfg.overlay = (cfg.overlay + 1) % 3
	ini.set("Anzeige", "Overlay", strconv.Itoa(cfg.overlay))
	toastShow("Overlay: " + ovlNames[cfg.overlay])
}

func toggleOnTop() {
	cfg.onTop = !cfg.onTop
	setTopmost(hMain, cfg.onTop)
	ini.set("Fenster", "Vordergrund", b01(cfg.onTop))
	if cfg.onTop {
		toastShow("Immer im Vordergrund: an")
	} else {
		toastShow("Immer im Vordergrund: aus")
	}
}

// speichert das Originalbild der Kamera (ohne Overlay) als JPG
func snapshot() {
	if len(eng.frame) == 0 {
		toastShow("Noch kein Bild für einen Snapshot")
		return
	}
	os.MkdirAll(cfg.snapDir, 0755)
	f := filepath.Join(cfg.snapDir, "BambuWatch_"+time.Now().Format("2006-01-02_15-04-05")+".jpg")
	if os.WriteFile(f, eng.frame, 0644) != nil {
		toastShow("Snapshot fehlgeschlagen – Ordner prüfen")
		return
	}
	toastShow("✓ Snapshot gespeichert")
}

func snapFolderOpen() {
	os.MkdirAll(cfg.snapDir, 0755)
	shellOpen(cfg.snapDir)
}

func minimize() {
	if cfg.tray {
		pShowWindow.Call(hMain, SW_HIDE)
		if firstTrayHint {
			trayBalloon(appName, "Läuft im Infobereich weiter – Klick auf das Symbol holt das Fenster zurück.")
			firstTrayHint = false
		}
		return
	}
	pShowWindow.Call(hMain, SW_MINIMIZE)
}

func mainShow() {
	tbState = -1 // Taskleisten-Knopf ist neu -> Fortschritt wieder setzen
	pShowWindow.Call(hMain, SW_SHOW)
	if isIconic(hMain) {
		pShowWindow.Call(hMain, SW_RESTORE)
	}
	pSetForegroundWindow.Call(hMain)
	invalidate(hMain)
}

// Groessen-Vorgaben: Bildbreite 400 / 640 / 960, Hoehe nach Seitenverhaeltnis
func sizeSet(vw float64) {
	r := windowRect(hMain)
	pMoveWindow.Call(hMain, uintptr(r.left), uintptr(r.top), uintptr(S(vw+2*pad)), uintptr(S(math.Round(vw/aspect)+barH+pad)), 1)
	invalidate(hMain)
}

func reconnect() {
	eng.helpersStart()
	titleUpdate()
}

func savePos() {
	if hMain == 0 || isIconic(hMain) {
		return
	}
	r := windowRect(hMain)
	if r.left <= -30000 {
		return
	}
	cfg.x, cfg.y = int(math.Round(L(r.left))), int(math.Round(L(r.top)))
	cfg.w, cfg.h = int(math.Round(L(r.right-r.left))), int(math.Round(L(r.bottom-r.top)))
	cfg.savePos(ini)
}

func quit() {
	popupClose()
	tipHide()
	settingsDestroy()
	loginClose()
	helpClose()
	eng.quit()
	savePos()
	trayIcon(2, hMain, 0, "", "", "")
	pDestroyWindow.Call(hMain)
}

// ---------------------------------------------------------------- Menues
const (
	miSnap = 1 + iota
	miOvl
	miLight
	miView
	miTop
	miSmall
	miMed
	miLarge
	miSnapDir
	miReconnect
	miSettings
	miHelp
	miQuit
	miShow
	miManage = 99
	miPrn    = 100
)

func contextMenu(x, y int32) {
	id := popupMenu(hMain, []menuItem{
		{id: miSnap, text: "Snapshot speichern\tS"},
		{id: miOvl, text: "Overlay umschalten\tO"},
		{id: miLight, text: "Druckerlicht an/aus\tL"},
		{id: miView, text: "Kamerabild / Druckobjekt umschalten", checked: eng.forceCover},
		{id: miTop, text: "Immer im Vordergrund\tT", checked: cfg.onTop},
		{},
		{text: "Größe", sub: []menuItem{{id: miSmall, text: "Klein\t1"}, {id: miMed, text: "Mittel\t2"}, {id: miLarge, text: "Groß\t3"}}},
		{id: miSnapDir, text: "Snapshot-Ordner öffnen"},
		{id: miReconnect, text: "Verbindung neu aufbauen\tF5"},
		{},
		{id: miSettings, text: "Einstellungen …\tF2"},
		{id: miHelp, text: "Hilfe\tF1"},
		{},
		{id: miQuit, text: "Beenden"},
	}, x, y)
	menuCmd(id)
}

func menuCmd(id int) {
	switch id {
	case miSnap:
		snapshot()
	case miOvl:
		cycleOverlay()
	case miLight:
		eng.lightToggle()
	case miView:
		eng.viewToggle()
	case miTop:
		toggleOnTop()
	case miSmall:
		sizeSet(400)
	case miMed:
		sizeSet(640)
	case miLarge:
		sizeSet(960)
	case miSnapDir:
		snapFolderOpen()
	case miReconnect:
		reconnect()
	case miSettings:
		settingsShow(0)
	case miHelp:
		helpShow()
	case miQuit:
		quit()
	case miShow:
		mainShow()
	case miManage:
		settingsShow(0)
	default:
		if id >= miPrn && id < miPrn+len(cfg.prn) {
			prnSwitch(id - miPrn)
		}
	}
	invalidate(hMain)
}

// Druckermenue unter dem Namen
func prnMenu() {
	var items []menuItem
	for i, p := range cfg.prn {
		t := p.name
		if p.model != "" {
			t += "  (" + p.model + ")"
		}
		items = append(items, menuItem{id: miPrn + i, text: t, checked: i == cfg.active})
	}
	items = append(items, menuItem{}, menuItem{id: miManage, text: "Drucker verwalten …"})
	r := windowRect(hMain)
	menuCmd(popupMenu(hMain, items, r.left+S(42), r.top+S(barH)))
}

func prnSwitch(i int) {
	if i == cfg.active {
		return
	}
	eng.prnSwitch(i)
	ini.set("Allgemein", "Aktiv", strconv.Itoa(cfg.active+1))
	titleUpdate()
}

func trayMenu() {
	p := cursorPos()
	menuCmd(popupMenu(hMain, []menuItem{{id: miShow, text: "Anzeigen"}, {id: miSnap, text: "Snapshot speichern"}, {}, {id: miQuit, text: "Beenden"}}, p.x, p.y))
}

var ovlNames = []string{"Aus", "Kompakt", "Ausführlich"}

// ---------------------------------------------------------------- Fensterprozedur
func mainClick(id string) {
	switch id {
	case "close":
		quit()
	case "min":
		minimize()
	case "help":
		helpShow()
	case "set":
		settingsShow(-1)
	case "pin":
		toggleOnTop()
	case "snap":
		snapshot()
	case "ovl":
		cycleOverlay()
	case "light":
		eng.lightToggle()
	case "view":
		eng.viewToggle()
	case "title":
		prnMenu()
	default:
		dragWindow(hMain)
	}
}

func mainProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if msgTBC != 0 && msg == msgTBC { // Taskleisten-Knopf existiert -> Fortschritt moeglich
		taskbarInit()
		tbState = -1
		taskbarUpdate()
		return 0
	}
	if msgTaskbarNew != 0 && msg == msgTaskbarNew { // Explorer neu gestartet -> Symbol neu anlegen
		trayIcon(0, hwnd, appIconSm, lastTrayTip, "", "")
		return 0
	}
	switch uint32(msg) {
	case WM_PAINT:
		paintWindow(hwnd, paintMain)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_NCHITTEST:
		x, y := mouseXY(lParam)
		r := windowRect(hwnd)
		b := S(pad + 1)
		cx, cy := x-r.left, y-r.top
		l, rt, t, bt := cx < b, cx >= r.right-r.left-b, cy < S(4), cy >= r.bottom-r.top-b
		switch {
		case t && l:
			return 13
		case t && rt:
			return 14
		case bt && l:
			return 16
		case bt && rt:
			return 17
		case l:
			return 10
		case rt:
			return 11
		case t:
			return 12
		case bt:
			return 15
		}
		return HTCLIENT
	case WM_GETMINMAXINFO:
		mm := (*minMaxInfo)(unsafe.Pointer(lParam))
		mm.minTrack = point{S(minW), S(minH)}
		return 0
	case WM_SIZING:
		if !cfg.aspect {
			break
		}
		rc := (*rect)(unsafe.Pointer(lParam))
		chW, chH := S(2*pad), S(barH+pad)
		switch wParam {
		case 3, 6: // oben / unten -> Breite folgt der Hoehe
			w := int32(math.Round(float64(rc.bottom-rc.top-chH)*aspect)) + chW
			if w < S(minW) {
				w = S(minW)
			}
			rc.right = rc.left + w
		default: // links, rechts, Ecken -> Hoehe folgt der Breite
			h := int32(math.Round(float64(rc.right-rc.left-chW)/aspect)) + chH
			if h < S(minH) {
				h = S(minH)
			}
			if wParam == 4 || wParam == 5 { // obere Ecken
				rc.top = rc.bottom - h
			} else {
				rc.bottom = rc.top + h
			}
		}
		return 1
	case WM_SIZE:
		invalidate(hwnd)
	case WM_EXITSIZEMOVE:
		savePos()
		return 0
	case WM_MOUSEMOVE:
		x, y := mouseXY(lParam)
		hv := mainHit(L(x), L(y))
		if hv != hover {
			hover = hv
			invalidate(hwnd)
		}
		tipTrack(hv)
		trackLeave(hwnd)
		return 0
	case WM_SETCURSOR:
		if int(lParam&0xFFFF) == HTCLIENT && hover != "" && hover != "dot0" && hover != "dot1" {
			setHandCursor()
			return 1
		}
	case WM_MOUSELEAVE:
		hover = ""
		tipTrack("")
		invalidate(hwnd)
		return 0
	case WM_LBUTTONDOWN, WM_LBUTTONDBLCLK:
		x, y := mouseXY(lParam)
		tipTrack("")
		tipHide()
		mainClick(mainHit(L(x), L(y)))
		return 0
	case WM_RBUTTONUP:
		p := cursorPos()
		tipHide()
		contextMenu(p.x, p.y)
		return 0
	case WM_CONTEXTMENU:
		p := cursorPos()
		contextMenu(p.x, p.y)
		return 0
	case WM_KEYDOWN:
		switch wParam {
		case 'S':
			snapshot()
		case 'O':
			cycleOverlay()
		case 'T':
			toggleOnTop()
		case 'L':
			eng.lightToggle()
		case '1', 0x61:
			sizeSet(400)
		case '2', 0x62:
			sizeSet(640)
		case '3', 0x63:
			sizeSet(960)
		case 0x70: // F1
			helpShow()
		case 0x71: // F2
			settingsShow(-1)
		case 0x74: // F5
			reconnect()
		}
		return 0
	case WM_TIMER:
		switch wParam {
		case 1:
			eng.everySecond()
			if hPop != 0 && time.Since(popAt) > 15*time.Second {
				popupClose()
			}
		case 2:
			tipTimer()
			if toast != "" && time.Since(toastAt) >= 1800*time.Millisecond {
				invalidate(hwnd)
			}
		}
		return 0
	case wmEvent:
		dispatchEvents()
		return 0
	case wmTray:
		switch lParam & 0xFFFF {
		case WM_LBUTTONUP:
			mainShow()
		case WM_RBUTTONUP:
			trayMenu()
		case 0x0405: // NIN_BALLOONUSERCLICK
			mainShow()
		}
		return 0
	case WM_CLOSE:
		quit()
		return 0
	case WM_DESTROY:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

const WM_LBUTTONUP = 0x0202

// Meldungen der Worker verteilen
func dispatchEvents() {
	for _, ev := range drainEvents() {
		switch {
		case ev.w != nil && ev.w == wProbe:
			probeEvent(ev)
		case ev.w != nil && ev.w == wHaList:
			haListEvent(ev)
		case ev.w != nil && ev.w == wAcc:
			accEvent(ev)
		default:
			eng.handle(ev)
		}
	}
}
