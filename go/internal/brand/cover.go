package brand

import (
	"image"
	"image/color"
	"math"
)

// Cover layout constants from the spec.
const (
	CoverMarkD   = 230 // mark diameter on the cover
	CoverGap     = 56  // gap between mark and text column
	TitleSize    = 110 // starting size, shrunk to fit
	TaglineSize  = 40
	DisclaimSize = 28
	RuleW        = 150
	RuleH        = 10
	NavyBandH    = 56 // soft navy band at the bottom edge, outside safe area
)

// CoverLayout is the computed cover geometry. Boxes are the exact pixel
// bounding boxes of the mark and every text/rule element; the unit test
// asserts they all lie inside the safe area.
type CoverLayout struct {
	MarkRect                        image.Rectangle
	TitleBox, TagBox, DiscBox, Rule image.Rectangle
	TitleSize, TagSize, DiscSize    float64
	TitleBase, TagBase, DiscBase    int
	TextX                           int
}

// Boxes returns all content boxes (mark + text + rule).
func (l CoverLayout) Boxes() []image.Rectangle {
	return []image.Rectangle{l.MarkRect, l.TitleBox, l.TagBox, l.Rule, l.DiscBox}
}

// LayoutCover computes the cover layout: the whole group (mark + text
// column) is centered horizontally on 820 and vertically on 462.
func LayoutCover(f *Fonts) CoverLayout {
	maxTextW := SafeX1 - SafeX0 - CoverMarkD - CoverGap // 914

	titleSize := f.FitSize(Wordmark, Title, TitleSize, 60, maxTextW)
	tagSize := f.FitSize(CoverTagline, Semi, TaglineSize, 20, maxTextW)
	discSize := f.FitSize(CoverDisclaimer, NotoSemi, DisclaimSize, 14, maxTextW)

	titleW := f.Measure(Title, titleSize, Wordmark)
	tagW := f.Measure(Semi, tagSize, CoverTagline)
	discW := f.Measure(NotoSemi, discSize, CoverDisclaimer)
	_, _, titleH := f.Metrics(Title, titleSize)
	_, _, tagH := f.Metrics(Semi, tagSize)
	_, _, discH := f.Metrics(NotoSemi, discSize)

	textW := max(titleW, max(tagW, max(discW, RuleW)))
	groupW := CoverMarkD + CoverGap + textW
	markX := CoverCX - groupW/2
	textX := markX + CoverMarkD + CoverGap

	const gap1 = 20 // title -> tagline
	const gap2 = 18 // tagline -> rule
	const gap3 = 16 // rule -> disclaimer
	totalH := titleH + gap1 + tagH + gap2 + RuleH + gap3 + discH
	y := CoverCY - totalH/2

	titleY := y
	titleBase := titleY + mustAscent(f, Title, titleSize)
	tagY := titleY + titleH + gap1
	tagBase := tagY + mustAscent(f, Semi, tagSize)
	ruleY := tagY + tagH + gap2
	discY := ruleY + RuleH + gap3
	discBase := discY + mustAscent(f, NotoSemi, discSize)

	markY := CoverCY - CoverMarkD/2
	return CoverLayout{
		MarkRect:  image.Rect(markX, markY, markX+CoverMarkD, markY+CoverMarkD),
		TitleBox:  image.Rect(textX, titleY, textX+titleW, titleY+titleH),
		TagBox:    image.Rect(textX, tagY, textX+tagW, tagY+tagH),
		DiscBox:   image.Rect(textX, discY, textX+discW, discY+discH),
		Rule:      image.Rect(textX, ruleY, textX+RuleW, ruleY+RuleH),
		TitleSize: titleSize, TagSize: tagSize, DiscSize: discSize,
		TitleBase: titleBase, TagBase: tagBase, DiscBase: discBase,
		TextX: textX,
	}
}

func mustAscent(f *Fonts, s Style, size float64) int {
	a, _, _ := f.Metrics(s, size)
	return a
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// decorWave paints one subtle horizontal sine contour line across the cover.
func decorWave(dst *image.RGBA, yBase int, amp, waveLen, phase float64, thick float64, c color.RGBA) {
	for x := 0; x < CoverW; x++ {
		y := float64(yBase) + amp*math.Sin(2*math.Pi*float64(x)/waveLen+phase)
		fillDisc(dst, x, int(y+0.5), thick/2, c)
	}
}

// RenderCover renders the 1640x924 FB cover and returns it with the content
// boxes (for the safe-area test).
func RenderCover(f *Fonts) (*image.RGBA, []image.Rectangle, error) {
	dst := image.NewRGBA(image.Rect(0, 0, CoverW, CoverH))
	fillRect(dst, dst.Bounds(), Cream)

	// Very subtle teal contour/wave background lines (low alpha).
	decor := color.RGBA{Teal.R, Teal.G, Teal.B, 13}
	decorWave(dst, 110, 26, 460, 0.0, 3, decor)
	decorWave(dst, 300, 30, 520, 1.3, 3, decor)
	decorWave(dst, 490, 24, 430, 2.6, 3, decor)
	decorWave(dst, 680, 30, 500, 0.7, 3, decor)
	decorWave(dst, 850, 26, 470, 2.0, 3, decor)

	// Soft navy band at the bottom edge, strictly outside the safe area.
	fillRect(dst, image.Rect(0, CoverH-NavyBandH, CoverW, CoverH), Navy)

	lay := LayoutCover(f)

	mark, err := RenderMark(CoverMarkD)
	if err != nil {
		return nil, nil, err
	}
	pasteOver(dst, mark, lay.MarkRect.Min.X, lay.MarkRect.Min.Y)

	f.DrawString(dst, Wordmark, Title, lay.TitleSize, Navy, lay.TextX, lay.TitleBase)
	f.DrawString(dst, CoverTagline, Semi, lay.TagSize, Teal, lay.TextX, lay.TagBase)
	// Short amber rule.
	fillRect(dst, lay.Rule, Amber)
	fillDiscSolid(dst, lay.Rule.Min.X+RuleH/2, lay.Rule.Min.Y+RuleH/2, RuleH/2, Amber)
	fillDiscSolid(dst, lay.Rule.Max.X-RuleH/2-1, lay.Rule.Min.Y+RuleH/2, RuleH/2, Amber)
	discCol := color.RGBA{Navy.R, Navy.G, Navy.B, 0xB3} // navy at ~70%
	f.DrawString(dst, CoverDisclaimer, NotoSemi, lay.DiscSize, discCol, lay.TextX, lay.DiscBase)

	return dst, lay.Boxes(), nil
}

// fillDiscSolid paints an opaque filled disc.
func fillDiscSolid(dst *image.RGBA, cx, cy int, rad int, c color.Color) {
	for y := cy - rad; y <= cy+rad; y++ {
		for x := cx - rad; x <= cx+rad; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= rad*rad {
				if (image.Point{X: x, Y: y}).In(dst.Bounds()) {
					dst.Set(x, y, c)
				}
			}
		}
	}
}
