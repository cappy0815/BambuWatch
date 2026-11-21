package main

// Integrationstest gegen simulierte Drucker/Home Assistant/Cloud (Python-Skripte im Scratchpad).
// Start: BW_FAKE=<scratchpad> go test -run Fake -v

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeDir(t *testing.T) string {
	d := os.Getenv("BW_FAKE")
	if d == "" {
		t.Skip("BW_FAKE nicht gesetzt")
	}
	return d
}

func startPy(t *testing.T, dir, script string, args ...string) {
	cmd := exec.Command("python3", append([]string{script}, args...)...)
	cmd.Dir = dir
	log, _ := os.Create(filepath.Join(os.TempDir(), filepath.Base(script)+".log"))
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	time.Sleep(1500 * time.Millisecond)
}

// sammelt die Zeilen eines Workers, bis stop() true liefert oder die Zeit um ist
func collect(t *testing.T, d time.Duration, fn func(ctx context.Context, o out), stop func(lines []string) bool) []string {
	drainEvents()
	w := startWorker("test", fn)
	defer w.stop()
	var lines []string
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		for _, e := range drainEvents() {
			if e.w != w {
				continue
			}
			l := e.line
			if e.img != nil {
				l += "#img" + string(rune('0'+len(e.img)%10))
				l = e.line + "#img"
			}
			if e.end {
				l = "END"
			}
			lines = append(lines, l)
			t.Log(trunc(l, 200))
		}
		if stop(lines) || (len(lines) > 0 && lines[len(lines)-1] == "END") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return lines
}

func has(lines []string, prefix string) string {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}

