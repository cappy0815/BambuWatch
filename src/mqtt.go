// Druckstatus per MQTT 3.1.1 (Logik aus BambuWatch_helper.ps1, Run-MqttLoop)
//
//	Heimnetz: <IP>:8883, Benutzer "bblp", Passwort = Access Code, Zertifikat selbstsigniert
//	Cloud:    us.mqtt.bambulab.com:8883 (China: cn.mqtt…), Benutzer "u_<uid>", Passwort = Token
//	Abo device/<Serie>/report, Anfrage "pushall" an device/<Serie>/request
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

func mqttStr(b []byte, s string) []byte {
	return append(append(b, byte(len(s)>>8), byte(len(s))), s...)
}

func mqttPacket(typ byte, body []byte) []byte {
	p := []byte{typ}
	n := len(body)
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 128
		}
		p = append(p, d)
		if n == 0 {
			break
		}
	}
	return append(p, body...)
}

func mqttConnectPkt(user, pw string) []byte {
	b := mqttStr(nil, "MQTT")
	b = append(b, 4, 0xC2, 0, 60) // 3.1.1, Benutzer+Passwort+Clean Session, Keep-Alive 60 s
	id := make([]byte, 5)
	rand.Read(id)
	b = mqttStr(b, "bw_"+hex.EncodeToString(id))
	b = mqttStr(b, user)
	b = mqttStr(b, pw)
	return mqttPacket(0x10, b)
}

func mqttSubscribePkt(topic string) []byte {
	b := mqttStr([]byte{0, 1}, topic)
	return mqttPacket(0x82, append(b, 0))
}

func mqttPublishPkt(topic, payload string) []byte {
	return mqttPacket(0x30, append(mqttStr(nil, topic), payload...))
}

type mqttPkt struct {
	typ, flags byte
	body       []byte
}

func mqttRead(r io.Reader) (mqttPkt, error) {
	var one [1]byte
	if _, err := io.ReadFull(r, one[:]); err != nil {
		return mqttPkt{}, err
	}
	p := mqttPkt{typ: one[0] & 0xF0, flags: one[0] & 0x0F}
	n, mult := 0, 1
	for i := 0; i < 4; i++ {
		if _, err := io.ReadFull(r, one[:]); err != nil {
			return p, err
		}
		n += int(one[0]&127) * mult
		mult *= 128
		if one[0]&128 == 0 {
			break
		}
	}
	if n > 16<<20 {
		return p, errors.New("MQTT-Paket zu gross")
	}
	p.body = make([]byte, n)
	if _, err := io.ReadFull(r, p.body); err != nil {
		return p, err
	}
	return p, nil
}

// Nutzdaten eines PUBLISH (Thema und ggf. Paket-ID ueberspringen)
func mqttPayload(p mqttPkt) []byte {
	b := p.body
	if len(b) < 2 {
		return nil
	}
	start := 2 + int(b[0])<<8 + int(b[1])
	if (p.flags>>1)&3 > 0 {
		start += 2
	}
	if start > len(b) {
		return nil
	}
	return b[start:]
}

type mqttCfg struct {
	host           string
	port           int
	verify         bool
	user, pw       string
	serial         string
	cloud          bool
	silenceTimeout time.Duration // 0 = 95 s
}

const pushall = `{"pushing":{"sequence_id":"0","command":"pushall"}}`

