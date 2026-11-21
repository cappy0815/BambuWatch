//go:build windows

// Zeichnen: alles mit GDI+ (Bilder, Text mit Transparenz, runde Formen), doppelt gepuffert.
// Koordinaten sind logische Pixel (100 % Anzeige); die Weltransformation skaliert auf die echte DPI.
package main

import (
	_ "embed"
	"encoding/binary"
	"math"
	"syscall"
	"unsafe"
)

//go:embed BambuWatch.ico
var icoBytes []byte

var (
	pGdipGraphicsClear     = gdiplus.NewProc("GdipGraphicsClear")
	pGdipResetWorld        = gdiplus.NewProc("GdipResetWorldTransform")
	pGdipResetClip         = gdiplus.NewProc("GdipResetClip")
	pGdipDrawRectangle     = gdiplus.NewProc("GdipDrawRectangle")
	pGdipDrawLine          = gdiplus.NewProc("GdipDrawLine")
	pGdipDrawPath          = gdiplus.NewProc("GdipDrawPath")
	pGdipSetCompositingQ   = gdiplus.NewProc("GdipSetCompositingQuality")
	pGdipCreateLineBrushRc = gdiplus.NewProc("GdipCreateLineBrushFromRect")
)

var (
	sc      = 1.0 // Skalierung (Windows-Anzeigeeinstellung 100 % = 1.0)
	fontAdd = 1.0 // Schrift groesser: 0..3 px (Einstellung)
)

func S(v float64) int32 { return int32(math.Round(v * sc)) }
func L(v int32) float64 { return float64(v) / sc }

// Farben ARGB (wie in 1.7)
const (
	cBG     = 0xFF1B1D21 // Fenster + Leiste
	cPanel  = 0xFF24272D // Eingabefelder, Auswahl
	cLine   = 0xFF363A42
	cTxt    = 0xFFE8EAED
	cTxt2   = 0xFF9AA0A8
	cGreen  = 0xFF00AE42 // Bambu-Gruen
	cAmber  = 0xFFF5A524
	cRed    = 0xFFE5484D
	cBlue   = 0xFF3B9EFF
	cOff    = 0xFF5A5F68
	cWhite  = 0xFFFFFFFF
	cHover  = 0xFF2C3036
	cImgBg  = 0xFF0E0F12
	cBtnDk  = 0xFF3A3F48
	glyphFn = "Segoe MDL2 Assets"
	uiFn    = "Segoe UI"
)

func f32(v float64) uintptr { return uintptr(math.Float32bits(float32(v))) }
func cref(argb uint32) uintptr {
	return uintptr((argb&0xFF)<<16 | (argb & 0xFF00) | (argb>>16)&0xFF)
}

func initGfx() {
	if pSetProcessDpiAwareCtx.Find() == nil {
		pSetProcessDpiAwareCtx.Call(^uintptr(1)) // DPI_AWARENESS_CONTEXT_SYSTEM_AWARE (-2)
	} else if pSetProcessDPIAware.Find() == nil {
		pSetProcessDPIAware.Call()
	}
	dc, _, _ := pGetDC.Call(0)
	dpi, _, _ := pGetDeviceCaps.Call(dc, 88) // LOGPIXELSX
	pReleaseDC.Call(0, dc)
	if dpi >= 96 {
		sc = float64(dpi) / 96
	}
	var tok uintptr
	in := struct {
		ver  uint32
		cb   uintptr
		sup1 int32
		sup2 int32
	}{ver: 1}
	pGdiplusStartup.Call(uintptr(unsafe.Pointer(&tok)), uintptr(unsafe.Pointer(&in)), 0)
	// Grafik nur zum Messen (logische Einheiten, ohne Skalierung)
	measDC, _, _ = pCreateCompatibleDC.Call(0)
	pGdipCreateFromHDC.Call(measDC, uintptr(unsafe.Pointer(&measG)))
	pGdipSetTextRendering.Call(measG, 5)
	pGdipScaleWorld.Call(measG, f32(sc), f32(sc), 0) // wie beim Zeichnen messen (Rasterung bei echter DPI)
}

var measDC, measG uintptr

// ---------------------------------------------------------------- Schriften und Formate (Cache)
type fontKey struct {
	fam   string
	px10  int
	style int
}

var (
	families = map[string]uintptr{}
	fonts    = map[fontKey]uintptr{}
	formats  = map[int]uintptr{}
)