func count(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func TestFakeA1(t *testing.T) {
	d := fakeDir(t)
	startPy(t, filepath.Join(d, "fake"), "fake_a1_15.py", "40")
	l := collect(t, 15*time.Second, func(ctx context.Context, o out) {
		runCamera(ctx, o, camCfg{ip: "127.0.0.1", code: "12345678", camType: "auto"})
	}, func(l []string) bool { return has(l, "ERR|") != "" })
	if has(l, "CAMTYPE|jpeg") == "" || count(l, "FRAME#img") != 5 || !strings.Contains(has(l, "ERR|"), "Kein Bild vom Drucker") {
		t.Fatalf("Kamera: %v", l)
	}
	l = collect(t, 10*time.Second, func(ctx context.Context, o out) {
		runMqtt(ctx, o, mqttCfg{host: "127.0.0.1", port: 8883, user: "bblp", pw: "12345678", serial: "03919A3B0400000"})
	}, func(l []string) bool { return count(l, "STATUS|") >= 3 })
	var sts []map[string]string
	for _, x := range l {
		if strings.HasPrefix(x, "STATUS|") {
			sts = append(sts, st(x))
		}
	}
	if len(sts) < 3 || sts[0]["pct"] != "42" || sts[0]["fil"] != "PETG" || sts[0]["light"] != "on" || sts[1]["slot"] != "Ext" ||
		sts[2]["st"] != "FINISH" || sts[2]["pct"] != "100" {
		t.Fatalf("MQTT: %v", sts)
	}
	l = collect(t, 8*time.Second, func(ctx context.Context, o out) {
		runLightLan(ctx, o, "127.0.0.1", "12345678", "03919A3B0400000", "on")
	}, func(l []string) bool { return false })
	if has(l, "LIGHT|on") == "" {
		t.Fatalf("Licht: %v", l)
	}
	// falscher Code: Login abgelehnt
	l = collect(t, 8*time.Second, func(ctx context.Context, o out) {
		runMqtt(ctx, o, mqttCfg{host: "127.0.0.1", port: 8883, user: "bblp", pw: "falsch", serial: "X"})
	}, func(l []string) bool { return has(l, "ERR|") != "" })
	if !strings.Contains(has(l, "ERR|"), "Access Code") {
		t.Fatalf("Login: %v", l)
	}
	// Cloud-Variante: Code 5 -> ERR|AUTH und Ende
	l = collect(t, 8*time.Second, func(ctx context.Context, o out) {
		runMqtt(ctx, o, mqttCfg{host: "127.0.0.1", port: 8883, user: "u_1", pw: "alt", serial: "X", cloud: true})
	}, func(l []string) bool { return false })
	if has(l, "ERR|AUTH|") == "" || l[len(l)-1] != "END" {
		t.Fatalf("Cloud-Auth: %v", l)
	}
}

func TestFakeHA(t *testing.T) {
	d := fakeDir(t)
	startPy(t, filepath.Join(d, "t14"), "fakews.py", "40")
	h := haCfg{urls: []string{"http://127.0.0.1:1", "http://127.0.0.1:18125"}, token: "LLAT"}
	l := collect(t, 15*time.Second, func(ctx context.Context, o out) { runHaStatus(ctx, o, h, "03919a1", 0, 0) },
		func(l []string) bool { return strings.Contains(strings.Join(l, "\n"), "st=FINISH") })
	if has(l, "HAENT|cam|camera.a1_kamera") == "" || has(l, "HAENT|cover|image.a1_titelbild") == "" ||
		has(l, "HAENT|light|light.a1_kammerbeleuchtung") == "" || has(l, "VIA|http://127.0.0.1:18125") == "" {
		t.Fatalf("HAENT: %v", l)
	}
	first := st(has(l, "STATUS|"))
	want := map[string]string{"st": "RUNNING", "pct": "42", "rem": "75", "lay": "57", "tlay": "210", "noz": "220", "nozt": "220",
		"bed": "65", "bedt": "", "job": "Benchy   Platte 1", "spd": "3", "fil": "PETG", "slot": "2", "ams": "1", "ip": "192.168.178.55",
		"light": "off", "amsl": "1.1,PLA,FF0000,0;1.2,PETG,00AE42,1;1.3,,000000,0;1.4,PLA Silk,FFFFFF,0"}
	for k, v := range want {
		if first[k] != v {
			t.Errorf("%s = %q, will %q", k, first[k], v)
		}
	}
	last := st(l[len(l)-1])
	if last["st"] != "FINISH" || last["light"] != "on" || last["pct"] != "43" {
		t.Fatalf("Events: %v", last)
	}
	// falsches Token
	l = collect(t, 8*time.Second, func(ctx context.Context, o out) {
		runHaStatus(ctx, o, haCfg{urls: h.urls[1:], token: "x"}, "03919A1", 0, 0)
	}, func(l []string) bool { return false })
	if has(l, "ERR|AUTH|") == "" || l[len(l)-1] != "END" {
		t.Fatalf("Token: %v", l)
	}
	// unbekannter Drucker
	l = collect(t, 8*time.Second, func(ctx context.Context, o out) { runHaStatus(ctx, o, h, "NIX", 0, 0) }, func(l []string) bool { return false })
	if !strings.Contains(has(l, "ERR|STOP|"), "nicht eingebunden") {
		t.Fatalf("unbekannt: %v", l)
	}
	l = collect(t, 8*time.Second, func(ctx context.Context, o out) { runHaLight(ctx, o, h, "light.a1_kammerbeleuchtung", "on") },
		func(l []string) bool { return false })
	if has(l, "LIGHT|on") == "" {
		t.Fatalf("HA-Licht: %v", l)
	}
}

func TestFakeCover(t *testing.T) {
	d := fakeDir(t)
	startPy(t, filepath.Join(d, "t14"), "fake14.py", "40")
	h := haCfg{urls: []string{"http://127.0.0.1:18123"}, token: "LLAT"}
	l := collect(t, 8*time.Second, func(ctx context.Context, o out) { runHaCamera(ctx, o, h, "image.a1_123_kamera", 300*time.Millisecond) },
		func(l []string) bool { return count(l, "FRAME#img") >= 3 })
	if has(l, "STATE|connected") == "" || count(l, "FRAME#img") < 3 {
		t.Fatalf("HA-Kamera: %v", l)
	}
	l = collect(t, 8*time.Second, func(ctx context.Context, o out) { runHaCamera(ctx, o, h, "camera.gibtsnicht", time.Second) },
		func(l []string) bool { return false })
	if !strings.Contains(has(l, "ERR|STOP|"), "gibt es in Home Assistant nicht") {
		t.Fatalf("HA-Kamera 404: %v", l)
	}
	l = collect(t, 8*time.Second, func(ctx context.Context, o out) {
		runCover(ctx, o, coverCfg{ha: h, entity: "image.a1_123_titelbild", token: "TOK", serial: "03919A1"})
	}, func(l []string) bool { return false })
	if has(l, "COVER|HA|#img") == "" {
		t.Fatalf("Cover HA: %v", l)
	}
	testAPI = "http://127.0.0.1:18124"
	defer func() { testAPI = "" }()
	l = collect(t, 8*time.Second, func(ctx context.Context, o out) {
		runCover(ctx, o, coverCfg{ha: h, entity: "image.leer", token: "TOK", serial: "03919A1"})
	}, func(l []string) bool { return false })
	if has(l, "COVER|Cloud|Benchy#img") == "" {
		t.Fatalf("Cover Cloud: %v", l)
	}
	l = collect(t, 8*time.Second, func(ctx context.Context, o out) {
		runCover(ctx, o, coverCfg{token: "ALT", serial: "03919A1"})
	}, func(l []string) bool { return false })
	if !strings.Contains(has(l, "ERR|"), "Anmeldung abgelaufen") {
		t.Fatalf("Cover abgelaufen: %v", l)
	}
	l = collect(t, 4*time.Second, func(ctx context.Context, o out) { runCover(ctx, o, coverCfg{}) }, func(l []string) bool { return false })
	if has(l, "NOCOVER|") == "" {
		t.Fatalf("Cover leer: %v", l)
	}
}

func TestFakeRtsp(t *testing.T) {
	d := fakeDir(t)
	l := collect(t, 15*time.Second, func(ctx context.Context, o out) {
		runCamera(ctx, o, camCfg{ip: "x", code: "c", ffmpeg: "/usr/bin/ffmpeg", testRtsp: filepath.Join(d, "mtx", "test.mp4")})
	}, func(l []string) bool { return count(l, "FRAME#img") >= 4 })
	if has(l, "STATE|connected") == "" || count(l, "FRAME#img") < 4 {
		t.Fatalf("RTSP: %v", l)
	}
	l = collect(t, 5*time.Second, func(ctx context.Context, o out) {
		runCamera(ctx, o, camCfg{ip: "x", code: "c", camType: "rtsp", ffmpeg: ""})
	}, func(l []string) bool { return false })
	if !strings.HasPrefix(has(l, "ERR|STOP|"), "ERR|STOP|Dieser Drucker sendet einen Videostream") {
		t.Fatalf("ohne ffmpeg: %v", l)
	}
}
