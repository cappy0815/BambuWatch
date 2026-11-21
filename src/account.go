// Bambu-Konto, Verbindungstest und Home-Assistant-Liste (Logik aus BambuWatch_helper.ps1:
// Run-Cloud, Out-Account, Resolve-User, Run-Probe, Run-HaList).
//
//	Login:   POST /v1/user-service/user/login {account, password, apiError}
//	         -> accessToken | loginType "verifyCode" (Code per E-Mail) | loginType "tfa" + tfaKey
//	Code:    POST /v1/user-service/user/login {account, code}; Fehler code 1 = abgelaufen, 2 = falsch
//	Neu:     POST /v1/user-service/user/sendemail/code {email, type: "codeLogin"}
//	2FA:     GET bambulab.com/api/csrf (Cookie bbl_csrf_token), POST /api/sign-in/tfa {tfaKey, tfaCode}
//	         mit Kopf x-bbl-csrf-token -> Cookie "token"
//	Drucker: GET /v1/iot-service/api/user/bind -> devices[] (dev_id, name, dev_access_code, online, …)
//	UID:     aus dem Token (JWT: username "u_…" oder uid), sonst /v1/design-user-service/my/preference
//
// Quellen: ha-bambulab (bambu_cloud.py) und BambuHelper, nicht offiziell von Bambu Lab.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

type accCfg struct {
	mode   string // login, code, resend, tfa, token
	region string
	email  string
	pass   string
	code   string // E-Mail- oder Authenticator-Code
	tfaKey string
	token  string
}

var testWeb string // nur Tests: statt https://bambulab.com

func webBase() string {
	if testWeb != "" {
		return testWeb
	}
	return "https://bambulab.com"
}

func apiBaseT(region string) string {
	if testAPI != "" {
		return testAPI
	}
	return apiBase(region)
}

func tokenFrom(j any) string {
	if t := toStr(jsonProp(j, "accessToken", "token")); t != "" {
		return t
	}
	return toStr(jsonProp(jsonProp(j, "data"), "accessToken", "token"))
}

// MQTT-Benutzername "u_<uid>": erst aus dem Token (JWT), dann ueber die API
func resolveUser(ctx context.Context, base, tok string) (string, error) {
	if parts := strings.Split(tok, "."); len(parts) == 3 {
		if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "=")); err == nil {
			var pl map[string]any
			if json.Unmarshal(b, &pl) == nil {
				if un := toStr(pl["username"]); strings.HasPrefix(un, "u_") {
					return un, nil
				}
				if uid := toStr(jsonProp(pl, "uid", "sub", "user_id")); uid != "" {
					return "u_" + uid, nil
				}
			}
		}
	}
	for _, path := range []string{"/v1/design-user-service/my/preference", "/v1/user-service/my/profile"} {
		r, j, err := bambuGet(ctx, base+path, tok)
		if err == errCloudflare {
			return "", err
		}
		if err != nil || r.status != 200 {
			continue
		}
		uid := toStr(jsonProp(j, "uidStr", "uid", "userId", "user_id"))
		if uid == "" {
			uid = toStr(jsonProp(jsonProp(j, "data"), "uidStr", "uid", "userId", "user_id"))
		}
		if uid != "" {
			return "u_" + uid, nil
		}
	}
	return "", nil
}

// nach erfolgreichem Login: Benutzer + Druckerliste ausgeben
func outAccount(ctx context.Context, o out, base, tok string) {
	r, j, err := bambuGet(ctx, base+"/v1/iot-service/api/user/bind", tok)
	if err != nil {
		accErr(o, err)
		return
	}
	if r.status == 401 || r.status == 403 {
		o.line("ERR|AUTH|Token ungültig oder abgelaufen")
		return
	}
	if r.status != 200 {
		o.err(fmt.Sprintf("Druckerliste nicht abrufbar (HTTP %d)", r.status))
		return
	}
	user, err := resolveUser(ctx, base, tok)
	if err != nil {
		accErr(o, err)
		return
	}
	if user == "" {
		o.err("Benutzerkennung (uid) nicht ermittelbar")
		return
	}
	o.line("LOGIN|token|" + tok)
	o.line("USER|" + user)
	devs := asList(jsonProp(j, "devices"))
	if devs == nil {
		devs = asList(jsonProp(j, "data"))
	}
	n := 0
	for _, d := range devs {
		sn := strings.ToUpper(toStr(jsonProp(d, "dev_id")))
		if sn == "" {
			continue
		}
		n++
		o.line("DEV|" + clean(sn) + "|" + clean(jsonProp(d, "name")) + "|" + clean(jsonProp(d, "dev_access_code")) + "|" +
			clean(jsonProp(d, "online")) + "|" + clean(jsonProp(d, "dev_product_name", "dev_model_name")))
	}
	if n == 0 {
		keys := []string{}
		for k := range asMap(j) {
			keys = append(keys, k)
		}
		o.err(fmt.Sprintf("Keine Drucker in diesem Konto gefunden (Antwort: Felder [%s], Einträge %d)", strings.Join(keys, ","), len(devs)))
	}
	o.line("DONE")
}

