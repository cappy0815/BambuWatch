package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func st(line string) map[string]string {
	m := map[string]string{}
	parseStatus(line, m)
	return m
}

func TestMergeA1(t *testing.T) {
	r := newReportState()
	var j map[string]any
	json.Unmarshal([]byte(`{"print":{"gcode_state":"RUNNING","mc_percent":42,"mc_remaining_time":73,"layer_num":12,"total_layer_num":200,
	"nozzle_temper":219.81,"nozzle_target_temper":220,"bed_temper":59.5,"bed_target_temper":60,"subtask_name":"Benchy|Test ä",
	"print_error":0,"spd_lvl":2,"ams":{"tray_now":"2","ams":[{"id":"0","tray":[{"id":"0","tray_color":"FF0000FF","tray_type":"PLA"},
	{"id":"1","tray_color":"00FF00FF","tray_type":"PLA"},{"id":"2","tray_color":"00AE42FF","tray_type":"PETG"},{"id":"3"}]}]},
	"vt_tray":{"tray_color":"FFFFFFFF","tray_type":"TPU"},"lights_report":[{"node":"chamber_light","mode":"on"}],
	"net":{"info":[{"ip":0},{"ip":922790080}]},"ipcam":{"rtsp_url":"disable"}}}`), &j)
	if !r.merge(j) {
		t.Fatal("merge")
	}
	m := st(r.statusLine())
	want := map[string]string{"st": "RUNNING", "pct": "42", "rem": "73", "noz": "220", "bed": "60", "job": "Benchy Test ä",
		"col": "00AE42FF", "fil": "PETG", "slot": "3", "ams": "1", "light": "on", "cam": "off", "ip": "192.168.0.55",
		"amsl": "1.1,PLA,FF0000,0;1.2,PLA,00FF00,0;1.3,PETG,00AE42,1;1.4,,,0;E,TPU,FFFFFF,0"}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, will %q", k, m[k], v)
		}
	}
	// Teilupdate: externe Spule aktiv, Licht aus
	json.Unmarshal([]byte(`{"print":{"mc_percent":43,"ams":{"tray_now":"254"},"lights_report":[{"node":"chamber_light","mode":"off"}]}}`), &j)
	r.merge(j)
	m = st(r.statusLine())
	if m["pct"] != "43" || m["slot"] != "Ext" || m["fil"] != "TPU" || m["light"] != "off" || m["st"] != "RUNNING" ||
		!strings.HasSuffix(m["amsl"], "E,TPU,FFFFFF,1") {
		t.Fatalf("%v", m)
	}
}

func TestMergeH2D(t *testing.T) {
	r := newReportState()
	var j map[string]any
	// rechts (id 0) 220/220, links (id 1) 150/0; aktive Duese links (state 0x10); snow links = AMS 1 Slot 2 (0x0001)
	json.Unmarshal([]byte(`{"print":{"gcode_state":"RUNNING","device":{"extruder":{"state":16,"info":[
		{"id":0,"temp":14418140,"snow":0},{"id":1,"temp":150,"snow":257}]},"bed":{"info":{"temp":3932220}}},
		"ams":{"ams":[{"id":"0","tray":[{"id":"0","tray_type":"PLA","tray_color":"111111FF"},{"id":"1","tray_type":"ABS","tray_color":"222222FF"}]},
		{"id":"1","tray":[{"id":"0","tray_type":"PETG","tray_color":"333333FF"},{"id":"1","tray_type":"ASA","tray_color":"444444FF"}]},
		{"id":"128","tray":[{"id":"0","tray_type":"PA","tray_color":"555555FF"}]}]}}}`), &j)
	r.merge(j)
	m := st(r.statusLine())
	want := map[string]string{"dual": "1", "act": "1", "nozR": "220", "noztR": "220", "nozL": "150", "noztL": "0",
		"noz": "150", "nozt": "0", "bed": "60", "bedt": "60", "fil": "ASA", "slot": "2", "ams": "2",
		"amsl": "1.1,PLA,111111,0;1.2,ABS,222222,0;2.1,PETG,333333,0;2.2,ASA,444444,1;HT1.1,PA,555555,0"}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, will %q", k, m[k], v)
		}
	}
}

func TestSplitJpegs(t *testing.T) {
	var got []string
	rest := splitJpegs([]byte{1, 0xFF, 0xD8, 5, 0xFF, 0xD9, 0xFF, 0xD8, 7}, func(b []byte) { got = append(got, string(b)) })
	if len(got) != 1 || got[0] != string([]byte{0xFF, 0xD8, 5, 0xFF, 0xD9}) || string(rest) != string([]byte{0xFF, 0xD8, 7}) {
		t.Fatalf("%x %x", got, rest)
	}
	rest = splitJpegs(append(rest, 0xFF), func(b []byte) { got = append(got, string(b)) })
	rest = splitJpegs(append(rest, 0xD9), func(b []byte) { got = append(got, string(b)) })
	if len(got) != 2 || len(rest) != 1 {
		t.Fatalf("%x %x", got, rest)
	}
}

func TestCamAuth(t *testing.T) {
	a := camAuth("12345678")
	if len(a) != 80 || a[0] != 0x40 || a[5] != 0x30 || string(a[16:20]) != "bblp" || string(a[48:56]) != "12345678" || a[56] != 0 {
		t.Fatalf("%x", a)
	}
	if escapeData("a b+c@") != "a%20b%2Bc%40" {
		t.Fatal(escapeData("a b+c@"))
	}
}

