// Package fonts embeds the Sakuna Bantay typefaces (copies of
// assets/fonts/, OFL texts included) and provides per-rune fallback
// text measurement/drawing used by brand and card renderers.
//
// CFF verification (2026-10-25, golang.org/x/image v0.47.0):
//
//	NotoSansJP-Regular.otf and NotoSansJP-Bold.otf are CFF-flavoured
//	OpenType (.otf). Older x/image versions could not rasterize CFF
//	outlines, but v0.47.0 CAN: opentype.Parse + opentype.NewFace succeed,
//	sfnt.GlyphIndex returns non-zero for U+6771 (東), U+4EAC (京) and
//	U+3042 (あ), GlyphAdvance reports ok=true, and drawing "東京タワー"
//	onto an RGBA canvas produces thousands of non-background pixels
//	(see TestJapaneseRenders). No fallback font is needed; Noto Sans JP
//	is used directly as the Japanese fallback in every style chain.
//
// If a future x/image upgrade breaks CFF rendering, the fallback plan is:
// ship a TrueType Japanese font (e.g. Noto Sans JP Hinted .ttf) under a
// different filename and prefer it in the Title/Semi/Noto chains, keeping
// the CFF .otf files only for the OFL record. The Has() + faceFor()
// logic below already skips undrawable faces, so adding a TTF fallback
// requires only an embed line and a chain entry.
package fonts

