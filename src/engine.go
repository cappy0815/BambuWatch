// Verbindungs-Steuerung – 1:1 aus BambuWatch_v1.7.au3 (_HelpersStart, __CloudCheck, __HsCheck,
// __HaCheck, _PollHelpers, _EverySecond, Druckobjekt, Licht, Bambu-Studio-Pause).
// Reihenfolge Status: Heimnetz -> Home Assistant -> Bambu-Cloud; Bild: Drucker -> Home Assistant.
// Laeuft komplett im UI-Thread; die Verbindungen selbst sind Worker (worker.go).
package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	lanGrace = 15 * time.Second // so lange darf das Heimnetz nach dem Start brauchen
	lanStale = 30 * time.Second // so lange ohne Heimnetz-Daten -> andere Quelle uebernimmt
)

var camTypeKeys = []string{"auto", "jpeg", "rtsp"}

// Rueckmeldungen an die Oberflaeche (setzt main; in Tests leer)
var (
	uiToast         = func(s string) {}
	uiNotify        = func(title, text string, col uint32) {}
	uiDirty         = func() {}
	uiStatusChanged = func() {} // Titel, Taskleiste, Licht-Knopf
	uiSave          = func() {} // Druckerprofile speichern
	uiTrayTip       = func(title, text string) {}
	studioRunning   = func() bool { return false }
	ffmpegFind      = func(setting string) string { return setting }
	now             = time.Now
)

type engine struct {
	s *settings

	wCam, wMq, wCl, wHs, wHa, wCov, wLight                 *worker
	restartCam, restartMq, restartCl, restartHs, restartHa time.Time

	src               string // Quelle des Status: "LAN", "HA", "Cloud"
	lanSt, lanStart   time.Time
	hsSt              time.Time
	camRun, mqRun     time.Time
	lightAt           time.Time
	lanFrame, camStrt time.Time

	// Zustaende: 0 aus, 1 verbinde, 2 verbunden (wartet), 3 Daten kommen, 4 Fehler
	iCam, iMq, iCl, iHs, iHa           int
	camMsg, mqMsg, clMsg, hsMsg, haMsg string
	clAuthBad, hsStop, haStop, camStop bool
	imgVia, camTypeNow                 string

	frame    []byte // letztes Kamerabild (JPEG)
	frameSeq int
	frameAt  time.Time

	cover                    []byte
	coverSeq                 int
	covKey, covKeyLoaded     string
	covTry                   time.Time
	covVia, covTitle, covMsg string
	forceCover               bool

	studioPause bool
	studioTick  int

	st        map[string]string
	haveSt    bool
	stAt      time.Time
	prevState string
}

func newEngine(s *settings) *engine {
	return &engine{s: s, st: map[string]string{}}
}

func (e *engine) p() *printer { return e.s.cur() }

func since(t time.Time) time.Duration {
	if t.IsZero() {
		return 1 << 62
	}
	return now().Sub(t)
}

func (e *engine) stv(k string) string { return e.st[k] }

func isActive(s string) bool {
	return s == "RUNNING" || s == "PAUSE" || s == "PREPARE" || s == "SLICING"
}

// ---------------------------------------------------------------- Start / Stopp
func (e *engine) helpersStart() {
	e.helpersStop(false)
	e.iCam, e.iMq, e.iCl = 0, 0, 0
	e.camMsg, e.mqMsg, e.clMsg = "", "", ""
	e.src = ""
	e.lanSt = time.Time{}
	e.lanStart = now()
	e.iHa, e.haMsg = 0, ""
	e.camStop, e.haStop = false, false
	e.lanFrame = time.Time{}
	e.camStrt = now()
	e.imgVia, e.camTypeNow = "", ""
	// Bambu Studio laeuft: nichts verbinden (studioCheck startet neu, sobald es beendet ist)
	if e.studioPause {
		uiDirty()
		return
	}
	e.camStart()
	e.iHs, e.hsMsg, e.hsStop, e.hsSt = 0, "", false, time.Time{}
	p := e.p()
	if p.stOn {
		if e.canLan(true) && e.lanAllowed() {
			e.mqStart()
		} else if p.conn == 1 {
			e.iMq = 4
			if p.serial == "" {
				e.mqMsg = "Seriennummer fehlt (Einstellungen)"
			} else {
				e.mqMsg = "IP-Adresse oder Access Code fehlt (Einstellungen)"
			}
		}
		e.hsCheck()
		e.cloudCheck()
	}
	uiStatusChanged()
	e.haCheck()
	uiDirty()
}

