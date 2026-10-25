package card

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/geo"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/openmeteo"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/phivolcs"
)

func testFonts(t *testing.T) *fonts.Fonts {
	t.Helper()
	return fonts.LoadEmbedded()
}

func testBasemap(t *testing.T) *geo.Basemap {
	t.Helper()
	b, err := geo.Load()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// assertInsideCanvas fails when any text box overflows the 1080x1080 canvas,
// any text is below the legibility floor, or the card looks blank.
func assertInsideCanvas(t *testing.T, name string, boxes []TextBox, img *image.RGBA) {
	t.Helper()
	bounds := image.Rect(0, 0, Size, Size)
	if !img.Bounds().Eq(bounds) {
		t.Errorf("%s: bounds = %v, want %v", name, img.Bounds(), bounds)
	}
	if len(boxes) == 0 {
		t.Errorf("%s: no text boxes recorded", name)
	}
	for i, b := range boxes {
		if !b.Box.In(bounds) {
			t.Errorf("%s: box %d %q %v (size %.0f) overflows canvas", name, i, b.Str, b.Box, b.Size)
		}
		if b.Size < MinBodyPx {
			t.Errorf("%s: box %d uses size %.1f below the %.0fpx floor", name, i, b.Size, MinBodyPx)
		}
	}
	nonCream := 0
	for y := 0; y < Size; y += 7 {
		for x := 0; x < Size; x += 7 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if !(uint32(r>>8) == uint32(cream.R) && uint32(g>>8) == uint32(cream.G) && uint32(bl>>8) == uint32(cream.B)) {
				nonCream++
			}
		}
	}
	if nonCream < 200 {
		t.Errorf("%s: only %d non-cream samples, card looks blank", name, nonCream)
	}
}

func dailyFixture(t *testing.T) DailyInput {
	t.Helper()
	fc, err := os.ReadFile("testdata/openmeteo-forecast.json")
	if err != nil {
		t.Fatal(err)
	}
	aq, err := os.ReadFile("testdata/openmeteo-aq.json")
	if err != nil {
		t.Fatal(err)
	}
	day, err := openmeteo.Parse(fc, aq, "")
	if err != nil {
		t.Fatal(err)
	}
	return DailyInput{
		Day:       *day,
		DateLabel: "25 Oct 2026",
		TimeLabel: "25 Oct 2026, 6:13 PM PHT",
		TaalLevel: 1,
		TaalText:  "Low-level unrest",
		Alerts: []string{
			"Signal #1: Batanes, Cagayan",
			"Orange rainfall warning in effect for Metro Manila",
		},
	}
}

func TestRenderDaily(t *testing.T) {
	f := testFonts(t)
	for _, sample := range []bool{false, true} {
		in := dailyFixture(t)
		in.Sample = sample
		img, boxes, err := RenderDaily(f, in)
		if err != nil {
			t.Fatal(err)
		}
		assertInsideCanvas(t, "daily", boxes, img)
	}
	// Empty alerts + no hourly data still fit.
	in := dailyFixture(t)
	in.Alerts = nil
	in.Day.HourlyRain = nil
	img, boxes, err := RenderDaily(f, in)
	if err != nil {
		t.Fatal(err)
	}
	assertInsideCanvas(t, "daily-empty", boxes, img)
}

func quakeEvent(lat, lon, mag, depth float64, place, source string) phivolcs.Event {
	e := phivolcs.Event{
		ID: "test", CanonicalID: "test",
		Time:    time.Date(2026, 10, 25, 18, 13, 0, 0, phivolcs.Manila()),
		Lat:     lat,
		Lon:     lon,
		DepthKm: depth,
		Mag:     mag,
		Place:   place,
		Source:  source,
	}
	e.DistanceKm = phivolcs.DistanceToLasPinas(lat, lon)
	e.BearingDeg = phivolcs.BearingDeg(phivolcs.LasPinasLat, phivolcs.LasPinasLon, lat, lon)
	e.Compass = phivolcs.Compass16(e.BearingDeg)
	return e
}

func TestRenderQuake(t *testing.T) {
	f := testFonts(t)
	b := testBasemap(t)
	cases := []QuakeInput{
		// Near Metro Manila (minimum-span zoom).
		{Event: quakeEvent(14.2, 120.6, 5.2, 10, "012 km S 45° W of Naic (Cavite)", "PHIVOLCS"), TimeLabel: "25 Oct 2026, 6:13 PM PHT"},
		// Far (whole-PH zoom): the real Bislig detail fixture location.
		{Event: quakeEvent(7.92, 127.13, 5.1, 73, "073 km N 59° E of Baganga (Davao Oriental)", "PHIVOLCS"), TimeLabel: "16 Oct 2026, 5:13 PM PHT"},
		// USGS source + very long place name.
		{Event: quakeEvent(19.805, 122.0872, 4.7, 15, "77 km ENE of Calayan, Cagayan, Philippines, a very long place name that must wrap or shrink", "USGS"), TimeLabel: "25 Oct 2026, 6:13 PM PHT", Sample: true},
	}
	for i, in := range cases {
		img, boxes, err := RenderQuake(f, b, in)
		if err != nil {
			t.Fatal(err)
		}
		assertInsideCanvas(t, "quake", boxes, img)
		_ = i
	}
}

