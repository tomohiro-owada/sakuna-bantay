package card

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/geo"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/phivolcs"
)

// QuakeInput is the plain data for the earthquake card.
type QuakeInput struct {
	Event     phivolcs.Event
	TimeLabel string // "25 Oct 2026, 6:13 PM PHT"
	Sample    bool
}

// quakeBBox returns the map bbox for a quake: MUST contain both the
// epicenter and Las Piñas plus ~0.6° margin, min span 4° (REVIEW_B #2).
func quakeBBox(lat, lon float64) geo.BBox {
	const lpLon, lpLat = 121.01, 14.44
	minLon := math.Min(lon, lpLon)
	maxLon := math.Max(lon, lpLon)
	minLat := math.Min(lat, lpLat)
	maxLat := math.Max(lat, lpLat)
	if maxLon-minLon < 4 {
		mid := (minLon + maxLon) / 2
		minLon, maxLon = mid-2, mid+2
	}
	if maxLat-minLat < 4 {
		mid := (minLat + maxLat) / 2
		minLat, maxLat = mid-2, mid+2
	}
	padLon := math.Max((maxLon-minLon)*0.12, 0.6)
	padLat := math.Max((maxLat-minLat)*0.12, 0.6)
	return geo.BBox{
		MinLon: math.Max(minLon-padLon, geo.ClipMinLon),
		MaxLon: math.Min(maxLon+padLon, geo.ClipMaxLon),
		MinLat: math.Max(minLat-padLat, geo.ClipMinLat),
		MaxLat: math.Min(maxLat+padLat, geo.ClipMaxLat),
	}
}

