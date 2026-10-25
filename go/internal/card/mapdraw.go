package card

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/geo"
)

// Shared map panel palette (used by NewMap and the signal NCR inset so
// both render identical land/sea).
var (
	seaCol  = color.RGBA{0xCF, 0xE2, 0xDB, 0xFF}
	landCol = color.RGBA{0xE4, 0xEC, 0xDC, 0xFF}
	lineCol = color.NRGBA{0x0F, 0x6B, 0x6B, 0x77}
)

// Map is a clipped map panel: everything is drawn in local coordinates
// (origin = panel top-left) and pasted onto the card, so rings and fills
// can never spill outside the panel rect.
type Map struct {
	Img      *image.RGBA
	Proj     geo.Proj
	F        *fonts.Fonts
	Boxes    []TextBox
	Off      image.Point // panel top-left on the card
	LandMask *image.RGBA // white = NE land, black = sea (for clipping shades)
}

// landMaskWhite is the mask colour for land pixels.
var landMaskWhite = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}

// BuildLandMask rasterizes NE land polys with proj onto a w x h mask:
// white = land, black = sea. Province fills and signal shading must be
// clipped to this mask so admin polygons that include water (e.g. Cavite
// covering southern Manila Bay) cannot paint the bay as land.
func BuildLandMask(proj geo.Proj, w, h int, land [][]geo.Pt) *image.RGBA {
	mask := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRect(mask, mask.Bounds(), color.RGBA{0x00, 0x00, 0x00, 0xFF})
	for _, poly := range land {
		if pts := projectRingWith(proj, poly); len(pts) >= 3 {
			fillPoly(mask, pts, landMaskWhite)
		}
	}
	return mask
}

// isMaskLand reports whether mask pixel (x,y) is land (white).
func isMaskLand(mask *image.RGBA, x, y int) bool {
	if !(image.Pt(x, y).In(mask.Bounds())) {
		return false
	}
	r, g, b, _ := mask.At(x, y).RGBA()
	return (r>>8) == 0xFF && (g>>8) == 0xFF && (b>>8) == 0xFF
}

// fillPolyMasked fills pts with c but only where mask is land. Used for
// province base fills and signal shading so water claimed by admin
// polygons (Manila Bay, etc.) stays sea.
func fillPolyMasked(dst *image.RGBA, pts []image.Point, mask *image.RGBA, c color.Color) {
	if len(pts) < 3 || mask == nil {
		if len(pts) >= 3 && mask == nil {
			fillPoly(dst, pts, c)
		}
		return
	}
	// Scanline fill (same even-odd rule as fillPoly) with mask check.
	minY, maxY := pts[0].Y, pts[0].Y
	for _, p := range pts {
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}
	b := dst.Bounds()
	if minY < b.Min.Y {
		minY = b.Min.Y
	}
	if maxY > b.Max.Y-1 {
		maxY = b.Max.Y - 1
	}
	// Mask is assumed to share the same origin/size as dst (both start at
	// 0,0 for panels and inset subs). Offset defensively if not.
	dx := mask.Bounds().Min.X - b.Min.X
	dy := mask.Bounds().Min.Y - b.Min.Y
	for y := minY; y <= maxY; y++ {
		var xs []float64
		for i := range pts {
			a, bb := pts[i], pts[(i+1)%len(pts)]
			if (a.Y <= y) == (bb.Y <= y) {
				continue
			}
			t := float64(y-a.Y) / float64(bb.Y-a.Y)
			xs = append(xs, float64(a.X)+t*float64(bb.X-a.X))
		}
		if len(xs) < 2 {
			continue
		}
		// Sort intersections (same as fillPoly) to handle concave rings.
		for i := 1; i < len(xs); i++ {
			for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
				xs[j], xs[j-1] = xs[j-1], xs[j]
			}
		}
		for i := 0; i+1 < len(xs); i += 2 {
			x0, x1 := xs[i], xs[i+1]
			for x := int(x0 + 0.5); x <= int(x1); x++ {
				if !isMaskLand(mask, x+dx, y+dy) {
					continue
				}
				setPx(dst, x, y, c)
			}
		}
	}
}

