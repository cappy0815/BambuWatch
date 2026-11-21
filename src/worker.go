// Arbeits-Goroutinen statt der PowerShell-Helfer aus der AutoIt-Version.
// Jede Verbindung laeuft in einer eigenen Goroutine und meldet sich mit denselben Zeilen wie
// der alte Helfer ("STATE|connecting", "STATUS|k=v|…", "ERR|text", "ERR|STOP|text" …).
// Bilder kommen als Bytes mit. Die Oberflaeche holt die Meldungen im eigenen Thread ab.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type event struct {
	w    *worker
	line string
	img  []byte
	end  bool // Worker ist beendet (entspricht "Helfer-Prozess beendet")
}

type worker struct {
	kind   string
	cancel context.CancelFunc
	start  time.Time
}

var (
	evMu sync.Mutex
	evQ  []event
	// wake weckt die Oberflaeche (Windows: PostMessage). Wird von main gesetzt.
	wake = func() {}
)

func postEvent(e event) {
	evMu.Lock()
	// Bild-Stau vermeiden: ein noch nicht abgeholtes Bild desselben Workers wird ersetzt
	if e.img != nil && strings.HasPrefix(e.line, "FRAME") {
		for i := range evQ {
			if evQ[i].w == e.w && evQ[i].img != nil && strings.HasPrefix(evQ[i].line, "FRAME") {
				evQ[i] = e
				evMu.Unlock()
				return
			}
		}
	}
	evQ = append(evQ, e)
	first := len(evQ) == 1
	evMu.Unlock()
	if first {
		wake()
	}
}

func drainEvents() []event {
	evMu.Lock()
	q := evQ
	evQ = nil
	evMu.Unlock()
	return q
}

// Ausgabe eines Workers; nach stop() wird nichts mehr gemeldet
type out struct {
	w   *worker
	ctx context.Context
}

func (o out) line(s string) {
	if o.ctx.Err() == nil {
		postEvent(event{w: o.w, line: s})
	}
}

func (o out) err(s string) { o.line("ERR|" + clean(s)) }

func (o out) image(prefix string, b []byte) {
	if o.ctx.Err() == nil {
		postEvent(event{w: o.w, line: prefix, img: b})
	}
}

func startWorker(kind string, fn func(ctx context.Context, o out)) *worker {
	ctx, cancel := context.WithCancel(context.Background())
	w := &worker{kind: kind, cancel: cancel, start: time.Now()}
	o := out{w: w, ctx: ctx}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				o.err(fmt.Sprint("interner Fehler: ", r))
			}
			postEvent(event{w: w, end: true})
		}()
		fn(ctx, o)
	}()
	return w
}

func (w *worker) stop() {
	if w != nil {
		w.cancel()
	}
}

// Wartet d oder bis zum Abbruch; false = abgebrochen
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Text fuer Zeilen: keine Zeilenumbrueche und kein "|"
func clean(v any) string {
	if v == nil {
		return ""
	}
	var s string
	switch t := v.(type) {
	case string:
		s = t
	case float64:
		s = strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			s = "True"
		} else {
			s = "False"
		}
	default:
		s = fmt.Sprint(t)
	}
	s = strings.NewReplacer("\r", " ", "\n", " ", "|", " ").Replace(s)
	return strings.TrimSpace(s)
}

// ---------------------------------------------------------------- Netz
// Schliesst die Verbindung, sobald der Worker gestoppt wird. Rueckgabe: Ueberwachung beenden
// (die Verbindung bleibt dabei offen).
func closeOnCancel(ctx context.Context, c net.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// Cipher-Suiten fuer die Drucker: Go laesst RSA-Schluesselaustausch seit 1.22 standardmaessig weg,
// aeltere Firmware koennte ihn brauchen (ungeprueft) -> ausdruecklich erlauben.
var printerSuites = func() []uint16 {
	var s []uint16
	for _, c := range tls.CipherSuites() {
		s = append(s, c.ID)
	}
	return s
}()

// TLS-Verbindung wie Open-Tls im Helfer. verify = Zertifikat pruefen (Bambu-Cloud), sonst
// alles annehmen (Drucker mit selbstsigniertem Zertifikat).
func openTLS(ctx context.Context, h string, port int, timeout time.Duration, verify bool) (*tls.Conn, error) {
	addr := net.JoinHostPort(h, strconv.Itoa(port))
	d := net.Dialer{Timeout: timeout}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		var dns *net.DNSError
		var ne net.Error
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.As(err, &dns):
			return nil, fmt.Errorf("Adresse %s nicht gefunden (Internet/DNS?)", h)
		case errors.As(err, &ne) && ne.Timeout():
			if verify {
				return nil, fmt.Errorf("Keine Antwort von %s (Internet?)", addr)
			}
			return nil, fmt.Errorf("Keine Antwort von %s (Drucker an, IP richtig, gleiches Netz?)", addr)
		}
		if verify {
			return nil, fmt.Errorf("Verbindung zu %s fehlgeschlagen (Internet?)", addr)
		}
		return nil, fmt.Errorf("Verbindung zu %s abgelehnt (Dienst am Drucker aus?)", addr)
	}
	if tc, ok := raw.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	cfg := &tls.Config{ServerName: h, MinVersion: tls.VersionTLS12}
	if !verify {
		cfg.InsecureSkipVerify = true
		cfg.CipherSuites = printerSuites
		if net.ParseIP(h) != nil {
			cfg.ServerName = "" // IP-Adresse: kein SNI
		}
	}
	c := tls.Client(raw, cfg)
	c.SetDeadline(time.Now().Add(timeout))
	stop := closeOnCancel(ctx, raw)
	err = c.HandshakeContext(ctx)
	stop()
	if err != nil {
		raw.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("TLS-Handshake mit %s fehlgeschlagen: %v", addr, err)
	}
	c.SetDeadline(time.Time{})
	return c, nil
}

// Port offen? (Test-Port im Helfer)
func testPort(ctx context.Context, h string, port int, timeout time.Duration) bool {
	d := net.Dialer{Timeout: timeout}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(h, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	c.Close()
	return true
}
