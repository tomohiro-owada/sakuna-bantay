package brand

import (
	"fmt"
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/srwiley/oksvg"
)

func rgbHex(c color.RGBA) string {
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

// fontsDir locates assets/fonts from the package dir (or BRAND_FONTS env).
func fontsDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("BRAND_FONTS"); d != "" {
		return d
	}
	return "../../../assets/fonts"
}

func loadTestFonts(t *testing.T) *Fonts {
	t.Helper()
	return LoadFonts(fontsDir(t), t.Logf)
}

func TestPaletteMatchesSpec(t *testing.T) {
	if got := rgbHex(Navy); got != NavyHex {
		t.Errorf("Navy = %s, want %s", got, NavyHex)
	}
	if got := rgbHex(Teal); got != TealHex {
		t.Errorf("Teal = %s, want %s", got, TealHex)
	}
	if got := rgbHex(TealLite); got != TealLiteHex {
		t.Errorf("TealLite = %s, want %s", got, TealLiteHex)
	}
	if got := rgbHex(Amber); got != AmberHex {
		t.Errorf("Amber = %s, want %s", got, AmberHex)
	}
	if got := rgbHex(Cream); got != CreamHex {
		t.Errorf("Cream = %s, want %s", got, CreamHex)
	}
	if got := rgbHex(White); got != WhiteHex {
		t.Errorf("White = %s, want %s", got, WhiteHex)
	}
}

func TestMarkSVGParses(t *testing.T) {
	svg := MarkSVG()
	if !strings.Contains(svg, `viewBox="0 0 512 512"`) {
		t.Error("mark SVG must use a 512x512 viewBox")
	}
	for _, want := range []string{"<circle", "<path", TealHex, AmberHex, WhiteHex} {
		if !strings.Contains(svg, want) {
			t.Errorf("mark SVG missing %q", want)
		}
	}
	if _, err := oksvg.ReadIconStream(strings.NewReader(svg)); err != nil {
		t.Fatalf("mark SVG does not parse: %v", err)
	}
	if FaviconSVG() != MarkSVG() {
		t.Error("favicon SVG must reuse the exact mark geometry")
	}
	if logo := LogoSVG(); !strings.Contains(logo, markInner) {
		t.Error("logo SVG must embed the same mark geometry")
	}
}

func TestRenderSizes(t *testing.T) {
	f := loadTestFonts(t)

	check := func(name string, w, h, wantW, wantH int) {
		t.Helper()
		if w != wantW || h != wantH {
			t.Errorf("%s = %dx%d, want %dx%d", name, w, h, wantW, wantH)
		}
	}

	prof720, err := RenderProfile(ProfileSize)
	if err != nil {
		t.Fatal(err)
	}
	check("profile-720", prof720.Bounds().Dx(), prof720.Bounds().Dy(), 720, 720)

	prof180, err := RenderProfile(ProfileSmall)
	if err != nil {
		t.Fatal(err)
	}
	check("profile-180", prof180.Bounds().Dx(), prof180.Bounds().Dy(), 180, 180)

	cover, _, err := RenderCover(f)
	if err != nil {
		t.Fatal(err)
	}
	check("cover", cover.Bounds().Dx(), cover.Bounds().Dy(), CoverW, CoverH)

	badges := []struct {
		name  string
		size  int
		cream bool
		frac  float64
	}{
		{"favicon-32", 32, false, 1.0},
		{"favicon-16", 16, false, 1.0},
		{"apple-touch-icon-180", 180, true, 0.80},
		{"icon-192", 192, true, 0.78},
		{"icon-512", 512, true, 0.78},
		{"logo-mark-96", 96, false, 1.0},
		{"logo-mark-144", 144, false, 1.0},
	}
	for _, b := range badges {
		img, err := RenderBadge(b.size, b.cream, b.frac)
		if err != nil {
			t.Fatal(err)
		}
		check(b.name, img.Bounds().Dx(), img.Bounds().Dy(), b.size, b.size)
	}

	chk, err := RenderCheckCircle()
	if err != nil {
		t.Fatal(err)
	}
	check("check-circle-48", chk.Bounds().Dx(), chk.Bounds().Dy(), CheckSize, CheckSize)

	mark, err := RenderMark(256)
	if err != nil {
		t.Fatal(err)
	}
	check("mark-256", mark.Bounds().Dx(), mark.Bounds().Dy(), 256, 256)
}

// TestCoverSafeArea asserts every content element (mark, texts, rule) lies
// inside the central safe area.
func TestCoverSafeArea(t *testing.T) {
	f := loadTestFonts(t)
	_, boxes, err := RenderCover(f)
	if err != nil {
		t.Fatal(err)
	}
	safe := SafeAreaRect()
	if len(boxes) != 5 {
		t.Fatalf("expected 5 content boxes, got %d", len(boxes))
	}
	names := []string{"mark", "title", "tagline", "rule", "disclaimer"}
	for i, b := range boxes {
		if b.Empty() {
			t.Errorf("%s box is empty", names[i])
		}
		if !b.In(safe) {
			t.Errorf("%s box %v outside safe area %v", names[i], b, safe)
		}
	}
	// The navy band must sit strictly below the safe area.
	if bandTop := CoverH - NavyBandH; bandTop <= SafeY1 {
		t.Errorf("navy band top %d intrudes into safe area (bottom %d)", bandTop, SafeY1)
	}
}

// TestProfileCenterPixel samples the profile picture to make sure the mark
// actually landed centered on cream (not a blank canvas).
func TestProfileCenterPixel(t *testing.T) {
	prof720, err := RenderProfile(ProfileSize)
	if err != nil {
		t.Fatal(err)
	}
	c := prof720.RGBAAt(ProfileSize/2, ProfileSize/2)
	if c == Cream {
		t.Error("profile center pixel is cream; mark missing")
	}
	corner := prof720.RGBAAt(4, 4)
	if corner != Cream {
		t.Errorf("profile corner = %v, want cream background", corner)
	}
}
