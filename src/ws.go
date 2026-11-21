// Kleiner WebSocket-Client (RFC 6455) fuer die Home-Assistant-API – die Go-Standardbibliothek
// hat keinen. Nur was gebraucht wird: Textnachrichten, Fragmente, Ping/Pong, Close.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type wsConn struct {
	c   net.Conn
	r   *bufio.Reader
	wmu sync.Mutex
}

// base = http(s)://host[:port][/pfad], path z. B. "/api/websocket"
func wsDial(ctx context.Context, base, path string, insecure bool, timeout time.Duration) (*wsConn, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + path)
	if err != nil || u.Host == "" {
		return nil, errors.New("Adresse ungültig")
	}
	secure := u.Scheme == "https" || u.Scheme == "wss"
	host := u.Host
	if u.Port() == "" {
		if secure {
			host = net.JoinHostPort(u.Hostname(), "443")
		} else {
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	d := net.Dialer{}
	raw, err := d.DialContext(dctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	c := raw
	if secure {
		tc := tls.Client(raw, &tls.Config{ServerName: u.Hostname(), InsecureSkipVerify: insecure})
		if err := tc.HandshakeContext(dctx); err != nil {
			raw.Close()
			return nil, err
		}
		c = tc
	}
	c.SetDeadline(time.Now().Add(timeout))
	key := make([]byte, 16)
	rand.Read(key)
	k := base64.StdEncoding.EncodeToString(key)
	req := "GET " + u.RequestURI() + " HTTP/1.1\r\nHost: " + u.Host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + k + "\r\nSec-WebSocket-Version: 13\r\nUser-Agent: BambuWatch\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		c.Close()
		return nil, err
	}
	r := bufio.NewReaderSize(c, 65536)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		c.Close()
		return nil, err
	}
	if resp.StatusCode != 101 {
		c.Close()
		return nil, fmt.Errorf("HTTP %d statt WebSocket", resp.StatusCode)
	}
	h := sha1.Sum([]byte(k + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(h[:]) {
		c.Close()
		return nil, errors.New("WebSocket-Antwort ungültig")
	}
	c.SetDeadline(time.Time{})
	return &wsConn{c: c, r: r}, nil
}

func (w *wsConn) Close() error { return w.c.Close() }

func (w *wsConn) writeFrame(op byte, data []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	h := []byte{0x80 | op}
	n := len(data)
	switch {
	case n < 126:
		h = append(h, 0x80|byte(n))
	case n < 65536:
		h = append(h, 0x80|126, byte(n>>8), byte(n))
	default:
		h = append(h, 0x80|127)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(n))
		h = append(h, b[:]...)
	}
	var mask [4]byte
	rand.Read(mask[:])
	h = append(h, mask[:]...)
	p := make([]byte, n)
	for i := range data {
		p[i] = data[i] ^ mask[i%4]
	}
	w.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := w.c.Write(append(h, p...))
	return err
}

func (w *wsConn) Send(s string) error { return w.writeFrame(1, []byte(s)) }

// naechste vollstaendige Text-/Binaernachricht (Ping wird beantwortet)
func (w *wsConn) Recv() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0F
		n := uint64(h[1] & 0x7F)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > 64<<20 {
			return nil, errors.New("WebSocket-Nachricht zu gross")
		}
		var mask []byte
		if h[1]&0x80 != 0 {
			mask = make([]byte, 4)
			if _, err := io.ReadFull(w.r, mask); err != nil {
				return nil, err
			}
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(w.r, p); err != nil {
			return nil, err
		}
		if mask != nil {
			for i := range p {
				p[i] ^= mask[i%4]
			}
		}
		switch op {
		case 8:
			return nil, errors.New("Home Assistant hat die Verbindung geschlossen")
		case 9:
			w.writeFrame(10, p)
			continue
		case 10:
			continue
		}
		msg = append(msg, p...)
		if fin {
			return msg, nil
		}
	}
}