func (e *engine) mqStart() {
	p := e.p()
	e.wMq = startWorker("mq", func(ctx ctxT, o out) {
		runMqtt(ctx, o, mqttCfg{host: p.ip, port: 8883, user: "bblp", pw: p.code, serial: p.serial})
	})
	e.iMq = 1
	e.mqRun = now()
}

// keepView: nur Verbindungen trennen, Bild, Druckobjekt und letzten Status behalten (Pause)
func (e *engine) helpersStop(keepView bool) {
	e.wCam.stop()
	e.wMq.stop()
	e.wCam, e.wMq = nil, nil
	e.restartCam, e.restartMq = time.Time{}, time.Time{}
	e.cloudStop()
	e.hsStopW()
	e.haStopW()
	if keepView {
		e.wCov.stop()
		e.wCov = nil
		e.wLight.stop()
		e.wLight = nil
		return
	}
	e.coverStop()
	e.haveSt = false
	e.st = map[string]string{}
	e.prevState = ""
}

func (e *engine) quit() {
	e.wLight.stop()
	e.helpersStop(false)
}

func (e *engine) canLan(status bool) bool {
	p := e.p()
	if p.ip == "" || p.code == "" {
		return false
	}
	return !status || p.serial != ""
}

func (e *engine) canCloud() bool {
	p := e.p()
	return p.stOn && p.conn != 1 && p.conn != 3 && e.s.token != "" && e.s.user != "" && p.serial != "" && !e.clAuthBad
}

func (e *engine) lanFresh() bool    { return since(e.lanSt) < lanStale }
func (e *engine) lanAllowed() bool  { return e.p().conn == 0 || e.p().conn == 1 }
func (e *engine) hsFresh() bool     { return since(e.hsSt) < lanStale }
func (e *engine) lanCamFresh() bool { return since(e.lanFrame) < lanStale }

// Bedarf fuer Ersatzquelle: Heimnetz liefert (noch) nicht
func (e *engine) needFallback(forced bool) bool {
	switch {
	case forced || !e.canLan(true):
		return true
	case e.lanSt.IsZero():
		return since(e.lanStart) > lanGrace
	}
	return !e.lanFresh()
}

// Cloud zuschalten, wenn das Heimnetz keinen Status liefert (Automatik) oder "Nur Bambu-Cloud"
func (e *engine) cloudCheck() {
	if e.wCl != nil || !e.canCloud() {
		return
	}
	p := e.p()
	if p.conn == 0 && e.hsUsable() {
		return // Home Assistant liefert -> Cloud nicht noetig
	}
	if !e.restartCl.IsZero() && since(e.restartCl) < 10*time.Second {
		return
	}
	if !e.needFallback(p.conn == 2) {
		return
	}
	e.restartCl = time.Time{}
	host := "us.mqtt.bambulab.com"
	if e.s.region == "China" {
		host = "cn.mqtt.bambulab.com"
	}
	c := mqttCfg{host: host, port: 8883, verify: true, user: e.s.user, pw: e.s.token, serial: p.serial, cloud: true}
	if testBroker != "" {
		c.host, c.port, c.verify = testBrokerHost()
	}
	e.wCl = startWorker("cloud", func(ctx ctxT, o out) { runMqtt(ctx, o, c) })
	e.iCl, e.clMsg = 1, ""
	uiDirty()
}

var testBroker string // nur Tests: "host:port", ohne Zertifikatspruefung

func testBrokerHost() (string, int, bool) {
	h, p, _ := strings.Cut(testBroker, ":")
	return h, atoiDef(p, 8883), false
}

func (e *engine) cloudStop() {
	e.wCl.stop()
	e.wCl = nil
	e.iCl, e.clMsg = 0, ""
	e.restartCl = time.Time{}
}

