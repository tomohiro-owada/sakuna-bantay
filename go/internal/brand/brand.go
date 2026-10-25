// Package brand implements the Sakuna Bantay brand spec (docs/BRAND_SPEC.md).
//
// It owns the palette constants, the logo-mark geometry (defined once as SVG
// markup in a 512x512 viewBox and reused for the SVG files, favicons and PNG
// rendering), and all render functions used by the go/tools/mkbrand tool.
package brand

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	xdraw "golang.org/x/image/draw"
	stdraw "image/draw"
)

// Palette (exact constants from the brand spec).
var (
	Navy     = color.RGBA{0x0E, 0x2A, 0x3B, 0xFF} // text, deep background
	Teal     = color.RGBA{0x0F, 0x6B, 0x6B, 0xFF} // primary
	TealLite = color.RGBA{0x2A, 0x9D, 0x8F, 0xFF} // secondary accents
	Amber    = color.RGBA{0xF2, 0xA5, 0x16, 0xFF} // alert accent, use sparingly
	Cream    = color.RGBA{0xF7, 0xF2, 0xE6, 0xFF} // light background
	White    = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

// Hex strings of the palette for SVG markup.
const (
	NavyHex     = "#0E2A3B"
	TealHex     = "#0F6B6B"
	TealLiteHex = "#2A9D8F"
	AmberHex    = "#F2A516"
	CreamHex    = "#F7F2E6"
	WhiteHex    = "#FFFFFF"
	// WaveLiteHex is a light teal tint for the lower wave stroke. The spec
	// secondary TealLite (#2A9D8F) is too low-contrast on the teal disc at
	// small sizes, so the wave uses this lighter tint instead.
	WaveLiteHex = "#8FD3C8"
	// DiscGradBottom is the slightly-darker-toward-navy foot of the mark disc.
	DiscGradBottom = "#0C3B4E"
)

// Canvas sizes from the spec.
const (
	MarkViewBox  = 512
	ProfileSize  = 720
	ProfileSmall = 180
	CoverW       = 1640
	CoverH       = 924
	CheckSize    = 48
)

// Cover safe area: all text and the mark must live inside it. The desktop
// profile picture overlaps bottom-left and mobile crops the sides, so the
// top/bottom strips and side margins are decoration/background only.
const (
	SafeX0  = 220
	SafeX1  = 1420
	SafeY0  = 150
	SafeY1  = 774
	CoverCX = 820
	CoverCY = 462
)

// SafeAreaRect returns the cover safe-area rectangle.
func SafeAreaRect() image.Rectangle {
	return image.Rect(SafeX0, SafeY0, SafeX1, SafeY1)
}

// markInner is the mark geometry, defined ONCE. Everything (mark SVG, logo
// lockup, favicon SVG, PNG rasterization) reuses this markup so the geometry
// cannot drift between deliverables.
//
// Concept: "watching the sky and the water" inside a circle — a teal disc, a
// big simple white rounded cloud (ONE closed path: flat bottom, fully rounded
// left/right ends, three lobes), two thick wave lines (water/flood) and an
// amber radar double-arc (bantay = watch) at the cloud's upper right. Thick
// shapes so it survives 40px.
const markInner = `<defs>` +
	`<linearGradient id="disc" x1="0" y1="0" x2="0" y2="1">` +
	`<stop offset="0" stop-color="` + TealHex + `"/>` +
	`<stop offset="1" stop-color="` + DiscGradBottom + `"/>` +
	`</linearGradient>` +
	`<clipPath id="clip"><circle cx="256" cy="256" r="250"/></clipPath>` +
	`</defs>` +
	`<circle cx="256" cy="256" r="256" fill="url(#disc)"/>` +
	`<g clip-path="url(#clip)">` +
	`<path d="M178 330 L334 330 A66 66 0 0 0 340.2 198.3 A86 86 0 0 0 170 214.6 A58 58 0 0 0 178 330 Z" fill="` + WhiteHex + `"/>` +
	`<path d="M120 365 Q165 341 210 365 T300 365 T390 365" fill="none" stroke="` + WhiteHex + `" stroke-width="44" stroke-linecap="round"/>` +
	`<path d="M130 420 Q175 396 220 420 T310 420 T400 420" fill="none" stroke="` + WaveLiteHex + `" stroke-width="40" stroke-linecap="round"/>` +
	`<g fill="none" stroke="` + AmberHex + `" stroke-linecap="round">` +
	`<path d="M386.4 122.3 A46 46 0 0 1 386.4 197.7" stroke-width="34"/>` +
	`<path d="M405.9 94.5 A80 80 0 0 1 405.9 225.5" stroke-width="34"/>` +
	`</g>` +
	`<circle cx="360" cy="160" r="20" fill="` + AmberHex + `"/>` +
	`</g>`

// MarkSVG returns the standalone mark SVG (512 viewBox, no text).
func MarkSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" width="512" height="512" role="img" aria-label="Sakuna Bantay mark">` + markInner + `</svg>`
}

