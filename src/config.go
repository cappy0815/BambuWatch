// Einstellungen und Druckerprofile – gleiche INI wie BambuWatch 1.7 (AutoIt), damit die neue EXE
// im selben Ordner sofort alle Drucker und Zugangsdaten hat.
// Geheimnisse: "dpapi:" + Hex (Windows-DPAPI, Beschreibung "BambuWatch", UTF-8), sonst Klartext.
package main

import (
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

// Zugriff auf die INI (Windows: GetPrivateProfileString, Tests: Speicher)
type iniStore interface {
	get(sec, key, def string) string
	set(sec, key, val string)
	del(sec, key string) // key "" = ganzer Abschnitt
}

// DPAPI (Windows); in Tests ein Platzhalter
var (
	dpapiProtect   = func(b []byte) []byte { return nil }
	dpapiUnprotect = func(b []byte) []byte { return nil }
)

func secretDecode(s string) string {
	if !strings.HasPrefix(s, "dpapi:") {
		return s
	}
	b, err := hex.DecodeString(s[6:])
	if err != nil || len(b) == 0 {
		return ""
	}
	return string(dpapiUnprotect(b)) // anderes Konto / anderer PC -> leer, neu eingeben
}

func secretEncode(s string) string {
	if s == "" {
		return ""
	}
	b := dpapiProtect([]byte(s))
	if b == nil {
		return s // Notfall: unverschluesselt (wie 1.7)
	}
	return "dpapi:" + strings.ToUpper(hex.EncodeToString(b))
}

type printer struct {
	name, ip, code, serial, model string
	camType                       int // 0 automatisch, 1 Einzelbilder, 2 Videostream
	imgSrc                        int // 0 automatisch, 1 nur Drucker, 2 nur Home Assistant
	haCam                         string
	conn                          int // Status: 0 automatisch, 1 Heimnetz, 2 Cloud, 3 Home Assistant
	camOn, stOn, coverOn          bool
	haCover, haLight              string
}

func newPrinter(name string) printer {
	return printer{name: name, camOn: true, stOn: true, coverOn: true}
}

type settings struct {
	overlay               int // 0 aus, 1 kompakt, 2 ausfuehrlich
	aspect, onTop, tray   bool
	alpha                 int
	notify, sound, studio bool
	snapDir               string
	fontAdd               int
	x, y, w, h            int
	token, user, email    string
	region                string
	haIn, haOut, haToken  string
	haInsecure            bool
	ffmpeg                string
	prn                   []printer
	active                int // Index in prn (0-basiert)
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func atoiDef(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

func loadSettings(st iniStore, docs string) *settings {
	g := func(sec, key, def string) string { return st.get(sec, key, def) }
	s := &settings{}
	s.overlay = atoiDef(g("Anzeige", "Overlay", "1"), 1)
	if s.overlay < 0 || s.overlay > 2 {
		s.overlay = 1
	}
	s.aspect = g("Anzeige", "Seitenverhaeltnis", "1") == "1"
	s.onTop = g("Fenster", "Vordergrund", "0") == "1"
	s.tray = g("Fenster", "Infobereich", "1") == "1"
	s.alpha = atoiDef(g("Fenster", "Deckkraft", "255"), 255)
	if s.alpha < 100 || s.alpha > 255 {
		s.alpha = 255
	}
	s.notify = g("Meldungen", "Hinweis", "1") == "1"
	s.sound = g("Meldungen", "Ton", "1") == "1"
	s.studio = g("Fenster", "BambuStudioPause", "1") == "1"
	s.snapDir = g("Snapshots", "Ordner", docs+`\BambuWatch`)
	s.fontAdd = clampI(atoiDef(g("Anzeige", "Schrift", "1"), 1), 0, 3)
	s.x = atoiDef(g("Fenster", "X", "-1"), -1)
	s.y = atoiDef(g("Fenster", "Y", "-1"), -1)
	s.w = atoiDef(g("Fenster", "W", "560"), 560)
	s.h = atoiDef(g("Fenster", "H", "0"), 0)
	s.token = secretDecode(g("Konto", "Token", ""))
	s.user = g("Konto", "Benutzer", "")
	s.email = g("Konto", "EMail", "")
	s.region = g("Konto", "Region", "Global")
	if s.region != "China" {
		s.region = "Global"
	}
	s.haIn = g("HomeAssistant", "AdresseHeimnetz", "")
	s.haOut = g("HomeAssistant", "AdresseUnterwegs", "")
	s.haToken = secretDecode(g("HomeAssistant", "Token", ""))
	s.haInsecure = g("HomeAssistant", "ZertifikatIgnorieren", "0") == "1"
	s.ffmpeg = g("Video", "Ffmpeg", "")
	s.loadPrinters(st)
	return s
}

func (s *settings) loadPrinters(st iniStore) {
	g := func(sec, key, def string) string { return st.get(sec, key, def) }
	n := atoiDef(g("Allgemein", "Anzahl", "0"), 0)
	// bis v1.3 galten Kamera/Status an-aus fuer alle Drucker -> Vorgabe fuer jedes Profil
	camDef, stDef := g("Anzeige", "Kamera", "1") != "0", g("Anzeige", "Status", "1") != "0"
	b := func(v string, def bool) bool {
		if v == "" {
			return def
		}
		return v != "0"
	}
	s.prn = nil
	s.active = 0
	if n == 0 {
		// Uebernahme aus v1.0 / v1.1 (ein Drucker)
		ip, ser, code := g("Drucker", "IP", ""), g("Drucker", "Seriennummer", ""), secretDecode(g("Drucker", "AccessCode", ""))
		if ip != "" || ser != "" || code != "" {
			name := g("Konto", "Drucker", "")
			if name == "" {
				name = "Mein Drucker"
			}
			p := newPrinter(name)
			p.ip, p.code, p.serial = ip, code, ser
			p.conn = atoiDef(g("Verbindung", "Modus", "0"), 0)
			p.camOn, p.stOn = camDef, stDef
			s.prn = append(s.prn, p)
			s.savePrinters(st)
			st.del("Drucker", "")
			st.del("Verbindung", "")
			st.del("Konto", "Drucker")
		}
	} else {
		for i := 1; i <= n; i++ {
			sec := "Drucker_" + strconv.Itoa(i)
			p := newPrinter(g(sec, "Name", "Drucker "+strconv.Itoa(i)))
			p.ip = g(sec, "IP", "")
			p.code = secretDecode(g(sec, "AccessCode", ""))
			p.serial = g(sec, "Seriennummer", "")
			p.model = g(sec, "Modell", "")
			p.camType = clampI(atoiDef(g(sec, "Kameratyp", "0"), 0), 0, 2)
			p.imgSrc = clampI(atoiDef(g(sec, "Bildquelle", "0"), 0), 0, 2)
			p.haCam = g(sec, "HAKamera", "")
			p.conn = clampI(atoiDef(g(sec, "StatusQuelle", "0"), 0), 0, 3)
			p.camOn = b(g(sec, "KameraAn", ""), camDef)
			p.stOn = b(g(sec, "StatusAn", ""), stDef)
			p.coverOn = g(sec, "Druckobjekt", "1") != "0"
			p.haCover = g(sec, "HATitelbild", "")
			p.haLight = g(sec, "HALicht", "")
			s.prn = append(s.prn, p)
		}
		s.active = atoiDef(g("Allgemein", "Aktiv", "1"), 1) - 1
	}
	if len(s.prn) == 0 {
		s.prn = append(s.prn, newPrinter("Mein Drucker"))
	}
	if s.active < 0 || s.active >= len(s.prn) {
		s.active = 0
	}
}

func b01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (s *settings) savePrinters(st iniStore) {
	st.set("Allgemein", "Anzahl", strconv.Itoa(len(s.prn)))
	st.set("Allgemein", "Aktiv", strconv.Itoa(s.active+1))
	for i, p := range s.prn {
		sec := "Drucker_" + strconv.Itoa(i+1)
		st.set(sec, "Name", p.name)
		st.set(sec, "IP", p.ip)
		st.set(sec, "AccessCode", secretEncode(p.code))
		st.set(sec, "Seriennummer", p.serial)
		st.set(sec, "Modell", p.model)
		st.set(sec, "Kameratyp", strconv.Itoa(p.camType))
		st.set(sec, "Bildquelle", strconv.Itoa(p.imgSrc))
		st.set(sec, "HAKamera", p.haCam)
		st.set(sec, "StatusQuelle", strconv.Itoa(p.conn))
		st.set(sec, "KameraAn", b01(p.camOn))
		st.set(sec, "StatusAn", b01(p.stOn))
		st.set(sec, "Druckobjekt", b01(p.coverOn))
		st.set(sec, "HATitelbild", p.haCover)
		st.set(sec, "HALicht", p.haLight)
	}
	// alte, fuer alle Drucker geltende Schalter (bis v1.3) sind jetzt pro Drucker gespeichert
	st.del("Anzeige", "Kamera")
	st.del("Anzeige", "Status")
	for i := len(s.prn) + 1; i <= len(s.prn)+20; i++ { // geloeschte Drucker entfernen
		st.del("Drucker_"+strconv.Itoa(i), "")
	}
}

func (s *settings) cur() *printer { return &s.prn[s.active] }

func (s *settings) haConfigured() bool { return s.haToken != "" && (s.haIn != "" || s.haOut != "") }

func (s *settings) ha() haCfg {
	return haCfg{urls: haURLs(s.haIn, s.haOut), token: s.haToken, insecure: s.haInsecure}
}

// ---------------------------------------------------------------- INI im Speicher (Tests)
type memIni map[string]map[string]string

func (m memIni) get(sec, key, def string) string {
	if v, ok := m[strings.ToLower(sec)][strings.ToLower(key)]; ok {
		return v
	}
	return def
}

func (m memIni) set(sec, key, val string) {
	s := strings.ToLower(sec)
	if m[s] == nil {
		m[s] = map[string]string{}
	}
	m[s][strings.ToLower(key)] = val
}

func (m memIni) del(sec, key string) {
	if key == "" {
		delete(m, strings.ToLower(sec))
		return
	}
	delete(m[strings.ToLower(sec)], strings.ToLower(key))
}

// alle allgemeinen Einstellungen + Druckerprofile schreiben (Schluessel wie 1.7)
func (s *settings) save(st iniStore) {
	st.set("Anzeige", "Overlay", strconv.Itoa(s.overlay))
	st.set("Anzeige", "Seitenverhaeltnis", b01(s.aspect))
	st.set("Anzeige", "Schrift", strconv.Itoa(s.fontAdd))
	st.set("Fenster", "Vordergrund", b01(s.onTop))
	st.set("Fenster", "Infobereich", b01(s.tray))
	st.set("Fenster", "Deckkraft", strconv.Itoa(s.alpha))
	st.set("Fenster", "BambuStudioPause", b01(s.studio))
	st.set("Meldungen", "Hinweis", b01(s.notify))
	st.set("Meldungen", "Ton", b01(s.sound))
	st.set("Snapshots", "Ordner", s.snapDir)
	s.saveAccount(st)
	st.set("HomeAssistant", "AdresseHeimnetz", s.haIn)
	st.set("HomeAssistant", "AdresseUnterwegs", s.haOut)
	st.set("HomeAssistant", "Token", secretEncode(s.haToken))
	st.set("HomeAssistant", "ZertifikatIgnorieren", b01(s.haInsecure))
	st.set("Video", "Ffmpeg", s.ffmpeg)
	s.savePrinters(st)
}

func (s *settings) saveAccount(st iniStore) {
	st.set("Konto", "Token", secretEncode(s.token))
	st.set("Konto", "Benutzer", s.user)
	st.set("Konto", "EMail", s.email)
	st.set("Konto", "Region", s.region)
}

func (s *settings) savePos(st iniStore) {
	st.set("Fenster", "X", strconv.Itoa(s.x))
	st.set("Fenster", "Y", strconv.Itoa(s.y))
	st.set("Fenster", "W", strconv.Itoa(s.w))
	st.set("Fenster", "H", strconv.Itoa(s.h))
}

// Kopie fuer das Einstellungsfenster (Aenderungen gelten erst mit "Speichern")
func (s *settings) clone() *settings {
	c := *s
	c.prn = append([]printer(nil), s.prn...)
	return &c
}

// Drucker aus dem Bambu-Konto einpflegen (__PrnMerge): vorhandene per Seriennummer aktualisieren,
// ein leeres Start-Profil wird durch den ersten Konto-Drucker ersetzt. Rueckgabe: neue Profile.
type accDev struct{ serial, name, code, online, model string }

var reDefaultName = regexp.MustCompile(`^(Mein Drucker|Drucker \d+)$`)

func (s *settings) merge(devs []accDev) int {
	added := 0
	for _, d := range devs {
		k := -1
		for i := range s.prn {
			if s.prn[i].serial == d.serial {
				k = i
			}
		}
		if k < 0 {
			for i := range s.prn {
				p := s.prn[i]
				if k < 0 && p.ip == "" && p.code == "" && p.serial == "" {
					k = i
				}
			}
		}
		if k < 0 {
			s.prn = append(s.prn, newPrinter(d.name))
			k = len(s.prn) - 1
			added++
		}
		p := &s.prn[k]
		p.serial = d.serial
		if p.name == "" || reDefaultName.MatchString(p.name) {
			p.name = d.name
		}
		if d.code != "" {
			p.code = d.code
		}
		if d.model != "" {
			p.model = d.model
		}
	}
	if s.active < 0 || s.active >= len(s.prn) {
		s.active = 0
	}
	return added
}