// ---------------------------------------------------------------- Status ueber Home Assistant
func (e *engine) canHs() bool {
	p := e.p()
	return p.stOn && (p.conn == 0 || p.conn == 3) && e.s.haConfigured() && p.serial != "" && !e.hsStop
}

// laeuft der HA-Status (ohne Fehler) oder liefert er? Dann braucht es die Cloud nicht
func (e *engine) hsUsable() bool { return e.hsFresh() || (e.wHs != nil && e.iHs != 4) }

func (e *engine) hsCheck() {
	if e.wHs != nil || !e.canHs() {
		return
	}
	if !e.restartHs.IsZero() && since(e.restartHs) < 10*time.Second {
		return
	}
	if !e.needFallback(e.p().conn == 3) {
		return
	}
	e.restartHs = time.Time{}
	h, ser := e.s.ha(), e.p().serial
	e.wHs = startWorker("hs", func(ctx ctxT, o out) { runHaStatus(ctx, o, h, ser, 0, 0) })
	e.iHs, e.hsMsg = 1, ""
	uiDirty()
}

func (e *engine) hsStopW() {
	e.wHs.stop()
	e.wHs = nil
	e.iHs, e.hsMsg = 0, ""
	e.restartHs = time.Time{}
}

// unterwegs: das Heimnetz liefert nicht, der Status kommt aber ueber Home Assistant oder die Cloud
func (e *engine) away() bool { return (e.src == "Cloud" || e.src == "HA") && !e.lanFresh() }

// der HA-Status meldet die Entitaeten des Druckers: Kamera, Titelbild, Licht -> ins Profil
func (e *engine) haEntFound(s string) {
	kind, ent, ok := strings.Cut(s, "|")
	if !ok || ent == "" {
		return
	}
	p := e.p()
	chg := false
	switch kind {
	case "cam":
		if p.haCam == "" {
			p.haCam, chg = ent, true
			uiToast("Kamera in Home Assistant gefunden")
		}
	case "cover":
		if p.haCover == "" {
			p.haCover, chg = ent, true
		}
	case "light":
		if p.haLight != ent {
			p.haLight, chg = ent, true
		}
	}
	if chg {
		uiSave()
	}
}

// alle 2 s: laeuft Bambu Studio? Dann alle Verbindungen trennen, danach automatisch wieder verbinden.
func (e *engine) studioCheck() {
	e.studioTick++
	if e.studioTick%2 != 0 {
		return
	}
	run := e.s.studio && studioRunning()
	if run && !e.studioPause {
		e.studioPause = true
		e.helpersStop(true)
		uiToast("Bambu Studio läuft – BambuWatch pausiert")
		uiDirty()
	} else if !run && e.studioPause {
		e.studioPause = false
		uiToast("Bambu Studio beendet – verbinde wieder")
		e.helpersStart()
	}
}

// ---------------------------------------------------------------- Kamera
func (e *engine) camStart() {
	e.wCam.stop()
	e.wCam = nil
	e.restartCam = time.Time{}
	p := e.p()
	if !(p.camOn && e.canLan(false) && p.imgSrc != 2 && !e.camStop) {
		return
	}
	c := camCfg{ip: p.ip, code: p.code, camType: camTypeKeys[p.camType], ffmpeg: ffmpegFind(e.s.ffmpeg), testRtsp: testRtsp}
	e.wCam = startWorker("cam", func(ctx ctxT, o out) { runCamera(ctx, o, c) })
	e.camRun = now()
	e.iCam, e.camMsg = 1, ""
}

var testRtsp string // nur Tests

// Bild ueber Home Assistant holen, wenn der Drucker im Heimnetz kein Bild liefert (oder "Nur Home Assistant")
func (e *engine) haCheck() {
	if e.wHa != nil {
		return
	}
	p := e.p()
	if !(p.camOn && p.imgSrc != 1 && e.s.haConfigured() && p.haCam != "" && !e.haStop) {
		return
	}
	if !e.restartHa.IsZero() && since(e.restartHa) < 10*time.Second {
		return
	}
	var need bool
	switch {
	case p.imgSrc == 2 || !e.canLan(false) || e.camStop:
		need = true
	case e.lanFrame.IsZero():
		need = since(e.camStrt) > lanGrace
	default:
		need = !e.lanCamFresh()
	}
	if !need {
		return
	}
	e.restartHa = time.Time{}
	h, ent := e.s.ha(), p.haCam
	e.wHa = startWorker("ha", func(ctx ctxT, o out) { runHaCamera(ctx, o, h, ent, haCamInterval) })
	e.iHa, e.haMsg = 1, ""
	uiDirty()
}

