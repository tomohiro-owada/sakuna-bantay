package card

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/openmeteo"
)

// DailyInput is the plain data for the daily Las Piñas card.
type DailyInput struct {
	Day       openmeteo.Today
	DateLabel string // "25 Oct 2026" (PHT calendar date line)
	TimeLabel string // "25 Oct 2026, 6:13 PM PHT" (header right)
	TaalLevel int
	TaalText  string // e.g. "Low-level unrest"
	Alerts    []string
	Sample    bool
	// Now is the render time. When it falls later than 06:00 PHT on the
	// target day (manual/late run), the rain chart starts at the current
	// hour (rounded down) so past hours are not shown. Zero means "posted
	// at 06:00": the full 06:00–23:00 window.
	Now time.Time
}

func phtLoc() *time.Location {
	if loc, err := time.LoadLocation("Asia/Manila"); err == nil {
		return loc
	}
	return time.FixedZone("PHT", 8*3600)
}

// DailyWindowStart returns the chart start hour for the target day
// (YYYY-MM-DD): 06:00 when rendered at/before 06:00 PHT on the target day
// (or when now is zero / another date), else the current hour rounded down
// (clamped to 06–23). PHASE_D 1b.
func DailyWindowStart(dayDate string, now time.Time) int {
	const base = 6
	if now.IsZero() {
		return base
	}
	if dayDate != now.In(phtLoc()).Format("2006-01-02") {
		return base
	}
	h := now.In(phtLoc()).Hour()
	if h <= base {
		return base
	}
	if h > 23 {
		return 23
	}
	return h
}

// DailyDisplay returns the displayed hourly window for a render time: the
// hours at/after the window start plus the max probability over that SAME
// window (used for the hero line, chart note and post text). PHASE_D 1b.
func DailyDisplay(day openmeteo.Today, now time.Time) (hours []openmeteo.HourRain, startHour, maxProb int) {
	startHour = DailyWindowStart(day.Date, now)
	for _, h := range day.HourlyRain {
		if h.Hour >= startHour {
			hours = append(hours, h)
		}
	}
	if len(hours) == 0 {
		hours = day.HourlyRain
		startHour = 6
	}
	for _, h := range hours {
		if h.Prob > maxProb {
			maxProb = h.Prob
		}
	}
	return hours, startHour, maxProb
}

