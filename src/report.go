// Druckerstatus zusammenfuehren (Merge-Report, Get-Tray, Get-AmsList, Out-Status aus dem Helfer).
// Der Drucker schickt nach dem Vollstatus nur Aenderungen; hier wird alles gesammelt.
package main

import (
	"encoding/binary"
	"math"
	"net"
	"strconv"
	"strings"
)

type reportState struct {
	st        map[string]any
	amsUnits  []any // AMS-Einheiten mit Slots
	trayNow   int   // aktiver Slot
	vt        map[string]any
	lanIP     string
	dual      bool // H2D: zwei Duesen
	noz, nozt [2]*float64
	snow      [2]int
	activeNoz int
	bedDev    []float64 // Bett aus device.bed.info.temp (neuere Firmware)
	rtsp      string    // ipcam.rtsp_url ("disable" = Liveview aus)
	lastOut   string
}

func newReportState() *reportState {
	return &reportState{st: map[string]any{}, trayNow: -1, snow: [2]int{-1, -1}}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// @(x): Liste oder Einzelwert als Liste
func asList(v any) []any {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		return t
	}
	return []any{v}
}

// [int]$x / [double]$x wie PowerShell (Zahlen und Zahlentexte)
func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func toInt(v any) (int64, bool) {
	f, ok := toFloat(v)
	if !ok {
		return 0, false
	}
	return int64(math.Round(f)), true
}

func toStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "True"
		}
		return "False"
	}
	return ""
}

func (r *reportState) merge(j map[string]any) bool {
	p := asMap(j["print"])
	if p == nil {
		return false
	}
	for k, v := range p {
		switch k {
		case "ams":
			a := asMap(v)
			if a == nil {
				continue
			}
			if u, ok := a["ams"]; ok && u != nil {
				r.amsUnits = asList(u)
			}
			if tn, ok := a["tray_now"]; ok && tn != nil && toStr(tn) != "" {
				if n, ok := toInt(tn); ok {
					r.trayNow = int(n)
				}
			}
		case "vt_tray":
			r.vt = asMap(v)
		case "device":
			d := asMap(v)
			if ex := asMap(d["extruder"]); ex != nil {
				if info, ok := ex["info"]; ok && info != nil {
					list := asList(info)
					for _, e := range list {
						em := asMap(e)
						id, ok := toInt(em["id"])
						if !ok || (id != 0 && id != 1) {
							continue
						}
						if t, ok := toInt(em["temp"]); ok {
							a, b := float64(t&0xFFFF), float64((t>>16)&0xFFFF)
							r.noz[id], r.nozt[id] = &a, &b
						}
						if s, ok := toInt(em["snow"]); ok {
							r.snow[id] = int(s)
						}
					}
					if len(list) >= 2 {
						r.dual = true
					}
				}
				if s, ok := toInt(ex["state"]); ok {
					r.activeNoz = int((s >> 4) & 0xF)
				}
			}
			if bed := asMap(d["bed"]); bed != nil {
				if info := asMap(bed["info"]); info != nil {
					if t, ok := toInt(info["temp"]); ok {
						r.bedDev = []float64{float64(t & 0xFFFF), float64((t >> 16) & 0xFFFF)}
					}
				}
			}
		case "ipcam":
			if u, ok := asMap(v)["rtsp_url"]; ok && u != nil {
				r.rtsp = toStr(u)
			}
		case "net":
			for _, n := range asList(asMap(v)["info"]) {
				ip, ok := toInt(asMap(n)["ip"])
				if !ok {
					continue
				}
				u := uint32(ip & 0xFFFFFFFF)
				if u != 0 {
					b := make([]byte, 4)
					binary.LittleEndian.PutUint32(b, u) // Bytes in little-endian-Reihenfolge (wie ha-bambulab)
					r.lanIP = net.IP(b).String()
					break
				}
			}
		default:
			r.st[k] = v
		}
	}
	return true
}

// Num($v, '0'): ganze Zahl, sonst leer
func num(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok && s == "" {
		return ""
	}
	f, ok := toFloat(v)
	if !ok {
		return ""
	}
	return strconv.FormatFloat(math.Round(f), 'f', 0, 64)
}

func numP(p *float64) string {
	if p == nil {
		return ""
	}
	return num(*p)
}

type tray struct {
	c, t, s, a string
	now        int
}