// style: 0 normal, 1 fett, 2 kursiv, 4 unterstrichen
func gpFont(fam string, px float64, style int) uintptr {
	k := fontKey{fam, int(math.Round(px * 10)), style}
	if f, ok := fonts[k]; ok {
		return f
	}
	ff, ok := families[fam]
	if !ok {
		pGdipCreateFontFamily.Call(up(fam), 0, uintptr(unsafe.Pointer(&ff)))
		families[fam] = ff
	}
	if ff == 0 { // Schrift fehlt -> Segoe UI
		if fam == uiFn {
			return 0
		}
		f := gpFont(uiFn, px, style)
		fonts[k] = f
		return f
	}
	var f uintptr
	pGdipCreateFont.Call(ff, f32(px), uintptr(style), 2, uintptr(unsafe.Pointer(&f))) // UnitPixel
	fonts[k] = f
	return f
}

// align/lalign: 0 links/oben, 1 Mitte, 2 rechts/unten; wrap = Zeilenumbruch
func gpFormat(align, lalign int, wrap bool) uintptr {
	k := align*10 + lalign
	if wrap {
		k += 100
	}
	if f, ok := formats[k]; ok {
		return f
	}
	flags := uintptr(0x00004000) // NoClip
	if !wrap {
		flags |= 0x00001000 // NoWrap
	}
	var f uintptr
	pGdipCreateStringFormat.Call(flags, 0, uintptr(unsafe.Pointer(&f)))
	pGdipSetStringAlign.Call(f, uintptr(align))
	pGdipSetStringLineAlign.Call(f, uintptr(lalign))
	if !wrap {
		pGdipSetStringTrimming.Call(f, 3) // EllipsisCharacter
	} else {
		pGdipSetStringTrimming.Call(f, 1) // Character
	}
	formats[k] = f
	return f
}

type rectF struct{ x, y, w, h float32 }

func utf16s(s string) []uint16 {
	p, _ := syscall.UTF16FromString(s)
	return p
}

// Breite eines Textes (logische px, Schriftgroesse inkl. Zuschlag)
func textW(s string, px float64, style int) float64 {
	return measure(s, uiFn, px+fontAdd, style, 0)
}

// Hoehe eines umbrochenen Textes bei Breite w
func textH(s string, px float64, style int, w float64) float64 {
	return measure(s, uiFn, px+fontAdd, style, w)
}

func measure(s, fam string, px float64, style int, wrapW float64) float64 {
	if s == "" {
		return 0
	}
	p := utf16s(s)
	lay := rectF{0, 0, 4000, 4000}
	f := gpFormat(0, 0, false)
	if wrapW > 0 {
		lay.w = float32(wrapW * 0.98) // lieber eine Zeile zu viel als abgeschnitten
		f = gpFormat(0, 0, true)
	}
	var out rectF
	pGdipMeasureString.Call(measG, uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)-1), gpFont(fam, px, style),
		uintptr(unsafe.Pointer(&lay)), f, uintptr(unsafe.Pointer(&out)), 0, 0)
	if wrapW > 0 {
		return float64(out.h)
	}
	return float64(out.w)
}

// ---------------------------------------------------------------- Leinwand
type canvas struct{ g uintptr }

func newCanvas(dc uintptr) *canvas {
	var g uintptr
	pGdipCreateFromHDC.Call(dc, uintptr(unsafe.Pointer(&g)))
	pGdipSetSmoothingMode.Call(g, 4) // AntiAlias
	pGdipSetTextRendering.Call(g, 5) // ClearTypeGridFit
	pGdipSetPixelOffset.Call(g, 4)   // Half
	pGdipSetInterpolation.Call(g, 7) // HighQualityBicubic
	pGdipScaleWorld.Call(g, f32(sc), f32(sc), 0)
	return &canvas{g}
}

func (c *canvas) free() { pGdipDeleteGraphics.Call(c.g) }

// doppelt gepuffert zeichnen; w, h logisch
func paintWindow(hwnd uintptr, draw func(c *canvas, w, h float64)) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	r := clientRect(hwnd)
	if r.right > 0 && r.bottom > 0 {
		mdc, _, _ := pCreateCompatibleDC.Call(hdc)
		bmp, _, _ := pCreateCompatibleBmp.Call(hdc, uintptr(r.right), uintptr(r.bottom))
		old, _, _ := pSelectObject.Call(mdc, bmp)
		c := newCanvas(mdc)
		draw(c, L(r.right), L(r.bottom))
		c.free()
		pBitBlt.Call(hdc, 0, 0, uintptr(r.right), uintptr(r.bottom), mdc, 0, 0, 0xCC0020)
		pSelectObject.Call(mdc, old)
		pDeleteObject.Call(bmp)
		pDeleteDC.Call(mdc)
	}
	pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
}