var haCamInterval = time.Second

func (e *engine) haStopW() {
	e.wHa.stop()
	e.wHa = nil
	e.iHa, e.haMsg = 0, ""
	e.restartHa = time.Time{}
}

// ---------------------------------------------------------------- Meldungen der Worker
func (e *engine) handle(ev event) {
	switch {
	case ev.w == nil:
	case ev.w == e.wCam:
		e.pollCam(ev, false)
	case ev.w == e.wHa:
		e.pollCam(ev, true)
	case ev.w == e.wMq:
		e.pollStatus(ev, 0)
	case ev.w == e.wCl:
		e.pollStatus(ev, 1)
	case ev.w == e.wHs:
		e.pollStatus(ev, 2)
	case ev.w == e.wLight:
		e.pollLight(ev)
	case ev.w == e.wCov:
		e.pollCover(ev)
	}
}

func cutPrefix(s, p string) (string, bool) {
	if strings.HasPrefix(s, p) {
		return s[len(p):], true
	}
	return "", false
}

// Kamera-Worker (Drucker) oder HA-Bild ($bHa)
func (e *engine) pollCam(ev event, ha bool) {
	state, msg := e.iCam, e.camMsg
	if ha {
		state, msg = e.iHa, e.haMsg
	}
	stop := false
	s := ev.line
	switch {
	case ev.end:
		state = 4
		if msg == "" {
			msg = "Kamera-Helfer beendet"
			if ha {
				msg = "Home-Assistant-Helfer beendet"
			}
		}
	case ev.img != nil && s == "FRAME":
		state, msg = 3, ""
		if ha {
			if !e.lanCamFresh() {
				e.frameLoad(ev.img, "HA")
			}
		} else {
			e.lanFrame = now()
			e.frameLoad(ev.img, "LAN")
			// Drucker liefert wieder selbst -> Home Assistant nicht mehr noetig
			if e.wHa != nil && e.p().imgSrc != 2 {
				e.haStopW()
			}
		}
	case s == "STATE|connecting":
		if state != 3 {
			state = 1
		}
	case s == "STATE|connected":
		state, msg = 2, ""
	case strings.HasPrefix(s, "CAMTYPE|"):
		e.camTypeNow = s[8:]
	case strings.HasPrefix(s, "VIA|"):
	case strings.HasPrefix(s, "ERR|STOP|"), strings.HasPrefix(s, "ERR|AUTH|"):
		state, msg, stop = 4, s[9:], true
	case strings.HasPrefix(s, "ERR|"):
		state, msg = 4, s[4:]
	case s != "":
		msg = trunc(s, 160)
	}
	if ha {
		e.iHa, e.haMsg = state, msg
		if stop {
			e.haStop = true
		}
		if ev.end {
			e.wHa = nil
			if !e.haStop {
				e.restartHa = now()
			}
		}
	} else {
		e.iCam, e.camMsg = state, msg
		if stop {
			e.camStop = true
		}
		if ev.end {
			e.wCam = nil
			if !e.camStop {
				e.restartCam = now()
			}
		}
	}
	uiDirty()
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func (e *engine) frameLoad(b []byte, via string) {
	if len(b) < 100 {
		return
	}
	e.frame = b
	e.frameSeq++
	e.frameAt = now()
	e.imgVia = via
	uiDirty()
}

// Status-Worker: 0 Heimnetz, 1 Bambu-Cloud, 2 Home Assistant
func (e *engine) pollStatus(ev event, kind int) {
	var state *int
	var msg *string
	switch kind {
	case 1:
		state, msg = &e.iCl, &e.clMsg
	case 2:
		state, msg = &e.iHs, &e.hsMsg
	default:
		state, msg = &e.iMq, &e.mqMsg
	}
	stop := false
	s := ev.line
	switch {
	case ev.end:
		*state = 4
		if *msg == "" {
			*msg = "Status-Helfer beendet"
		}
	case strings.HasPrefix(s, "STATUS|"):
		*state, *msg = 3, ""
		e.statusFrom(s, kind)
	case s == "STATE|connecting":
		*state = 1
	case s == "STATE|connected":
		*state, *msg = 2, ""
	case strings.HasPrefix(s, "HAENT|"):
		e.haEntFound(s[6:])
	case strings.HasPrefix(s, "VIA|"):
	case strings.HasPrefix(s, "ERR|AUTH|"):
		*state, *msg = 4, s[9:]
		if kind == 1 {
			e.cloudAuthFailed()
		}
		if kind == 2 {
			stop = true
		}
	case strings.HasPrefix(s, "ERR|STOP|"):
		*state, *msg, stop = 4, s[9:], true
	case strings.HasPrefix(s, "ERR|"):
		*state, *msg = 4, s[4:]
	case s != "":
		*msg = trunc(s, 160)
	}
	switch kind {
	case 1:
		if ev.end {
			e.wCl = nil
			if !e.clAuthBad {
				e.restartCl = now()
			}
		}
	case 2:
		if stop {
			e.hsStop = true
		}
		if ev.end {
			e.wHs = nil
			if !e.hsStop {
				e.restartHs = now()
			}
		}
	default:
		if ev.end {
			e.wMq = nil
			e.restartMq = now()
		}
	}
	uiDirty()
}

// Status uebernehmen: Heimnetz vor Home Assistant vor Cloud, jeweils solange frisch
func (e *engine) statusFrom(line string, kind int) {
	p := e.p()
	switch kind {
	case 0:
		e.lanSt = now()
		e.src = "LAN"
		// Heimnetz liefert -> Cloud und HA nicht mehr noetig (ausser fest eingestellt)
		if e.wCl != nil && p.conn != 2 {
			e.cloudStop()
		}
		if e.wHs != nil && p.conn != 3 {
			e.hsStopW()
		}
		e.statusParse(line)
	case 2:
		if e.lanFresh() {
			return
		}
		e.hsSt = now()
		e.src = "HA"
		if e.wCl != nil && p.conn == 0 {
			e.cloudStop()
		}
		e.statusParse(line)
		e.ipCheck()
	default:
		if e.lanFresh() || e.hsFresh() {
			return
		}
		e.src = "Cloud"
		e.statusParse(line)
		e.ipCheck()
	}
}

var reIPv4 = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)