// FaviconSVG returns the site favicon SVG (same geometry as the mark).
func FaviconSVG() string {
	return MarkSVG()
}

// Wordmark and taglines from the spec.
const (
	Wordmark        = "Sakuna Bantay"
	LogoTagline     = "Weather & disaster watch · Las Piñas"
	CoverTagline    = "Weather & disaster watch · Las Piñas & South Metro"
	CoverDisclaimer = "Unofficial · Non-profit · Open data"
)

// LogoSVG returns the horizontal lockup: mark + wordmark + tagline.
// SVG text is fine here per the spec.
func LogoSVG() string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1400 360" width="1400" height="360" role="img" aria-label="Sakuna Bantay logo">`)
	b.WriteString(`<g transform="translate(16,16) scale(0.640625)">`)
	b.WriteString(markInner)
	b.WriteString(`</g>`)
	b.WriteString(`<text x="384" y="182" font-family="Montserrat, 'Noto Sans', sans-serif" font-weight="800" font-size="100" fill="` + NavyHex + `" letter-spacing="0">` + Wordmark + `</text>`)
	b.WriteString(`<text x="388" y="262" font-family="Montserrat, 'Noto Sans', sans-serif" font-weight="600" font-size="34" fill="` + TealHex + `" letter-spacing="0">` + strings.ReplaceAll(LogoTagline, "&", "&amp;") + `</text>`)
	b.WriteString(`<rect x="390" y="290" width="150" height="10" rx="5" fill="` + AmberHex + `"/>`)
	b.WriteString(`</svg>`)
	return b.String()
}

// RasterizeSVG renders svg markup to a w x h RGBA image using oksvg +
// rasterx (pure Go, anti-aliased).
func RasterizeSVG(svg string, w, h int) (*image.RGBA, error) {
	icon, err := oksvg.ReadIconStream(strings.NewReader(svg))
	if err != nil {
		return nil, err
	}
	icon.SetTarget(0, 0, float64(w), float64(h))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, dst, dst.Bounds())
	dasher := rasterx.NewDasher(w, h, scanner)
	icon.Draw(dasher, 1.0)
	return dst, nil
}

// scaleDown high-quality-downscales src to w x h with CatmullRom.
func scaleDown(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), stdraw.Over, nil)
	return dst
}

// RenderMark renders the mark at exactly px x px: rasterize big, then
// high-quality downscale (CatmullRom) for anti-aliased edges.
func RenderMark(px int) (*image.RGBA, error) {
	big := px * 4
	if big < MarkViewBox {
		big = MarkViewBox
	}
	if big > 2048 {
		big = 2048
	}
	img, err := RasterizeSVG(MarkSVG(), big, big)
	if err != nil {
		return nil, err
	}
	if big == px {
		return img, nil
	}
	return scaleDown(img, px, px), nil
}