import (
	"embed"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

//go:embed Montserrat-ExtraBold.ttf
var montserratExtraBold []byte

//go:embed Montserrat-SemiBold.ttf
var montserratSemiBold []byte

//go:embed Montserrat-Bold.ttf
var montserratBold []byte

//go:embed NotoSans-Regular.ttf
var notoRegular []byte

//go:embed NotoSans-Medium.ttf
var notoMedium []byte

//go:embed NotoSans-SemiBold.ttf
var notoSemiBold []byte

//go:embed NotoSans-Bold.ttf
var notoBold []byte

//go:embed NotoSansJP-Regular.otf
var notoJPRegular []byte

//go:embed NotoSansJP-Bold.otf
var notoJPBold []byte

//go:embed OFL.txt NotoSans-OFL.txt
var licenses embed.FS

// Embedded returns the raw bytes of an embedded font file by base name,
// e.g. Embedded("NotoSans-Regular.ttf").
func Embedded(name string) []byte {
	switch name {
	case "Montserrat-ExtraBold.ttf":
		return montserratExtraBold
	case "Montserrat-SemiBold.ttf":
		return montserratSemiBold
	case "Montserrat-Bold.ttf":
		return montserratBold
	case "NotoSans-Regular.ttf":
		return notoRegular
	case "NotoSans-Medium.ttf":
		return notoMedium
	case "NotoSans-SemiBold.ttf":
		return notoSemiBold
	case "NotoSans-Bold.ttf":
		return notoBold
	case "NotoSansJP-Regular.otf":
		return notoJPRegular
	case "NotoSansJP-Bold.otf":
		return notoJPBold
	}
	return nil
}

// Licenses returns the embedded OFL license filesystem.
func Licenses() embed.FS { return licenses }

// Text styles. Title/tagline use Montserrat statics; the disclaimer uses
// Noto Sans; per-rune fallback covers glyphs the primary lacks.
type Style int

const (
	Title    Style = iota // Montserrat ExtraBold
	Semi                  // Montserrat SemiBold
	NotoSemi              // Noto Sans SemiBold
	NotoReg               // Noto Sans Regular
)

// faceSet is one usable font in a fallback chain.
type faceSet struct {
	ot   *opentype.Font
	sf   *sfnt.Font
	name string
}

// Fonts holds the loaded typefaces plus a face cache.
// Sizes are in pixels: faces are built at 72 DPI so 1pt == 1px.
type Fonts struct {
	sets  map[Style][]*faceSet
	faces map[string]font.Face
	mu    sync.Mutex
}

func mustParse(name string, b []byte) (*opentype.Font, *sfnt.Font) {
	sf, err := sfnt.Parse(b)
	if err != nil {
		panic(fmt.Sprintf("fonts: parse %s: %v", name, err))
	}
	ot, err := opentype.Parse(b)
	if err != nil {
		panic(fmt.Sprintf("fonts: parse %s: %v", name, err))
	}
	return ot, sf
}

// drawable reports whether the font can actually produce a face.
// CFF-flavoured files that x/image cannot rasterize are skipped here
// instead of failing the whole load.
func drawable(ot *opentype.Font) bool {
	_, err := opentype.NewFace(ot, &opentype.FaceOptions{Size: 12, DPI: 72, Hinting: font.HintingFull})
	return err == nil
}

func setFromBytes(name string, b []byte) *faceSet {
	if len(b) == 0 {
		panic(fmt.Sprintf("fonts: missing embedded font %s", name))
	}
	ot, sf := mustParse(name, b)
	if !drawable(ot) {
		return nil
	}
	return &faceSet{ot: ot, sf: sf, name: name}
}

func chain(ss ...*faceSet) []*faceSet {
	var out []*faceSet
	for _, s := range ss {
		if s != nil {
			out = append(out, s)
		}
	}
	return out
}

// LoadEmbedded loads the typefaces from the embedded copies.
func LoadEmbedded() *Fonts {
	montXB := setFromBytes("Montserrat-ExtraBold.ttf", montserratExtraBold)
	montSB := setFromBytes("Montserrat-SemiBold.ttf", montserratSemiBold)
	montBold := setFromBytes("Montserrat-Bold.ttf", montserratBold)
	_ = montBold
	notoB := setFromBytes("NotoSans-Bold.ttf", notoBold)
	notoSB := setFromBytes("NotoSans-SemiBold.ttf", notoSemiBold)
	notoMed := setFromBytes("NotoSans-Medium.ttf", notoMedium)
	notoReg := setFromBytes("NotoSans-Regular.ttf", notoRegular)
	jpBold := setFromBytes("NotoSansJP-Bold.otf", notoJPBold)
	jpReg := setFromBytes("NotoSansJP-Regular.otf", notoJPRegular)

	if montXB == nil || montSB == nil || notoSB == nil || notoReg == nil {
		panic("fonts: missing required drawable fonts (Montserrat-ExtraBold/SemiBold, NotoSans-SemiBold/Regular)")
	}
	return &Fonts{
		sets: map[Style][]*faceSet{
			Title:    chain(montXB, notoB, notoSB, notoReg, jpBold, jpReg),
			Semi:     chain(montSB, notoSB, notoReg, jpBold, jpReg),
			NotoSemi: chain(notoSB, notoMed, notoReg, notoB, jpBold, jpReg),
			NotoReg:  chain(notoReg, notoMed, notoSB, jpReg, jpBold),
		},
		faces: map[string]font.Face{},
	}
}

// LoadFromDir loads the typefaces from a directory (e.g. assets/fonts).
// It mirrors LoadEmbedded but reads files from disk; undrawable faces
// (e.g. CFF on old x/image) are skipped. verbose may be nil.
func LoadFromDir(dir string, verbose func(string, ...any)) *Fonts {
	if verbose == nil {
		verbose = func(string, ...any) {}
	}
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			panic(fmt.Sprintf("fonts: read %s: %v", name, err))
		}
		return b
	}
	mk := func(name string) *faceSet {
		b := read(name)
		ot, sf := mustParse(name, b)
		if !drawable(ot) {
			verbose("fonts: font %s not drawable, skipping", name)
			return nil
		}
		return &faceSet{ot: ot, sf: sf, name: name}
	}
	montXB := mk("Montserrat-ExtraBold.ttf")
	montSB := mk("Montserrat-SemiBold.ttf")
	montBold := mk("Montserrat-Bold.ttf")
	_ = montBold
	notoB := mk("NotoSans-Bold.ttf")
	notoSB := mk("NotoSans-SemiBold.ttf")
	notoMed := mk("NotoSans-Medium.ttf")
	notoReg := mk("NotoSans-Regular.ttf")
	jpBold := mk("NotoSansJP-Bold.otf")
	jpReg := mk("NotoSansJP-Regular.otf")
	if montXB == nil || montSB == nil || notoSB == nil || notoReg == nil {
		panic("fonts: missing required drawable fonts (Montserrat-ExtraBold/SemiBold, NotoSans-SemiBold/Regular)")
	}
	return &Fonts{
		sets: map[Style][]*faceSet{
			Title:    chain(montXB, notoB, notoSB, notoReg, jpBold, jpReg),
			Semi:     chain(montSB, notoSB, notoReg, jpBold, jpReg),
			NotoSemi: chain(notoSB, notoMed, notoReg, notoB, jpBold, jpReg),
			NotoReg:  chain(notoReg, notoMed, notoSB, jpReg, jpBold),
		},
		faces: map[string]font.Face{},
	}
}

