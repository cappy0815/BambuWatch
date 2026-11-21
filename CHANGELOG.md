# Changelog

## 2.0 (21.11.2026)
- Komplett neu gebaut (Go statt AutoIt): eine einzige EXE mit eigenem Icon, ohne PowerShell-Helfer
- Drucker, Zugangsdaten und Einstellungen aus 1.x werden übernommen (gleiche BambuWatch.ini)
- Scharfe Darstellung bei Bildschirm-Skalierung (125 %, 150 % …)
- Schriftgröße gilt sofort überall, dunkle Tooltips, Changelog zum Rollen
- Hilfe: Link zur Projektseite, Taste L in der Tastenübersicht

## 1.7 (22.10.2026)
- Pause, solange Bambu Studio läuft: alle Verbindungen getrennt, danach automatisch wieder verbunden (abschaltbar)

## 1.6.1 (22.10.2026)
- Fix: Videostream (H2D, X1, P2S) mit aktuellem ffmpeg – Zertifikat des Druckers wird akzeptiert

## 1.6 (22.10.2026)
- Umschalter Kamerabild / Druckobjekt (Knopf oben und Rechtsklick-Menü)
- Status-Helfer schonender: kompletter Status höchstens alle 5 Minuten angefordert, bei Verbindungsfehlern wachsende Wartezeit bis 1 Minute

## 1.5 (22.10.2026)
- Druckstatus über Home Assistant (ha-bambulab) – Reihenfolge Heimnetz, Home Assistant, Bambu-Cloud; neue Status-Quelle „Nur Home Assistant“
- HA-Kamera, Titelbild und Licht werden über die Seriennummer automatisch gefunden
- Unterwegs: Heimnetz-Versuche nur noch einmal pro Minute, klarere Anzeige
- Fortschrittsbalken im Taskleisten-Symbol
- Meldungsfenster mit Bild des Druckobjekts
- Druckerlicht an/aus (Glühbirne oben, Taste L)
- Ausführliches Overlay: alle AMS-Slots mit Farbe und Material

## 1.4 (22.10.2026)
- Kamerabild und Druckstatus an/aus jetzt pro Drucker (Einstellungen › Drucker)
- Kein Kamerabild? Dann erscheint das Bild des Druckobjekts – aus Home Assistant (Titelbild) oder vom letzten Cloud-Auftrag
- Home Assistant: Bild-Entitäten werden mit geladen, Titelbild wird automatisch vorgeschlagen
- Home Assistant: A1/P1-Kamera auch als Bild-Entität (ha-bambulab-Option) nutzbar

## 1.3 (22.10.2026)
- Fix: Bambu-Konto mit nur einem Drucker meldete „keine Drucker gefunden“
- Fix: Absturz „Duplicate function name“ beim Start
- Schriftgröße einstellbar (Standard bis +3 px), Vorgabe +1 px
- Genauere Meldung, falls Bambu keine Drucker liefert

## 1.2 (22.10.2026)
- Mehrere Drucker: Profile anlegen, Wechsel per Klick auf den Namen oben links
- Pro Drucker einstellbar: Status-Quelle, Bildquelle, Kameratyp, Home-Assistant-Kamera
- Bambu-Anmeldung übernimmt alle Drucker des Kontos
- Kamerabild über Home Assistant – auch unterwegs, mit Adresse fürs Heimnetz und für unterwegs
- H2D, X1, P2S: Videostream über ffmpeg, automatische Erkennung des Kameratyps
- H2D: beide Düsen im Overlay, AMS-Nummer bei mehreren AMS, Hinweis bei ausgeschaltetem Liveview

## 1.1 (22.10.2026)
- Anmeldung mit dem Bambu-Konto (E-Mail-Code, Authenticator-App oder Token aus dem Browser)
- Seriennummer und Access Code kommen aus dem Konto, die Drucker-IP aus dem Status – nichts mehr abtippen
- Druckstatus und Meldungen auch unterwegs über die Bambu-Cloud, im Heimnetz weiter direkt vom Drucker
- Automatische Umschaltung Heimnetz/Cloud, Plakette „Cloud“ im Bild
- Einstellungen mit zwei Reitern, Hinweis bei abgelaufener Anmeldung

## 1.0 (22.10.2026)
- Live-Bild des Bambu A1 im kleinen, frei skalierbaren Fenster ohne Windows-Rahmen
- Druckstatus als Overlay (kompakt / ausführlich): Fortschritt, Restzeit, Fertig-Uhrzeit, Schicht, Temperaturen, Tempo, Filament mit Farbe
- Meldung im Infobereich bei Druckende, Pause und Abbruch, optional mit Ton
- Snapshot per Klick oder Taste S
- Immer im Vordergrund, Deckkraft, Minimieren in den Infobereich, Größen-Vorgaben
- Position und Größe werden gemerkt, Seitenverhältnis bleibt beim Ziehen
- Einstellungen mit Verbindungstest, Access Code verschlüsselt gespeichert
- Comic-TV-Icon, Hilfe und Changelog