// NewMap creates a w x h panel for bbox, filled with sea tint + NE land
// fill + province outlines, framed with a rounded teal border.
//
// Base land comes ONLY from Natural Earth: geoBoundaries admin polygons
// may include large water areas (southern Manila Bay is inside Cavite),
// so filling provinces with the land colour would paint the bay as land.
// Signal shading must likewise be clipped to the NE mask (see
// fillPolyMasked). fillPoly sorts scanline intersections so concave rings
// cannot leave straight seam edges.
func NewMap(f *fonts.Fonts, b *geo.Basemap, bb geo.BBox, w, h int, refLat float64, off image.Point) *Map {
	m := &Map{
		Img: image.NewRGBA(image.Rect(0, 0, w, h)),
		F:   f,
		Off: off,
	}
	m.Proj = bb.Fit(0, 0, float64(w), float64(h), refLat)
	m.LandMask = BuildLandMask(m.Proj, w, h, b.Land)
	fillRect(m.Img, m.Img.Bounds(), seaCol) // sea
	for _, poly := range b.Land {
		if pts := m.projectRing(poly); len(pts) >= 3 {
			fillPoly(m.Img, pts, landCol)
		}
	}
	for _, prov := range b.Provinces {
		for _, poly := range prov.Polys {
			if pts := m.projectRing(poly); len(pts) >= 2 {
				polyPath(m.Img, append(pts, pts[0]), 1.5, lineCol)
			}
		}
	}
	strokeRoundRect(m.Img, m.Img.Bounds(), 14, 3, teal)
	return m
}

func (m *Map) projectRing(ring []geo.Pt) []image.Point {
	return projectRingWith(m.Proj, ring)
}

// projectRingWith projects a lon/lat ring with an explicit projection.
func projectRingWith(proj geo.Proj, ring []geo.Pt) []image.Point {
	pts := make([]image.Point, 0, len(ring))
	for _, pt := range ring {
		x, y := proj.Project(pt.Lon, pt.Lat)
		pts = append(pts, image.Pt(int(x+0.5), int(y+0.5)))
	}
	return pts
}

// XY projects lon/lat to local panel pixels.
func (m *Map) XY(lon, lat float64) (int, int) {
	x, y := m.Proj.Project(lon, lat)
	return int(x + 0.5), int(y + 0.5)
}

// Text draws on the panel (records boxes in card coordinates on Paste).
func (m *Map) Text(str string, s fonts.Style, size float64, col color.Color, x, y int) int {
	w := m.F.DrawString(m.Img, str, s, size, col, x, y)
	asc, desc, _ := m.F.Metrics(s, size)
	m.Boxes = append(m.Boxes, TextBox{Box: image.Rect(x, y-asc, x+w, y+desc), Size: size, Str: str})
	return w
}

// Paste composites the panel onto the card and merges text boxes.
func (m *Map) Paste(c *Canvas) {
	r := image.Rectangle{Min: m.Off, Max: m.Off.Add(m.Img.Bounds().Size())}
	draw.Draw(c.Img, r, m.Img, image.Point{}, draw.Over)
	for _, b := range m.Boxes {
		b.Box = b.Box.Add(m.Off)
		c.Boxes = append(c.Boxes, b)
	}
}

// StarMarker draws an amber/red epicenter star with a dark outline.
func (m *Map) StarMarker(x, y int, r float64) {
	pts := starPath(x, y, r, r*0.45)
	fillPoly(m.Img, pts, amber)
	polyPath(m.Img, append(pts, pts[0]), 3, red)
	fillDisc(m.Img, x, y, r*0.16, red)
}

