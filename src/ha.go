// Home Assistant und Bambu-Cloud (Logik aus BambuWatch_helper.ps1)
//
//	Bild:    GET /api/camera_proxy/<camera.*> bzw. /api/image_proxy/<image.*>
//	Licht:   POST /api/services/light/turn_on|turn_off {"entity_id": …}
//	Status:  WebSocket /api/websocket (auth, config/entity_registry/list, get_states,
//	         subscribe_events state_changed); Entitaeten ueber unique_id <Serie>_<Schluessel>
//	         wie ha-bambulab (sensor/definitions.py, light.py, image.py, camera.py)
//	Cover:   HA-Bild-Entitaet ("Titelbild"), sonst Bambu-Cloud: GET /v1/user-service/my/tasks
//	         ?deviceId=<Serie>&limit=1, Feld hits[].cover (Bild-URL ohne Anmeldung ladbar)
//
// Alles mit langlebigem Zugangstoken (Authorization: Bearer).
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type haCfg struct {
	urls     []string // Heimnetz, unterwegs
	token    string
	insecure bool
}

func haURLs(in, outside string) []string {
	var r []string
	for _, u := range []string{in, outside} {
		u = strings.TrimRight(strings.TrimSpace(u), "/")
		if u != "" {
			r = append(r, u)
		}
	}
	return r
}

type httpRes struct {
	status int
	body   []byte
}

func httpClient(insecure bool, timeout time.Duration) *http.Client {
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
		ResponseHeaderTimeout: timeout, ForceAttemptHTTP2: false}
	return &http.Client{Timeout: timeout, Transport: tr}
}

func isCertErr(err error) bool {
	var ce *tls.CertificateVerificationError
	var ua x509.UnknownAuthorityError
	var he x509.HostnameError
	var ci x509.CertificateInvalidError
	return errors.As(err, &ce) || errors.As(err, &ua) || errors.As(err, &he) || errors.As(err, &ci)
}

// GET/POST auf die HA-REST-API; Fehlertexte wie Invoke-Ha im Helfer
func haDo(ctx context.Context, h haCfg, base, method, path string, body []byte) (httpRes, error) {
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return httpRes{}, fmt.Errorf("%s nicht erreichbar (Adresse ungültig)", base)
	}
	if body == nil {
		req.Body = nil
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "BambuWatch")
	req.Header.Set("Authorization", "Bearer "+h.token)
	resp, err := httpClient(h.insecure, 8*time.Second).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return httpRes{}, ctx.Err()
		}
		if isCertErr(err) {
			return httpRes{}, fmt.Errorf(`%s: Zertifikat wird nicht akzeptiert (selbstsigniert? Option "Zertifikat nicht prüfen")`, base)
		}
		msg := err.Error()
		var ue *url.Error
		if errors.As(err, &ue) {
			msg = ue.Err.Error()
		}
		return httpRes{}, fmt.Errorf("%s nicht erreichbar (%s)", base, msg)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	return httpRes{resp.StatusCode, b}, nil
}

// ---------------------------------------------------------------- Kamera ueber HA
func runHaCamera(ctx context.Context, o out, h haCfg, entity string, interval time.Duration) {
	if len(h.urls) == 0 || h.token == "" || entity == "" {
		o.line("ERR|STOP|Home Assistant nicht vollständig eingerichtet")
		return
	}
	proxy := "/api/camera_proxy/"
	if strings.HasPrefix(entity, "image.") {
		proxy = "/api/image_proxy/"
	}
	cur, first, last := 0, true, ""
	o.line("STATE|connecting")
	for ctx.Err() == nil {
		ok := false
		for k := 0; k < len(h.urls); k++ {
			idx := (cur + k) % len(h.urls)
			r, err := haDo(ctx, h, h.urls[idx], "GET", proxy+entity, nil)
			if err != nil {
				last = err.Error()
				continue
			}
			if r.status == 401 {
				o.line("ERR|AUTH|Home-Assistant-Token ungültig")
				return
			}
			if r.status == 404 {
				o.line("ERR|STOP|Kamera " + entity + " gibt es in Home Assistant nicht")
				return
			}
			if r.status != 200 || len(r.body) < 100 {
				last = fmt.Sprintf("Home Assistant liefert kein Bild (HTTP %d - Drucker aus?)", r.status)
				continue
			}
			cur, ok = idx, true
			if first {
				o.line("STATE|connected")
				o.line("VIA|" + h.urls[idx])
				first = false
			}
			o.image("FRAME", r.body)
			break
		}
		if !ok {
			if ctx.Err() != nil {
				return
			}
			o.err(last)
			first = true
			sleepCtx(ctx, 5*time.Second)
		} else {
			sleepCtx(ctx, interval)
		}
	}
}

