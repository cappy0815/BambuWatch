//go:build windows

// Einstellungen: drei Reiter (Drucker / Konten & Dienste / Anzeige & Fenster) wie in 1.7.
// Arbeitskopie der Profile: Aenderungen gelten erst mit "Speichern".
package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	camTypeNames = []string{"Automatisch erkennen", "Einzelbilder (A1, P1)", "Videostream (H2D, X1, P2S)"}
	imgSrcNames  = []string{"Automatisch", "Nur Drucker (Heimnetz)", "Nur Home Assistant"}
	connNames    = []string{"Automatisch", "Nur Heimnetz", "Nur Bambu-Cloud", "Nur Home Assistant"}
	regionKeys   = []string{"Global", "China"}
	regionNames  = []string{"Europa / USA", "China"}
	alphaSteps   = []int{255, 230, 204, 178}
	alphaNames   = []string{"100 %", "90 %", "80 %", "70 %"}
	fontNames    = []string{"Standard", "+1 px", "+2 px", "+3 px"}

	reHost   = regexp.MustCompile(`^[A-Za-z0-9.\-]+$`)
	reSerial = regexp.MustCompile(`^[A-Z0-9]+$`)
)

var (
	dSet   *dialog
	setW   *settings // Arbeitskopie
	setSel int       // angezeigter Drucker (0-basiert)

	setConn, setImg, setCamT, setRegion, setOvl, setAlph, setFont int
	setHaCam, setHaCov                                            string
	setHaCams, setHaImgs                                          [][2]string // Entitaet, Anzeigename
	codeVisible                                                   bool

	wProbe, wHaList, wAcc    *worker
	probeAt, haListAt, accAt time.Time

	// Bausteine
	sPrnSel, sName, sConn, sTogCam, sTogSt, sIp, sSerial, sCode, sEye, sProbeCam, sProbeMq *widget
	sImg, sCamT, sHaCam, sTogCover, sHaCov                                                 *widget
	sAccLbl, sLogout, sRegion, sHaIn, sHaOut, sHaTok, sTogHaIns, sHaRes, sFf, sFfRes       *widget
	sOvl, sAlpha, sFont, sTogAspect, sTogTop, sTogTray, sTogStudio, sTogNotify, sTogSound  *widget
	sSnap                                                                                  *widget
)

func strip(s string) string { return strings.Join(strings.Fields(s), "") }