// RenderDaily renders the daily Las Piñas card.
func RenderDaily(f *fonts.Fonts, in DailyInput) (*image.RGBA, []TextBox, error) {
	c := New(f)
	body := c.Chrome("Daily · Las Piñas", in.TimeLabel, []string{
		"DOST-PAGASA (CC BY 4.0)", "Open-Meteo (CC BY 4.0)", "PHIVOLCS",
	}, in.Sample)
	_ = body
	if in.Sample {
		c.sampleBand()
	}

	// Date line.
	dateSize := c.Fit(in.DateLabel, fonts.Title, 44, MinBodyPx, 1000)
	c.Text(in.DateLabel, fonts.Title, dateSize, navy, 40, 232)

	// Hero: big max temp left, details right. The "up to X%" number is the
	// max over the SAME displayed window as the chart (PHASE_D 1b).
	_, startHour, windowMax := DailyDisplay(in.Day, in.Now)
	tmax := fmt.Sprintf("%.0f°", in.Day.TMax)
	c.Text(tmax, fonts.Title, 150, navy, 40, 376)
	rx := 330
	c.Text(fmt.Sprintf("Feels like %.0f°", in.Day.ApparentMax), fonts.Semi, 44, teal, rx, 262)
	c.Text(fmt.Sprintf("Low %.0f°", in.Day.TMin), fonts.Semi, 40, grayTx, rx, 318)
	rain := fmt.Sprintf("Rain %.0f mm · up to %d%%", in.Day.PrecipSum, windowMax)
	rainSize := c.Fit(rain, fonts.Semi, 36, MinBodyPx, Size-40-rx)
	c.Text(rain, fonts.Semi, rainSize, grayTx, rx, 368)

	// Rain bars panel: fixed 0–100% scale with faint gridlines at
	// 25/50/75/100 + small % axis labels (REVIEW_B #5). One bar per hour,
	// wide bars with a small gap. % value above bars >= 20%; when the day's
	// max is < 20% the chart shows a centered low-chance note instead.
	py := 420
	c.Text(fmt.Sprintf("Rain chance from %02d:00 (PHT)", startHour), fonts.Semi, 26, navy, 40, py)
	plotTop, plotBot := py+30, py+170
	hours, _, _ := DailyDisplay(in.Day, in.Now)
	n := len(hours)
	if n == 0 {
		c.Text("No hourly data", fonts.NotoReg, 24, grayTx, 40, plotBot)
	} else {
		plotW := Size - 80
		slot := float64(plotW) / float64(n)
		barW := int(slot * 0.85) // wide bars, small gap
		if barW < 8 {
			barW = 8
		}
		const labelH = 24 // room above 100% for the top % labels
		barTop := plotTop + labelH
		barH := plotBot - barTop
		gridCol := color.NRGBA{teal.R, teal.G, teal.B, 0x44}
		// Faint gridlines + axis labels (drawn first, bars over them).
		for _, pct := range []int{25, 50, 75, 100} {
			gy := barTop + (100-pct)*barH/100
			fillRect(c.Img, image.Rect(40, gy, Size-40, gy+1), gridCol)
			al := fmt.Sprintf("%d%%", pct)
			c.Text(al, fonts.NotoReg, MinBodyPx, grayTx, 40, gy-6)
		}
		// REVIEW_C #3 + PHASE_D 1b: single number for header, chart and
		// text is the max hourly probability over the DISPLAYED window
		// (from hourly data), not the daily aggregate. Thresholds shared
		// with posttext: <20% low, 20–49% some chance, >=50% likely.
		// % labels at >=20, likely color at >=50.
		maxProb := windowMax
		if maxProb < 20 {
			note := "Mababa ang tsansa ng ulan · Low chance of rain all day"
			nsize := c.Fit(note, fonts.NotoSemi, 24, MinBodyPx, plotW-40)
			lw := c.F.Measure(fonts.NotoSemi, nsize, note)
			c.Text(note, fonts.NotoSemi, nsize, grayTx, 40+(plotW-lw)/2, (plotTop+plotBot)/2+8)
		}
		for i, h := range hours {
			bx := 40 + int(float64(i)*slot+(slot-float64(barW))/2)
			bh := int(float64(h.Prob) / 100 * float64(barH))
			if h.Prob > 0 && bh < 4 {
				bh = 4
			}
			col := tealLt
			if h.Prob >= 50 {
				col = teal
			}
			if h.Prob >= 80 {
				col = amber
			}
			fillRect(c.Img, image.Rect(bx, plotBot-bh, bx+barW, plotBot), col)
			if h.Prob >= 20 {
				lbl := fmt.Sprintf("%d%%", h.Prob)
				lw := c.F.Measure(fonts.NotoSemi, MinBodyPx, lbl)
				c.Text(lbl, fonts.NotoSemi, MinBodyPx, navy, 40+int(float64(i)*slot+slot/2)-lw/2, plotBot-bh-8)
			}
			if h.Hour%3 == 0 {
				hl := fmt.Sprintf("%02d", h.Hour)
				lw := c.F.Measure(fonts.NotoReg, MinBodyPx, hl)
				c.Text(hl, fonts.NotoReg, MinBodyPx, grayTx, 40+int(float64(i)*slot+slot/2)-lw/2, plotBot+30)
			}
		}
		thickLine(c.Img, 40, plotBot, Size-40, plotBot, 2, grayTx)
	}

	// Chips: UV, AQI, PM2.5, Taal. Taller chips (160px) so two-line category
	// text keeps >= 12px padding. AQI and PM2.5 share the SAME time basis:
	// the daily max of hourly values for the target day (labelled "max"),
	// so a single hour's inconsistent AQI/PM2.5 pair can never appear side
	// by side. Both carry the model-est. note.
	cy, chH := 660, 160
	chips := [][3]string{
		{"UV INDEX", fmt.Sprintf("%.0f", in.Day.UVMax), in.Day.UVCategory},
		{"US AQI · max", fmt.Sprintf("%d", in.Day.AQIMax), in.Day.AQICategory + " · model est."},
		{"PM2.5 · max", fmt.Sprintf("%.1f µg/m³", in.Day.PM25Max), "model est."},
		{"TAAL", fmt.Sprintf("Level %d", in.TaalLevel), in.TaalText},
	}
	cw := (Size - 80 - 3*16) / 4
	for i, ch := range chips {
		x0 := 40 + i*(cw+16)
		r := image.Rect(x0, cy, x0+cw, cy+chH)
		fillRoundRect(c.Img, r, 14, white)
		strokeRoundRect(c.Img, r, 14, 2, teal)
		tsize := c.Fit(ch[0], fonts.NotoSemi, MinBodyPx, MinBodyPx, cw-24)
		c.Text(ch[0], fonts.NotoSemi, tsize, grayTx, x0+12, cy+34)
		vsize := c.Fit(ch[1], fonts.Title, 36, MinBodyPx, cw-24)
		c.Text(ch[1], fonts.Title, vsize, navy, x0+12, cy+76)
		subs := c.Wrap(fonts.NotoReg, MinBodyPx, ch[2], cw-24)
		for j, ln := range subs {
			if j >= 2 {
				break
			}
			c.Text(ln, fonts.NotoReg, MinBodyPx, grayTx, x0+12, cy+106+j*lineStep(MinBodyPx))
		}
	}

	// Alerts strip (below the taller 160px chips).
	ay := 850
	c.Text("Alerts", fonts.Semi, 26, navy, 40, ay)
	lines := in.Alerts
	if len(lines) == 0 {
		lines = []string{"No active PAGASA warnings for NCR area"}
	}
	y := ay + 34
	for i, a := range lines {
		if i >= 2 {
			break
		}
		for _, ln := range c.Wrap(fonts.NotoReg, 25, "• "+a, Size-80) {
			if y > 950 {
				break
			}
			c.Text(ln, fonts.NotoReg, 25, navy, 40, y)
			y += lineStep(25)
		}
	}
	return c.Img, c.Boxes, nil
}