// ---------------------------------------------------------------- Licht ueber HA
func runHaLight(ctx context.Context, o out, h haCfg, entity, state string) {
	if len(h.urls) == 0 || h.token == "" || entity == "" || state == "" {
		o.err("Licht: Home Assistant nicht vollständig eingerichtet")
		return
	}
	svc := "turn_off"
	if state == "on" {
		svc = "turn_on"
	}
	body, _ := json.Marshal(map[string]string{"entity_id": entity})
	last := ""
	for _, u := range h.urls {
		r, err := haDo(ctx, h, u, "POST", "/api/services/light/"+svc, body)
		if err != nil {
			last = err.Error()
			continue
		}
		if r.status == 401 {
			o.line("ERR|AUTH|Home-Assistant-Token ungültig")
			return
		}
		if r.status != 200 {
			last = fmt.Sprintf("HTTP %d", r.status)
			continue
		}
		o.line("LIGHT|" + state)
		return
	}
	o.err("Licht: " + last)
}

// ---------------------------------------------------------------- Bambu-Cloud (HTTPS)
func apiBase(region string) string {
	if region == "China" {
		return "https://api.bambulab.cn"
	}
	return "https://api.bambulab.com"
}

var errCloudflare = errors.New("CLOUDFLARE")

// Kopfzeilen wie ha-bambulab / BambuHelper (OrcaSlicer)
func bambuGet(ctx context.Context, u, bearer string) (httpRes, any, error) {
	return bambuDo(ctx, nil, "GET", u, nil, bearer, nil)
}

// HTTPS-Aufruf zur Bambu-Cloud; body wird als JSON gesendet, jar haelt Cookies (2FA)
func bambuDo(ctx context.Context, jar http.CookieJar, method, u string, body any, bearer string, extra map[string]string) (httpRes, any, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return httpRes{}, nil, err
	}
	req.Header.Set("User-Agent", "bambu_network_agent/01.09.05.01")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-BBL-Client-Name", "OrcaSlicer")
	req.Header.Set("X-BBL-Client-Type", "slicer")
	req.Header.Set("X-BBL-Client-Version", "01.09.05.51")
	req.Header.Set("X-BBL-Language", "de-DE")
	req.Header.Set("X-BBL-OS-Type", "windows")
	req.Header.Set("X-BBL-Agent-Version", "01.09.05.01")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	cl := httpClient(testInsecure, 15*time.Second)
	cl.Jar = jar
	resp, err := cl.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return httpRes{}, nil, ctx.Err()
		}
		return httpRes{}, nil, fmt.Errorf("Bambu-Server nicht erreichbar (%v)", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if (resp.StatusCode == 403 || resp.StatusCode == 429) && strings.Contains(strings.ToLower(string(b)), "cloudflare") {
		return httpRes{}, nil, errCloudflare
	}
	var j any
	json.Unmarshal(b, &j)
	return httpRes{resp.StatusCode, b}, j, nil
}

// nur fuer Tests gegen einen simulierten Server
var (
	testInsecure bool
	testAPI      string
)

// Wert eines JSON-Feldes; leere Texte zaehlen als nicht vorhanden (Get-JsonProp)
func jsonProp(o any, names ...string) any {
	m := asMap(o)
	for _, n := range names {
		v, ok := m[n]
		if !ok || v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		return v
	}
	return nil
}

// ---------------------------------------------------------------- Druckobjekt (Cover)
type coverCfg struct {
	ha     haCfg
	entity string // HA-Bild-Entitaet (Titelbild)
	token  string // Bambu-Token
	serial string
	region string
}