// Drucker-IP aus dem Status uebernehmen (Cloud/HA) -> Kamerabild im Heimnetz ohne Abtippen
func (e *engine) ipCheck() {
	p := e.p()
	if p.conn == 1 {
		return
	}
	n := e.stv("ip")
	if n == "" || n == p.ip || n == "0.0.0.0" || !reIPv4.MatchString(n) {
		return
	}
	had := p.ip != ""
	p.ip = n
	uiSave()
	if had {
		uiToast("Neue Drucker-IP übernommen: " + n)
	} else {
		uiToast("Drucker-IP übernommen: " + n)
	}
	// Kamera und Heimnetz-Status mit der neuen IP neu starten (Cloud laeuft weiter)
	e.camStop = false
	e.camStrt = now()
	e.camStart()
	e.wMq.stop()
	e.wMq = nil
	e.restartMq = time.Time{}
	if p.stOn && e.canLan(true) && e.lanAllowed() {
		e.mqStart()
		e.lanStart = now()
	}
}

// Cloud meldet: Token abgelaufen/ungueltig -> nicht weiter probieren, einmal Bescheid geben
func (e *engine) cloudAuthFailed() {
	if e.clAuthBad {
		return
	}
	e.clAuthBad = true
	uiToast("Bambu-Anmeldung abgelaufen")
	if e.s.notify {
		uiTrayTip(appName, "Bambu-Anmeldung abgelaufen – für den Status von unterwegs in den Einstellungen neu anmelden.")
	}
}