// fillRect fills r (clipped to dst) with c.
func fillRect(dst *image.RGBA, r image.Rectangle, c color.Color) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	stdraw.Draw(dst, r, &image.Uniform{c}, image.Point{}, stdraw.Src)
}

// pasteOver draws src onto dst with its top-left at (x, y).
func pasteOver(dst *image.RGBA, src image.Image, x, y int) {
	r := image.Rectangle{Min: image.Point{x, y}, Max: image.Point{x + src.Bounds().Dx(), y + src.Bounds().Dy()}}
	stdraw.Draw(dst, r, src, image.Point{}, stdraw.Over)
}

// blendPixel blends c over dst at (x, y) (for low-alpha decoration strokes).
func blendPixel(dst *image.RGBA, x, y int, c color.RGBA) {
	if !(image.Point{X: x, Y: y}.In(dst.Bounds())) {
		return
	}
	a := uint32(c.A)
	if a == 0 {
		return
	}
	inv := 255 - a
	d := dst.RGBAAt(x, y)
	dst.SetRGBA(x, y, color.RGBA{
		R: uint8((uint32(c.R)*a + uint32(d.R)*inv) / 255),
		G: uint8((uint32(c.G)*a + uint32(d.G)*inv) / 255),
		B: uint8((uint32(c.B)*a + uint32(d.B)*inv) / 255),
		A: 0xFF,
	})
}

// fillDisc paints a filled disc of radius rad (for thick decoration strokes).
func fillDisc(dst *image.RGBA, cx, cy int, rad float64, c color.RGBA) {
	r := int(rad + 1)
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := float64(x-cx), float64(y-cy)
			if dx*dx+dy*dy <= rad*rad {
				blendPixel(dst, x, y, c)
			}
		}
	}
}

// RenderProfile renders the FB profile picture: cream full-bleed background
// with the mark large and centered (~78% of width). No text.
func RenderProfile(size int) (*image.RGBA, error) {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	fillRect(dst, dst.Bounds(), Cream)
	markD := size * 78 / 100
	mark, err := RenderMark(markD)
	if err != nil {
		return nil, err
	}
	pasteOver(dst, mark, (size-markD)/2, (size-markD)/2)
	return dst, nil
}

// RenderBadge renders a square icon of the mark at size x size.
// With creamBG it composites onto cream with the mark at markFrac of the
// width (padding around it); otherwise the background stays transparent.
func RenderBadge(size int, creamBG bool, markFrac float64) (*image.RGBA, error) {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	markD := int(float64(size)*markFrac + 0.5)
	mark, err := RenderMark(markD)
	if err != nil {
		return nil, err
	}
	if creamBG {
		fillRect(dst, dst.Bounds(), Cream)
	}
	pasteOver(dst, mark, (size-markD)/2, (size-markD)/2)
	return dst, nil
}

// RenderCheckCircle renders the small-size legibility check: the profile
// picture downscaled to 48px and masked to a circle.
func RenderCheckCircle() (*image.RGBA, error) {
	prof, err := RenderProfile(ProfileSize)
	if err != nil {
		return nil, err
	}
	small := scaleDown(prof, CheckSize, CheckSize)
	dst := image.NewRGBA(image.Rect(0, 0, CheckSize, CheckSize))
	c := float64(CheckSize-1) / 2
	for y := 0; y < CheckSize; y++ {
		for x := 0; x < CheckSize; x++ {
			d := (float64(x)-c)*(float64(x)-c) + (float64(y)-c)*(float64(y)-c)
			dist := math.Sqrt(d)
			alpha := 1.0
			if dist > c+0.5 {
				continue
			} else if dist > c-0.5 {
				alpha = c + 0.5 - dist // 1px feathered edge
			}
			px := small.RGBAAt(x, y)
			px.A = uint8(float64(px.A) * alpha)
			dst.SetRGBA(x, y, px)
		}
	}
	return dst, nil
}