// Ausgabe "COVER|quelle|titel" mit Bild, "NOCOVER|grund" oder "ERR|text"
func runCover(ctx context.Context, o out, c coverCfg) {
	var msgs []string
	hadErr, tried := false, false
	// 1. Home Assistant: Bild-Entitaet (ha-bambulab "Titelbild" / cover_image)
	if c.entity != "" && c.ha.token != "" && len(c.ha.urls) > 0 {
		tried = true
		haMsg, haErr := "", false
		for _, u := range c.ha.urls {
			// erste erreichbare Adresse zaehlt; nur deren Antwort entscheidet ueber Fehler / kein Bild
			r, err := haDo(ctx, c.ha, u, "GET", "/api/image_proxy/"+c.entity, nil)
			if err != nil {
				haMsg, haErr = "HA: "+err.Error(), true
				continue
			}
			haErr = false
			if r.status == 401 {
				haMsg, haErr = "HA: Token ungültig", true
				break
			}
			if r.status == 404 {
				haMsg = "HA: " + c.entity + " gibt es nicht"
				break
			}
			if r.status == 200 && len(r.body) >= 100 {
				o.image("COVER|HA|", r.body)
				return
			}
			haMsg = fmt.Sprintf("HA: kein Titelbild (HTTP %d)", r.status)
			break
		}
		if haErr {
			hadErr = true
		}
		msgs = append(msgs, haMsg)
	}
	// 2. Bambu-Cloud: letzter Auftrag dieses Druckers
	if c.token != "" && c.serial != "" {
		tried = true
		err := func() error {
			base := apiBase(c.region)
			if testAPI != "" {
				base = testAPI
			}
			r, j, err := bambuGet(ctx, base+"/v1/user-service/my/tasks?deviceId="+url.QueryEscape(c.serial)+"&limit=1", c.token)
			if err != nil {
				return err
			}
			if r.status == 401 || r.status == 403 {
				return errors.New("Anmeldung abgelaufen")
			}
			if r.status != 200 {
				return fmt.Errorf("HTTP %d", r.status)
			}
			var hit any
			for _, h := range asList(jsonProp(j, "hits")) {
				if h == nil {
					continue
				}
				if dev := toStr(jsonProp(h, "deviceId")); dev != "" && dev != c.serial {
					continue
				}
				hit = h
				break
			}
			cov := ""
			if hit != nil {
				cov = toStr(jsonProp(hit, "cover"))
			}
			if cov == "" {
				msgs = append(msgs, "Cloud: kein Auftrag mit Bild")
				return nil
			}
			req, _ := http.NewRequestWithContext(ctx, "GET", cov, nil)
			req.Header.Set("User-Agent", "BambuWatch")
			resp, err := httpClient(testInsecure, 15*time.Second).Do(req)
			if err != nil {
				return fmt.Errorf("Bild nicht ladbar (%v)", err)
			}
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
			resp.Body.Close()
			if resp.StatusCode != 200 || len(b) < 100 {
				return fmt.Errorf("Bild nicht ladbar (HTTP %d)", resp.StatusCode)
			}
			o.image("COVER|Cloud|"+clean(jsonProp(hit, "title", "designTitle")), b)
			return errDone
		}()
		if err == errDone {
			return
		}
		if err != nil {
			m := err.Error()
			if err == errCloudflare {
				m = "vom Bambu-Schutz geblockt"
			}
			msgs = append(msgs, "Cloud: "+m)
			hadErr = true
		}
	}
	if !tried {
		o.line("NOCOVER|weder Home-Assistant-Titelbild noch Bambu-Konto eingerichtet")
		return
	}
	txt := clean(strings.Join(msgs, " · "))
	if hadErr {
		o.line("ERR|" + txt)
	} else {
		o.line("NOCOVER|" + txt)
	}
}

var errDone = errors.New("fertig")

// ---------------------------------------------------------------- Status ueber HA (WebSocket)
// Schluessel aus ha-bambulab, die BambuWatch braucht (unique_id = <Serie>_<Schluessel>)
var haWant = map[string]bool{"print_status": true, "print_progress": true, "remaining_time": true, "current_layer": true,
	"total_layers": true, "nozzle_temp": true, "target_nozzle_temp": true, "bed_temp": true, "target_bed_temp": true,
	"subtask_name": true, "speed_profile": true, "ip_address": true, "left_nozzle_temp": true, "left_target_nozzle_temp": true,
	"right_nozzle_temp": true, "right_target_nozzle_temp": true, "chamber_light": true, "camera_light": true,
	"camera": true, "p1p_camera": true, "cover_image": true}

type haVal struct {
	s string
	a map[string]any
}

type haState struct {
	m       map[string]string // entity_id -> Schluessel bzw. "tray|<ams>|<slot>" / "ext|<n>"
	v       map[string]haVal  // Schluessel -> Zustand
	lastOut string
}