// gemeinsame Schleife fuer Heimnetz und Cloud
func runMqtt(ctx context.Context, o out, c mqttCfg) {
	if c.serial == "" {
		o.err("Seriennummer fehlt (Einstellungen)")
		return
	}
	if c.silenceTimeout == 0 {
		c.silenceTimeout = 95 * time.Second
	}
	topicRep := "device/" + c.serial + "/report"
	topicReq := "device/" + c.serial + "/request"
	st := newReportState()
	var pushAt time.Time // Schonung: "pushall" hoechstens alle 5 min
	fails := 0
	for ctx.Err() == nil {
		stopRetry := false
		err := func() error {
			o.line("STATE|connecting")
			conn, err := openTLS(ctx, c.host, c.port, 8*time.Second, c.verify)
			if err != nil {
				return err
			}
			defer conn.Close()
			stop := closeOnCancel(ctx, conn)
			defer stop()
			r := bufio.NewReader(conn)
			conn.SetDeadline(time.Now().Add(8 * time.Second))
			if _, err := conn.Write(mqttConnectPkt(c.user, c.pw)); err != nil {
				return err
			}
			p, err := mqttRead(r)
			if err != nil {
				return errors.New("Keine Antwort auf MQTT-Login")
			}
			if p.typ != 0x20 || len(p.body) < 2 {
				return errors.New("Unerwartete Antwort auf MQTT-Login")
			}
			switch rc := p.body[1]; {
			case rc == 4 || rc == 5:
				if c.cloud {
					// Token abgelaufen/ungueltig -> nicht endlos weiterprobieren
					o.line("ERR|AUTH|Bambu-Anmeldung abgelaufen oder ungültig - bitte neu anmelden")
					stopRetry = true
					return nil
				}
				return errors.New("MQTT-Login abgelehnt - Access Code pruefen")
			case rc != 0:
				return fmt.Errorf("MQTT-Login abgelehnt (Code %d)", rc)
			}
			conn.Write(mqttSubscribePkt(topicRep))
			if pushAt.IsZero() || time.Since(pushAt) >= 5*time.Minute {
				conn.Write(mqttPublishPkt(topicReq, pushall))
				pushAt = time.Now()
				st = newReportState() // nur bei vollem Status von vorn anfangen
			}
			st.lastOut = ""
			conn.SetDeadline(time.Time{})
			o.line("STATE|connected")

			pkts := make(chan mqttPkt, 16)
			rerr := make(chan error, 1)
			go func() {
				for {
					p, err := mqttRead(r)
					if err != nil {
						rerr <- err
						close(pkts)
						return
					}
					pkts <- p
				}
			}()
			ping := time.NewTicker(30 * time.Second)
			defer ping.Stop()
			silence := time.NewTimer(c.silenceTimeout)
			defer silence.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ping.C:
					conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
					conn.Write([]byte{0xC0, 0x00})
				case <-silence.C:
					return errors.New("Keine Daten mehr vom Drucker")
				case p, ok := <-pkts:
					if !ok {
						if ctx.Err() != nil {
							return nil
						}
						e := <-rerr
						if errors.Is(e, io.EOF) {
							return errors.New("Verbindung geschlossen")
						}
						return errors.New("Verbindung vom Drucker geschlossen")
					}
					silence.Reset(c.silenceTimeout)
					if p.typ == 0x90 && len(p.body) >= 3 && p.body[2] == 0x80 {
						return errors.New("Abo abgelehnt - Seriennummer pruefen")
					}
					if p.typ == 0x30 {
						var j map[string]any
						if json.Unmarshal(mqttPayload(p), &j) == nil && st.merge(j) {
							if line := st.statusLine(); line != st.lastOut {
								o.line(line)
								st.lastOut = line
							}
							fails = 0
						}
					}
				}
			}
		}()
		if stopRetry || ctx.Err() != nil {
			return
		}
		if err != nil {
			o.err(err.Error())
		}
		fails++
		wait := 5 << min(fails-1, 4) // 5, 10, 20, 40, 60 s
		if wait > 60 {
			wait = 60
		}
		sleepCtx(ctx, time.Duration(wait)*time.Second)
	}
}

// Licht im Heimnetz schalten: kurz anmelden, Befehl senden, abmelden
func runLightLan(ctx context.Context, o out, ip, code, serial, state string) {
	if serial == "" {
		o.err("Seriennummer fehlt")
		return
	}
	conn, err := openTLS(ctx, ip, 8883, 6*time.Second, false)
	if err != nil {
		o.err("Licht: " + err.Error())
		return
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(6 * time.Second))
	conn.Write(mqttConnectPkt("bblp", code))
	p, err := mqttRead(conn)
	if err != nil || p.typ != 0x20 || len(p.body) < 2 || p.body[1] != 0 {
		o.err("Licht: MQTT-Login abgelehnt - Access Code pruefen")
		return
	}
	cmd := `{"system":{"sequence_id":"0","command":"ledctrl","led_node":"chamber_light","led_mode":"` + state +
		`","led_on_time":500,"led_off_time":500,"loop_times":0,"interval_time":0}}`
	conn.Write(mqttPublishPkt("device/"+serial+"/request", cmd))
	sleepCtx(ctx, 300*time.Millisecond)
	conn.Write([]byte{0xE0, 0x00}) // DISCONNECT
	o.line("LIGHT|" + state)
}
