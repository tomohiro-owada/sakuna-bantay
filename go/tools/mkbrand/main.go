// Command mkbrand renders all Sakuna Bantay brand deliverables (see
// docs/BRAND_SPEC.md) into the brand/ directory at the repo root.
//
// Usage:
//
//	mkbrand -out ../brand -fonts ../assets/fonts
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/brand"
)

func writePNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		log.Fatalf("encode %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		log.Fatalf("close %s: %v", path, err)
	}
	fmt.Printf("wrote %s (%dx%d)\n", path, img.Bounds().Dx(), img.Bounds().Dy())
}

func writeFile(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", path, len(content))
}

func must(img *image.RGBA, err error) *image.RGBA {
	if err != nil {
		log.Fatal(err)
	}
	return img
}

func main() {
	out := flag.String("out", "../brand", "output directory for brand deliverables")
	fontsDir := flag.String("fonts", "../assets/fonts", "directory with brand fonts")
	flag.Parse()

	log.SetFlags(0)
	verbose := func(format string, args ...any) { log.Printf(format, args...) }

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", *out, err)
	}
	favDir := filepath.Join(*out, "favicon")
	if err := os.MkdirAll(favDir, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", favDir, err)
	}

	fonts := brand.LoadFonts(*fontsDir, verbose)

	// 1. Mark SVG (geometry defined once in the brand package).
	writeFile(filepath.Join(*out, "sakuna-bantay-mark.svg"), brand.MarkSVG())

	// 2. Horizontal lockup logo SVG.
	writeFile(filepath.Join(*out, "sakuna-bantay-logo.svg"), brand.LogoSVG())

	// 3. FB profile pictures.
	writePNG(filepath.Join(*out, "profile-720.png"), must(brand.RenderProfile(brand.ProfileSize)))
	writePNG(filepath.Join(*out, "profile-180.png"), must(brand.RenderProfile(brand.ProfileSmall)))

	// 4. FB cover.
	cover, _, err := brand.RenderCover(fonts)
	if err != nil {
		log.Fatal(err)
	}
	writePNG(filepath.Join(*out, "cover-1640x924.png"), cover)

	// 5. Site favicons.
	writeFile(filepath.Join(favDir, "favicon.svg"), brand.FaviconSVG())
	badge := func(name string, size int, cream bool, frac float64) {
		writePNG(filepath.Join(favDir, name), must(brand.RenderBadge(size, cream, frac)))
	}
	badge("favicon-32.png", 32, false, 1.0)
	badge("favicon-16.png", 16, false, 1.0)
	badge("apple-touch-icon-180.png", 180, true, 0.80)
	badge("icon-192.png", 192, true, 0.78)
	badge("icon-512.png", 512, true, 0.78)
	badge("logo-mark-96.png", 96, false, 1.0)
	badge("logo-mark-144.png", 144, false, 1.0)

	// 6. Small-size legibility check.
	writePNG(filepath.Join(*out, "check-circle-48.png"), must(brand.RenderCheckCircle()))
}