func settingsShow(tab int) {
	if dSet.alive() {
		pSetForegroundWindow.Call(dSet.hwnd)
		if tab >= 0 {
			dSet.setPage(tab)
		}
		return
	}
	if !isVisible(hMain) || isIconic(hMain) {
		mainShow()
	}
	setW = cfg.clone()
	setSel = setW.active
	setHaCams, setHaImgs = nil, nil
	const W = 480.0
	d := newDialog("Einstellungen", W)
	dSet = d
	curPage = -1
	y := d.head("Einstellungen")
	d.tabs([]string{"Drucker", "Konten & Dienste", "Anzeige & Fenster"}, []float64{20, 114, 268}, []float64{90, 150, 150}, y+4)
	y0 := y + 34

	// ======== Seite 1: Drucker
	curPage = 0
	y = y0
	sPrnSel = d.cycle("", 20, y+2, 262, "prnNext")
	d.pill("+ Neu", 290, y, 76, 26, cBtnDk, "prnNew")
	d.pill("Entfernen", 372, y, 88, 26, cBtnDk, "prnDel")
	y += 34
	sName = d.field("Name", "", 20, y, 230, false)
	d.label("Druckstatus über", 262, y, 198, 16, cTxt2, 10.7, 0, 0)
	sConn = d.cycle("", 262, y+19, 198, "conn")
	y += 52
	sTogCam = d.toggle("Kamerabild anzeigen", true, 20, y, 220, "tog:cam")
	sTogSt = d.toggle("Druckstatus abrufen", true, 262, y, 198, "tog:st")
	y += 24
	y = d.section("Heimnetz", y)
	sIp = d.field("IP-Adresse", "", 20, y, 190, false)
	sSerial = d.field("Seriennummer", "", 220, y, W-240, false)
	y += 52
	sCode = d.field("Access Code", "", 20, y, 190, true)
	sEye = d.add(&widget{id: "eye", x: 214, y: y + 18, w: 26, h: 24, draw: func(c *canvas, wd *widget, hot bool) {
		col := uint32(cTxt2)
		if codeVisible {
			col = cGreen
		} else if hot {
			col = cTxt
		}
		c.glyph(0xE890, 14.7, col, wd.x, wd.y, wd.w, wd.h)
	}})
	d.pill("Verbindung testen", W-160, y+17, 140, 26, cBtnDk, "probe")
	y += 50
	sProbeCam = d.label("", 20, y, W-40, 18, cTxt2, 11.3, 0, 0)
	sProbeMq = d.label("", 20, y+18, W-40, 18, cTxt2, 11.3, 0, 0)
	y += 38
	y = d.section("Kamerabild", y)
	d.label("Bildquelle", 20, y, 90, 22, cTxt, 12, 0, 0)
	sImg = d.cycle("", 110, y, 220, "img")
	y += 28
	d.label("Kameratyp", 20, y, 90, 22, cTxt, 12, 0, 0)
	sCamT = d.cycle("", 110, y, 220, "camt")
	y += 28
	d.label("HA-Kamera", 20, y, 90, 22, cTxt, 12, 0, 0)
	sHaCam = d.cycle("", 110, y, 220, "hacam")
	d.pill("Aus HA laden", W-136, y-2, 116, 26, cBtnDk, "haload")
	y += 30
	y = d.section("Druckobjekt statt Kamerabild", y)
	sTogCover = d.toggle("Bild des Druckobjekts zeigen, wenn kein Kamerabild kommt", true, 20, y, W-40, "tog:cover")
	y += 26
	d.label("HA-Titelbild", 20, y, 90, 22, cTxt, 12, 0, 0)
	sHaCov = d.cycle("", 110, y, 220, "hacov")
	y += 28
	y = d.hint("Druckobjekt: „Titelbild“ aus Home Assistant (ha-bambulab), sonst letzter Cloud-Auftrag. „Automatisch“ heißt: im Heimnetz direkt vom Drucker, sonst über Home Assistant, der Status zuletzt über die Bambu-Cloud. HA-Kamera, Titelbild und Licht findet BambuWatch über die Seriennummer selbst.", 20, y, W-40)
	y1 := y

	// ======== Seite 2: Konten & Dienste
	curPage = 1
	y = y0
	y = d.section("Bambu-Konto", y)
	sAccLbl = d.add(&widget{x: 20, y: y, w: W - 40, h: 34, col: cTxt2, draw: func(c *canvas, wd *widget, hot bool) {
		c.wrap(wd.text, 12, 0, wd.col, wd.x, wd.y, wd.w, wd.h+10, 0)
	}})
	y += 40
	d.pill("Anmelden …", 20, y, 116, 26, cGreen, "login")
	sLogout = d.pill("Abmelden", 142, y, 96, 26, cBtnDk, "logout")
	d.label("Region", 266, y, 54, 26, cTxt2, 11.3, 0, 0)
	setRegion = 0
	if cfg.region == "China" {
		setRegion = 1
	}
	sRegion = d.cycle(regionNames[setRegion], 320, y+2, 140, "region")
	y += 32
	y = d.hint("Beim Anmelden werden alle Drucker des Kontos übernommen – mit Seriennummer und Access Code. Status kommt dann auch unterwegs.", 20, y, W-40)
	y = d.section("Home Assistant (Bild und Status, auch unterwegs)", y)
	sHaIn = d.field("Adresse im Heimnetz", cfg.haIn, 20, y, 214, false)
	sHaOut = d.field("Adresse von unterwegs", cfg.haOut, 246, y, W-266, false)
	y += 52
	sHaTok = d.field("Langlebiges Zugriffstoken (HA-Profil › Sicherheit)", cfg.haToken, 20, y, 300, true)
	y += 50
	sTogHaIns = d.toggle("Zertifikat nicht prüfen (selbstsigniert)", cfg.haInsecure, 20, y, 280, "tog:hains")
	d.pill("Verbindung testen", W-160, y-2, 140, 26, cBtnDk, "haload")
	y += 28
	sHaRes = d.label("z. B. http://192.168.1.10:8123 und https://ha.deine-domain.de – eine Adresse reicht.", 20, y, W-40, 18, cTxt2, 11.3, 0, 0)
	y += 22
	y = d.section("Videostream (H2D, X1, P2S)", y)
	sFf = d.input(cfg.ffmpeg, 20, y, W-80, false)
	d.pill("…", W-54, y, 34, 24, cBtnDk, "ffbrowse")
	y += 28
	sFfRes = d.label("", 20, y, W-40, 18, cTxt2, 11.3, 0, 0)
	y += 20
	y = d.hint("ffmpeg.exe (kostenlos, z. B. von ffmpeg.org) macht aus dem Videostream Einzelbilder. Liegt sie neben BambuWatch oder im PATH, wird sie automatisch gefunden.", 20, y, W-40)
	y2 := y

	// ======== Seite 3: Anzeige & Fenster
	curPage = 2
	y = y0
	y = d.section("Anzeige", y)
	d.label("Status-Overlay", 20, y, 120, 22, cTxt, 12, 0, 0)
	setOvl = cfg.overlay
	sOvl = d.cycle(ovlNames[setOvl], 140, y, 98, "ovl")
	d.label("Deckkraft", 250, y, 70, 22, cTxt, 12, 0, 0)
	setAlph = 0
	for i, a := range alphaSteps {
		if a == cfg.alpha {
			setAlph = i
		}
	}
	sAlpha = d.cycle(alphaNames[setAlph], 320, y, 98, "alpha")
	y += 28
	sTogAspect = d.toggle("Seitenverhältnis beim Ziehen halten", cfg.aspect, 20, y, W-40, "tog:aspect")
	y += 26
	d.label("Schriftgröße", 20, y, 120, 22, cTxt, 12, 0, 0)
	setFont = cfg.fontAdd
	sFont = d.cycle(fontNames[setFont], 140, y, 98, "font")
	d.label("gilt nach dem Speichern", 250, y, 210, 22, cTxt2, 10.7, 0, 0)
	y += 28
	y = d.section("Fenster", y)
	sTogTop = d.toggle("Immer im Vordergrund", cfg.onTop, 20, y, 210, "tog:top")
	sTogTray = d.toggle("Minimieren in den Infobereich", cfg.tray, 240, y, 220, "tog:tray")
	y += 26
	sTogStudio = d.toggle("Pausieren, solange Bambu Studio läuft", cfg.studio, 20, y, W-40, "tog:studio")
	y += 26
	y = d.section("Meldungen (aktiver Drucker)", y)
	sTogNotify = d.toggle("Hinweis bei Druckende, Pause, Abbruch", cfg.notify, 20, y, 280, "tog:notify")
	sTogSound = d.toggle("mit Ton", cfg.sound, 320, y, 120, "tog:sound")
	y += 26
	y = d.section("Snapshots", y)
	sSnap = d.input(cfg.snapDir, 20, y, W-80, false)
	d.pill("…", W-54, y, 34, 24, cBtnDk, "snapbrowse")
	y += 34
	y3 := y

	curPage = -1
	y = maxF(y1, y2, y3) + 14
	d.pill("Speichern", W-230, y, 100, 30, cGreen, "save")
	d.pill("Abbrechen", W-120, y, 100, 30, cBtnDk, "cancel")
	y += 48

	d.onClick = setClick
	d.onOK = settingsSave
	d.onClose = settingsClose
	d.page = 0
	if tab > 0 {
		d.page = tab
	}
	d.show(y, hMain)
	prnLoad()
	ffShow()
	accountLabel()
}