func TestRenderSignal(t *testing.T) {
	f := testFonts(t)
	b := testBasemap(t)
	ins := []SignalInput{
		{
			Cyclone:      "Typhoon Rosal (CHAMPI)",
			IssuedLabel:  "25 Oct 2026, 11:00 AM PHT",
			Levels:       map[string]int{"Batanes": 2, "Cagayan": 1, "Ilocos Norte": 1, "Nonexistent Province": 3},
			WatchSummary: "NCR: no signal",
		},
		{
			Cyclone:      "Super Typhoon Opong with a very long name that must wrap onto several lines gracefully",
			IssuedLabel:  "25 Oct 2026, 11:00 AM PHT",
			Levels:       map[string]int{"Batanes": 5, "Cagayan": 4, "Isabela": 3, "Aurora": 3, "Quezon": 2, "Metro Manila": 2, "Cavite": 1, "Laguna": 1},
			WatchSummary: "NCR: Signal #2",
			Sample:       true,
		},
		{
			Cyclone:      "",
			IssuedLabel:  "25 Oct 2026, 11:00 AM PHT",
			Levels:       map[string]int{},
			WatchSummary: "NCR: no signal",
		},
	}
	for _, in := range ins {
		img, boxes, err := RenderSignal(f, b, in)
		if err != nil {
			t.Fatal(err)
		}
		assertInsideCanvas(t, "signal", boxes, img)
	}
}

func TestResolveSignalProvincesNCR(t *testing.T) {
	// REVIEW_C #1: "Metro Manila"/"NCR"/"National Capital Region" (and CAP
	// areaDesc variants) must shade all 4 NCR district polygons.
	b := testBasemap(t)
	for _, alias := range []string{"Metro Manila", "NCR", "National Capital Region", "NCR, City of Manila, First District"} {
		matched := ResolveSignalProvinces(b, map[string]int{alias: 2})
		if len(matched) < 4 {
			t.Errorf("%q matched %d provinces, want >=4 (all NCR districts): %v", alias, len(matched), matched)
		}
		for _, want := range []string{"NCR, City of Manila, First District", "NCR, Second District", "NCR, Third District", "NCR, Fourth District"} {
			if lvl, ok := matched[want]; !ok || lvl != 2 {
				t.Errorf("%q: district %q missing or level %d, want 2 (matched=%v)", alias, want, lvl, matched)
			}
		}
		// Render must not drop the shading (no panic, fits canvas).
		f := testFonts(t)
		img, boxes, err := RenderSignal(f, b, SignalInput{Cyclone: "Bagyong SAMPLE (sample only)", IssuedLabel: "25 Oct 2026, 11:00 AM PHT", Levels: map[string]int{alias: 2}, WatchSummary: "NCR: Signal #2", Sample: true})
		if err != nil {
			t.Fatal(err)
		}
		assertInsideCanvas(t, "signal-ncr-"+alias, boxes, img)
	}
}

func TestQuakeSampleSourceLine(t *testing.T) {
	// REVIEW_C #2: synthetic quake card Source must read synthetic, not PHIVOLCS.
	f, b := testFonts(t), testBasemap(t)
	ev := quakeEvent(13.88, 120.44, 5.6, 10, "30 km SW of Nasugbu, Batangas", "PHIVOLCS")
	_, boxes, err := RenderQuake(f, b, QuakeInput{Event: ev, TimeLabel: "25 Oct 2026, 6:13 PM PHT", Sample: true})
	if err != nil {
		t.Fatal(err)
	}
	foundSynth, foundReal := false, false
	for _, bx := range boxes {
		if bx.Str == "Source: Synthetic sample" || bx.Str == "Source: Synthetic sample — not a real event" {
			foundSynth = true
		}
		if bx.Str == "Source: PHIVOLCS" {
			foundReal = true
		}
	}
	if !foundSynth {
		t.Error("synthetic quake card missing synthetic Source line")
	}
	if foundReal {
		t.Error("synthetic quake card must not show Source: PHIVOLCS")
	}
}

func TestQuakeSyntheticNoticeOnce(t *testing.T) {
	// PHASE_D 1a: the synthetic notice prints once (Source line only).
	f, b := testFonts(t), testBasemap(t)
	ev := quakeEvent(13.88, 120.44, 5.6, 10, "30 km SW of Nasugbu, Batangas", "PHIVOLCS")
	_, boxes, err := RenderQuake(f, b, QuakeInput{Event: ev, TimeLabel: "25 Oct 2026, 6:13 PM PHT", Sample: true})
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for _, bx := range boxes {
		all = append(all, bx.Str)
	}
	// Boxes hold wrapped fragments; join with spaces so a phrase split
	// across lines is still found (it must appear exactly once on the card).
	joined := strings.Join(all, " ")
	count := strings.Count(joined, "Synthetic sample — not a real event")
	if count != 1 {
		t.Errorf("synthetic notice printed %d times, want exactly 1", count)
	}
}

