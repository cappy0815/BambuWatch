//go:build windows

// Hilfe (zwei Reiter) und Changelog.
package main

import "strings"

var (
	dHelp, dLog *dialog
	helpLogo    *gimg
)

func helpItem(d *dialog, key, text string, y, W float64) float64 {
	tw := W - 40 - 112
	d.label(key, 20, y, 108, 18, cTxt, 11.3, 1, 0)
	wd := d.wrapLbl(text, 132, y+1, tw, cTxt2, 11.3, 0, 0)
	return y + 6 + wd.h
}

func helpShow() {
	if dHelp.alive() {
		pSetForegroundWindow.Call(dHelp.hwnd)
		return
	}
	if !isVisible(hMain) || isIconic(hMain) {
		mainShow()
	}
	if helpLogo == nil {
		helpLogo = loadImage(icoPNG())
	}
	const W = 560.0
	d := newDialog("Hilfe", W)
	dHelp = d
	curPage = -1
	// Kopf: Icon, Titel, Versions-Chip, Schliessen
	d.add(&widget{x: 20, y: 16, w: 48, h: 48, draw: func(c *canvas, wd *widget, hot bool) {
		c.imageFit(helpLogo, wd.x, wd.y, wd.w, wd.h, 0)
	}})
	d.label(appName, 80, 16, 260, 28, cTxt, 20, 1, 0)
	d.label("Live-Bild und Druckstatus für Bambu-Lab-Drucker", 80, 44, 330, 18, cTxt2, 12, 0, 0)
	d.add(&widget{x: W - 104, y: 24, w: 50, h: 22, draw: func(c *canvas, wd *widget, hot bool) {
		c.rrect(wd.x, wd.y, wd.w, wd.h, 4, cGreen)
		c.text("v"+appVersion, 12, 1, cWhite, wd.x, wd.y, wd.w, wd.h, 1, 1)
	}})
	d.closeX()
	d.add(&widget{x: 20, y: 74, w: W - 40, h: 2, draw: func(c *canvas, wd *widget, hot bool) { c.fill(wd.x, wd.y, wd.w, wd.h, cGreen) }})
	d.tabs([]string{"Einrichtung & Bedienung", "Anzeige & Hinweise"}, []float64{20, 214}, []float64{190, 170}, 84)

	y0 := 112.0
	curPage = 0
	y := d.section("Einrichtung", y0)
	y = helpItem(d, "Am einfachsten", "Einstellungen › Konten & Dienste › Bambu-Konto › Anmelden. Alle Drucker des Kontos werden übernommen, die IP kommt aus dem ersten Status.", y, W)
	y = helpItem(d, "Am Drucker", "„LAN Mode Liveview“ einschalten, sonst kein Kamerabild (Bezeichnung je nach Firmware/Sprache leicht anders). Developer Mode ist zum Zuschauen nicht nötig.", y, W)
	y = helpItem(d, "Ohne Konto", "Einstellungen › Drucker: IP-Adresse, Access Code und Seriennummer eintragen, dann „Verbindung testen“. Läuft dann nur im Heimnetz.", y, W)
	y = helpItem(d, "H2D, X1, P2S", "Bild kommt als Videostream – dafür ffmpeg.exe angeben (Konten & Dienste). Status im Heimnetz nur mit „LAN Only“ + Developer Mode am Drucker, sonst über die Cloud.", y, W)
	y = d.section("Bedienung", y)
	y = helpItem(d, "Drucker wechseln", "Klick auf den Druckernamen oben links – dort auch „Drucker verwalten“. Kamera, Status und Druckobjekt sind pro Drucker einstellbar.", y, W)
	y = helpItem(d, "Verschieben", "an der Leiste oder direkt am Bild ziehen", y, W)
	y = helpItem(d, "Größe", "am Rand ziehen (Seitenverhältnis bleibt, abschaltbar) oder Rechtsklick › Größe", y, W)
	y = helpItem(d, "Rechtsklick", "aufs Bild öffnet das Menü mit allen Funktionen", y, W)
	y = helpItem(d, "Tasten", "S Snapshot · O Overlay · L Licht · T Vordergrund · 1/2/3 Größe · F1 Hilfe · F2 Einstellungen · F5 neu verbinden", y, W)
	y = helpItem(d, "Position", "Größe und Platz werden gespeichert", y, W)
	y1 := y

	curPage = 1
	y = d.section("Anzeige", y0)
	y = helpItem(d, "Punkte links", "1. Punkt Kamera, 2. Punkt Druckstatus (Heimnetz, Home Assistant oder Cloud): grün läuft · gelb verbindet · rot Fehler · grau aus (Maus drauf = Details)", y, W)
	y = helpItem(d, "Overlay", "Kompakt: Fortschritt, Restzeit, Fertig-Uhrzeit. Ausführlich: dazu Auftrag, Schicht, Temperaturen, Tempo, aktives Filament und alle AMS-Slots mit Farbe", y, W)
	y = helpItem(d, "Taskleiste", "Fortschrittsbalken im Taskleisten-Symbol (grün druckt, gelb pausiert, rot abgebrochen), Prozent im Fenstertitel, Fertig-Uhrzeit im Tooltip des Infobereichs", y, W)
	y = helpItem(d, "Bild", "Knopf mit dem Bild oben (oder Rechtsklick): schaltet zwischen Kamerabild und Druckobjekt um. Grün = Druckobjekt ist gewählt.", y, W)
	y = helpItem(d, "Licht", "Glühbirne oben oder Taste L: schaltet die Druckerbeleuchtung – im Heimnetz direkt, sonst über Home Assistant", y, W)
	y = helpItem(d, "Meldungen", "Fenster unten rechts mit Bild des Druckobjekts bei Druckende, Pause (z. B. Filament leer) und Abbruch – mit Ton, wenn gewünscht", y, W)
	y = helpItem(d, "Snapshot", "speichert das Originalbild der Kamera (ohne Overlay) als JPG", y, W)
	y = helpItem(d, "Druckobjekt", "Kommt kein Kamerabild (unterwegs, Kamera aus, Störung), zeigt BambuWatch das Bild des Druckobjekts: Titelbild aus Home Assistant, sonst vom letzten Auftrag in der Bambu-Cloud. Oben links steht, woher es kommt.", y, W)
	y = d.section("Gut zu wissen", y)
	y = helpItem(d, "Bambu Studio", "Solange Bambu Studio läuft, trennt BambuWatch alle Verbindungen und verbindet sich danach von selbst wieder (abschaltbar unter Anzeige & Fenster)", y, W)
	y = helpItem(d, "Unterwegs", "Bild und Status kommen über Home Assistant (wenn eingerichtet), sonst der Status über die Bambu-Cloud – oben links steht, woher. Das Heimnetz wird dann nur noch einmal pro Minute probiert.", y, W)
	y = helpItem(d, "Bildrate", "Der A1 liefert Einzelbilder, kein flüssiges Video. Ohne laufenden Druck kommt manchmal gar kein Bild – BambuWatch versucht es dann automatisch weiter.", y, W)
	y = helpItem(d, "Protokoll", "Kamera und Status nutzen die von der Community dokumentierte LAN-Schnittstelle. Bambu Lab unterstützt sie offiziell nicht; ein Firmware-Update kann sie ändern.", y, W)
	y = helpItem(d, "Sicherheit", "Passwort wird nie gespeichert. Access Codes, Bambu- und HA-Token liegen mit Windows (DPAPI) verschlüsselt in der INI. Drucker-Ports bitte nicht ins Internet freigeben – dafür ist der Weg über Home Assistant da.", y, W)
	y2 := y

	curPage = -1
	y = maxF(y1, y2) + 12
	d.pill("Changelog – was ist neu?", (W-220)/2, y, 220, 28, cBtnDk, "log")
	y += 42
	d.hline(20, y, W-40, cLine)
	y += 10
	d.label("Gebaut von Justus Jonas & Michael Nowak", 20, y, W-40, 17, cTxt2, 11.3, 2, 1)
	d.label("– mit Claude am Druckbett", 20, y+17, W-40, 17, cTxt2, 11.3, 2, 1)
	y += 38
	host := strings.TrimPrefix(strings.SplitN(appPage, "#", 2)[0], "https://")
	host = strings.TrimSuffix(host, "/")
	d.link("Projektseite: "+host, 20, y, W-40, 20, 1, "page")
	y += 30
	d.pill("Alles klar", (W-110)/2, y, 110, 28, cGreen, "ok")
	y += 46
	d.onClick = func(id string) {
		switch id {
		case "log":
			changelogShow()
		case "page":
			shellOpen(appPage)
		case "ok":
			helpClose()
		}
	}
	d.onOK = helpClose
	d.onClose = helpClose
	d.show(y, hMain)
}

