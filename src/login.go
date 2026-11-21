//go:build windows

// Bei Bambu Lab anmelden (E-Mail + Passwort mit Code oder Authenticator, oder Token aus dem Browser).
// Ablauf wie ha-bambulab / BambuHelper; die Anfragen laufen im Worker (account.go).
package main

import (
	"strings"
	"time"
)

var (
	dAcc                                *dialog
	aEmail, aPass, aCode, aToken        *widget
	aCodeLbl, aStatus, aResend, aRegion *widget
	aCodeRow                            []*widget
	accRegion                           int
	accStage, accTfaKey, accEmail       string
	accTok, accUser                     string
	accByToken                          bool
	accDevs                             []accDev
)

func loginShow() {
	if dAcc.alive() {
		pSetForegroundWindow.Call(dAcc.hwnd)
		return
	}
	const W = 440.0
	d := newDialog("Bambu-Konto", W)
	dAcc = d
	curPage = -1
	y := d.head("Bei Bambu Lab anmelden")
	y = d.hint("BambuWatch übernimmt damit alle Drucker des Kontos mit Seriennummer und Access Code (die IP kommt aus dem Status) und zeigt den Druckstatus auch unterwegs. Das Passwort wird nicht gespeichert – nur das Anmelde-Token, verschlüsselt mit Windows.", 20, y+8, W-40)
	y = d.section("Mit E-Mail und Passwort", y)
	aEmail = d.field("E-Mail", cfg.email, 20, y, 230, false)
	aPass = d.field("Passwort", "", 260, y, W-280, true)
	y += 52
	d.label("Region", 20, y, 50, 28, cTxt2, 11.3, 0, 0)
	accRegion = 0
	if dSet.alive() {
		accRegion = setRegion
	} else if cfg.region == "China" {
		accRegion = 1
	}
	aRegion = d.cycle(regionNames[accRegion], 70, y+3, 130, "region")
	d.pill("Anmelden", W-140, y, 120, 28, cGreen, "login")
	y += 40
	// Zeile fuer den Code (E-Mail oder Authenticator) - erscheint bei Bedarf
	aCodeLbl = d.label("Code aus der E-Mail", 20, y, W-40, 16, cTxt2, 10.7, 0, 0)
	aCode = d.input("", 20, y+18, 130, false)
	btn := d.pill("Bestätigen", 160, y+17, 110, 26, cGreen, "code")
	aResend = d.link("Neuen Code senden", 284, y+18, 140, 24, 0, "resend")
	aCodeRow = []*widget{aCodeLbl, aCode, btn, aResend}
	for _, wd := range aCodeRow {
		wd.hide = true
	}
	y += 50
	aStatus = d.add(&widget{x: 20, y: y, w: W - 40, h: 34, col: cTxt2, draw: func(c *canvas, wd *widget, hot bool) {
		c.wrap(wd.text, 11.3, 0, wd.col, wd.x, wd.y, wd.w, wd.h+10, 0)
	}})
	y += 38
	y = d.section("Oder: Token aus dem Browser", y)
	y = d.hint("Hilft, wenn die Anmeldung oben blockiert wird: auf bambulab.com anmelden, F12 drücken → Anwendung (Application) → Cookies → bambulab.com → Wert von „token“ kopieren und hier einfügen.", 20, y, W-40)
	y += 2
	aToken = d.input("", 20, y, W-170, true)
	d.pill("Token prüfen", W-140, y-1, 120, 26, cBtnDk, "token")
	y += 44
	d.pill("Schließen", (W-110)/2, y, 110, 28, cBtnDk, "close2")
	y += 46
	d.onClick = accClick
	d.onOK = func() {
		switch {
		case accStage != "" && strings.TrimSpace(aCode.val()) != "":
			accClick("code")
		case strings.TrimSpace(aToken.val()) != "" && aPass.val() == "":
			accClick("token")
		default:
			accClick("login")
		}
		if dAcc.alive() {
			dAcc.redraw()
		}
	}
	d.onClose = loginClose
	accStage = ""
	owner := hMain
	if dSet.alive() {
		owner = dSet.hwnd
	}
	d.show(y, owner)
	if cfg.email != "" {
		pSetFocus.Call(aPass.edit)
	} else {
		pSetFocus.Call(aEmail.edit)
	}
}

func loginClose() {
	wAcc.stop()
	wAcc = nil
	dAcc.close()
	dAcc = nil
	accStage = ""
}