// DotMarker draws the Las Piñas dot + label, clamped inside the panel so
// the label never spills outside the frame or over a far-corner legend.
func (m *Map) DotMarker(label string) {
	x, y := m.XY(121.01, 14.44)
	fillDisc(m.Img, x, y, 9, navy)
	strokeCircle(m.Img, x, y, 9, 3, white)
	pw, ph := m.Img.Bounds().Dx(), m.Img.Bounds().Dy()
	lw := m.F.Measure(fonts.NotoSemi, MinBodyPx, label)
	lx, ly := x+14, y+8
	if lx+lw > pw-4 {
		lx = x - 14 - lw
	}
	if lx < 4 {
		lx = 4
	}
	if ly > ph-6 {
		ly = ph - 6
	}
	if ly < 24 {
		ly = 24
	}
	m.Text(label, fonts.NotoSemi, MinBodyPx, navy, lx, ly)
}

// sampleOverlay draws a faint diagonal SAMPLE across the map panel only
// (REVIEW_B #7): low alpha so the geography stays readable, clearly visible,
// and — unlike a full-canvas watermark — never behind the big M value/text.
func (m *Map) sampleOverlay() {
	const label = "SAMPLE"
	size := 110.0
	w := m.F.Measure(fonts.Title, size, label)
	_, _, lh := m.F.Metrics(fonts.Title, size)
	tmp := image.NewRGBA(image.Rect(0, 0, w+40, lh+40))
	m.F.DrawString(tmp, label, fonts.Title, size,
		color.NRGBA{red.R, red.G, red.B, 0x33}, 20, 20+lh/2+20)
	rot := rotateImage(tmp, -30*math.Pi/180)
	pw, ph := m.Img.Bounds().Dx(), m.Img.Bounds().Dy()
	off := image.Pt((pw-rot.Bounds().Dx())/2, (ph-rot.Bounds().Dy())/2)
	draw.Draw(m.Img, image.Rectangle{Min: off, Max: off.Add(rot.Bounds().Size())},
		rot, image.Point{}, draw.Over)
}

// ShadeProvinces fills provinces (by normalized name) with the given colors,
// clipped to the NE land mask so water claimed by admin polygons stays sea.
func (m *Map) ShadeProvinces(idx map[string]*geo.Province, colors map[string]color.Color) {
	for norm, col := range colors {
		prov := idx[norm]
		if prov == nil {
			continue
		}
		for _, poly := range prov.Polys {
			if pts := m.projectRing(poly); len(pts) >= 3 {
				fillPolyMasked(m.Img, pts, m.LandMask, col)
			}
		}
	}
}

// DotMarkerAvoid draws the Las Piñas dot + label but shifts the label to the
// right of avoid (in panel-local coordinates) when they would overlap, so the
// label never sits on top of the inset locator rectangle.
func (m *Map) DotMarkerAvoid(label string, avoid image.Rectangle) {
	x, y := m.XY(121.01, 14.44)
	fillDisc(m.Img, x, y, 9, navy)
	strokeCircle(m.Img, x, y, 9, 3, white)
	pw, ph := m.Img.Bounds().Dx(), m.Img.Bounds().Dy()
	lw := m.F.Measure(fonts.NotoSemi, MinBodyPx, label)
	lx, ly := x+14, y+8
	// If the default label box would overlap the avoided rect, push it to
	// the right of that rect.
	lblH := 24
	if lblBox := image.Rect(lx, ly-lblH, lx+lw, ly+8); lblBox.Overlaps(avoid) {
		lx = avoid.Max.X + 6
	}
	if lx+lw > pw-4 {
		lx = x - 14 - lw
		// If even the left side still overlaps (tiny panel), keep the
		// right-of-avoid placement and clamp instead.
		if image.Rect(lx, ly-lblH, lx+lw, ly+8).Overlaps(avoid) {
			lx = avoid.Max.X + 6
		}
	}
	if lx < 4 {
		lx = 4
	}
	if lx+lw > pw-4 {
		lx = pw - 4 - lw
	}
	if ly > ph-6 {
		ly = ph - 6
	}
	if ly < 24 {
		ly = 24
	}
	m.Text(label, fonts.NotoSemi, MinBodyPx, navy, lx, ly)
}
