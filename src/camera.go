// Kamerabild im Heimnetz (Logik aus BambuWatch_helper.ps1, Run-Camera*)
//
//	A1, A1 mini, P1: TLS Port 6000, 80-Byte-Anmeldung, dann je Bild 16-Byte-Kopf + JPEG
//	H2D, X1, P2S:    RTSPS Port 322, ffmpeg macht daraus 2 JPEG pro Sekunde
//
// Quellen: Community (bambu-rs / ha-bambulab), nicht offiziell von Bambu Lab.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

func camAuth(code string) []byte {
	a := make([]byte, 80)
	binary.LittleEndian.PutUint32(a[0:], 0x40)
	binary.LittleEndian.PutUint32(a[4:], 0x3000)
	copy(a[16:], "bblp")
	c := []byte(code)
	if len(c) > 32 {
		c = c[:32]
	}
	copy(a[48:], c)
	return a
}

// ein Bild lesen; nil ohne Fehler = kein JPEG (ueberspringen)
func readCamFrame(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr)
	if n < 100 || n > 8000000 {
		return nil, fmt.Errorf("Unerwarteter Bild-Header (Laenge %d) - Kamera aus oder Protokoll geaendert", n)
	}
	jpg := make([]byte, n)
	if _, err := io.ReadFull(r, jpg); err != nil {
		return nil, err
	}
	if jpg[0] != 0xFF || jpg[1] != 0xD8 {
		return nil, nil
	}
	return jpg, nil
}

type camCfg struct {
	ip, code, camType, ffmpeg string
	testRtsp                  string // nur Tests: Videodatei/URL statt Drucker
}

func runCamera(ctx context.Context, o out, c camCfg) {
	if c.ip == "" {
		o.err("IP-Adresse fehlt (Einstellungen)")
		return
	}
	if c.code == "" {
		o.err("Access Code fehlt (Einstellungen)")
		return
	}
	typ := c.camType
	if c.testRtsp != "" {
		typ = "rtsp"
	}
	if typ == "auto" {
		for ctx.Err() == nil {
			if testPort(ctx, c.ip, 6000, 2500*time.Millisecond) {
				typ = "jpeg"
				break
			}
			if testPort(ctx, c.ip, 322, 2500*time.Millisecond) {
				typ = "rtsp"
				break
			}
			o.err("Kamera nicht erreichbar (Port 6000/322) - Drucker an, LAN Mode Liveview an, gleiches Netz?")
			sleepCtx(ctx, 5*time.Second)
		}
		if ctx.Err() != nil {
			return
		}
		o.line("CAMTYPE|" + typ)
	}
	if typ == "rtsp" {
		runCameraRtsp(ctx, o, c)
	} else {
		runCameraJpeg(ctx, o, c)
	}
}

func runCameraJpeg(ctx context.Context, o out, c camCfg) {
	auth := camAuth(c.code)
	for ctx.Err() == nil {
		func() {
			o.line("STATE|connecting")
			conn, err := openTLS(ctx, c.ip, 6000, 8*time.Second, false)
			if err != nil {
				if ctx.Err() == nil {
					o.err(err.Error())
				}
				return
			}
			defer conn.Close()
			stop := closeOnCancel(ctx, conn)
			defer stop()
			conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
			if _, err := conn.Write(auth); err != nil {
				o.err("Kein Bild vom Drucker (Kamera aus, LAN-Liveview deaktiviert oder Access Code falsch?)")
				return
			}
			o.line("STATE|connected")
			for ctx.Err() == nil {
				conn.SetReadDeadline(time.Now().Add(8 * time.Second))
				jpg, err := readCamFrame(conn)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					var ne net.Error
					if errors.As(err, &ne) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(err.Error(), "closed") {
						o.err("Kein Bild vom Drucker (Kamera aus, LAN-Liveview deaktiviert oder Access Code falsch?)")
					} else {
						o.err(err.Error())
					}
					return
				}
				if jpg != nil {
					o.image("FRAME", jpg)
				}
			}
		}()
		sleepCtx(ctx, 3*time.Second)
	}
}