func maxF(v ...float64) float64 {
	m := v[0]
	for _, x := range v[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func setClick(id string) {
	switch id {
	case "prnNext":
		if prnStore() {
			setSel = (setSel + 1) % len(setW.prn)
			prnLoad()
		}
	case "prnNew":
		if prnStore() {
			setW.prn = append(setW.prn, newPrinter("Drucker "+strconv.Itoa(len(setW.prn)+1)))
			setSel = len(setW.prn) - 1
			prnLoad()
			pSetFocus.Call(sName.edit)
		}
	case "prnDel":
		prnDel()
	case "conn":
		setConn = (setConn + 1) % 4
		sConn.text = connNames[setConn]
	case "img":
		setImg = (setImg + 1) % 3
		sImg.text = imgSrcNames[setImg]
	case "camt":
		setCamT = (setCamT + 1) % 3
		sCamT.text = camTypeNames[setCamT]
	case "hacam":
		setHaCam = cycleEntity(setHaCams, setHaCam, sHaCam, "– keine –")
	case "hacov":
		setHaCov = cycleEntity(setHaImgs, setHaCov, sHaCov, "– keines –")
	case "haload":
		haLoad()
	case "eye":
		codeVisible = !codeVisible
		ch := uintptr(0x25CF)
		if codeVisible {
			ch = 0
		}
		sendMsg(sCode.edit, EM_SETPASSWORDCHAR, ch, 0)
		invalidate(sCode.edit)
	case "probe":
		probeStart()
	case "login":
		loginShow()
	case "logout":
		accountClear()
		accountLabel()
		eng.helpersStart()
		toastShow("Bei Bambu abgemeldet")
	case "region":
		setRegion = (setRegion + 1) % 2
		sRegion.text = regionNames[setRegion]
	case "ffbrowse":
		if f := openFileDlg(dSet.hwnd, "ffmpeg.exe wählen", "ffmpeg (ffmpeg.exe)\x00ffmpeg.exe\x00"); f != "" {
			sFf.set(f)
		}
		ffShow()
	case "snapbrowse":
		if f := folderDlg(dSet.hwnd, "Ordner für Snapshots wählen"); f != "" {
			sSnap.set(f)
		}
	case "ovl":
		setOvl = (setOvl + 1) % 3
		sOvl.text = ovlNames[setOvl]
	case "alpha":
		setAlph = (setAlph + 1) % 4
		sAlpha.text = alphaNames[setAlph]
		setAlphaWin(hMain, alphaSteps[setAlph]) // Vorschau
	case "font":
		setFont = (setFont + 1) % 4
		sFont.text = fontNames[setFont]
	case "save":
		settingsSave()
	case "cancel":
		settingsClose()
	}
	if dSet.alive() {
		dSet.redraw()
	}
}

func setAlphaWin(h uintptr, a int) { setAlpha(h, a) }

// naechster Eintrag der HA-Liste; Index 0 = "– keine –"
func cycleEntity(list [][2]string, cur string, wd *widget, none string) string {
	if len(list) == 0 {
		wd.text = "erst „Aus HA laden“"
		return cur
	}
	i := 0
	for k, e := range list {
		if e[0] == cur {
			i = k + 1
		}
	}
	i = (i + 1) % (len(list) + 1)
	if i == 0 {
		cur = ""
	} else {
		cur = list[i-1][0]
	}
	wd.text = entityText(list, cur, none)
	return cur
}

func entityText(list [][2]string, cur, none string) string {
	if cur == "" {
		return none
	}
	for _, e := range list {
		if e[0] == cur {
			return e[1] + " (" + cur + ")"
		}
	}
	return cur
}

// ---------------------------------------------------------------- Drucker-Reiter
// Felder -> Arbeitskopie (mit Pruefung); false bei ungueltigen Eingaben
func prnStore() bool {
	ip := strip(sIp.val())
	serial := strings.ToUpper(strip(sSerial.val()))
	if ip != "" && !reHost.MatchString(ip) {
		dSet.setPage(0)
		probeShow("err:IP-Adresse enthält ungültige Zeichen", "")
		return false
	}
	if serial != "" && !reSerial.MatchString(serial) {
		dSet.setPage(0)
		probeShow("", "err:Seriennummer: nur Buchstaben und Ziffern")
		return false
	}
	p := &setW.prn[setSel]
	name := strings.TrimSpace(sName.val())
	if name == "" {
		name = "Drucker " + strconv.Itoa(setSel+1)
	}
	p.name, p.ip, p.code, p.serial = name, ip, strip(sCode.val()), serial
	p.conn, p.imgSrc, p.camType, p.haCam = setConn, setImg, setCamT, setHaCam
	p.camOn, p.stOn, p.coverOn, p.haCover = sTogCam.on, sTogSt.on, sTogCover.on, setHaCov
	return true
}

// Arbeitskopie -> Felder
func prnLoad() {
	p := setW.prn[setSel]
	t := p.name
	if p.model != "" {
		t += " · " + p.model
	}
	t += "   (" + strconv.Itoa(setSel+1) + "/" + strconv.Itoa(len(setW.prn))
	if setSel == setW.active {
		t += ", aktiv"
	}
	sPrnSel.text = t + ")"
	sName.set(p.name)
	sIp.set(p.ip)
	sSerial.set(p.serial)
	sCode.set(p.code)
	setConn, setImg, setCamT = p.conn, p.imgSrc, p.camType
	sConn.text, sImg.text, sCamT.text = connNames[setConn], imgSrcNames[setImg], camTypeNames[setCamT]
	setHaCam, setHaCov = p.haCam, p.haCover
	sHaCam.text = entityText(setHaCams, setHaCam, "– keine –")
	sHaCov.text = entityText(setHaImgs, setHaCov, "– keines –")
	sTogCam.on, sTogSt.on, sTogCover.on = p.camOn, p.stOn, p.coverOn
	probeShow("", "")
	dSet.redraw()
}

func prnDel() {
	if len(setW.prn) <= 1 {
		probeShow("err:Der letzte Drucker kann nicht entfernt werden", "")
		return
	}
	k := setSel
	setW.prn = append(setW.prn[:k], setW.prn[k+1:]...)
	if setW.active == k {
		setW.active = 0
	} else if setW.active > k {
		setW.active--
	}
	if setSel >= len(setW.prn) {
		setSel = len(setW.prn) - 1
	}
	prnLoad()
}

// ---------------------------------------------------------------- Verbindungstest
func probeStart() {
	if wProbe != nil {
		return
	}
	ip, code := strip(sIp.val()), strip(sCode.val())
	serial := strings.ToUpper(strip(sSerial.val()))
	if ip == "" || code == "" {
		probeShow("err:Bitte IP-Adresse und Access Code eintragen", "")
		return
	}
	if !reHost.MatchString(ip) {
		probeShow("err:IP-Adresse enthält ungültige Zeichen", "")
		return
	}
	c := probeCfg{ip: ip, code: code, serial: serial, camType: camTypeKeys[setCamT], ffmpeg: ffmpegFind(strings.TrimSpace(sFf.val()))}
	wProbe = startWorker("probe", func(ctx ctxT, o out) { runProbe(ctx, o, c) })
	probeAt = time.Now()
	sProbeCam.text, sProbeCam.col = "Teste Kamera und Status … (bis ca. 30 s)", cAmber
	sProbeMq.text = ""
	dSet.redraw()
}

func probeEvent(ev event) {
	switch {
	case ev.end:
		if wProbe != nil {
			probeShow("err:Test ohne Ergebnis beendet", "")
		}
		wProbe = nil
	case strings.HasPrefix(ev.line, "PROBE|"):
		p := strings.SplitN(ev.line, "|", 3)
		if len(p) == 3 {
			probeShow(p[1], p[2])
		}
		probeStop()
	case strings.HasPrefix(ev.line, "ERR|"):
		probeShow("err:"+ev.line[4:], "")
		probeStop()
	}
}

func probeStop() {
	wProbe.stop()
	wProbe = nil
}

// "ok:Text" / "warn:Text" / "err:Text"
func probeShow(cam, mq string) {
	if !dSet.alive() {
		return
	}
	probeLine(sProbeCam, "Kamera", cam)
	probeLine(sProbeMq, "Status", mq)
	dSet.redraw()
}

func probeLine(wd *widget, what, s string) {
	if s == "" {
		wd.text = ""
		return
	}
	kind, txt, _ := strings.Cut(s, ":")
	col, sym := uint32(cRed), "✗"
	switch kind {
	case "ok":
		col, sym = cGreen, "✓"
	case "warn":
		col, sym = cAmber, "!"
	}
	wd.text, wd.col = sym+"  "+what+": "+txt, col
}

// ---------------------------------------------------------------- Home Assistant: Liste / Test
func haLoad() {
	if wHaList != nil {
		return
	}
	in, outside, tok := strings.TrimSpace(sHaIn.val()), strings.TrimSpace(sHaOut.val()), strings.TrimSpace(sHaTok.val())
	if (in == "" && outside == "") || tok == "" {
		haMsg("Adresse und Token von Home Assistant eintragen (Reiter „Konten & Dienste“)", cRed)
		return
	}
	h := haCfg{urls: haURLs(in, outside), token: tok, insecure: sTogHaIns.on}
	setHaCams, setHaImgs = nil, nil
	wHaList = startWorker("halist", func(ctx ctxT, o out) { runHaList(ctx, o, h) })
	haListAt = time.Now()
	haMsg("Frage Home Assistant …", cAmber)
}

var reCamImg = regexp.MustCompile(`_(kamera|camera)$`)
var reCoverImg = regexp.MustCompile(`_(titelbild|cover_image)$`)
var reDomain = regexp.MustCompile(`^[a-z_]+\.`)
var reLastPart = regexp.MustCompile(`_[^_]+$`)

func haListEvent(ev event) {
	s := ev.line
	switch {
	case ev.end:
		wHaList = nil
	case strings.HasPrefix(s, "CAM|"), strings.HasPrefix(s, "IMG|"):
		p := strings.SplitN(s, "|", 3)
		if len(p) < 3 {
			return
		}
		name := p[2]
		if name == "" {
			name = p[1]
		}
		e := [2]string{p[1], name}
		if p[0] == "CAM" {
			setHaCams = append(setHaCams, e)
		} else {
			setHaImgs = append(setHaImgs, e)
			// ha-bambulab kann die A1/P1-Kamera auch als Bild-Entitaet anlegen
			if reCamImg.MatchString(p[1]) {
				setHaCams = append(setHaCams, e)
			}
		}
	case strings.HasPrefix(s, "HAOK|"):
		p := strings.Split(s, "|")
		haMsg("✓  Verbunden über "+p[1]+" – "+strconv.Itoa(len(setHaCams))+" Kamera(s), "+strconv.Itoa(len(setHaImgs))+" Bild(er)", cGreen)
		if dSet.alive() {
			if setHaCam == "" && len(setHaCams) > 0 {
				setHaCam = setHaCams[0][0]
			}
			sHaCam.text = entityText(setHaCams, setHaCam, "– keine –")
			if setHaCov == "" {
				setHaCov = haCoverGuess()
			}
			sHaCov.text = entityText(setHaImgs, setHaCov, "– keines –")
			dSet.redraw()
		}
	case strings.HasPrefix(s, "ERR|AUTH|"):
		haMsg("✗  "+s[9:], cRed)
	case strings.HasPrefix(s, "ERR|"):
		haMsg("✗  "+s[4:], cRed)
	}
}

// Vorschlag fuers Titelbild: ha-bambulab nennt es ..._titelbild bzw. ..._cover_image;
// bevorzugt das Geraet der gewaehlten HA-Kamera (gleicher Namensanfang)
func haCoverGuess() string {
	pre := ""
	if setHaCam != "" {
		pre = reLastPart.ReplaceAllString(reDomain.ReplaceAllString(setHaCam, ""), "")
	}
	first := ""
	for _, e := range setHaImgs {
		id := e[0]
		if !reCoverImg.MatchString(id) {
			continue
		}
		if first == "" {
			first = id
		}
		if pre != "" && strings.HasPrefix(strings.TrimPrefix(id, "image."), pre) {
			return id
		}
	}
	return first
}

func haMsg(s string, col uint32) {
	if !dSet.alive() {
		return
	}
	sHaRes.text, sHaRes.col = s, col
	dSet.redraw()
}

func ffShow() {
	f := ffmpegFind(strings.TrimSpace(sFf.val()))
	if f != "" {
		sFfRes.text, sFfRes.col = "✓  gefunden: "+f, cGreen
	} else {
		sFfRes.text, sFfRes.col = "ffmpeg.exe nicht gefunden – nur für Videostream-Drucker nötig", cTxt2
	}
	dSet.redraw()
}

// Text im Konto-Bereich
func accountLabel() {
	if !dSet.alive() {
		return
	}
	switch {
	case cfg.token == "":
		sAccLbl.text, sAccLbl.col = "Nicht angemeldet.\nOptional: für den Status von unterwegs und die automatische Einrichtung.", cTxt2
	case eng.clAuthBad:
		sAccLbl.text, sAccLbl.col = "Anmeldung abgelaufen – bitte neu anmelden.\nBis dahin Status nur im Heimnetz", cAmber
	default:
		who := " (per Token)"
		if cfg.email != "" {
			who = ": " + cfg.email
		}
		sAccLbl.text, sAccLbl.col = "Angemeldet"+who+"\nStatus von unterwegs für alle Konto-Drucker", cGreen
	}
	sLogout.hide = cfg.token == ""
	dSet.redraw()
}

func accountClear() {
	cfg.token, cfg.user = "", ""
	eng.clAuthBad = false
	eng.cloudStop()
	ini.set("Konto", "Token", "")
	ini.set("Konto", "Benutzer", "")
}

// ---------------------------------------------------------------- Speichern
// alles, was eine neue Verbindung noetig macht, als ein Vergleichstext
func connSig() string {
	p := cfg.cur()
	return strings.Join([]string{strconv.Itoa(cfg.active), p.ip, p.code, p.serial, strconv.Itoa(p.conn), strconv.Itoa(p.camType),
		strconv.Itoa(p.imgSrc), p.haCam, cfg.haIn, cfg.haOut, cfg.haToken, b01(cfg.haInsecure), cfg.ffmpeg, b01(p.camOn),
		b01(p.stOn), cfg.region, cfg.token, b01(p.coverOn), p.haCover}, "|")
}

func settingsSave() {
	if !prnStore() {
		return
	}
	old := connSig()
	cfg.prn = setW.prn
	cfg.active = setW.active
	cfg.haIn = strings.TrimSpace(sHaIn.val())
	cfg.haOut = strings.TrimSpace(sHaOut.val())
	cfg.haToken = strings.TrimSpace(sHaTok.val())
	cfg.haInsecure = sTogHaIns.on
	cfg.ffmpeg = strings.TrimSpace(sFf.val())
	// Region gewechselt -> das Token gehoert zur alten Region, also neu anmelden
	if regionKeys[setRegion] != cfg.region {
		cfg.region = regionKeys[setRegion]
		if cfg.token != "" {
			accountClear()
			toastShow("Region geändert – bitte neu anmelden")
		}
	}
	cfg.overlay = setOvl
	cfg.alpha = alphaSteps[setAlph]
	cfg.fontAdd = setFont
	fontAdd = float64(setFont)
	cfg.aspect = sTogAspect.on
	cfg.onTop = sTogTop.on
	cfg.tray = sTogTray.on
	cfg.notify = sTogNotify.on
	cfg.sound = sTogSound.on
	cfg.studio = sTogStudio.on
	if s := strings.TrimSpace(sSnap.val()); s != "" {
		cfg.snapDir = s
	}
	cfg.save(ini)
	setTopmost(hMain, cfg.onTop)
	setAlpha(hMain, cfg.alpha)
	settingsDestroy()
	if connSig() != old {
		frameDrop()
		eng.helpersStart()
	}
	titleUpdate()
}

// Kamerabild verwerfen (neue Verbindung)
func frameDrop() {
	eng.frame = nil
	eng.frameSeq++
	eng.frameAt = time.Time{}
}

func settingsClose() {
	setAlpha(hMain, cfg.alpha) // Deckkraft-Vorschau zuruecknehmen
	settingsDestroy()
}

func settingsDestroy() {
	probeStop()
	wHaList.stop()
	wHaList = nil
	loginClose()
	dSet.close()
	dSet = nil
	codeVisible = false
}

// Zeitueberschreitungen (aus dem Sekundentakt)
func settingsTick() {
	if wProbe != nil && time.Since(probeAt) > 40*time.Second {
		probeShow("err:Test ohne Ergebnis beendet", "")
		probeStop()
	}
	if wHaList != nil && time.Since(haListAt) > 40*time.Second {
		wHaList.stop()
		wHaList = nil
	}
	if wAcc != nil && time.Since(accAt) > 45*time.Second {
		accMsg("Keine Antwort vom Bambu-Server", cRed)
		wAcc.stop()
		wAcc = nil
	}
}