// Has reports whether the embedded font name contains a glyph for r.
// Exported for tests and debugging.
func (f *Fonts) Has(style Style, r rune) bool {
	for _, fs := range f.sets[style] {
		if has(fs.sf, r) {
			return true
		}
	}
	return false
}

// FaceForName returns the chain entry name used for rune r under style.
// Exported for tests: lets callers assert Japanese runes resolve to the
// NotoSansJP files (proving the CFF fonts participate in fallback).
func (f *Fonts) FaceForName(s Style, size float64, r rune) string {
	var picked *faceSet
	for _, fs := range f.sets[s] {
		if has(fs.sf, r) {
			picked = fs
			break
		}
	}
	if picked == nil {
		picked = f.sets[s][len(f.sets[s])-1]
	}
	return picked.name
}

func has(sf *sfnt.Font, r rune) bool {
	var buf sfnt.Buffer
	idx, err := sf.GlyphIndex(&buf, r)
	if err != nil {
		return false
	}
	return idx != 0
}

// faceFor returns the drawing face for rune r under style s at size px:
// first chain entry with the glyph wins (no tofu).
func (f *Fonts) faceFor(s Style, size float64, r rune) font.Face {
	var picked *faceSet
	for _, fs := range f.sets[s] {
		if has(fs.sf, r) {
			picked = fs
			break
		}
	}
	if picked == nil {
		picked = f.sets[s][len(f.sets[s])-1]
	}
	key := fmt.Sprintf("%s-%g", picked.name, size)
	f.mu.Lock()
	defer f.mu.Unlock()
	if fc, ok := f.faces[key]; ok {
		return fc
	}
	fc, err := opentype.NewFace(picked.ot, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		panic(fmt.Sprintf("fonts: face %s %g: %v", picked.name, size, err))
	}
	f.faces[key] = fc
	return fc
}

// Measure returns the pixel width of str in style s at size px.
func (f *Fonts) Measure(s Style, size float64, str string) int {
	var adv fixed.Int26_6
	var prev rune
	var prevFace font.Face
	for i, r := range str {
		fc := f.faceFor(s, size, r)
		if i > 0 && prevFace == fc {
			adv += fc.Kern(prev, r)
		}
		a, _ := fc.GlyphAdvance(r)
		adv += a
		prev, prevFace = r, fc
	}
	return adv.Ceil()
}

// Metrics returns ascent/descent/line-height in pixels for style at size.
func (f *Fonts) Metrics(s Style, size float64) (ascent, descent, lineH int) {
	fc := f.faceFor(s, size, 'M')
	m := fc.Metrics()
	ascent = m.Ascent.Ceil()
	descent = m.Descent.Ceil()
	lineH = ascent + descent
	return ascent, descent, lineH
}

// DrawString draws str onto dst with the baseline at (x, y), switching faces
// per rune when the primary typeface lacks the glyph. Returns pixel width.
func (f *Fonts) DrawString(dst *image.RGBA, str string, s Style, size float64, col color.Color, x, y int) int {
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(col)}
	dot := fixed.P(x, y)
	var prev rune
	var prevFace font.Face
	start := dot.X
	for i, r := range str {
		fc := f.faceFor(s, size, r)
		if i > 0 && prevFace == fc {
			dot.X += fc.Kern(prev, r)
		}
		d.Face = fc
		d.Dot = dot
		d.DrawString(string(r))
		a, _ := fc.GlyphAdvance(r)
		dot.X += a
		prev, prevFace = r, fc
	}
	return (dot.X - start).Ceil()
}

// FitSize shrinks size until str fits maxW (down to minSize).
func (f *Fonts) FitSize(str string, s Style, size, minSize float64, maxW int) float64 {
	for size > minSize && f.Measure(s, size, str) > maxW {
		size -= 2
	}
	return size
}