func accErr(o out, err error) {
	if err == errCloudflare {
		o.err(`Vom Bambu-Schutz (Cloudflare) geblockt - bitte den Weg "Token aus dem Browser" nutzen`)
		return
	}
	o.err(err.Error())
}

func sendCode(ctx context.Context, base, email string) {
	bambuDo(ctx, nil, "POST", base+"/v1/user-service/user/sendemail/code", map[string]string{"email": email, "type": "codeLogin"}, "", nil)
}

func runAccount(ctx context.Context, o out, c accCfg) {
	base := apiBaseT(c.region)
	switch c.mode {
	case "login":
		if c.email == "" || c.pass == "" {
			o.err("E-Mail und Passwort eintragen")
			return
		}
		r, j, err := bambuDo(ctx, nil, "POST", base+"/v1/user-service/user/login",
			map[string]string{"account": c.email, "password": c.pass, "apiError": ""}, "", nil)
		if err != nil {
			accErr(o, err)
			return
		}
		if t := tokenFrom(j); t != "" {
			outAccount(ctx, o, base, t)
			return
		}
		switch toStr(jsonProp(j, "loginType")) {
		case "verifyCode": // Bambu verschickt den Code selbst
			o.line("LOGIN|code")
			return
		case "tfa":
			o.line("LOGIN|tfa|" + clean(jsonProp(j, "tfaKey")))
			return
		}
		if r.status == 400 || r.status == 401 {
			o.err("Anmeldung abgelehnt - E-Mail oder Passwort falsch?")
			return
		}
		o.err(fmt.Sprintf("Unbekannte Antwort vom Bambu-Server (HTTP %d)", r.status))
	case "resend":
		sendCode(ctx, base, c.email)
		o.line("LOGIN|code")
	case "code":
		r, j, err := bambuDo(ctx, nil, "POST", base+"/v1/user-service/user/login",
			map[string]string{"account": c.email, "code": c.code}, "", nil)
		if err != nil {
			accErr(o, err)
			return
		}
		if t := tokenFrom(j); t != "" {
			outAccount(ctx, o, base, t)
			return
		}
		switch clean(jsonProp(j, "code")) {
		case "1": // abgelaufen -> neuen anfordern (wie ha-bambulab)
			sendCode(ctx, base, c.email)
			o.err("Code abgelaufen - ein neuer Code wurde per E-Mail geschickt")
		case "2":
			o.err("Code falsch - bitte nochmal eingeben")
		default:
			o.err(fmt.Sprintf("Code nicht akzeptiert (HTTP %d)", r.status))
		}
	case "tfa":
		jar, _ := cookiejar.New(nil)
		web := webBase()
		wu, _ := url.Parse(web)
		bambuDo(ctx, jar, "GET", web+"/api/csrf", nil, "", nil)
		csrf := ""
		for _, ck := range jar.Cookies(wu) {
			if ck.Name == "bbl_csrf_token" {
				csrf = ck.Value
			}
		}
		if csrf == "" { // Rueckfall wie ha-bambulab: Login per E-Mail-Code
			sendCode(ctx, base, c.email)
			o.line("LOGIN|code")
			return
		}
		r, j, err := bambuDo(ctx, jar, "POST", web+"/api/sign-in/tfa", map[string]string{"tfaKey": c.tfaKey, "tfaCode": c.code}, "",
			map[string]string{"x-bbl-csrf-token": csrf})
		if err != nil {
			accErr(o, err)
			return
		}
		if r.status == 403 && strings.Contains(string(r.body), "missing_cookie") {
			sendCode(ctx, base, c.email)
			o.line("LOGIN|code")
			return
		}
		tok := ""
		for _, ck := range jar.Cookies(wu) {
			if ck.Name == "token" {
				tok = ck.Value
			}
		}
		if tok == "" {
			tok = tokenFrom(j)
		}
		if tok != "" {
			outAccount(ctx, o, base, tok)
			return
		}
		o.err("Code aus der Authenticator-App nicht akzeptiert")
	case "token":
		t := strings.Trim(strings.TrimSpace(c.token), `"`)
		t = strings.TrimPrefix(t, "Bearer ")
		if t == "" {
			o.err("Token ist leer")
			return
		}
		outAccount(ctx, o, base, t)
	}
}

// ---------------------------------------------------------------- Verbindungstest (Heimnetz)
type probeCfg struct {
	ip, code, serial, camType, ffmpeg string
}

