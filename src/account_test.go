package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Meldungen eines Workers bis "fertig" einsammeln
func runCollect(t *testing.T, fn func(ctx context.Context, o out)) []string {
	t.Helper()
	drainEvents()
	w := startWorker("t", fn)
	var lines []string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range drainEvents() {
			if ev.w != w {
				continue
			}
			if ev.end {
				return lines
			}
			lines = append(lines, ev.line)
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.stop()
	t.Fatalf("Zeitueberschreitung: %v", lines)
	return nil
}

func jwt(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	e := base64.RawURLEncoding.EncodeToString
	return e([]byte(`{"alg":"none"}`)) + "." + e(b) + ".sig"
}

func fakeCloud(t *testing.T) *httptest.Server {
	tokA := jwt(map[string]any{"username": "u_1234"})
	devices := func(tok string) string {
		switch tok {
		case "leer":
			return `{"message":"success","code":null,"error":null,"devices":[]}`
		case "zwei":
			return `{"devices":[{"dev_id":"03919a1","name":"Michas A1","dev_access_code":"12345678","online":true,"dev_product_name":"A1"},
			{"dev_id":"0948H2D","name":"H2D Büro","dev_access_code":"87654321","online":false,"dev_product_name":"H2D"}]}`
		}
		return `{"message":"success","devices":[{"dev_id":"03919a1","name":"Michas A1","dev_access_code":"12345678","online":true,"dev_product_name":"A1"}]}`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/user-service/user/login", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		switch {
		case b["account"] == "cf@x":
			w.WriteHeader(403)
			io.WriteString(w, "<html>Attention Required! | Cloudflare</html>")
		case b["password"] == "geheim" && b["account"] == "direct@x":
			io.WriteString(w, `{"accessToken":"`+tokA+`"}`)
		case b["password"] == "geheim" && b["account"] == "code@x":
			io.WriteString(w, `{"loginType":"verifyCode"}`)
		case b["password"] == "geheim" && b["account"] == "tfa@x":
			io.WriteString(w, `{"loginType":"tfa","tfaKey":"KEY1"}`)
		case b["password"] != "":
			w.WriteHeader(400)
			io.WriteString(w, `{"code":3}`)
		case b["code"] == "111111":
			io.WriteString(w, `{"accessToken":"zwei"}`)
		case b["code"] == "000000":
			w.WriteHeader(400)
			io.WriteString(w, `{"code":1}`)
		default:
			w.WriteHeader(400)
			io.WriteString(w, `{"code":2}`)
		}
	})
	mux.HandleFunc("/v1/user-service/user/sendemail/code", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	mux.HandleFunc("/api/csrf", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "bbl_csrf_token", Value: "CSRF1", Path: "/"})
		w.WriteHeader(204)
	})
	mux.HandleFunc("/api/sign-in/tfa", func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("bbl_csrf_token")
		if err != nil || ck.Value != "CSRF1" || r.Header.Get("x-bbl-csrf-token") != "CSRF1" {
			w.WriteHeader(403)
			io.WriteString(w, `{"error":"missing_cookie"}`)
			return
		}
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if b["tfaCode"] == "654321" && b["tfaKey"] == "KEY1" {
			http.SetCookie(w, &http.Cookie{Name: "token", Value: "opaque", Path: "/"})
			io.WriteString(w, `{}`)
			return
		}
		w.WriteHeader(400)
	})
	mux.HandleFunc("/v1/iot-service/api/user/bind", func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "kaputt" {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, devices(tok))
	})
	mux.HandleFunc("/v1/design-user-service/my/preference", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"uid":987}`)
	})
	// Home Assistant
	mux.HandleFunc("/api/states", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer LLAT" {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, `[{"entity_id":"camera.a1_kamera","attributes":{"friendly_name":"A1 Kamera"}},
		{"entity_id":"image.a1_titelbild","attributes":{"friendly_name":"A1 Titelbild"}},{"entity_id":"sensor.x","attributes":{}}]`)
	})
	s := httptest.NewServer(mux)
	testAPI, testWeb = s.URL, s.URL
	t.Cleanup(func() { s.Close(); testAPI, testWeb = "", "" })
	return s
}

func want(t *testing.T, name string, lines []string, sub ...string) {
	t.Helper()
	all := strings.Join(lines, "\n")
	for _, s := range sub {
		if !strings.Contains(all, s) {
			t.Errorf("%s: %q fehlt in\n%s", name, s, all)
		}
	}
}

func TestAccount(t *testing.T) {
	fakeCloud(t)
	acc := func(c accCfg) []string {
		return runCollect(t, func(ctx context.Context, o out) { runAccount(ctx, o, c) })
	}
	want(t, "direkt", acc(accCfg{mode: "login", email: "direct@x", pass: "geheim"}), "USER|u_1234", "DEV|03919A1|Michas A1|12345678|True|A1", "DONE")
	want(t, "falsches Passwort", acc(accCfg{mode: "login", email: "direct@x", pass: "x"}), "ERR|Anmeldung abgelehnt")
	want(t, "Code noetig", acc(accCfg{mode: "login", email: "code@x", pass: "geheim"}), "LOGIN|code")
	want(t, "Code ok", acc(accCfg{mode: "code", email: "code@x", code: "111111"}), "USER|u_987", "DEV|0948H2D|H2D Büro|87654321|False|H2D")
	want(t, "Code falsch", acc(accCfg{mode: "code", email: "code@x", code: "999999"}), "ERR|Code falsch")
	want(t, "Code alt", acc(accCfg{mode: "code", email: "code@x", code: "000000"}), "ERR|Code abgelaufen")
	want(t, "2FA noetig", acc(accCfg{mode: "login", email: "tfa@x", pass: "geheim"}), "LOGIN|tfa|KEY1")
	want(t, "2FA", acc(accCfg{mode: "tfa", email: "tfa@x", tfaKey: "KEY1", code: "654321"}), "LOGIN|token|opaque", "USER|u_987")
	want(t, "Cloudflare", acc(accCfg{mode: "login", email: "cf@x", pass: "geheim"}), "Cloudflare")
	want(t, "Token Bearer", acc(accCfg{mode: "token", token: `"Bearer ` + jwt(map[string]any{"uid": "55"}) + `"`}), "USER|u_55")
	want(t, "Token kaputt", acc(accCfg{mode: "token", token: "kaputt"}), "ERR|AUTH|")
	want(t, "leeres Konto", acc(accCfg{mode: "token", token: "leer"}), "Keine Drucker", "Einträge 0")
	want(t, "neu senden", acc(accCfg{mode: "resend", email: "code@x"}), "LOGIN|code")
}

func TestHaList(t *testing.T) {
	s := fakeCloud(t)
	l := runCollect(t, func(ctx context.Context, o out) {
		runHaList(ctx, o, haCfg{urls: []string{"http://127.0.0.1:1", s.URL}, token: "LLAT"})
	})
	want(t, "HaList", l, "CAM|camera.a1_kamera|A1 Kamera", "IMG|image.a1_titelbild|A1 Titelbild", "HAOK|"+s.URL+"|1", "DONE")
	l = runCollect(t, func(ctx context.Context, o out) { runHaList(ctx, o, haCfg{urls: []string{s.URL}, token: "x"}) })
	want(t, "HaList Token", l, "ERR|AUTH|")
}

// ---------------------------------------------------------------- simulierter A1 (TLS 6000 + 8883)
func fakeA1(t *testing.T, code string) string {
	cert, err := tls.X509KeyPair([]byte(testCert), []byte(testKey))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	l6, _ := tls.Listen("tcp", "127.0.0.1:0", cfg)
	host, p6, _ := net.SplitHostPort(l6.Addr().String())
	l8, err := tls.Listen("tcp", net.JoinHostPort(host, "8883"), cfg)
	if err != nil {
		t.Skip("Port 8883 belegt")
	}
	var wg sync.WaitGroup
	t.Cleanup(func() { l6.Close(); l8.Close(); wg.Wait() })
	go func() {
		for {
			c, err := l6.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				a := make([]byte, 80)
				if _, err := io.ReadFull(c, a); err != nil || string(a[48:48+len(code)]) != code {
					return
				}
				jpg := append([]byte{0xFF, 0xD8}, make([]byte, 2000)...)
				h := make([]byte, 16)
				binary.LittleEndian.PutUint32(h, uint32(len(jpg)))
				c.Write(append(h, jpg...))
				time.Sleep(200 * time.Millisecond)
			}()
		}
	}()
	go func() {
		for {
			c, err := l8.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				p, err := mqttRead(c)
				if err != nil || p.typ != 0x10 {
					return
				}
				ok := strings.Contains(string(p.body), code)
				rc := byte(5)
				if ok {
					rc = 0
				}
				c.Write([]byte{0x20, 2, 0, rc})
				if !ok {
					return
				}
				mqttRead(c) // subscribe
				c.Write([]byte{0x90, 3, 0, 1, 0})
				mqttRead(c) // pushall
				c.Write(mqttPublishPkt("device/X/report", `{"print":{"gcode_state":"IDLE"}}`))
				time.Sleep(200 * time.Millisecond)
			}()
		}
	}()
	_ = p6
	return l6.Addr().String()
}

func TestProbe(t *testing.T) {
	addr := fakeA1(t, "12345678")
	host, port, _ := net.SplitHostPort(addr)
	if port != "6000" {
		// der Test-Drucker laeuft nicht auf 6000 -> Kamera ueber Port 6000 ist hier nicht pruefbar,
		// nur der Status-Teil (8883)
		r := probeMqtt(context.Background(), probeCfg{ip: host, code: "12345678", serial: "X"})
		if r != "ok:Status empfangen" {
			t.Fatal(r)
		}
		r = probeMqtt(context.Background(), probeCfg{ip: host, code: "falsch00", serial: "X"})
		if !strings.HasPrefix(r, "err:Login abgelehnt") {
			t.Fatal(r)
		}
	}
}
