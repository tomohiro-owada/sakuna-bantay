package card

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sort"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/geo"
)

// SignalInput is the plain data for the typhoon / wind-signal card.
// Levels maps province names (CAP areaDesc style, e.g. "Batanes") to the
// max active wind signal (1–5).
type SignalInput struct {
	Cyclone      string // e.g. "Typhoon Rosal (CHAMPI)"
	IssuedLabel  string // "25 Oct 2026, 6:13 PM PHT"
	Levels       map[string]int
	WatchSummary string // e.g. "NCR: Signal #2" / "NCR: no signal"
	Sample       bool
}

// SignalColors shades provinces by wind signal level (1 light-amber .. 5
// dark red).
var SignalColors = map[int]color.RGBA{
	1: {0xF7, 0xD7, 0x74, 0xFF},
	2: {0xF2, 0xA5, 0x16, 0xFF},
	3: {0xE0, 0x7B, 0x39, 0xFF},
	4: {0xC0, 0x39, 0x2B, 0xFF},
	5: {0x6E, 0x14, 0x14, 0xFF},
}

var signalTextCol = map[int]color.Color{
	1: navy, 2: navy, 3: white, 4: white, 5: white,
}

// RenderSignal renders the typhoon / wind-signal card: Luzon–Visayas map
// (lon 116–127, lat 9–21) with provinces shaded by signal level.
func RenderSignal(f *fonts.Fonts, b *geo.Basemap, in SignalInput) (*image.RGBA, []TextBox, error) {
	c := New(f)
	body := c.Chrome("Typhoon · Wind Signals", in.IssuedLabel, []string{
		"DOST-PAGASA (CC BY 4.0)", "geoBoundaries (CC BY 3.0 IGO)", "Natural Earth",
	}, in.Sample)
	_ = body

	panel := image.Rect(40, 176, 700, 946)
	bb := geo.BBox{MinLon: 116, MinLat: 9, MaxLon: 127, MaxLat: 21}
	m := NewMap(f, b, bb, panel.Dx(), panel.Dy(), 0, panel.Min)

	// Resolve Levels (CAP areaDesc style) to actual basemap provinces.
	// Normalized Levels keys collapse aliases ("Metro Manila", "NCR",
	// "National Capital Region" all -> "metro manila"), so one entry must
	// shade all 4 NCR district polygons (REVIEW_C #1). Iterate provinces
	// (not Levels) so aliases fan out instead of collapsing via Index().
	matched := ResolveSignalProvinces(b, in.Levels)
	colors := map[string]color.Color{}
	for provName, lvl := range matched {
		colors[geo.NormalizeName(provName)] = SignalColors[lvl]
	}
	for provName, lvl := range matched {
		col := SignalColors[lvl]
		var prov *geo.Province
		for i := range b.Provinces {
			if b.Provinces[i].Name == provName {
				prov = &b.Provinces[i]
				break
			}
		}
		if prov == nil {
			continue
		}
		for _, poly := range prov.Polys {
			if pts := m.projectRing(poly); len(pts) >= 3 {
				fillPolyMasked(m.Img, pts, m.LandMask, col)
			}
		}
	}
	// Re-stroke shaded provinces in a darker shade of the fill so borders
	// stay visible without burying tiny islands (a teal re-stroke would
	// completely cover e.g. Batanes at this scale).
	for provName, lvl := range matched {
		var prov *geo.Province
		for i := range b.Provinces {
			if b.Provinces[i].Name == provName {
				prov = &b.Provinces[i]
				break
			}
		}
		if prov == nil {
			continue
		}
		for _, poly := range prov.Polys {
			if pts := m.projectRing(poly); len(pts) >= 2 {
				polyPath(m.Img, append(pts, pts[0]), 1, darker(SignalColors[lvl]))
			}
		}
	}
	if in.Sample {
		m.sampleOverlay()
	}

	// PHASE_D 1c: zoomed NCR / South Metro inset in the bottom-left corner
	// (sea west of Mindoro — no Luzon land of interest). The legend moves
	// to the top-right so the two never overlap.
	drawSignalInset(m, b, matched)
	locator := locatorRect(m)
	drawLocator(m)
	// The Las Piñas dot sits inside the inset footprint: offset its label
	// to the right of the locator rectangle so the two never overlap.
	m.DotMarkerAvoid("Las Piñas", locator)

	// Legend chip (top-right inside panel).
	lx := panel.Dx() - 12 - 212
	ly := 8
	fillRoundRect(m.Img, image.Rect(lx, ly, lx+212, ly+5*34+24), 10,
		color.NRGBA{cream.R, cream.G, cream.B, 0xE6})
	for lvl := 1; lvl <= 5; lvl++ {
		yy := ly + 12 + (lvl-1)*34
		fillRect(m.Img, image.Rect(lx+12, yy, lx+44, yy+24), SignalColors[lvl])
		m.Text(fmt.Sprintf("Signal #%d", lvl), fonts.NotoSemi, MinBodyPx, navy, lx+52, yy+20)
	}
	m.Paste(c)

	// Right column. Wrapped text uses normal ~1.2 line height (REVIEW_B #8).
	x := 724
	maxW := Size - 40 - x
	name := in.Cyclone
	if name == "" {
		name = "No active cyclone"
	}
	nsize := c.Fit(name, fonts.Title, 44, 28, maxW)
	nlines := c.Wrap(fonts.Title, nsize, name, maxW)
	nstep := lineStep(nsize)
	for i, ln := range nlines {
		if i >= 3 {
			break
		}
		c.Text(ln, fonts.Title, nsize, navy, x, 250+i*nstep)
	}
	y := 250 + len(nlines)*nstep
	if y < 300 {
		y = 300
	}
	issued := "Issued " + in.IssuedLabel
	isize := c.Fit(issued, fonts.NotoReg, 24, MinBodyPx, maxW)
	istep := lineStep(isize)
	for _, ln := range c.Wrap(fonts.NotoReg, isize, issued, maxW) {
		c.Text(ln, fonts.NotoReg, isize, grayTx, x, y+8)
		y += istep
	}
	y += 32
	wsize := c.Fit(in.WatchSummary, fonts.Title, 40, 26, maxW)
	wstep := lineStep(wsize)
	for _, ln := range c.Wrap(fonts.Title, wsize, in.WatchSummary, maxW) {
		c.Text(ln, fonts.Title, wsize, teal, x, y)
		y += wstep
	}
	y += 16
	names := sortedKeys(in.Levels)
	areaWord := "areas"
	if len(names) == 1 {
		areaWord = "area"
	}
	c.Text(fmt.Sprintf("%d %s with signals", len(names), areaWord), fonts.Semi, 28, navy, x, y)
	y += lineStep(28)
	top := names
	if len(top) > 6 {
		top = top[:6]
	}
	lstep := lineStep(24)
	for _, n := range top {
		ln := fmt.Sprintf("#%d %s", in.Levels[n], n)
		ln = c.Ellipsize(ln, fonts.NotoReg, 24, maxW)
		c.Text(ln, fonts.NotoReg, 24, grayTx, x, y)
		y += lstep
	}
	if len(names) > 6 {
		c.Text(fmt.Sprintf("+%d more", len(names)-6), fonts.NotoReg, 24, grayTx, x, y)
		y += lineStep(24) + 9
	}
	nstep2 := lineStep(MinBodyPx)
	for _, ln := range c.Wrap(fonts.NotoReg, MinBodyPx, "Provinces shaded by max active signal. Follow PAGASA updates.", maxW) {
		c.Text(ln, fonts.NotoReg, MinBodyPx, grayTx, x, y)
		y += nstep2
	}
	return c.Img, c.Boxes, nil
}