func parseStatus(line string, into map[string]string) {
	for _, f := range strings.Split(line, "|")[1:] {
		if k, v, ok := strings.Cut(f, "="); ok {
			into[k] = v
		}
	}
}

func (e *engine) statusParse(line string) {
	parseStatus(line, e.st)
	e.haveSt = true
	e.stAt = now()
	// Zustandswechsel melden
	cur := e.stv("st")
	if e.prevState != "" && cur != e.prevState {
		was := isActive(e.prevState)
		if was && cur == "FINISH" {
			uiNotify("Druck fertig", e.stv("job"), 0xFF00AE42)
		}
		if was && cur == "FAILED" {
			uiNotify("Druck abgebrochen", e.stv("job"), 0xFFE5484D)
		}
		if e.prevState == "RUNNING" && cur == "PAUSE" {
			uiNotify("Druck pausiert", "Am Drucker nachsehen (z. B. Filament)", 0xFFF5A524)
		}
	}
	if cur != "" {
		e.prevState = cur
	}
	uiStatusChanged()
}

// ---------------------------------------------------------------- einmal pro Sekunde
func (e *engine) everySecond() {
	e.studioCheck()
	if e.studioPause {
		uiStatusChanged()
		uiDirty()
		return
	}
	// unterwegs: Kamera und Heimnetz-Status nur einmal pro Minute fuer 20 s probieren
	if e.away() {
		if e.wCam != nil && !e.lanCamFresh() && since(e.camRun) > 20*time.Second {
			e.wCam.stop()
			e.wCam = nil
			e.iCam, e.camMsg = 0, "unterwegs – nächster Versuch in 1 min"
			e.restartCam = now()
		}
		if e.wMq != nil && since(e.mqRun) > 20*time.Second {
			e.wMq.stop()
			e.wMq = nil
			e.iMq, e.mqMsg = 0, "unterwegs – nächster Versuch in 1 min"
			e.restartMq = now()
		}
	}
	retry := 10 * time.Second
	if e.away() {
		retry = time.Minute
	}
	if !e.restartCam.IsZero() && since(e.restartCam) > retry {
		e.camStart()
	}
	if !e.restartMq.IsZero() && since(e.restartMq) > retry {
		e.restartMq = time.Time{}
		if e.p().stOn && e.canLan(true) && e.lanAllowed() {
			e.mqStart()
		}
	}
	e.hsCheck()
	e.cloudCheck()
	e.haCheck()
	e.coverCheck()
	e.lightTimeout()
	uiStatusChanged()
	uiDirty()
}

// ---------------------------------------------------------------- wirksame Quellen (Punkte, Hinweise)
// Bildquelle: Home Assistant, wenn der Drucker gerade kein Bild liefert und HA im Spiel ist
func (e *engine) camEff() (state int, msg, via string) {
	p := e.p()
	if !e.lanCamFresh() && (p.imgSrc == 2 || e.wHa != nil || (e.haStop && e.s.haConfigured() && p.haCam != "")) {
		return e.iHa, e.haMsg, "Home Assistant"
	}
	return e.iCam, e.camMsg, "Drucker"
}

// Status: Heimnetz, sonst Home Assistant, sonst Cloud - je nachdem, was im Spiel ist
func (e *engine) statusEff() (state int, msg, via string) {
	p := e.p()
	if !e.lanFresh() && p.conn != 1 {
		hs := p.conn == 3 || e.wHs != nil || e.src == "HA" || e.hsStop
		cl := p.conn == 2 || e.wCl != nil || e.src == "Cloud" || (e.clAuthBad && e.s.token != "")
		if hs && (e.src == "HA" || p.conn == 3 || !cl) {
			return e.iHs, e.hsMsg, "Home Assistant"
		}
		if cl {
			return e.iCl, e.clMsg, "Bambu-Cloud"
		}
	}
	return e.iMq, e.mqMsg, "Heimnetz"
}

// ---------------------------------------------------------------- Druckobjekt-Bild
func (e *engine) liveFrameOk() bool {
	return e.frame != nil && e.p().camOn && since(e.frameAt) < 10*time.Second
}