func helpClose() {
	changelogClose()
	dHelp.close()
	dHelp = nil
}

type logEntry struct{ ver, date, lines string }

var changelog = []logEntry{
	{"2.0", "21.11.2026", "Komplett neu gebaut (Go statt AutoIt): eine einzige EXE mit eigenem Icon, ohne PowerShell-Helfer|" +
		"Drucker, Zugangsdaten und Einstellungen aus 1.x werden übernommen (gleiche BambuWatch.ini)|" +
		"Scharfe Darstellung bei Bildschirm-Skalierung (125 %, 150 % …)|" +
		"Schriftgröße gilt sofort überall, dunkle Tooltips, Changelog zum Rollen|" +
		"Hilfe: Link zur Projektseite, Taste L in der Tastenübersicht"},
	{"1.7", "22.10.2026", "Pause, solange Bambu Studio läuft: alle Verbindungen getrennt, danach automatisch wieder verbunden (abschaltbar)"},
	{"1.6.1", "22.10.2026", "Fix: Videostream (H2D, X1, P2S) mit aktuellem ffmpeg – Zertifikat des Druckers wird akzeptiert"},
	{"1.6", "22.10.2026", "Umschalter Kamerabild / Druckobjekt (Knopf oben und Rechtsklick-Menü)|" +
		"Status-Helfer schonender: kompletter Status höchstens alle 5 Minuten angefordert, bei Verbindungsfehlern wachsende Wartezeit bis 1 Minute"},
	{"1.5", "22.10.2026", "Druckstatus über Home Assistant (ha-bambulab) – Reihenfolge Heimnetz, Home Assistant, Bambu-Cloud; neue Status-Quelle „Nur Home Assistant“|" +
		"HA-Kamera, Titelbild und Licht werden über die Seriennummer automatisch gefunden|" +
		"Unterwegs: Heimnetz-Versuche nur noch einmal pro Minute, klarere Anzeige|" +
		"Fortschrittsbalken im Taskleisten-Symbol|" +
		"Meldungsfenster mit Bild des Druckobjekts|" +
		"Druckerlicht an/aus (Glühbirne oben, Taste L)|" +
		"Ausführliches Overlay: alle AMS-Slots mit Farbe und Material"},
	{"1.4", "22.10.2026", "Kamerabild und Druckstatus an/aus jetzt pro Drucker (Einstellungen › Drucker)|" +
		"Kein Kamerabild? Dann erscheint das Bild des Druckobjekts – aus Home Assistant (Titelbild) oder vom letzten Cloud-Auftrag|" +
		"Home Assistant: Bild-Entitäten werden mit geladen, Titelbild wird automatisch vorgeschlagen|" +
		"Home Assistant: A1/P1-Kamera auch als Bild-Entität (ha-bambulab-Option) nutzbar"},
	{"1.3", "22.10.2026", "Fix: Bambu-Konto mit nur einem Drucker meldete „keine Drucker gefunden“|" +
		"Fix: Absturz „Duplicate function name“ beim Start|" +
		"Schriftgröße einstellbar (Standard bis +3 px), Vorgabe +1 px|" +
		"Genauere Meldung, falls Bambu keine Drucker liefert"},
	{"1.2", "22.10.2026", "Mehrere Drucker: Profile anlegen, Wechsel per Klick auf den Namen oben links|" +
		"Pro Drucker einstellbar: Status-Quelle, Bildquelle, Kameratyp, Home-Assistant-Kamera|" +
		"Bambu-Anmeldung übernimmt alle Drucker des Kontos|" +
		"Kamerabild über Home Assistant – auch unterwegs, mit Adresse fürs Heimnetz und für unterwegs|" +
		"H2D, X1, P2S: Videostream über ffmpeg, automatische Erkennung des Kameratyps|" +
		"H2D: beide Düsen im Overlay, AMS-Nummer bei mehreren AMS, Hinweis bei ausgeschaltetem Liveview"},
	{"1.1", "22.10.2026", "Anmeldung mit dem Bambu-Konto (E-Mail-Code, Authenticator-App oder Token aus dem Browser)|" +
		"Seriennummer und Access Code kommen aus dem Konto, die Drucker-IP aus dem Status – nichts mehr abtippen|" +
		"Druckstatus und Meldungen auch unterwegs über die Bambu-Cloud, im Heimnetz weiter direkt vom Drucker|" +
		"Automatische Umschaltung Heimnetz/Cloud, Plakette „Cloud“ im Bild|" +
		"Einstellungen mit zwei Reitern, Hinweis bei abgelaufener Anmeldung"},
	{"1.0", "22.10.2026", "Live-Bild des Bambu A1 im kleinen, frei skalierbaren Fenster ohne Windows-Rahmen|" +
		"Druckstatus als Overlay (kompakt / ausführlich): Fortschritt, Restzeit, Fertig-Uhrzeit, Schicht, Temperaturen, Tempo, Filament mit Farbe|" +
		"Meldung im Infobereich bei Druckende, Pause und Abbruch, optional mit Ton|" +
		"Snapshot per Klick oder Taste S|" +
		"Immer im Vordergrund, Deckkraft, Minimieren in den Infobereich, Größen-Vorgaben|" +
		"Position und Größe werden gemerkt, Seitenverhältnis bleibt beim Ziehen|" +
		"Einstellungen mit Verbindungstest, Access Code verschlüsselt gespeichert|" +
		"Comic-TV-Icon, Hilfe und Changelog"},
}

func changelogShow() {
	if dLog.alive() {
		pSetForegroundWindow.Call(dLog.hwnd)
		return
	}
	const W = 440.0
	d := newDialog("Changelog", W)
	dLog = d
	curPage = -1
	y := d.head("Changelog")
	d.scrollFrom = 51
	for _, v := range changelog {
		y = d.section("Version "+v.ver+"  ·  "+v.date, y)
		for _, l := range strings.Split(v.lines, "|") {
			d.label("•", 24, y, 12, 18, cGreen, 12, 1, 0)
			wd := d.wrapLbl(l, 40, y+1, W-60, cTxt, 12, 0, 0)
			y += wd.h + 3
		}
	}
	y += 16
	d.pill("Schließen", (W-110)/2, y, 110, 28, cGreen, "ok")
	y += 46
	d.onClick = func(id string) {
		if id == "ok" {
			changelogClose()
		}
	}
	d.onOK = changelogClose
	d.onClose = changelogClose
	owner := hMain
	if dHelp.alive() {
		owner = dHelp.hwnd
	}
	d.show(y, owner)
}

func changelogClose() {
	dLog.close()
	dLog = nil
}