// signalInsetBBox is the zoomed NCR / South Metro view (PHASE_D 1c):
// NCR districts, Cavite, Laguna, Rizal, northern Batangas.
var signalInsetBBox = geo.BBox{MinLon: 120.75, MinLat: 14.15, MaxLon: 121.35, MaxLat: 14.80}

// signalInsetLabel is the inset title.
const signalInsetLabel = "NCR & South Metro"

// signalInsetRect returns the inset rect in panel-local coordinates:
// bottom-left corner (sea west of Mindoro — no Luzon land of interest).
func signalInsetRect(pw, ph int) image.Rectangle {
	const iw, ih = 250, 230
	return image.Rect(12, ph-12-ih, 12+iw, ph-12)
}

// drawSignalInset renders the zoomed NCR / South Metro inset: NE land only
// for the base (admin polygons may claim Manila Bay water), provinces
// shaded by level clipped to the NE mask, Las Piñas dot, frame and label.
// Drawn on the main panel before Paste (label text is recorded for the
// overflow test).
func drawSignalInset(m *Map, b *geo.Basemap, matched map[string]int) {
	rect := signalInsetRect(m.Img.Bounds().Dx(), m.Img.Bounds().Dy())
	iw, ih := rect.Dx(), rect.Dy()
	sub := image.NewRGBA(image.Rect(0, 0, iw, ih))
	proj := signalInsetBBox.Fit(0, 0, float64(iw), float64(ih), 0)
	mask := BuildLandMask(proj, iw, ih, b.Land)
	fillRect(sub, sub.Bounds(), seaCol)
	for _, poly := range b.Land {
		if pts := projectRingWith(proj, poly); len(pts) >= 3 {
			fillPoly(sub, pts, landCol)
		}
	}
	for provName, lvl := range matched {
		var prov *geo.Province
		for i := range b.Provinces {
			if b.Provinces[i].Name == provName {
				prov = &b.Provinces[i]
				break
			}
		}
		if prov == nil {
			continue
		}
		col := SignalColors[lvl]
		for _, poly := range prov.Polys {
			if pts := projectRingWith(proj, poly); len(pts) >= 3 {
				fillPolyMasked(sub, pts, mask, col)
			}
		}
	}
	for _, prov := range b.Provinces {
		for _, poly := range prov.Polys {
			if pts := projectRingWith(proj, poly); len(pts) >= 2 {
				polyPath(sub, append(pts, pts[0]), 1, lineCol)
			}
		}
	}
	// Las Piñas dot (no text label: the inset is too small; the main map
	// already labels it).
	lx, ly := proj.Project(121.01, 14.44)
	fillDisc(sub, int(lx+0.5), int(ly+0.5), 5, navy)
	strokeCircle(sub, int(lx+0.5), int(ly+0.5), 5, 2, white)
	draw.Draw(m.Img, rect, sub, image.Point{}, draw.Src)
	strokeRoundRect(m.Img, rect, 10, 3, teal)
	// Label pill ABOVE the inset frame so it never hides a map corner.
	lw := m.F.Measure(fonts.NotoSemi, MinBodyPx, signalInsetLabel)
	pill := image.Rect(rect.Min.X, rect.Min.Y-38, rect.Min.X+lw+16, rect.Min.Y-6)
	// Clamp inside the panel (inset sits near the bottom, so there is room
	// above; defensive clamp keeps the overflow test green).
	if pill.Min.Y < 4 {
		pill = pill.Add(image.Pt(0, 4-pill.Min.Y))
	}
	fillRoundRect(m.Img, pill, 8, color.NRGBA{cream.R, cream.G, cream.B, 0xE6})
	m.Text(signalInsetLabel, fonts.NotoSemi, MinBodyPx, navy, pill.Min.X+8, pill.Max.Y-8)
}

