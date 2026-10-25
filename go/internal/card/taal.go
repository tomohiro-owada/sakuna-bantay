package card

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
)

// TaalInput is the plain data for the Taal gauge card.
type TaalInput struct {
	Level         int    // 0–5
	LevelText     string // e.g. "Low-level unrest"
	BulletinTitle string // e.g. "Taal Volcano Summary of 24Hr Observation 25 October 2026 12:00 AM"
	BulletinDate  string // pretty date for display
	Sample        bool
}

// gaugeSegColors colors the six discrete level segments 0–5
// (REVIEW_B #9): 0 teal … 5 dark red.
var gaugeSegColors = []color.RGBA{
	{0x0F, 0x6B, 0x6B, 0xFF}, // 0 teal
	{0x2A, 0x9D, 0x8F, 0xFF}, // 1 teal-lite
	{0xF7, 0xD7, 0x74, 0xFF}, // 2 light amber
	{0xF2, 0xA5, 0x16, 0xFF}, // 3 amber
	{0xE0, 0x7B, 0x39, 0xFF}, // 4 orange-red
	{0x6E, 0x14, 0x14, 0xFF}, // 5 dark red
}

// dimSeg blends a segment color toward cream for non-current segments
// (45% opacity over the cream card background, pre-blended to opaque).
func dimSeg(col color.RGBA) color.RGBA {
	const k = 0.45
	return color.RGBA{
		R: uint8(k*float64(col.R) + (1-k)*float64(cream.R)),
		G: uint8(k*float64(col.G) + (1-k)*float64(cream.G)),
		B: uint8(k*float64(col.B) + (1-k)*float64(cream.B)),
		A: 0xFF,
	}
}

// RenderTaal renders the Taal gauge card: semicircular 0–5 gauge with six
// discrete segments, the needle at the CENTER of the current level's
// segment, big level text, bulletin date, reminders.
func RenderTaal(f *fonts.Fonts, in TaalInput) (*image.RGBA, []TextBox, error) {
	c := New(f)
	body := c.Chrome("Taal Volcano", in.BulletinDate, []string{"PHIVOLCS"}, in.Sample)
	_ = body
	if in.Sample {
		c.sampleBand()
	}

	lvl := in.Level
	if lvl < 0 {
		lvl = 0
	}
	if lvl > 5 {
		lvl = 5
	}
	// Big level text.
	head := fmt.Sprintf("Alert Level %d", lvl)
	hsize := c.Fit(head, fonts.Title, 72, 40, Size-80)
	c.Text(head, fonts.Title, hsize, navy, 40, 270)
	tsize := c.Fit(in.LevelText, fonts.Semi, 44, 28, Size-80)
	c.Text(in.LevelText, fonts.Semi, tsize, teal, 40, 326)

	// Gauge: center (540, 760), radius 270. Six discrete segments of 30°
	// each with a small gap; numbers centered on each segment outside the
	// arc; needle to the center of the current level's segment; current
	// segment full opacity, others 45%.
	cx, cy, rad := 540, 760, 270.0
	segW := math.Pi / 6
	gap := 4 * math.Pi / 180
	centerAngle := func(level int) float64 { return math.Pi - (float64(level)+0.5)*segW }
	for i := 0; i < 6; i++ {
		a0 := math.Pi - float64(i+1)*segW + gap/2
		a1 := math.Pi - float64(i)*segW - gap/2
		col := gaugeSegColors[i]
		if i != lvl {
			col = dimSeg(col)
		}
		steps := 48
		for s := 0; s <= steps; s++ {
			a := a0 + (a1-a0)*float64(s)/float64(steps)
			fillDisc(c.Img, cx+int(math.Round(rad*math.Cos(a))), cy-int(math.Round(rad*math.Sin(a))), 13, col)
		}
	}
	// Level numbers centered on each segment, outside the arc.
	for i := 0; i < 6; i++ {
		a := centerAngle(i)
		nx := cx + int(math.Round((rad+58)*math.Cos(a)))
		ny := cy - int(math.Round((rad+58)*math.Sin(a)))
		num := fmt.Sprintf("%d", i)
		lw := c.F.Measure(fonts.Title, 30, num)
		dx := 0
		if i == 0 {
			dx = -8
		}
		if i == 5 {
			dx = 8
		}
		col := navy
		if i == lvl {
			// Emphasize the current level number.
			col = navy
		}
		c.Text(num, fonts.Title, 30, col, nx-lw/2+dx, ny+11)
	}
	// Needle to the CENTER of the current level's segment.
	a := centerAngle(lvl)
	nx := cx + int(math.Round((rad-40)*math.Cos(a)))
	ny := cy - int(math.Round((rad-40)*math.Sin(a)))
	thickLine(c.Img, cx, cy, nx, ny, 12, navy)
	fillDisc(c.Img, cx, cy, 22, navy)
	fillDisc(c.Img, cx, cy, 10, cream)

	// Bulletin line + reminders: moved lower with spacing, using the empty
	// bottom space so they never crowd the gauge base (REVIEW_B #9).
	y := 872
	if in.BulletinTitle != "" {
		for _, ln := range c.Wrap(fonts.NotoReg, 24, in.BulletinTitle, Size-80) {
			if y > 912 {
				break
			}
			lw := c.F.Measure(fonts.NotoReg, 24, ln)
			c.Text(ln, fonts.NotoReg, 24, grayTx, (Size-lw)/2, y)
			y += lineStep(24)
		}
	}
	y += 8
	rem := "Entry to Taal Volcano Island (PDZ) not allowed. Follow PHIVOLCS updates."
	for _, ln := range c.Wrap(fonts.Semi, 25, rem, Size-80) {
		lw := c.F.Measure(fonts.Semi, 25, ln)
		c.Text(ln, fonts.Semi, 25, red, (Size-lw)/2, y)
		y += lineStep(25)
	}
	return c.Img, c.Boxes, nil
}