func accClick(id string) {
	switch id {
	case "region":
		accRegion = (accRegion + 1) % 2
		aRegion.text = regionNames[accRegion]
	case "login":
		email, pass := strings.TrimSpace(aEmail.val()), aPass.val()
		if email == "" || pass == "" {
			accMsg("E-Mail und Passwort eintragen", cRed)
			return
		}
		accEmail, accByToken = email, false
		accRun(accCfg{mode: "login", email: email, pass: pass}, "Melde an …")
	case "code":
		code := strip(aCode.val())
		if code == "" {
			accMsg("Bitte den Code eintragen", cRed)
			return
		}
		if accStage == "tfa" {
			accRun(accCfg{mode: "tfa", email: accEmail, tfaKey: accTfaKey, code: code}, "Prüfe Code …")
		} else {
			accRun(accCfg{mode: "code", email: accEmail, code: code}, "Prüfe Code …")
		}
	case "resend":
		if accEmail != "" {
			accRun(accCfg{mode: "resend", email: accEmail}, "Fordere neuen Code an …")
		}
	case "token":
		tok := strings.TrimSpace(aToken.val())
		if tok == "" {
			accMsg("Bitte zuerst das Token einfügen", cRed)
			return
		}
		accEmail, accByToken = "", true
		accRun(accCfg{mode: "token", token: tok}, "Prüfe Token …")
	case "close2":
		loginClose()
	}
}

func accRun(c accCfg, busy string) {
	if wAcc != nil {
		return
	}
	accTok, accUser, accDevs = "", "", nil
	c.region = regionKeys[accRegion]
	wAcc = startWorker("account", func(ctx ctxT, o out) { runAccount(ctx, o, c) })
	accAt = time.Now()
	accMsg(busy, cAmber)
}

func accMsg(s string, col uint32) {
	if !dAcc.alive() {
		return
	}
	aStatus.text, aStatus.col = s, col
	dAcc.redraw()
}

func accEvent(ev event) {
	s := ev.line
	switch {
	case ev.end:
		wAcc = nil
	case s == "LOGIN|code":
		accCodeRow("code")
	case strings.HasPrefix(s, "LOGIN|tfa|"):
		accTfaKey = s[10:]
		accCodeRow("tfa")
	case strings.HasPrefix(s, "LOGIN|token|"):
		accTok = s[12:]
	case strings.HasPrefix(s, "USER|"):
		accUser = s[5:]
	case strings.HasPrefix(s, "DEV|"):
		p := strings.Split(s, "|")
		if len(p) >= 6 {
			accDevs = append(accDevs, accDev{serial: p[1], name: p[2], code: p[3], online: p[4], model: p[5]})
		}
	case s == "DONE":
		wAcc.stop()
		wAcc = nil
		accDone()
	case strings.HasPrefix(s, "ERR|AUTH|"):
		accMsg(s[9:], cRed)
	case strings.HasPrefix(s, "ERR|"):
		accMsg(s[4:], cRed)
	}
}

// Code-Zeile zeigen: "code" = per E-Mail, "tfa" = Authenticator-App
func accCodeRow(stage string) {
	if !dAcc.alive() {
		return
	}
	accStage = stage
	for _, wd := range aCodeRow {
		wd.hide = false
	}
	aCode.set("")
	if stage == "tfa" {
		aCodeLbl.text = "Code aus der Authenticator-App"
		aResend.hide = true
		accMsg("Zwei-Faktor-Anmeldung: Code aus der Authenticator-App eintragen.", cTxt)
	} else {
		aCodeLbl.text = "Code aus der E-Mail an " + accEmail
		accMsg("Bambu hat dir einen Bestätigungscode per E-Mail geschickt.", cTxt)
	}
	dAcc.applyPage()
	pSetFocus.Call(aCode.edit)
}

// Anmeldung fertig: alle Drucker des Kontos als Profile uebernehmen
func accDone() {
	if accTok == "" || accUser == "" || len(accDevs) == 0 {
		return // Fehlermeldung kam schon vom Worker
	}
	cfg.token, cfg.user = accTok, accUser
	cfg.email = accEmail
	if accByToken {
		cfg.email = ""
	}
	cfg.region = regionKeys[accRegion]
	eng.clAuthBad = false
	added := cfg.merge(accDevs)
	cfg.savePrinters(ini)
	cfg.saveAccount(ini)
	if dSet.alive() {
		// offene Einstellungen: dieselben Drucker auch in die Arbeitskopie
		prnStore()
		setW.merge(accDevs)
		if setSel >= len(setW.prn) {
			setSel = setW.active
		}
		prnLoad()
		setRegion = accRegion
		sRegion.text = regionNames[setRegion]
		accountLabel()
	}
	n := len(accDevs)
	loginClose()
	frameDrop()
	eng.helpersStart()
	titleUpdate()
	msg := "Angemeldet · " + itoa(n) + " Drucker"
	if added > 0 {
		msg += ", " + itoa(added) + " neu"
	}
	toastShow(msg)
}