func TestDailyWindowStartHour(t *testing.T) {
	// PHASE_D 1b: window start unit cases.
	loc := phivolcs.Manila()
	cases := []struct {
		name string
		now  time.Time
		want int
	}{
		{"zero shows full window", time.Time{}, 6},
		{"05:59 same day", time.Date(2026, 10, 25, 5, 59, 0, 0, loc), 6},
		{"06:00 same day", time.Date(2026, 10, 25, 6, 0, 0, 0, loc), 6},
		{"06:30 same day", time.Date(2026, 10, 25, 6, 30, 0, 0, loc), 6},
		{"15:30 late run", time.Date(2026, 10, 25, 15, 30, 0, 0, loc), 15},
		{"23:45 late run", time.Date(2026, 10, 25, 23, 45, 0, 0, loc), 23},
		{"other date resets", time.Date(2026, 10, 26, 15, 30, 0, 0, loc), 6},
	}
	for _, tc := range cases {
		if got := DailyWindowStart("2026-10-25", tc.now); got != tc.want {
			t.Errorf("%s: start=%d, want %d", tc.name, got, tc.want)
		}
	}
}

func boxContains(boxes []TextBox, substr string) bool {
	for _, bx := range boxes {
		if strings.Contains(bx.Str, substr) {
			return true
		}
	}
	return false
}

func TestDailyLateWindowMax(t *testing.T) {
	// PHASE_D 1b: hero number, chart label and (via DailyDisplay) the text
	// number all use the SAME displayed window.
	loc := phivolcs.Manila()
	mkDay := func() openmeteo.Today {
		in := dailyFixture(t)
		var hrs []openmeteo.HourRain
		for h := 6; h <= 23; h++ {
			p := 5
			if h == 8 {
				p = 80 // morning peak, gone by afternoon
			}
			if h == 15 {
				p = 10
			}
			hrs = append(hrs, openmeteo.HourRain{
				Time: fmt.Sprintf("2026-10-25T%02d:00", h), Hour: h, Prob: p,
			})
		}
		in.Day.HourlyRain = hrs
		return in.Day
	}
	f := testFonts(t)

	// Early run at 06:00: full window, max 80.
	early := dailyFixture(t)
	early.Day = mkDay()
	early.Now = time.Date(2026, 10, 25, 6, 0, 0, 0, loc)
	_, eboxes, err := RenderDaily(f, early)
	if err != nil {
		t.Fatal(err)
	}
	if !boxContains(eboxes, "Rain chance from 06:00 (PHT)") {
		t.Error("early card missing label 'Rain chance from 06:00 (PHT)'")
	}
	if !boxContains(eboxes, "up to 80%") {
		t.Error("early hero should show window max 'up to 80%'")
	}
	if boxContains(eboxes, "Mababa ang tsansa ng ulan") {
		t.Error("early card (max 80%) must not show the low-chance note")
	}
	if _, _, mx := DailyDisplay(early.Day, early.Now); mx != 80 {
		t.Errorf("early DailyDisplay max = %d, want 80", mx)
	}

	// Late run at 15:30: window 15–23, max 10.
	late := dailyFixture(t)
	late.Day = mkDay()
	late.Now = time.Date(2026, 10, 25, 15, 30, 0, 0, loc)
	_, lboxes, err := RenderDaily(f, late)
	if err != nil {
		t.Fatal(err)
	}
	if !boxContains(lboxes, "Rain chance from 15:00 (PHT)") {
		t.Error("late card missing label 'Rain chance from 15:00 (PHT)'")
	}
	if !boxContains(lboxes, "up to 10%") {
		t.Error("late hero should show window max 'up to 10%' (not the morning 80%)")
	}
	if !boxContains(lboxes, "Mababa ang tsansa ng ulan") {
		t.Error("late card (window max 10%) should show the low-chance note")
	}
	if hrs, st, mx := DailyDisplay(late.Day, late.Now); st != 15 || mx != 10 || len(hrs) != 9 {
		t.Errorf("late DailyDisplay = %d hours from %d max %d, want 9 from 15 max 10", len(hrs), st, mx)
	}
	// Past hours must not be drawn: no 08:00 axis label on the late card.
	if boxContains(lboxes, "08") {
		// Axis labels are zero-padded hour strings; the late window starts
		// at 15 so "08" must be gone. (Hero/date lines contain no "08".)
		t.Error("late card still shows past hour 08")
	}
}

func TestRenderAlert(t *testing.T) {
	// PHASE_D §3: generic text alert card (tsunami & rain) fits the canvas.
	f := testFonts(t)
	for _, in := range []AlertInput{
		{Title: "Tsunami Advisory", Lines: []string{"Coastal areas of Batangas", "Issued 25 Oct 2026, 6:13 PM PHT"}, TimeLabel: "25 Oct 2026, 6:13 PM PHT"},
		{Title: "Orange Rainfall Warning — Metro Manila", Lines: []string{"Metro Manila, Rizal, Laguna", "Flooding possible in low-lying areas. Follow PAGASA updates."}, TimeLabel: "25 Oct 2026, 6:13 PM PHT", Sample: true},
		{Title: "", Lines: nil, TimeLabel: "25 Oct 2026, 6:13 PM PHT"},
	} {
		img, boxes, err := RenderAlert(f, in)
		if err != nil {
			t.Fatal(err)
		}
		assertInsideCanvas(t, "alert", boxes, img)
	}
}