func (c *canvas) clear(col uint32) { pGdipGraphicsClear.Call(c.g, uintptr(col)) }

func brush(col uint32) uintptr {
	var br uintptr
	pGdipCreateSolidFill.Call(uintptr(col), uintptr(unsafe.Pointer(&br)))
	return br
}

func (c *canvas) fill(x, y, w, h float64, col uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	br := brush(col)
	pGdipFillRectangle.Call(c.g, br, f32(x), f32(y), f32(w), f32(h))
	pGdipDeleteBrush.Call(br)
}

func roundPath(x, y, w, h, r float64) uintptr {
	var path uintptr
	pGdipCreatePath.Call(0, uintptr(unsafe.Pointer(&path)))
	if r*2 > h {
		r = h / 2
	}
	if r*2 > w {
		r = w / 2
	}
	d := 2 * r
	if d <= 0 {
		d = 0.01
	}
	pGdipAddPathArc.Call(path, f32(x), f32(y), f32(d), f32(d), f32(180), f32(90))
	pGdipAddPathArc.Call(path, f32(x+w-d), f32(y), f32(d), f32(d), f32(270), f32(90))
	pGdipAddPathArc.Call(path, f32(x+w-d), f32(y+h-d), f32(d), f32(d), f32(0), f32(90))
	pGdipAddPathArc.Call(path, f32(x), f32(y+h-d), f32(d), f32(d), f32(90), f32(90))
	pGdipClosePathFigure.Call(path)
	return path
}

func (c *canvas) rrect(x, y, w, h, r float64, col uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	path := roundPath(x, y, w, h, r)
	br := brush(col)
	pGdipFillPath.Call(c.g, br, path)
	pGdipDeleteBrush.Call(br)
	pGdipDeletePath.Call(path)
}

// Pillen-Knopf mit leichtem Verlauf (wie 1.7)
func (c *canvas) pill(x, y, w, h float64, label string, col uint32, hot bool) {
	if hot {
		col = lighter(col)
	}
	path := roundPath(x, y, w, h, h/2)
	var br uintptr
	rc := rectF{float32(x), float32(y - 1), float32(w), float32(h + 2)}
	pGdipCreateLineBrushRc.Call(uintptr(unsafe.Pointer(&rc)), uintptr(col), uintptr(darker(col)), 1, 0, uintptr(unsafe.Pointer(&br))) // vertikal
	if br == 0 {
		br = brush(col)
	}
	pGdipFillPath.Call(c.g, br, path)
	pGdipDeleteBrush.Call(br)
	pGdipDeletePath.Call(path)
	c.text(label, 12, 1, cWhite, x, y, w, h, 1, 1)
}

func (c *canvas) ellipse(x, y, w, h float64, col uint32) {
	br := brush(col)
	pGdipFillEllipse.Call(c.g, br, f32(x), f32(y), f32(w), f32(h))
	pGdipDeleteBrush.Call(br)
}

func (c *canvas) ellipseLine(x, y, w, h float64, col uint32, width float64) {
	var pen uintptr
	pGdipCreatePen1.Call(uintptr(col), f32(width), 2, uintptr(unsafe.Pointer(&pen)))
	pGdipDrawEllipse.Call(c.g, pen, f32(x), f32(y), f32(w), f32(h))
	pGdipDeletePen.Call(pen)
}

func (c *canvas) frame(x, y, w, h float64, col uint32) {
	var pen uintptr
	pGdipCreatePen1.Call(uintptr(col), f32(1), 2, uintptr(unsafe.Pointer(&pen)))
	pGdipDrawRectangle.Call(c.g, pen, f32(x+0.5), f32(y+0.5), f32(w-1), f32(h-1))
	pGdipDeletePen.Call(pen)
}

// Text einzeilig mit "…" bei Ueberlaenge; px ohne Zuschlag (der kommt automatisch dazu)
func (c *canvas) text(s string, px float64, style int, col uint32, x, y, w, h float64, align, lalign int) {
	c.textFam(s, uiFn, px+fontAdd, style, col, x, y, w, h, align, lalign)
}

func (c *canvas) glyph(code rune, px float64, col uint32, x, y, w, h float64) {
	c.textFam(string(code), glyphFn, px, 0, col, x, y, w, h, 1, 1)
}