// gibt es eine Quelle? Home-Assistant-Bild oder Bambu-Konto (Cloud-Auftraege)
func (e *engine) coverSrc() bool {
	p := e.p()
	if e.s.haConfigured() && p.haCover != "" {
		return true
	}
	return e.s.token != "" && p.serial != "" && !e.clAuthBad
}

func (e *engine) coverCan() bool {
	if !(e.p().coverOn || e.forceCover) {
		return false
	}
	return e.coverSrc()
}

func (e *engine) coverShown() bool {
	return e.cover != nil && (e.forceCover || (e.p().coverOn && !e.liveFrameOk()))
}

// Bild holen, wenn noetig. Neu bei anderem Auftrag, sonst alle 10 min (nach Fehler jede Minute).
// Solange die Kamera liefert, wird nichts abgefragt.
func (e *engine) coverCheck() {
	if e.wCov != nil {
		if since(e.covTry) > time.Minute { // Notbremse, falls es haengt
			e.wCov.stop()
			e.wCov = nil
		}
		return
	}
	if !e.coverCan() || (e.liveFrameOk() && !e.forceCover) {
		return
	}
	p := e.p()
	// der Kamera nach dem (Neu-)Start Zeit lassen - ausser sie ist aus oder es ist umgeschaltet
	if p.camOn && e.frameAt.IsZero() && since(e.camStrt) < lanGrace && !e.forceCover {
		return
	}
	key := p.serial + "|" + p.haCover + "|" + e.stv("job")
	wait := 5 * time.Second
	if key == e.covKey {
		wait = time.Minute
		if e.cover != nil {
			wait = 10 * time.Minute
		}
	}
	if !e.covTry.IsZero() && since(e.covTry) < wait {
		return
	}
	e.covKey = key
	e.covTry = now()
	c := coverCfg{serial: p.serial, region: e.s.region}
	if e.s.haConfigured() && p.haCover != "" {
		c.ha, c.entity = e.s.ha(), p.haCover
	}
	if e.s.token != "" && !e.clAuthBad {
		c.token = e.s.token
	}
	e.wCov = startWorker("cover", func(ctx ctxT, o out) { runCover(ctx, o, c) })
}

func (e *engine) pollCover(ev event) {
	s := ev.line
	switch {
	case ev.end:
		e.wCov = nil
	case ev.img != nil && strings.HasPrefix(s, "COVER|"):
		f := strings.SplitN(s, "|", 3)
		if len(f) == 3 {
			e.coverLoad(ev.img, f[1], f[2])
		}
	case strings.HasPrefix(s, "NOCOVER|"):
		e.coverDrop()
		e.covMsg = s[8:]
		if e.forceCover { // umgeschaltet, aber es gibt kein Bild -> zurueck zur Kamera
			e.forceCover = false
			uiToast("Kein Druckobjekt-Bild gefunden")
		}
	case strings.HasPrefix(s, "ERR|"):
		// Fehler: altes Bild nur behalten, wenn es noch zum selben Auftrag gehoert
		if e.covKeyLoaded != e.covKey {
			e.coverDrop()
		}
		e.covMsg = s[4:]
	}
	uiDirty()
}

func (e *engine) coverLoad(b []byte, via, title string) {
	if len(b) < 100 {
		return
	}
	e.coverDrop()
	e.cover = b
	e.coverSeq++
	e.covVia, e.covTitle = via, title
	e.covKeyLoaded = e.covKey
	e.covMsg = ""
	uiDirty()
}

// die Oberflaeche konnte das Bild nicht lesen
func (e *engine) coverBad() {
	e.coverDrop()
	e.covMsg = "Bild nicht lesbar"
}

func (e *engine) coverDrop() {
	e.cover = nil
	e.coverSeq++
	e.covVia, e.covTitle, e.covKeyLoaded = "", "", ""
	uiDirty()
}

// beim Neuverbinden / Druckerwechsel: alles zuruecksetzen
func (e *engine) coverStop() {
	e.wCov.stop()
	e.wCov = nil
	e.covKey = ""
	e.covTry = time.Time{}
	e.covMsg = ""
	e.coverDrop()
}