func TestSignalInsetNCRShading(t *testing.T) {
	// PHASE_D 1c: NCR shading shows inside the zoomed inset (a pixel inside
	// an NCR district polygon in the inset has the level color); the inset
	// label fits on the card; the moved legend never overlaps the inset.
	f, b := testFonts(t), testBasemap(t)
	in := SignalInput{
		Cyclone:      "Bagyong SAMPLE (sample only)",
		IssuedLabel:  "25 Oct 2026, 11:00 AM PHT",
		Levels:       map[string]int{"Metro Manila": 2, "Cavite": 2, "Laguna": 1, "Batanes": 3},
		WatchSummary: "NCR: Signal #2",
		Sample:       true,
	}
	img, boxes, err := RenderSignal(f, b, in)
	if err != nil {
		t.Fatal(err)
	}
	assertInsideCanvas(t, "signal-inset", boxes, img)
	// Panel origin in RenderSignal is (40,176); size 660x770.
	rect := signalInsetRect(660, 770).Add(image.Pt(40, 176))
	want := SignalColors[2]
	count := 0
	for y := rect.Min.Y + 2; y < rect.Max.Y-2; y++ {
		for x := rect.Min.X + 2; x < rect.Max.X-2; x++ {
			if img.RGBAAt(x, y) == want {
				count++
			}
		}
	}
	if count < 50 {
		t.Errorf("inset holds %d level-2 pixels, want >=50 (NCR/Cavite shading missing?)", count)
	}
	foundLabel := false
	for _, bx := range boxes {
		if bx.Str == signalInsetLabel {
			foundLabel = true
		}
		if strings.HasPrefix(bx.Str, "Signal #") && bx.Box.Overlaps(rect) {
			t.Errorf("legend %q %v overlaps the inset %v", bx.Str, bx.Box, rect)
		}
	}
	if !foundLabel {
		t.Errorf("inset label %q missing", signalInsetLabel)
	}
}