// Ausgabe "PROBE|art:text|art:text" (art = ok, warn, err) fuer Kamera und Status
func runProbe(ctx context.Context, o out, c probeCfg) {
	if c.code == "" {
		o.err("Access Code fehlt (Einstellungen)")
		return
	}
	cam, mq := "", ""
	rtsp := c.camType == "rtsp" || (c.camType == "auto" && !testPort(ctx, c.ip, 6000, 2500*time.Millisecond) && testPort(ctx, c.ip, 322, 2500*time.Millisecond))
	if rtsp {
		if c.ffmpeg == "" {
			cam = "warn:Videostream-Drucker (Port 322) erkannt - für das Bild ffmpeg.exe angeben"
		} else {
			cam = "ok:Videostream-Drucker (Port 322) erkannt - Bild über ffmpeg"
		}
	} else {
		conn, err := openTLS(ctx, c.ip, 6000, 6*time.Second, false)
		if err != nil {
			cam = "err:" + clean(err.Error())
		} else {
			conn.Write(camAuth(c.code))
			conn.SetReadDeadline(time.Now().Add(8 * time.Second))
			jpg, err := readCamFrame(conn)
			switch {
			case err != nil:
				cam = "warn:Verbunden, aber kein Bild in 8 s (Kamera aus? Access Code?)"
			case jpg == nil:
				cam = "warn:Daten empfangen, aber kein JPEG"
			default:
				cam = fmt.Sprintf("ok:Bild empfangen (%d KB)", (len(jpg)+512)/1024)
			}
			conn.Close()
		}
	}
	mq = probeMqtt(ctx, c)
	o.line("PROBE|" + cam + "|" + mq)
}

func probeMqtt(ctx context.Context, c probeCfg) string {
	if c.serial == "" {
		return "err:Seriennummer fehlt"
	}
	conn, err := openTLS(ctx, c.ip, 8883, 6*time.Second, false)
	if err != nil {
		return "err:" + clean(err.Error())
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	conn.Write(mqttConnectPkt("bblp", c.code))
	p, err := mqttRead(conn)
	if err != nil || p.typ != 0x20 || len(p.body) < 2 {
		return "err:Unerwartete Antwort"
	}
	if p.body[1] != 0 {
		return fmt.Sprintf("err:Login abgelehnt (Code %d) - Access Code pruefen", p.body[1])
	}
	conn.Write(mqttSubscribePkt("device/" + c.serial + "/report"))
	conn.Write(mqttPublishPkt("device/"+c.serial+"/request", `{"pushing":{"sequence_id":"0","command":"pushall"}}`))
	end := time.Now().Add(8 * time.Second)
	conn.SetDeadline(end)
	for time.Now().Before(end) {
		p, err := mqttRead(conn)
		if err != nil {
			break
		}
		if p.typ == 0x90 && len(p.body) >= 3 && p.body[2] == 0x80 {
			return "err:Abo abgelehnt - Seriennummer pruefen"
		}
		if p.typ == 0x30 {
			return "ok:Status empfangen"
		}
	}
	return "warn:Angemeldet, aber kein Status in 8 s (Seriennummer? Bei H2/X1: LAN Only + Developer Mode)"
}

// ---------------------------------------------------------------- Home Assistant: Kameras und Bilder
// Ausgabe "CAM|entity|Name", "IMG|entity|Name", "HAOK|adresse|anzahl", "DONE"
func runHaList(ctx context.Context, o out, h haCfg) {
	if len(h.urls) == 0 || h.token == "" {
		o.err("Adresse und Token von Home Assistant eintragen")
		return
	}
	last := ""
	for _, u := range h.urls {
		r, err := haDo(ctx, h, u, "GET", "/api/states", nil)
		if err != nil {
			last = err.Error()
			continue
		}
		if r.status == 401 {
			o.line("ERR|AUTH|Home-Assistant-Token ungültig")
			return
		}
		if r.status != 200 {
			last = fmt.Sprintf("%s: HTTP %d", u, r.status)
			continue
		}
		var list []map[string]any
		if json.Unmarshal(r.body, &list) != nil {
			last = u + ": Antwort nicht lesbar"
			continue
		}
		n := 0
		for _, e := range list {
			id := toStr(e["entity_id"])
			name := clean(asMap(e["attributes"])["friendly_name"])
			switch {
			case strings.HasPrefix(id, "image."):
				o.line("IMG|" + clean(id) + "|" + name)
			case strings.HasPrefix(id, "camera."):
				n++
				o.line("CAM|" + clean(id) + "|" + name)
			}
		}
		o.line(fmt.Sprintf("HAOK|%s|%d", u, n))
		o.line("DONE")
		return
	}
	o.err(last)
}

var _ = http.MethodGet