// aktuelles Filament: tray_now 0..n = AMS-Slot (ams_id*4 + tray_id), 254 = externe Spule, 255 = keins
func (r *reportState) tray() tray {
	res := tray{now: -1}
	now := r.trayNow
	if r.dual {
		// H2D: Slot der aktiven Duese aus "snow" (oben AMS-Nr., unten Slot) wie ha-bambulab
		if r.activeNoz >= 0 && r.activeNoz < 2 {
			if sn := r.snow[r.activeNoz]; sn >= 0 {
				now = (sn>>8)*4 + (sn & 0x3)
			}
		}
	}
	res.now = now
	if now == 254 && r.vt != nil {
		res.c, res.t, res.s = toStr(r.vt["tray_color"]), toStr(r.vt["tray_type"]), "Ext"
		return res
	}
	if now >= 0 && now < 254 && r.amsUnits != nil {
		aID, tID := now/4, now%4
		for _, u := range r.amsUnits {
			um := asMap(u)
			if id, _ := toInt(um["id"]); int(id) != aID {
				continue
			}
			for _, t := range asList(um["tray"]) {
				tm := asMap(t)
				if id, ok := toInt(tm["id"]); ok && int(id) == tID {
					res.c, res.t = toStr(tm["tray_color"]), toStr(tm["tray_type"])
					res.s, res.a = strconv.Itoa(tID+1), strconv.Itoa(aID+1)
				}
			}
		}
	}
	return res
}

// Kammerlicht aus print.lights_report (node chamber_light, mode on/off/flashing)
func (r *reportState) light() string {
	for _, l := range asList(r.st["lights_report"]) {
		lm := asMap(l)
		if lm != nil && toStr(lm["node"]) == "chamber_light" {
			return toStr(lm["mode"])
		}
	}
	return ""
}

func color6(s string) string {
	if len(s) >= 6 {
		return s[:6]
	}
	return s
}

var amsTypeClean = strings.NewReplacer(",", " ", ";", " ")

// alle Slots: "Einheit.Slot,Typ,RRGGBB,aktiv" durch ";" getrennt; externe Spule als "E"
func (r *reportState) amsList(now int) string {
	var out []string
	for _, u := range r.amsUnits {
		um := asMap(u)
		if um == nil {
			continue
		}
		uid64, _ := toInt(um["id"])
		uid := int(uid64)
		ul := strconv.Itoa(uid + 1)
		if uid >= 128 {
			ul = "HT" + strconv.Itoa(uid-127)
		}
		for _, t := range asList(um["tray"]) {
			tm := asMap(t)
			if tm == nil {
				continue
			}
			tid64, _ := toInt(tm["id"])
			tid := int(tid64)
			act := "0"
			if uid < 128 && now == uid*4+tid {
				act = "1"
			}
			out = append(out, ul+"."+strconv.Itoa(tid+1)+","+amsTypeClean.Replace(clean(tm["tray_type"]))+","+color6(toStr(tm["tray_color"]))+","+act)
		}
	}
	if r.vt != nil && toStr(r.vt["tray_type"]) != "" {
		act := "0"
		if now == 254 {
			act = "1"
		}
		out = append(out, "E,"+amsTypeClean.Replace(clean(r.vt["tray_type"]))+","+color6(toStr(r.vt["tray_color"]))+","+act)
	}
	return strings.Join(out, ";")
}

// Reihenfolge der Felder wie im Helfer (die Oberflaeche liest sie per Name)
var statusKeys = []string{"st", "pct", "rem", "lay", "tlay", "noz", "nozt", "bed", "bedt", "job", "err", "spd",
	"col", "fil", "slot", "ams", "ip", "cam", "dual", "nozR", "noztR", "nozL", "noztL", "act", "light", "amsl"}

func statusLineFrom(f map[string]string) string {
	var b strings.Builder
	b.WriteString("STATUS")
	for _, k := range statusKeys {
		b.WriteString("|" + k + "=" + f[k])
	}
	return b.String()
}

func (r *reportState) statusLine() string {
	s := r.st
	tr := r.tray()
	f := map[string]string{
		"st": clean(s["gcode_state"]), "pct": num(s["mc_percent"]), "rem": num(s["mc_remaining_time"]),
		"lay": num(s["layer_num"]), "tlay": num(s["total_layer_num"]),
		"noz": num(s["nozzle_temper"]), "nozt": num(s["nozzle_target_temper"]),
		"bed": num(s["bed_temper"]), "bedt": num(s["bed_target_temper"]),
		"job": clean(s["subtask_name"]), "err": num(s["print_error"]), "spd": num(s["spd_lvl"]),
		"col": clean(tr.c), "fil": clean(tr.t), "slot": clean(tr.s), "ams": clean(tr.a),
		"ip": clean(r.lanIP), "nozR": numP(r.noz[0]), "noztR": numP(r.nozt[0]),
		"nozL": numP(r.noz[1]), "noztL": numP(r.nozt[1]),
		"light": clean(r.light()), "amsl": clean(r.amsList(tr.now)),
	}
	if r.bedDev != nil {
		f["bed"], f["bedt"] = num(r.bedDev[0]), num(r.bedDev[1])
	}
	if r.rtsp == "disable" {
		f["cam"] = "off"
	}
	if r.dual {
		f["dual"] = "1"
		f["act"] = strconv.Itoa(r.activeNoz)
		if r.activeNoz >= 0 && r.activeNoz < 2 {
			f["noz"], f["nozt"] = numP(r.noz[r.activeNoz]), numP(r.nozt[r.activeNoz])
		}
	}
	return statusLineFrom(f)
}