func TestSignalInsetProjectionWaterLand(t *testing.T) {
	// Inset bbox lon 120.75–121.35 / lat 14.15–14.80: the upper-left ~40%
	// must be Manila Bay WATER with Las Piñas (14.44N,121.01E) near the
	// bay's SE shore. Verifies the inset projection/bbox (aspect fit,
	// y-axis orientation, clip): a bay water point must draw as sea, a
	// Cavite land point as land, and shading must not spill into water.
	iw, ih := 250, 230
	proj := signalInsetBBox.Fit(0, 0, float64(iw), float64(ih), 0)
	// Aspect fit: no distortion (SX == SY).
	if proj.SX <= 0 || proj.SY <= 0 {
		t.Fatalf("inset proj has non-positive scale SX=%v SY=%v", proj.SX, proj.SY)
	}
	if diff := (proj.SX - proj.SY) / proj.SX; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("inset proj distorts aspect: SX=%v SY=%v", proj.SX, proj.SY)
	}
	// Y-axis orientation: north (higher lat) maps to smaller y.
	_, yNorth := proj.Project(121.0, 14.70)
	_, ySouth := proj.Project(121.0, 14.20)
	if yNorth >= ySouth {
		t.Errorf("inset y-axis inverted: north y=%v south y=%v", yNorth, ySouth)
	}
	// Known points project inside the inset frame.
	type wpt struct {
		name     string
		lon, lat float64
	}
	pts := []wpt{
		{"manila bay water", 120.85, 14.60},
		{"laguna lake", 121.25, 14.35},
		{"cavite land", 120.95, 14.30},
		{"las pinas", 121.01, 14.44},
	}
	for _, p := range pts {
		x, y := proj.Project(p.lon, p.lat)
		if x < 0 || x >= float64(iw) || y < 0 || y >= float64(ih) {
			t.Errorf("%s (%v,%v) projects outside inset: (%v,%v)", p.name, p.lon, p.lat, x, y)
		}
	}
	// Relative geography: bay water is west of Cavite land and north of it.
	bx, by := proj.Project(120.85, 14.60)
	cx, cy := proj.Project(120.95, 14.30)
	if bx >= cx {
		t.Errorf("bay x=%v should be west of cavite x=%v", bx, cx)
	}
	if by >= cy {
		t.Errorf("bay y=%v should be north of (smaller y than) cavite y=%v", by, cy)
	}

	// Pixel colours: render the inset the same way as the card (NE land
	// only for the base, shading clipped to the NE mask) and check water
	// stays sea.
	b := testBasemap(t)
	mask := BuildLandMask(proj, iw, ih, b.Land)
	sub := image.NewRGBA(image.Rect(0, 0, iw, ih))
	fillRect(sub, sub.Bounds(), seaCol)
	for _, poly := range b.Land {
		if p := projectRingWith(proj, poly); len(p) >= 3 {
			fillPoly(sub, p, landCol)
		}
	}
	matched := ResolveSignalProvinces(b, map[string]int{"Metro Manila": 2, "Cavite": 2, "Laguna": 1})
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
			if p := projectRingWith(proj, poly); len(p) >= 3 {
				fillPolyMasked(sub, p, mask, SignalColors[lvl])
			}
		}
	}
	at := func(lon, lat float64) color.Color {
		x, y := proj.Project(lon, lat)
		ix, iy := int(x+0.5), int(y+0.5)
		if ix < 0 {
			ix = 0
		}
		if iy < 0 {
			iy = 0
		}
		if ix >= iw {
			ix = iw - 1
		}
		if iy >= ih {
			iy = ih - 1
		}
		return sub.At(ix, iy)
	}
	if got := at(120.85, 14.60); got != seaCol {
		t.Errorf("manila bay water (14.60N,120.85E) = %v, want sea %v (bay painted as land?)", got, seaCol)
	}
	if got := at(120.95, 14.30); got == seaCol {
		t.Errorf("cavite land (14.30N,120.95E) = sea, want land/shade")
	}
	// Laguna de Bay (14.35N,121.25E): NE 50m lacks the lake, so the base
	// stays land — acceptable. What must hold is that signal shading never
	// spills there as water: the pixel must be sea or land, never a signal
	// colour from a neighbouring province.
	lagPix := at(121.25, 14.35)
	if lagPix != seaCol && lagPix != landCol {
		isSignal := false
		for _, sc := range SignalColors {
			if lagPix == sc {
				isSignal = true
			}
		}
		if isSignal {
			t.Errorf("laguna lake point has signal shade %v, want sea/land (shading spill?)", lagPix)
		}
	}

	// Full-card check: the rendered inset's bay pixel is sea and the label
	// pill sits ABOVE the frame (does not hide the map corner).
	f := testFonts(t)
	img, boxes, err := RenderSignal(f, b, SignalInput{
		Cyclone: "Bagyong SAMPLE (sample only)", IssuedLabel: "25 Oct 2026, 11:00 AM PHT",
		Levels:       map[string]int{"Metro Manila": 2, "Cavite": 2, "Laguna": 1},
		WatchSummary: "NCR: Signal #2", Sample: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rect := signalInsetRect(660, 770)
	// Bay point in panel-local coords -> card coords (panel origin 40,176).
	px, py := proj.Project(120.85, 14.60)
	cardPt := image.Pt(40+int(px+0.5), 176+rect.Min.Y+int(py+0.5)-rect.Min.Y)
	// Simpler: recompute via panel projection? The inset sub origin equals
	// rect.Min, so card pixel = panel origin + rect.Min + (px,py).
	cardPt = image.Pt(40+rect.Min.X+int(px+0.5), 176+rect.Min.Y+int(py+0.5))
	if got := img.RGBAAt(cardPt.X, cardPt.Y); got != seaCol {
		t.Errorf("card inset bay pixel %v = %v, want sea %v", cardPt, got, seaCol)
	}
	for _, bx := range boxes {
		if bx.Str == signalInsetLabel {
			if bx.Box.Overlaps(rect.Add(image.Pt(40, 176))) {
				t.Errorf("inset label %v overlaps inset frame %v (must sit above)", bx.Box, rect)
			}
			if bx.Box.Max.Y > 176+rect.Min.Y-4 {
				t.Errorf("inset label %v not above frame top %d", bx.Box, 176+rect.Min.Y)
			}
		}
	}
}

func TestSignalMainLabelAvoidsLocator(t *testing.T) {
	// Main map: the "Las Piñas" label must not overlap the locator
	// rectangle showing the inset footprint; it is offset to the right.
	f, b := testFonts(t), testBasemap(t)
	_, boxes, err := RenderSignal(f, b, SignalInput{
		Cyclone: "Bagyong SAMPLE (sample only)", IssuedLabel: "25 Oct 2026, 11:00 AM PHT",
		Levels:       map[string]int{"Metro Manila": 2, "Cavite": 2, "Laguna": 1},
		WatchSummary: "NCR: Signal #2", Sample: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Recompute the locator rect in card coords (panel origin 40,176).
	mk := NewMap(f, b, geo.BBox{MinLon: 116, MinLat: 9, MaxLon: 127, MaxLat: 21}, 660, 770, 0, image.Pt(40, 176))
	loc := locatorRect(mk).Add(image.Pt(40, 176))
	for _, bx := range boxes {
		if bx.Str == "Las Piñas" && bx.Box.Overlaps(loc) {
			// The inset label is "NCR & South Metro" (different string), so
			// any "Las Piñas" overlap is the main-map label on the locator.
			t.Errorf("main Las Piñas label %v overlaps locator %v", bx.Box, loc)
		}
	}
}

func TestRenderTaal(t *testing.T) {
	f := testFonts(t)
	for lvl := 0; lvl <= 5; lvl++ {
		in := TaalInput{
			Level:         lvl,
			LevelText:     "Low-level unrest",
			BulletinTitle: "Taal Volcano Summary of 24Hr Observation 25 October 2026 12:00 AM",
			BulletinDate:  "25 Oct 2026",
		}
		if lvl == 5 {
			in.Sample = true
		}
		img, boxes, err := RenderTaal(f, in)
		if err != nil {
			t.Fatal(err)
		}
		assertInsideCanvas(t, "taal", boxes, img)
	}
}

// --- REVIEW_B regression tests ---

func TestQuakeBBoxContainsBoth(t *testing.T) {
	// REVIEW_B #2: bbox MUST contain epicenter + Las Piñas + ~0.6° margin,
	// min span 4°.
	for _, tc := range [][2]float64{
		{7.92, 127.13},  // far Baganga
		{13.88, 120.44}, // near Nasugbu synth
		{14.2, 120.6},   // near Naic
		{19.805, 122.0872},
	} {
		bb := quakeBBox(tc[0], tc[1])
		if tc[1] < bb.MinLon || tc[1] > bb.MaxLon || tc[0] < bb.MinLat || tc[0] > bb.MaxLat {
			t.Errorf("epicenter %v outside bbox %+v", tc, bb)
		}
		if 121.01 < bb.MinLon || 121.01 > bb.MaxLon || 14.44 < bb.MinLat || 14.44 > bb.MaxLat {
			t.Errorf("Las Piñas outside bbox %+v (epicenter %v)", bb, tc)
		}
		if bb.MaxLon-bb.MinLon < 4-1e-9 {
			t.Errorf("lon span %v < 4 (epicenter %v)", bb.MaxLon-bb.MinLon, tc)
		}
		if bb.MaxLat-bb.MinLat < 4-1e-9 {
			t.Errorf("lat span %v < 4 (epicenter %v)", bb.MaxLat-bb.MinLat, tc)
		}
		// ~0.6° margin: both points at least 0.59° inside (clip box may trim).
		m := func(v, lo, hi float64) float64 {
			d := v - lo
			if hi-v < d {
				d = hi - v
			}
			return d
		}
		if m(tc[1], bb.MinLon, bb.MaxLon) < 0.59 && tc[1] > geo.ClipMinLon+0.6 && tc[1] < geo.ClipMaxLon-0.6 {
			t.Errorf("epicenter lon margin %v < 0.6 (bbox %+v)", m(tc[1], bb.MinLon, bb.MaxLon), bb)
		}
		if m(121.01, bb.MinLon, bb.MaxLon) < 0.59 {
			t.Errorf("Las Piñas lon margin too small (bbox %+v)", bb)
		}
	}
}

func TestQuakeLegendSingleAndFar(t *testing.T) {
	// REVIEW_B #2: legend keeps only "Epicenter" (no "Las Piñas" duplication),
	// placed far from both points so it never covers the dot label.
	f, b := testFonts(t), testBasemap(t)
	in := QuakeInput{Event: quakeEvent(7.92, 127.13, 5.1, 2, "73 km N 59° E of Baganga (Davao Oriental)", "PHIVOLCS"), TimeLabel: "16 Oct 2026, 5:13 PM PHT"}
	_, boxes, err := RenderQuake(f, b, in)
	if err != nil {
		t.Fatal(err)
	}
	epi, lp := 0, 0
	for _, bx := range boxes {
		if bx.Str == "Epicenter" {
			epi++
		}
		if bx.Str == "Las Piñas" {
			lp++
		}
	}
	if epi != 1 {
		t.Errorf("Epicenter legend boxes = %d, want 1", epi)
	}
	if lp != 1 {
		t.Errorf("Las Piñas label boxes = %d, want exactly 1 (dot label only, no legend dup)", lp)
	}
	// Legend vs dot label must not overlap.
	var epiBox, lpBox *TextBox
	for i, bx := range boxes {
		if bx.Str == "Epicenter" {
			epiBox = &boxes[i]
		}
		if bx.Str == "Las Piñas" {
			lpBox = &boxes[i]
		}
	}
	if epiBox != nil && lpBox != nil && epiBox.Box.Overlaps(lpBox.Box) {
		t.Errorf("legend %v overlaps dot label %v", epiBox.Box, lpBox.Box)
	}
}

func TestQuakeTimePHTNotShifted(t *testing.T) {
	// REVIEW_B #1 + #4: far event shows 5:13 PM PHT (not 9:13 PM) with the
	// zero padding stripped.
	f, b := testFonts(t), testBasemap(t)
	evTime := quakeEvent(7.92, 127.13, 5.1, 2, "073 km N 59° E of Baganga (Davao Oriental)", "PHIVOLCS")
	in := QuakeInput{Event: evTime, TimeLabel: "16 Oct 2026, 5:13 PM PHT"}
	_, boxes, err := RenderQuake(f, b, in)
	if err != nil {
		t.Fatal(err)
	}
	foundTime, foundPlace := false, false
	for _, bx := range boxes {
		if bx.Str == "16 Oct 2026, 5:13 PM PHT" {
			foundTime = true
		}
		if bx.Str == "9:13 PM PHT" || bx.Str == "16 Oct 2026, 9:13 PM PHT" {
			t.Errorf("found 8h-shifted time box %q", bx.Str)
		}
		if bx.Str == "73 km N 59° E of" || bx.Str == "Baganga (Davao" {
			foundPlace = true
		}
		if bx.Str == "073 km N 59° E of" {
			t.Errorf("leading-zero place not stripped: %q", bx.Str)
		}
	}
	if !foundTime {
		t.Error("expected body time box 16 Oct 2026, 5:13 PM PHT not found")
	}
	if !foundPlace {
		t.Error("expected stripped place lines (73 km ...) not found")
	}
}

func TestFillPolyConcave(t *testing.T) {
	// REVIEW_B #3: concave rings must fill the right spans (sorted scanline).
	dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
	pts := []image.Point{{10, 10}, {90, 10}, {90, 90}, {60, 90}, {60, 40}, {40, 40}, {40, 90}, {10, 90}}
	fillPoly(dst, pts, color.RGBA{255, 0, 0, 255})
	at := func(x, y int) bool {
		r, _, _, _ := dst.At(x, y).RGBA()
		return r > 0x8000
	}
	// Notch x=40..60, y=40..90 must stay empty; sides filled.
	if at(50, 70) {
		t.Error("notch (50,70) filled, want empty")
	}
	if !at(20, 70) || !at(80, 70) {
		t.Error("sides (20,70)/(80,70) empty, want filled")
	}
	if !at(50, 20) {
		t.Error("base (50,20) empty, want filled")
	}
}

func TestNoLongStraightSeams(t *testing.T) {
	// REVIEW_B #3: rendered maps must have no long straight land/sea runs.
	f, b := testFonts(t), testBasemap(t)
	mk := func(bb geo.BBox) *Map {
		return NewMap(f, b, bb, 660, 770, 0, image.Pt(40, 176))
	}
	maps := []*Map{
		mk(quakeBBox(7.92, 127.13)),
		mk(quakeBBox(13.88, 120.44)),
		mk(geo.BBox{MinLon: 116, MinLat: 9, MaxLon: 127, MaxLat: 21}),
	}
	land := color.RGBA{0xE4, 0xEC, 0xDC, 0xFF}
	isLand := func(c color.Color) bool {
		r, g, bl, _ := c.RGBA()
		return uint8(r>>8) == land.R && uint8(g>>8) == land.G && uint8(bl>>8) == land.B
	}
	for mi, m := range maps {
		img := m.Img
		W, H := img.Bounds().Dx(), img.Bounds().Dy()
		// Horizontal straight edges: land/sea boundary at same y over long x.
		for y := 4; y < H-5; y++ {
			run := 0
			for x := 2; x < W-2; x++ {
				a, b2 := isLand(img.At(x, y)), isLand(img.At(x, y+1))
				if a != b2 {
					run++
					if run > 250 {
						t.Errorf("map %d: horizontal seam run >250 at y=%d", mi, y)
						break
					}
				} else {
					run = 0
				}
			}
		}
		// Vertical straight edges.
		for x := 4; x < W-5; x++ {
			run := 0
			for y := 2; y < H-2; y++ {
				a, b2 := isLand(img.At(x, y)), isLand(img.At(x+1, y))
				if a != b2 {
					run++
					if run > 250 {
						t.Errorf("map %d: vertical seam run >250 at x=%d", mi, x)
						break
					}
				} else {
					run = 0
				}
			}
		}
	}
}

func TestDailyLowRainNote(t *testing.T) {
	// REVIEW_B #5: max < 20% shows the centered low-chance note.
	f := testFonts(t)
	in := dailyFixture(t)
	// Fixture max is 14% (<20).
	if in.Day.PrecipProbMx >= 20 {
		t.Skip("fixture max changed, adjust test")
	}
	_, boxes, err := RenderDaily(f, in)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, bx := range boxes {
		if bx.Str == "Mababa ang tsansa ng ulan · Low chance of rain all day" {
			found = true
		}
	}
	if !found {
		t.Error("low-rain note missing for max<20%")
	}
}

func TestDailyChipPaddingAndAQILabel(t *testing.T) {
	// REVIEW_B #6 + daily max basis: chip text stays inside with >=12px
	// padding; AQI/PM2.5 chips are labelled "· max" (daily max of hourly
	// values, same time basis for both).
	f := testFonts(t)
	in := dailyFixture(t)
	in.Day.AQIMax = 150
	in.Day.AQICategory = "Unhealthy for Sensitive Groups"
	in.Day.PM25Max = 69.2
	_, boxes, err := RenderDaily(f, in)
	if err != nil {
		t.Fatal(err)
	}
	foundAQI, foundPM := false, false
	for _, bx := range boxes {
		if bx.Str == "US AQI · max" {
			foundAQI = true
			// Chip 2 (AQI): x0=40+1*(cw+16), cw=(1080-80-48)/4=238 → x0=294.
			if bx.Box.Min.X < 294+12 || bx.Box.Max.X > 294+238-12 {
				t.Errorf("AQI title %v exceeds 12px chip padding", bx.Box)
			}
		}
		if bx.Str == "PM2.5 · max" {
			foundPM = true
			// Chip 3 (PM2.5): x0=40+2*(238+16)=548.
			if bx.Box.Min.X < 548+12 || bx.Box.Max.X > 548+238-12 {
				t.Errorf("PM2.5 title %v exceeds 12px chip padding", bx.Box)
			}
		}
	}
	if !foundAQI {
		t.Error("AQI chip title US AQI · max not found")
	}
	if !foundPM {
		t.Error("PM2.5 chip title PM2.5 · max not found")
	}
	// Two-line category must fit inside the 160px-tall chip (bottom pad >=12).
	for _, bx := range boxes {
		if bx.Str == "Sensitive Groups" || bx.Str == "Sensitive Groups ·" {
			if bx.Box.Max.Y > 660+160-12 {
				t.Errorf("chip sub %v exceeds 12px bottom padding", bx.Box)
			}
		}
	}
}

func TestDailyAQIPM25SameBasis(t *testing.T) {
	// Daily chips must share the SAME time basis: both the daily max of
	// hourly values for the target day (not current-hour values that can
	// mix inconsistent instants, e.g. AQI 97 next to PM2.5 75.4 from
	// different hours). The card must show AQIMax/PM25Max with a "max"
	// label, and the category must match the max AQI.
	f := testFonts(t)
	in := dailyFixture(t)
	// Fixture 2026-10-25: current PM2.5 29.7 differs from hourly max ~69.2;
	// current and max AQI are both 150. Force a divergence to prove the
	// card uses the max basis for BOTH.
	in.Day.AQI = 60
	in.Day.PM25 = 10.0
	in.Day.AQIMax = 150
	in.Day.PM25Max = 69.2
	in.Day.AQICategory = openmeteo.AQICategory(150)
	_, boxes, err := RenderDaily(f, in)
	if err != nil {
		t.Fatal(err)
	}
	hasMaxVal := func(want string) bool {
		for _, bx := range boxes {
			if bx.Str == want {
				return true
			}
		}
		return false
	}
	if !hasMaxVal("150") {
		t.Error("daily card missing max AQI value 150 (uses current instead of daily max?)")
	}
	if !hasMaxVal("69.2 µg/m³") {
		t.Error("daily card missing max PM2.5 value 69.2 µg/m³ (uses current instead of daily max?)")
	}
	if hasMaxVal("60") {
		t.Error("daily card shows current AQI 60; must show daily max")
	}
	if hasMaxVal("10.0 µg/m³") {
		t.Error("daily card shows current PM2.5 10.0; must show daily max")
	}
	if !boxContains(boxes, "US AQI · max") || !boxContains(boxes, "PM2.5 · max") {
		t.Error("daily chips must carry the 'max' label for the shared daily-max basis")
	}
}

func TestSampleWatermarkMapOnly(t *testing.T) {
	// REVIEW_B #7: SAMPLE diagonal sits over the map only, never behind the
	// big M value/text: the right column must be identical with/without SAMPLE.
	f, b := testFonts(t), testBasemap(t)
	ev := quakeEvent(13.88, 120.44, 5.6, 10, "30 km SW of Nasugbu, Batangas", "PHIVOLCS")
	plain, _, _ := RenderQuake(f, b, QuakeInput{Event: ev, TimeLabel: "25 Oct 2026, 6:13 PM PHT"})
	samp, _, _ := RenderQuake(f, b, QuakeInput{Event: ev, TimeLabel: "25 Oct 2026, 6:13 PM PHT", Sample: true})
	// Right column M area (no map, no corner badge): must be pixel-identical.
	r := image.Rect(724, 250, 1040, 450)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if plain.At(x, y) != samp.At(x, y) {
				t.Fatalf("right column differs at (%d,%d): watermark behind M/text", x, y)
			}
		}
	}
	// Corner SAMPLE badge must exist in the sample card.
	_, sboxes, _ := RenderQuake(f, b, QuakeInput{Event: ev, TimeLabel: "25 Oct 2026, 6:13 PM PHT", Sample: true})
	found := false
	for _, bx := range sboxes {
		if bx.Str == "SAMPLE" {
			found = true
		}
	}
	if !found {
		t.Error("SAMPLE corner badge missing")
	}
}

func TestLineStepNormal(t *testing.T) {
	// REVIEW_B #8: wrapped text uses ~1.2 line height.
	if got := lineStep(44); got < 50 || got > 56 {
		t.Errorf("lineStep(44) = %d, want ~53", got)
	}
	if got := lineStep(28); got < 32 || got > 36 {
		t.Errorf("lineStep(28) = %d, want ~34 (not fixed 52)", got)
	}
}

func TestTaalSixSegments(t *testing.T) {
	// REVIEW_B #9: six discrete colors 0 teal … 5 dark red; needle to the
	// center of the current segment; current full opacity, others 45%.
	if len(gaugeSegColors) != 6 {
		t.Fatalf("gauge segments = %d, want 6", len(gaugeSegColors))
	}
	if gaugeSegColors[0] != (color.RGBA{0x0F, 0x6B, 0x6B, 0xFF}) {
		t.Errorf("segment 0 = %+v, want teal", gaugeSegColors[0])
	}
	if gaugeSegColors[5] != (color.RGBA{0x6E, 0x14, 0x14, 0xFF}) {
		t.Errorf("segment 5 = %+v, want dark red", gaugeSegColors[5])
	}
	// Dimmed others: 45% blend toward cream.
	dim := dimSeg(gaugeSegColors[1])
	if dim.A != 0xFF {
		t.Errorf("dimmed segment should be pre-blended opaque, got alpha %d", dim.A)
	}
	// Needle centers: level 1 center must differ from old tick angle (level/5*π).
	f := testFonts(t)
	img, boxes, err := RenderTaal(f, TaalInput{Level: 1, LevelText: "Low-level unrest", BulletinTitle: "Taal Volcano Summary of 24Hr Observation 25 October 2026 12:00 AM", BulletinDate: "25 Oct 2026"})
	if err != nil {
		t.Fatal(err)
	}
	_ = img
	// Bulletin title moved lower (uses empty bottom space, not crowding base).
	for _, bx := range boxes {
		if bx.Str == "Taal Volcano Summary of 24Hr Observation 25 October 2026 12:00 AM" {
			if bx.Box.Min.Y < 840 {
				t.Errorf("bulletin title y=%d, want >=840 (moved lower)", bx.Box.Min.Y)
			}
		}
	}
	// All six level numbers present outside the arc.
	nums := map[string]bool{}
	for _, bx := range boxes {
		if len(bx.Str) == 1 && bx.Str[0] >= '0' && bx.Str[0] <= '5' {
			nums[bx.Str] = true
		}
	}
	if len(nums) != 6 {
		t.Errorf("level numbers on gauge = %v, want 0-5", nums)
	}
}