func (c *canvas) textFam(s, fam string, px float64, style int, col uint32, x, y, w, h float64, align, lalign int) {
	if s == "" || w < 4 || h < 4 {
		return
	}
	p := utf16s(s)
	r := rectF{float32(x), float32(y), float32(w), float32(h)}
	br := brush(col)
	pGdipDrawString.Call(c.g, uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)-1), gpFont(fam, px, style),
		uintptr(unsafe.Pointer(&r)), gpFormat(align, lalign, false), br)
	pGdipDeleteBrush.Call(br)
}

// umbrochener Text (oben ausgerichtet)
func (c *canvas) wrap(s string, px float64, style int, col uint32, x, y, w, h float64, align int) {
	if s == "" {
		return
	}
	p := utf16s(s)
	r := rectF{float32(x), float32(y), float32(w), float32(h)}
	br := brush(col)
	pGdipDrawString.Call(c.g, uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)-1), gpFont(uiFn, px+fontAdd, style),
		uintptr(unsafe.Pointer(&r)), gpFormat(align, 0, true), br)
	pGdipDeleteBrush.Call(br)
}

func lighter(c uint32) uint32 {
	r, g, b := (c>>16)&0xFF, (c>>8)&0xFF, c&0xFF
	r, g, b = r+(255-r)/6, g+(255-g)/6, b+(255-b)/6
	return c&0xFF000000 | r<<16 | g<<8 | b
}

func darker(c uint32) uint32 {
	r, g, b := (c>>16)&0xFF, (c>>8)&0xFF, c&0xFF
	return c&0xFF000000 | (r*8/10)<<16 | (g*8/10)<<8 | b*8/10
}

// ---------------------------------------------------------------- Bilder (JPEG/PNG aus dem Speicher)
type gimg struct {
	h, stream uintptr
	w, hh     int
	data      []byte // bleibt referenziert, solange das Bild lebt
}

func loadImage(b []byte) *gimg {
	if len(b) < 32 {
		return nil
	}
	data := append([]byte(nil), b...)
	st, _, _ := pSHCreateMemStream.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	if st == 0 {
		return nil
	}
	var img uintptr
	if r, _, _ := pGdipCreateBmpStream.Call(st, uintptr(unsafe.Pointer(&img))); r != 0 || img == 0 {
		comCall(st, 2) // Release
		return nil
	}
	var w, h uint32
	pGdipGetImageWidth.Call(img, uintptr(unsafe.Pointer(&w)))
	pGdipGetImageHeight.Call(img, uintptr(unsafe.Pointer(&h)))
	if w == 0 || h == 0 {
		pGdipDisposeImage.Call(img)
		comCall(st, 2)
		return nil
	}
	return &gimg{h: img, stream: st, w: int(w), hh: int(h), data: data}
}

func (i *gimg) free() {
	if i == nil {
		return
	}
	pGdipDisposeImage.Call(i.h)
	comCall(i.stream, 2)
	i.h, i.stream = 0, 0
}

func (c *canvas) image(i *gimg, x, y, w, h float64) {
	if i == nil || i.h == 0 {
		return
	}
	pGdipDrawImageRect.Call(c.g, i.h, f32(x), f32(y), f32(w), f32(h))
}

// Bild einpassen (Seitenverhaeltnis bleibt); maxF begrenzt die Vergroesserung (0 = egal)
func (c *canvas) imageFit(i *gimg, x, y, w, h, maxF float64) {
	if i == nil || w < 2 || h < 2 {
		return
	}
	f := math.Min(w/float64(i.w), h/float64(i.hh))
	if maxF > 0 && f > maxF {
		f = maxF
	}
	dw, dh := float64(i.w)*f, float64(i.hh)*f
	c.image(i, x+(w-dw)/2, y+(h-dh)/2, dw, dh)
}

// groesstes PNG aus der ICO-Datei (fuer Hilfe-Kopf und Meldungen)
func icoPNG() []byte {
	b := icoBytes
	if len(b) < 6 {
		return nil
	}
	n := int(binary.LittleEndian.Uint16(b[4:]))
	best, bestW := []byte(nil), -1
	for i := 0; i < n && 6+16*i+16 <= len(b); i++ {
		e := b[6+16*i:]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		size, off := int(binary.LittleEndian.Uint32(e[8:])), int(binary.LittleEndian.Uint32(e[12:]))
		if off+size <= len(b) && w > bestW && w <= 128 {
			best, bestW = b[off:off+size], w
		}
	}
	return best
}