func TestConfigLoad(t *testing.T) {
	ini := memIni{}
	ini.set("Allgemein", "Anzahl", "2")
	ini.set("Allgemein", "Aktiv", "2")
	ini.set("Drucker_1", "Name", "A1")
	ini.set("Drucker_1", "IP", "192.168.1.5")
	ini.set("Drucker_1", "AccessCode", "plain123")
	ini.set("Drucker_2", "Name", "H2D")
	ini.set("Drucker_2", "Kameratyp", "7")
	ini.set("Drucker_2", "KameraAn", "0")
	ini.set("Anzeige", "Status", "0")
	ini.set("HomeAssistant", "AdresseUnterwegs", "https://ha.example/")
	ini.set("HomeAssistant", "Token", "tok")
	s := loadSettings(ini, `C:\Docs`)
	if len(s.prn) != 2 || s.active != 1 || s.cur().name != "H2D" || s.cur().camType != 2 || s.cur().camOn || s.cur().stOn {
		t.Fatalf("%+v", s.prn)
	}
	if s.prn[0].code != "plain123" || !s.prn[0].camOn || s.prn[0].stOn || !s.prn[0].coverOn {
		t.Fatalf("%+v", s.prn[0])
	}
	if h := s.ha(); len(h.urls) != 1 || h.urls[0] != "https://ha.example" || !s.haConfigured() {
		t.Fatalf("%+v", h)
	}
	if s.snapDir != `C:\Docs\BambuWatch` || s.overlay != 1 || !s.studio {
		t.Fatalf("%+v", s)
	}
	s.savePrinters(ini)
	if ini.get("Drucker_2", "KameraAn", "") != "0" || ini.get("Anzeige", "Status", "x") != "x" || ini.get("Allgemein", "Aktiv", "") != "2" {
		t.Fatal(ini)
	}
	// Uebernahme aus v1.0
	old := memIni{}
	old.set("Drucker", "IP", "10.0.0.2")
	old.set("Konto", "Drucker", "Alt")
	s = loadSettings(old, "")
	if len(s.prn) != 1 || s.prn[0].name != "Alt" || old.get("Drucker", "IP", "") != "" || old.get("Drucker_1", "IP", "") != "10.0.0.2" {
		t.Fatalf("%+v %v", s.prn, old)
	}
}

func TestHaStatusLine(t *testing.T) {
	h := &haState{m: map[string]string{}, v: map[string]haVal{
		"print_status":   {s: "running"},
		"remaining_time": {s: "1.25", a: map[string]any{"unit_of_measurement": "h"}},
		"bed_temp":       {s: "149", a: map[string]any{"unit_of_measurement": "°F"}},
		"tray|0300ABC|1": {a: map[string]any{"type": "PLA", "color": "#FF0000FF", "active": false}},
		"tray|0300ABC|2": {a: map[string]any{"type": "PETG", "color": "#00AE42FF", "active": true}},
		"ext|":           {a: map[string]any{"type": "TPU", "color": "#FFFFFFFF"}},
	}}
	m := st(h.statusLine())
	if m["st"] != "RUNNING" || m["rem"] != "75" || m["bed"] != "65" || m["fil"] != "PETG" || m["slot"] != "2" || m["ams"] != "1" ||
		m["amsl"] != "1.1,PLA,FF0000,0;1.2,PETG,00AE42,1;E,TPU,FFFFFF,0" {
		t.Fatalf("%v", m)
	}
}

func TestSettingsRoundtrip(t *testing.T) {
	dpapiProtect = func(b []byte) []byte { return append([]byte("X"), b...) }
	dpapiUnprotect = func(b []byte) []byte { return b[1:] }
	defer func() {
		dpapiProtect, dpapiUnprotect = func(b []byte) []byte { return nil }, func(b []byte) []byte { return nil }
	}()
	ini := memIni{}
	s := loadSettings(ini, `C:\Docs`)
	s.overlay, s.fontAdd, s.haToken, s.token, s.alpha = 2, 3, "LLAT", "tok", 204
	if n := s.merge([]accDev{{serial: "SN1", name: "A1", code: "c1", model: "A1"}, {serial: "SN2", name: "H2D", code: "c2", model: "H2D"}}); n != 1 {
		t.Fatal("merge", n)
	}
	if s.prn[0].serial != "SN1" || s.prn[0].name != "A1" || s.prn[0].code != "c1" || s.prn[1].name != "H2D" {
		t.Fatalf("%+v", s.prn)
	}
	s.prn[0].ip = "1.2.3.4"
	s.save(ini)
	if !strings.HasPrefix(ini.get("Konto", "Token", ""), "dpapi:") {
		t.Fatal("Token nicht verschluesselt")
	}
	r := loadSettings(ini, `C:\Docs`)
	if r.overlay != 2 || r.fontAdd != 3 || r.haToken != "LLAT" || r.token != "tok" || r.alpha != 204 || len(r.prn) != 2 || r.prn[1].code != "c2" {
		t.Fatalf("%+v", r)
	}
	c := r.clone()
	c.prn[0].name = "geaendert"
	if r.prn[0].name == "geaendert" {
		t.Fatal("clone teilt Druckerliste")
	}
}