// RenderQuake renders the earthquake card: basemap zoomed to include the
// epicenter and Las Piñas, star at epicenter, dot at Las Piñas, dashed
// distance ring around Las Piñas, big M value + details on the right.
func RenderQuake(f *fonts.Fonts, b *geo.Basemap, in QuakeInput) (*image.RGBA, []TextBox, error) {
	c := New(f)
	src := "PHIVOLCS"
	if in.Event.Source == "USGS" {
		src = "USGS (public domain)"
	}
	body := c.Chrome("Earthquake", in.TimeLabel, []string{
		src, "geoBoundaries (CC BY 3.0 IGO)", "Natural Earth",
	}, in.Sample)
	_ = body

	e := in.Event
	bb := quakeBBox(e.Lat, e.Lon)

	panel := image.Rect(40, 176, 700, 946)
	m := NewMap(f, b, bb, panel.Dx(), panel.Dy(), 0, panel.Min)
	ex, ey := m.XY(e.Lon, e.Lat)

	// Dashed distance ring around Las Piñas with radius = pixel distance to
	// the epicenter, so the ring passes through the epicenter by
	// construction. All drawing is clipped to the panel (Map image bounds).
	lx, ly := m.XY(121.01, 14.44)
	ringPx := math.Hypot(float64(ex-lx), float64(ey-ly))
	if ringPx > 8 && ringPx < 3000 {
		dashedCircle(m.Img, lx, ly, ringPx, 3, 14, 10, color.NRGBA{navy.R, navy.G, navy.B, 0xCC})
		lbl := fmt.Sprintf("%.0f km from Las Piñas", e.DistanceKm)
		lw := f.Measure(fonts.NotoSemi, MinBodyPx, lbl)
		tx := lx - lw/2
		ty := int(float64(ly)-ringPx) - 10
		if ty < 30 {
			ty = int(float64(ly)+ringPx) + 30
		}
		// Clamp inside the panel (huge rings otherwise push the label out).
		pw, ph := m.Img.Bounds().Dx(), m.Img.Bounds().Dy()
		tx = min(max(tx, 4), max(pw-lw-4, 4))
		ty = min(max(ty, 30), max(ph-30, 30))
		m.Text(lbl, fonts.NotoSemi, MinBodyPx, navy, tx, ty)
	}
	if in.Sample {
		m.sampleOverlay()
	}

	m.StarMarker(ex, ey, 26)
	m.DotMarker("Las Piñas")
	// Legend chip: epicenter only (the Las Piñas dot already has its point
	// label; duplicating it in the legend is redundant). Place the chip in
	// the corner farthest from both points so it never covers a point label.
	placeLegend(m, ex, ey, lx, ly)
	m.Paste(c)

	// Right column. Times are always shown in PHT; the location text has its
	// zero padding stripped ("073 km" → "73 km").
	x := 724
	c.Text(fmt.Sprintf("M%.1f", e.Mag), fonts.Title, 118, red, x, 320)
	c.Text(fmt.Sprintf("Depth %.0f km", e.DepthKm), fonts.Semi, 34, navy, x, 372)
	evTime := e.Time.In(phivolcs.Manila())
	tlines := c.Wrap(fonts.NotoReg, 26, evTime.Format("2 Jan 2006, 3:04 PM")+" PHT", Size-40-x)
	y := 420
	stepT := lineStep(26)
	for _, ln := range tlines {
		c.Text(ln, fonts.NotoReg, 26, navy, x, y)
		y += stepT
	}
	y += 8
	place := phivolcs.CleanPlace(e.Place)
	plines := c.Wrap(fonts.Semi, 30, place, Size-40-x)
	stepP := lineStep(30)
	for i, ln := range plines {
		if i >= 3 {
			ln = c.Ellipsize(ln, fonts.Semi, 30, Size-40-x)
			c.Text(ln, fonts.Semi, 30, navy, x, y)
			y += stepP
			break
		}
		c.Text(ln, fonts.Semi, 30, navy, x, y)
		y += stepP
	}
	y += 8
	dist := fmt.Sprintf("%.0f km %s of Las Piñas", e.DistanceKm, e.Compass)
	stepD := lineStep(26)
	for _, ln := range c.Wrap(fonts.NotoReg, 26, dist, Size-40-x) {
		c.Text(ln, fonts.NotoReg, 26, grayTx, x, y)
		y += stepD
	}
	y += 12
	srcLine := "Source: " + src
	noteLine := "Preliminary; check PHIVOLCS for updates."
	if in.Sample {
		// PHASE_D 1a: the synthetic notice prints once (Source line only).
		srcLine = "Source: Synthetic sample — not a real event"
		noteLine = "Sample illustration only."
	}
	for _, ln := range c.Wrap(fonts.NotoSemi, 24, srcLine, Size-40-x) {
		c.Text(ln, fonts.NotoSemi, 24, grayTx, x, y)
		y += lineStep(24)
	}
	stepN := lineStep(MinBodyPx)
	for _, ln := range c.Wrap(fonts.NotoReg, MinBodyPx, noteLine, Size-40-x) {
		c.Text(ln, fonts.NotoReg, MinBodyPx, grayTx, x, y)
		y += stepN
	}
	return c.Img, c.Boxes, nil
}

// placeLegend draws the single-entry "Epicenter" chip in the panel corner
// farthest from both the epicenter (ex,ey) and Las Piñas (lx,ly).
func placeLegend(m *Map, ex, ey, lx, ly int) {
	const legW, legH = 212, 44
	pw, ph := m.Img.Bounds().Dx(), m.Img.Bounds().Dy()
	corners := []image.Point{
		{12, 8},
		{pw - 12 - legW, 8},
		{12, ph - 8 - legH},
		{pw - 12 - legW, ph - 8 - legH},
	}
	best := corners[0]
	bestScore := -1.0
	for _, cp := range corners {
		cx, cy := float64(cp.X+legW/2), float64(cp.Y+legH/2)
		d := math.Hypot(cx-float64(ex), cy-float64(ey)) +
			math.Hypot(cx-float64(lx), cy-float64(ly))
		if d > bestScore {
			bestScore, best = d, cp
		}
	}
	r := image.Rectangle{Min: best, Max: best.Add(image.Pt(legW, legH))}
	fillRoundRect(m.Img, r, 10, color.NRGBA{cream.R, cream.G, cream.B, 0xE6})
	m.StarMarker(best.X+24, best.Y+legH/2, 11)
	m.Text("Epicenter", fonts.NotoSemi, MinBodyPx, navy, best.X+44, best.Y+legH/2+8)
}