func (h *haState) num(k string, round bool) (float64, bool) {
	v, ok := h.v[k]
	if !ok || v.s == "" || v.s == "unknown" || v.s == "unavailable" {
		return 0, false
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(v.s), 64)
	if err != nil {
		return 0, false
	}
	if toStr(v.a["unit_of_measurement"]) == "°F" {
		d = (d - 32) * 5 / 9
	}
	return d, true
}

func (h *haState) numS(k string) string {
	d, ok := h.num(k, true)
	if !ok {
		return ""
	}
	return strconv.FormatFloat(math.Round(d), 'f', 0, 64)
}

func (h *haState) str(k string) string {
	v, ok := h.v[k]
	if !ok || v.s == "unknown" || v.s == "unavailable" {
		return ""
	}
	return v.s
}

// Restzeit in Minuten; HA kann die Einheit umstellen (ha-bambulab schlaegt Stunden vor)
func (h *haState) minutes(k string) string {
	d, ok := h.num(k, false)
	if !ok {
		return ""
	}
	d = math.Round(d*1000) / 1000 // wie Format "0.###"
	switch toStr(h.v[k].a["unit_of_measurement"]) {
	case "h":
		d *= 60
	case "s":
		d /= 60
	case "d":
		d *= 1440
	}
	return strconv.FormatFloat(math.Round(d), 'f', 0, 64)
}