// locatorRect returns the inset footprint rectangle in panel-local coords.
func locatorRect(m *Map) image.Rectangle {
	x0, y0 := m.XY(signalInsetBBox.MinLon, signalInsetBBox.MaxLat)
	x1, y1 := m.XY(signalInsetBBox.MaxLon, signalInsetBBox.MinLat)
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	return image.Rect(x0, y0, x1, y1)
}

// drawLocator draws the small rectangle on the main map showing the inset
// footprint.
func drawLocator(m *Map) {
	r := locatorRect(m)
	polyPath(m.Img, []image.Point{{r.Min.X, r.Min.Y}, {r.Max.X, r.Min.Y}, {r.Max.X, r.Max.Y}, {r.Min.X, r.Max.Y}, {r.Min.X, r.Min.Y}}, 2, navy)
}

// ResolveSignalProvinces maps signal Levels (CAP areaDesc style) to actual
// basemap province names -> level. Levels keys are normalized first
// (taking the max when several keys collapse to one norm, e.g. "NCR" and
// "Metro Manila"), then fanned out to every basemap province sharing that
// norm so "Metro Manila"/"NCR"/"National Capital Region" shade all 4 NCR
// district polygons.
func ResolveSignalProvinces(b *geo.Basemap, levels map[string]int) map[string]int {
	normLevels := map[string]int{}
	for name, lvl := range levels {
		if lvl < 1 || lvl > 5 {
			continue
		}
		norm := geo.NormalizeName(name)
		if lvl > normLevels[norm] {
			normLevels[norm] = lvl
		}
	}
	out := map[string]int{}
	for _, prov := range b.Provinces {
		if lvl, ok := normLevels[geo.NormalizeName(prov.Name)]; ok {
			out[prov.Name] = lvl
		}
	}
	return out
}

// darker returns a 65%-brightness variant of c for shaded-province outlines.
func darker(c color.Color) color.Color {
	r, g, b, a := c.RGBA()
	const k = 0.65
	return color.NRGBA{
		R: uint8(float64(r>>8) * k),
		G: uint8(float64(g>>8) * k),
		B: uint8(float64(b>>8) * k),
		A: uint8(a >> 8),
	}
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