// Herkunft fuer die Plakette. Cloud: passt der Auftragsname nicht zum laufenden Druck,
// ist es nur der letzte Cloud-Auftrag (z. B. Druck von SD-Karte oder im LAN-Modus gestartet).
func (e *engine) coverLabel() string {
	if e.covVia != "Cloud" {
		return "Druckobjekt (HA)"
	}
	job := e.stv("job")
	if job != "" && e.covTitle != "" && !strings.Contains(strings.ToLower(job), strings.ToLower(e.covTitle)) &&
		!strings.Contains(strings.ToLower(e.covTitle), strings.ToLower(job)) {
		return "Letzter Cloud-Auftrag"
	}
	return "Druckobjekt (Cloud)"
}

func ageText(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s < 60:
		return strconv.Itoa(s) + " s"
	case s < 3600:
		return strconv.Itoa(s/60) + " min"
	}
	return strconv.Itoa(s/3600) + " h"
}

func (e *engine) noCamWhy() string {
	switch {
	case e.forceCover && e.liveFrameOk():
		return "umgeschaltet"
	case !e.p().camOn:
		return "Kamera aus"
	case e.frame != nil && !e.frameAt.IsZero():
		return "Kamerabild seit " + ageText(since(e.frameAt)) + " weg"
	case e.away():
		return "unterwegs"
	}
	return "kein Kamerabild"
}

// Knopf "Bild" oben / Rechtsklick: Kamerabild <-> Druckobjekt
func (e *engine) viewToggle() {
	e.forceCover = !e.forceCover
	if e.forceCover && e.cover == nil {
		if !e.coverSrc() {
			e.forceCover = false
			uiToast("Druckobjekt: Home Assistant oder Bambu-Konto einrichten")
			return
		}
		e.covKey = "" // sofort holen
		e.covTry = time.Time{}
		e.coverCheck()
		uiToast("Druckobjekt wird geladen …")
	} else if e.forceCover {
		uiToast("Druckobjekt")
	} else {
		uiToast("Kamerabild")
	}
	uiDirty()
}

// ---------------------------------------------------------------- Druckerlicht
// im Heimnetz direkt (MQTT), sonst ueber Home Assistant; Cloud nicht (nicht geprueft)
func (e *engine) lightToggle() {
	if e.wLight != nil {
		return
	}
	if e.studioPause {
		uiToast("Pausiert – Licht bitte in Bambu Studio schalten")
		return
	}
	state := "on"
	if e.stv("light") == "on" {
		state = "off"
	}
	p := e.p()
	switch {
	case e.lanFresh() && e.canLan(true):
		ip, code, ser := p.ip, p.code, p.serial
		e.wLight = startWorker("light", func(ctx ctxT, o out) { runLightLan(ctx, o, ip, code, ser, state) })
	case e.s.haConfigured() && p.haLight != "":
		h, ent := e.s.ha(), p.haLight
		e.wLight = startWorker("light", func(ctx ctxT, o out) { runHaLight(ctx, o, h, ent, state) })
	default:
		uiToast("Licht: nur im Heimnetz oder über Home Assistant")
		return
	}
	e.lightAt = now()
}

func (e *engine) pollLight(ev event) {
	s := ev.line
	switch {
	case ev.end:
		e.wLight = nil
	case strings.HasPrefix(s, "LIGHT|"):
		e.st["light"] = s[6:] // sofort anzeigen, der naechste Status bestaetigt es
		uiStatusChanged()
		if e.stv("light") == "on" {
			uiToast("Licht an")
		} else {
			uiToast("Licht aus")
		}
	case strings.HasPrefix(s, "ERR|AUTH|"):
		uiToast("Licht: " + s[9:])
	case strings.HasPrefix(s, "ERR|"):
		uiToast(s[4:])
	}
}

func (e *engine) lightTimeout() {
	if e.wLight != nil && since(e.lightAt) > 20*time.Second {
		e.wLight.stop()
		e.wLight = nil
	}
}

// ---------------------------------------------------------------- Druckerwechsel
func (e *engine) prnSwitch(i int) {
	if i == e.s.active || i < 0 || i >= len(e.s.prn) {
		return
	}
	e.s.active = i
	e.frame = nil
	e.frameSeq++
	e.frameAt = time.Time{}
	e.helpersStart()
	uiToast(e.p().name)
}