func (h *haState) statusLine() string {
	spd := map[string]string{"silent": "1", "standard": "2", "sport": "3", "ludicrous": "4"}[h.str("speed_profile")]
	var keys []string
	for k := range h.v {
		if strings.HasPrefix(k, "tray|") || strings.HasPrefix(k, "ext|") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var amsOrder, list, ext []string
	c, t, sl, am := "", "", "", ""
	idx := func(s string) int {
		for i, x := range amsOrder {
			if x == s {
				return i
			}
		}
		amsOrder = append(amsOrder, s)
		return len(amsOrder) - 1
	}
	for _, k := range keys {
		a := h.v[k].a
		typ, col, act := "", "", false
		if a != nil {
			if e, _ := a["empty"].(bool); !e {
				typ = toStr(a["type"])
			}
			col = color6(strings.TrimLeft(toStr(a["color"]), "#"))
			act, _ = a["active"].(bool)
		}
		p := strings.Split(k, "|")
		a01 := "0"
		if act {
			a01 = "1"
		}
		if p[0] == "tray" {
			un := strconv.Itoa(idx(p[1]) + 1)
			list = append(list, un+"."+p[2]+","+amsTypeClean.Replace(clean(typ))+","+col+","+a01)
			if act {
				c, t, sl, am = col, typ, p[2], un
			}
		} else {
			if typ != "" {
				ext = append(ext, "E,"+amsTypeClean.Replace(clean(typ))+","+col+","+a01)
			}
			if act {
				c, t, sl, am = col, typ, "Ext", ""
			}
		}
	}
	list = append(list, ext...) // externe Spule ans Ende
	dual := ""
	if _, ok := h.v["left_nozzle_temp"]; ok {
		dual = "1"
	}
	light := h.str("chamber_light")
	if light == "" {
		light = h.str("camera_light")
	}
	return statusLineFrom(map[string]string{
		"st": clean(strings.ToUpper(h.str("print_status"))), "pct": h.numS("print_progress"), "rem": h.minutes("remaining_time"),
		"lay": h.numS("current_layer"), "tlay": h.numS("total_layers"),
		"noz": h.numS("nozzle_temp"), "nozt": h.numS("target_nozzle_temp"), "bed": h.numS("bed_temp"), "bedt": h.numS("target_bed_temp"),
		"job": clean(h.str("subtask_name")), "spd": spd, "col": clean(c), "fil": clean(t), "slot": clean(sl), "ams": clean(am),
		"ip": clean(h.str("ip_address")), "dual": dual,
		"nozR": h.numS("right_nozzle_temp"), "noztR": h.numS("right_target_nozzle_temp"),
		"nozL": h.numS("left_nozzle_temp"), "noztL": h.numS("left_target_nozzle_temp"),
		"light": clean(light), "amsl": clean(strings.Join(list, ";")),
	})
}

func (h *haState) apply(eid string, state any) bool {
	k, ok := h.m[eid]
	if !ok {
		return false
	}
	sm := asMap(state)
	if sm == nil {
		delete(h.v, k)
		return true
	}
	h.v[k] = haVal{s: toStr(sm["state"]), a: asMap(sm["attributes"])}
	return true
}

// Registry auswerten: Entitaeten des Druckers ueber die unique_id
func (h *haState) mapRegistry(reg any, serial string) map[string]string {
	ser := strings.ToUpper(serial)
	reTray := regexp.MustCompile(`_` + regexp.QuoteMeta(ser) + `_AMS_(.+)_TRAY_(\d)$`)
	reExt := regexp.MustCompile(`_` + regexp.QuoteMeta(ser) + `_EXTERNALSPOOL(\d?)_EXTERNAL_SPOOL$`)
	h.m, h.v = map[string]string{}, map[string]haVal{}
	found := map[string]string{}
	for _, e := range asList(reg) {
		em := asMap(e)
		if em == nil || toStr(em["platform"]) != "bambu_lab" {
			continue
		}
		uidRaw := toStr(em["unique_id"])
		uid := strings.ToUpper(uidRaw)
		eid := toStr(em["entity_id"])
		if strings.HasPrefix(uid, ser+"_") {
			key := uidRaw[len(ser)+1:]
			if haWant[key] {
				h.m[eid] = key
				found[key] = eid
			}
		} else if m := reTray.FindStringSubmatch(uid); m != nil {
			h.m[eid] = "tray|" + m[1] + "|" + m[2]
		} else if m := reExt.FindStringSubmatch(uid); m != nil {
			h.m[eid] = "ext|" + m[1]
		}
	}
	return found
}

var reEntityID = regexp.MustCompile(`"entity_id"\s*:\s*"([^"]+)"`)

type wsMsg struct {
	b   []byte
	err error
}

func runHaStatus(ctx context.Context, o out, h haCfg, serial string, pingEvery, silenceMax time.Duration) {
	if len(h.urls) == 0 || h.token == "" {
		o.line("ERR|STOP|Home Assistant nicht eingerichtet")
		return
	}
	if serial == "" {
		o.line("ERR|STOP|Seriennummer fehlt (Einstellungen)")
		return
	}
	if pingEvery == 0 {
		pingEvery = 30 * time.Second
	}
	if silenceMax == 0 {
		silenceMax = 75 * time.Second
	}
	cur := 0
	for ctx.Err() == nil {
		stopNow := false
		err := func() error {
			o.line("STATE|connecting")
			var ws *wsConn
			last := ""
			for k := 0; k < len(h.urls) && ws == nil; k++ {
				idx := (cur + k) % len(h.urls)
				c, err := wsDial(ctx, h.urls[idx], "/api/websocket", h.insecure, 8*time.Second)
				if err != nil {
					if ctx.Err() != nil {
						return nil
					}
					m := err.Error()
					if isCertErr(err) {
						m = `Zertifikat wird nicht akzeptiert (selbstsigniert? Option "Zertifikat nicht prüfen")`
					}
					last = h.urls[idx] + ": " + m
					continue
				}
				ws, cur = c, idx
			}
			if ws == nil {
				return fmt.Errorf("Home Assistant nicht erreichbar (%s)", last)
			}
			defer ws.Close()
			stop := closeOnCancel(ctx, ws.c)
			defer stop()
			// Empfang in eigener Goroutine
			msgs := make(chan wsMsg, 64)
			quit := make(chan struct{})
			defer close(quit)
			go func() {
				for {
					b, err := ws.Recv()
					select {
					case msgs <- wsMsg{b, err}:
					case <-quit:
						return
					}
					if err != nil {
						return
					}
				}
			}()
			recv := func(d time.Duration) (map[string]any, []byte, error) {
				t := time.NewTimer(d)
				defer t.Stop()
				select {
				case <-ctx.Done():
					return nil, nil, ctx.Err()
				case <-t.C:
					return nil, nil, nil
				case m := <-msgs:
					if m.err != nil {
						if strings.Contains(m.err.Error(), "geschlossen") {
							return nil, nil, m.err
						}
						return nil, nil, errors.New("Verbindung zu Home Assistant unterbrochen")
					}
					var j map[string]any
					json.Unmarshal(m.b, &j)
					return j, m.b, nil
				}
			}
			result := func(id int, d time.Duration) (any, error) {
				end := time.Now().Add(d)
				for time.Now().Before(end) {
					j, _, err := recv(time.Until(end))
					if err != nil {
						return nil, err
					}
					if j == nil {
						continue
					}
					if toStr(j["type"]) == "result" {
						if n, _ := toInt(j["id"]); int(n) == id {
							if ok, _ := j["success"].(bool); !ok {
								return nil, errors.New("Home Assistant lehnt ab: " + clean(jsonProp(asMap(j["error"]), "message")))
							}
							return j["result"], nil
						}
					}
				}
				return nil, errors.New("Home Assistant antwortet nicht")
			}
			// Anmeldung
			j, _, err := recv(8 * time.Second)
			if err != nil {
				return err
			}
			if j == nil || toStr(j["type"]) != "auth_required" {
				return errors.New("Unerwartete Antwort von Home Assistant")
			}
			b, _ := json.Marshal(map[string]string{"type": "auth", "access_token": h.token})
			ws.Send(string(b))
			j, _, err = recv(8 * time.Second)
			if err != nil {
				return err
			}
			if j != nil && toStr(j["type"]) == "auth_invalid" {
				o.line("ERR|AUTH|Home-Assistant-Token ungültig")
				stopNow = true
				return nil
			}
			if j == nil || toStr(j["type"]) != "auth_ok" {
				return errors.New("Anmeldung bei Home Assistant fehlgeschlagen")
			}
			ws.Send(`{"id":1,"type":"config/entity_registry/list"}`)
			reg, err := result(1, 30*time.Second)
			if err != nil {
				return err
			}
			st := &haState{}
			found := st.mapRegistry(reg, serial)
			if _, ok := found["print_status"]; !ok {
				o.line("ERR|STOP|Drucker " + serial + " ist in Home Assistant nicht eingebunden (Integration ha-bambulab)")
				stopNow = true
				return nil
			}
			if e, ok := found["camera"]; ok {
				o.line("HAENT|cam|" + e)
			} else if e, ok := found["p1p_camera"]; ok {
				o.line("HAENT|cam|" + e)
			}
			if e, ok := found["cover_image"]; ok {
				o.line("HAENT|cover|" + e)
			}
			if e, ok := found["chamber_light"]; ok {
				o.line("HAENT|light|" + e)
			} else if e, ok := found["camera_light"]; ok {
				o.line("HAENT|light|" + e)
			}
			ws.Send(`{"id":2,"type":"get_states"}`)
			states, err := result(2, 30*time.Second)
			if err != nil {
				return err
			}
			for _, s := range asList(states) {
				if s != nil {
					st.apply(toStr(asMap(s)["entity_id"]), s)
				}
			}
			ws.Send(`{"id":3,"type":"subscribe_events","event_type":"state_changed"}`)
			if _, err := result(3, 15*time.Second); err != nil {
				return err
			}
			o.line("STATE|connected")
			o.line("VIA|" + h.urls[cur])
			emit := func() {
				if l := st.statusLine(); l != st.lastOut {
					o.line(l)
					st.lastOut = l
				}
			}
			emit()
			id := 10
			pingAt, heard, flushAt := time.Now(), time.Now(), time.Now()
			dirty := false
			for ctx.Err() == nil {
				_, raw, err := recv(500 * time.Millisecond)
				if err != nil {
					return err
				}
				if raw != nil {
					heard = time.Now()
					// schneller Vorfilter: nur Nachrichten, die eine unserer Entitaeten nennen
					if bytes.Contains(raw, []byte(`"event"`)) {
						if m := reEntityID.FindSubmatch(raw); m != nil {
							if _, ok := st.m[string(m[1])]; ok {
								var ev map[string]any
								if json.Unmarshal(raw, &ev) == nil {
									d := asMap(asMap(ev["event"])["data"])
									if d != nil && st.apply(toStr(d["entity_id"]), d["new_state"]) {
										dirty = true
									}
								}
							}
						}
					}
				}
				// Aenderungen kommen oft im Pulk -> hoechstens 2x pro Sekunde ausgeben
				if dirty && time.Since(flushAt) >= 500*time.Millisecond {
					emit()
					dirty, flushAt = false, time.Now()
				}
				if time.Since(pingAt) >= pingEvery {
					id++
					ws.Send(`{"id":` + strconv.Itoa(id) + `,"type":"ping"}`)
					pingAt = time.Now()
					// Lebenszeichen: Status auch ohne Aenderung erneut melden (sonst gilt er als veraltet)
					st.lastOut = ""
					emit()
					dirty = false
				}
				if time.Since(heard) >= silenceMax {
					return errors.New("Keine Antwort mehr von Home Assistant")
				}
			}
			return nil
		}()
		if stopNow || ctx.Err() != nil {
			return
		}
		if err != nil {
			o.err(err.Error())
		}
		sleepCtx(ctx, 10*time.Second)
	}
}
