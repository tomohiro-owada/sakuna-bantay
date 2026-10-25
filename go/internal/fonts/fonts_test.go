package fonts

import (
	"image"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// TestEmbeddedPresent ensures every expected typeface is embedded.
func TestEmbeddedPresent(t *testing.T) {
	for _, name := range []string{
		"Montserrat-ExtraBold.ttf",
		"Montserrat-SemiBold.ttf",
		"Montserrat-Bold.ttf",
		"NotoSans-Regular.ttf",
		"NotoSans-Medium.ttf",
		"NotoSans-SemiBold.ttf",
		"NotoSans-Bold.ttf",
		"NotoSansJP-Regular.otf",
		"NotoSansJP-Bold.otf",
	} {
		if len(Embedded(name)) == 0 {
			t.Errorf("embedded font %s is empty/missing", name)
		}
	}
	if Embedded("nope.ttf") != nil {
		t.Error("Embedded(unknown) should return nil")
	}
}

// TestJapaneseFallbackRoutesToCFF verifies the CFF NotoSansJP .otf files
// actually participate in fallback: latin 'A' must resolve to a
// Montserrat/Noto face while Japanese runes must resolve to NotoSansJP.
func TestJapaneseFallbackRoutesToCFF(t *testing.T) {
	f := LoadEmbedded()
	if got := f.FaceForName(Title, 24, 'A'); strings.Contains(got, "JP") {
		t.Errorf("latin 'A' resolved to %s, want non-JP face", got)
	}
	for _, tc := range []struct {
		r    rune
		name string
	}{
		{0x6771, "東"},
		{0x4EAC, "京"},
		{0x3042, "あ"},
		{0x8A9E, "語"},
	} {
		for _, st := range []Style{Title, Semi, NotoSemi, NotoReg} {
			got := f.FaceForName(st, 24, tc.r)
			if !strings.Contains(got, "NotoSansJP") {
				t.Errorf("rune U+%04X (%s) style %d resolved to %s, want NotoSansJP", tc.r, tc.name, st, got)
			}
			if !f.Has(st, tc.r) {
				t.Errorf("Has(style %d, U+%04X) = false, want true", st, tc.r)
			}
		}
	}
	// NotoSans-Regular must NOT have the Japanese glyph (proves fallback matters).
	if f.FaceForName(NotoReg, 24, 'A') == f.FaceForName(NotoReg, 24, 0x6771) {
		t.Error("expected different faces for 'A' and U+6771 under NotoReg")
	}
}

// TestJapaneseRenders verifies the CFF .otf actually rasterizes Japanese
// glyphs with golang.org/x/image/font/opentype (not just GlyphIndex):
// drawing must produce non-background pixels and advances must be ok.
func TestJapaneseRenders(t *testing.T) {
	f := LoadEmbedded()
	dst := image.NewRGBA(image.Rect(0, 0, 400, 100))
	white := image.White
	for y := 0; y < 100; y++ {
		for x := 0; x < 400; x++ {
			dst.Set(x, y, white)
		}
	}
	w := f.DrawString(dst, "東京タワー Tokyo", Title, 40, image.Black, 10, 70)
	if w <= 0 {
		t.Fatal("DrawString returned zero width")
	}
	nonWhite := 0
	for y := 0; y < 100; y++ {
		for x := 0; x < 400; x++ {
			r, g, b, _ := dst.At(x, y).RGBA()
			if r != 0xffff || g != 0xffff || b != 0xffff {
				nonWhite++
			}
		}
	}
	if nonWhite < 500 {
		t.Errorf("only %d non-white pixels rendering Japanese text, want >= 500 (CFF not rasterizing?)", nonWhite)
	}
	// GlyphAdvance ok=true for a JP rune through the real face.
	fc := f.faceFor(Title, 40, 0x6771)
	if _, ok := fc.GlyphAdvance(0x6771); !ok {
		t.Error("GlyphAdvance(東) ok=false")
	}
	var _ font.Face = fc
	var _ = fixed.P
}

// TestMeasureAndFit exercises Measure/Metrics/FitSize, incl. mixed script.
func TestMeasureAndFit(t *testing.T) {
	f := LoadEmbedded()
	if w := f.Measure(Title, 24, "Sakuna Bantay"); w <= 0 {
		t.Error("Measure returned non-positive width")
	}
	if w := f.Measure(Title, 24, "東京タワー"); w <= 0 {
		t.Error("Measure(JP) returned non-positive width")
	}
	a, d, h := f.Metrics(Title, 24)
	if a <= 0 || d < 0 || h <= 0 {
		t.Errorf("bad metrics %d %d %d", a, d, h)
	}
	got := f.FitSize("Sakuna Bantay Weather & disaster watch", Title, 110, 20, 300)
	if got >= 110 || got < 20 {
		t.Errorf("FitSize = %g, want shrunk but >= 20", got)
	}
	if got2 := f.FitSize("Hi", Title, 24, 20, 10000); got2 != 24 {
		t.Errorf("FitSize(no shrink needed) = %g, want 24", got2)
	}
}