// JPEG-Grenzen im ffmpeg-Datenstrom (FF D8 ... FF D9)
func splitJpegs(data []byte, emit func([]byte)) (rest []byte) {
	pos := 0
	for {
		s := bytes.Index(data[pos:], []byte{0xFF, 0xD8})
		if s < 0 {
			if len(data) > 0 {
				return append([]byte(nil), data[len(data)-1:]...) // ein FF am Ende koennte ein Anfang sein
			}
			return nil
		}
		s += pos
		e := bytes.Index(data[s+2:], []byte{0xFF, 0xD9})
		if e < 0 {
			return append([]byte(nil), data[s:]...)
		}
		e += s + 2
		emit(append([]byte(nil), data[s:e+2]...))
		pos = e + 2
	}
}

var (
	reFf401     = regexp.MustCompile(`(?i)401|unauthorized`)
	reFfRefused = regexp.MustCompile(`(?i)refused|timed out|timeout`)
)

func runCameraRtsp(ctx context.Context, o out, c camCfg) {
	if c.ffmpeg == "" {
		o.line("ERR|STOP|Dieser Drucker sendet einen Videostream - dafür ffmpeg.exe in den Einstellungen angeben")
		return
	}
	if _, err := os.Stat(c.ffmpeg); err != nil {
		o.line("ERR|STOP|Dieser Drucker sendet einen Videostream - dafür ffmpeg.exe in den Einstellungen angeben")
		return
	}
	var inArgs []string
	if c.testRtsp != "" {
		inArgs = []string{"-re", "-stream_loop", "-1", "-i", c.testRtsp}
	} else {
		u := "rtsps://bblp:" + escapeData(c.code) + "@" + c.ip + ":322/streaming/live/1"
		// -tls_verify 0: selbstsigniertes Zertifikat; neuere ffmpeg-Versionen pruefen sonst (0x80090325)
		inArgs = []string{"-rtsp_transport", "tcp", "-tls_verify", "0", "-i", u}
	}
	args := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, inArgs...)
	args = append(args, "-an", "-vf", "fps=2", "-q:v", "5", "-f", "mjpeg", "pipe:1")
	for ctx.Err() == nil {
		func() {
			o.line("STATE|connecting")
			cmd := exec.CommandContext(ctx, c.ffmpeg, args...)
			hideWindow(cmd)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				o.err(err.Error())
				return
			}
			var stderr bytes.Buffer
			cmd.Stderr = &limitedBuf{b: &stderr, max: 16384}
			if err := cmd.Start(); err != nil {
				o.err("ffmpeg startet nicht: " + err.Error())
				return
			}
			defer func() {
				if cmd.ProcessState == nil {
					cmd.Process.Kill()
					cmd.Wait()
				}
			}()
			// Daten lesen; ohne Bild 20 s -> Abbruch
			chunks := make(chan []byte, 8)
			go func() {
				defer close(chunks)
				buf := make([]byte, 65536)
				for {
					n, err := stdout.Read(buf)
					if n > 0 {
						chunks <- append([]byte(nil), buf[:n]...)
					}
					if err != nil {
						return
					}
				}
			}()
			var pending []byte
			first := true
			timer := time.NewTimer(20 * time.Second)
			defer timer.Stop()
		read:
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					o.err("Kein Bild aus dem Videostream (20 s) - LAN Mode Liveview an?")
					return
				case b, ok := <-chunks:
					if !ok {
						break read
					}
					timer.Reset(20 * time.Second)
					pending = splitJpegs(append(pending, b...), func(j []byte) {
						if first {
							o.line("STATE|connected")
							first = false
						}
						o.image("FRAME", j)
					})
				}
			}
			// ffmpeg beendet: Grund aus der Fehlerausgabe melden
			done := make(chan struct{})
			go func() { cmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				cmd.Process.Kill()
				<-done
			}
			et := ""
			for _, l := range strings.Split(stderr.String(), "\n") {
				if strings.TrimSpace(l) != "" {
					et = strings.TrimSpace(l)
				}
			}
			switch {
			case reFf401.MatchString(et):
				et = "Videostream: Access Code falsch?"
			case reFfRefused.MatchString(et):
				et = "Videostream nicht erreichbar - LAN Mode Liveview an?"
			}
			if et != "" {
				et = ": " + et
			}
			o.err("ffmpeg beendet" + et)
		}()
		sleepCtx(ctx, 5*time.Second)
	}
}

type limitedBuf struct {
	b   *bytes.Buffer
	max int
}

func (l *limitedBuf) Write(p []byte) (int, error) {
	if l.b.Len() > l.max {
		l.b.Reset()
	}
	return l.b.Write(p)
}

// wie .NET Uri.EscapeDataString (Leerzeichen -> %20, nicht "+")
func escapeData(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
