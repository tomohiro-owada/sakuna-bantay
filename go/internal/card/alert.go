package card

import (
	"image"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
)

// AlertInput is the plain data for the generic text alert card (1080x1080):
// used for tsunami & rain alerts, which have no dedicated map/gauge visual
// (PHASE_D §3).
type AlertInput struct {
	Title     string   // e.g. "Tsunami Advisory" / "Orange Rainfall Warning"
	Lines     []string // short detail lines (areas, headline, instruction)
	TimeLabel string   // "25 Oct 2026, 6:13 PM PHT" (header right)
	Sources   []string // footer sources; defaults to PAGASA
	Sample    bool
}

// RenderAlert renders the generic text alert card: brand chrome, big red
// title, bulleted detail lines.
func RenderAlert(f *fonts.Fonts, in AlertInput) (*image.RGBA, []TextBox, error) {
	c := New(f)
	srcs := in.Sources
	if len(srcs) == 0 {
		srcs = []string{"DOST-PAGASA (CC BY 4.0)"}
	}
	body := c.Chrome("Alert", in.TimeLabel, srcs, in.Sample)
	_ = body
	if in.Sample {
		c.sampleBand()
	}
	title := in.Title
	if title == "" {
		title = "Weather Alert"
	}
	tsize := c.Fit(title, fonts.Title, 64, 30, Size-80)
	y := 280
	for i, ln := range c.Wrap(fonts.Title, tsize, title, Size-80) {
		if i >= 3 {
			break
		}
		c.Text(ln, fonts.Title, tsize, red, 40, y)
		y += lineStep(tsize)
	}
	y += 28
	lines := in.Lines
	if len(lines) == 0 {
		lines = []string{"See PAGASA and your LGU for details."}
	}
	n := 0
	for _, ln := range lines {
		for _, w := range c.Wrap(fonts.NotoReg, 30, "• "+ln, Size-80) {
			if y > 940 || n >= 8 {
				break
			}
			c.Text(w, fonts.NotoReg, 30, navy, 40, y)
			y += lineStep(30)
			n++
		}
		y += 10
	}
	return c.Img, c.Boxes, nil
}
