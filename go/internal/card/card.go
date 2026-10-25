// Package card renders the four 1080x1080 Sakuna Bantay cards (daily,
// earthquake, typhoon/signal, Taal gauge) in Go using go/internal/fonts and
// go/internal/brand. See docs/SPEC.md §card.
//
// Renderers take plain structs (from the phase-A packages) so they are
// testable with fixtures. Every renderer returns the drawn image plus the
// bounding boxes of all text elements; the tests assert nothing overflows
// the canvas and all body text is >= MinBodyPx.
package card

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strings"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/brand"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
	xdraw "golang.org/x/image/draw"
)

// Size is the card canvas (1080x1080 per spec).
const Size = 1080

// MinBodyPx is the minimum legible body text size on the 1080 canvas.
const MinBodyPx = 22.0

var (
	navy   = color.RGBA{0x0E, 0x2A, 0x3B, 0xFF}
	teal   = color.RGBA{0x0F, 0x6B, 0x6B, 0xFF}
	tealLt = color.RGBA{0x2A, 0x9D, 0x8F, 0xFF}
	amber  = color.RGBA{0xF2, 0xA5, 0x16, 0xFF}
	cream  = color.RGBA{0xF7, 0xF2, 0xE6, 0xFF}
	white  = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	red    = color.RGBA{0xC0, 0x39, 0x2B, 0xFF}
	grayTx = color.RGBA{0x3D, 0x55, 0x66, 0xFF} // muted navy for secondary text
)

// TextBox is one drawn text element: its pixel box, the point size used,
// and the string (for self-describing overflow failures). Renderers return
// these so tests can assert nothing overflows the canvas and everything
// meets the legibility floor.
type TextBox struct {
	Box  image.Rectangle
	Size float64
	Str  string
}

// Canvas is a draw surface plus the text boxes drawn on it (for overflow
// tests). Sizes are recorded so tests can enforce MinBodyPx.
type Canvas struct {
	Img   *image.RGBA
	F     *fonts.Fonts
	Boxes []TextBox
}

// New creates a cream 1080x1080 canvas.
func New(f *fonts.Fonts) *Canvas {
	dst := image.NewRGBA(image.Rect(0, 0, Size, Size))
	fillRect(dst, dst.Bounds(), cream)
	return &Canvas{Img: dst, F: f}
}

// Text draws str with the baseline at (x, y), records its box + size, and
// returns the pixel width. The caller must have fitted str to maxW (see
// Fit/Ellipsize); Text clips defensively but never wraps.
func (c *Canvas) Text(str string, s fonts.Style, size float64, col color.Color, x, y int) int {
	w := c.F.DrawString(c.Img, str, s, size, col, x, y)
	asc, desc, _ := c.F.Metrics(s, size)
	c.Boxes = append(c.Boxes, TextBox{Box: image.Rect(x, y-asc, x+w, y+desc), Size: size, Str: str})
	return w
}

// Fit shrinks size until str fits maxW (down to minSize).
func (c *Canvas) Fit(str string, s fonts.Style, size, minSize float64, maxW int) float64 {
	return c.F.FitSize(str, s, size, minSize, maxW)
}

// Wrap greedly wraps text to lines of at most maxW (splits overlong words by
// rune for Japanese). Returns the lines.
func (c *Canvas) Wrap(s fonts.Style, size float64, text string, maxW int) []string {
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		cur := ""
		flush := func() {
			if cur != "" {
				lines = append(lines, cur)
				cur = ""
			}
		}
		for _, w := range words {
			// Break overlong words rune by rune.
			for c.F.Measure(s, size, w) > maxW {
				cut := w
				for len(cut) > 0 && c.F.Measure(s, size, cut) > maxW {
					cut = cut[:len(cut)-1]
					for len(cut) > 0 && (cut[len(cut)-1]&0xC0) == 0x80 {
						cut = cut[:len(cut)-1]
					}
				}
				if cut == "" || cut == w {
					break
				}
				if cur == "" {
					lines = append(lines, cut)
				} else if c.F.Measure(s, size, cur+" "+cut) <= maxW {
					lines = append(lines, cur+" "+cut)
					cur = ""
				} else {
					flush()
					lines = append(lines, cut)
				}
				w = strings.TrimPrefix(w, cut)
				w = strings.TrimLeft(w, " ")
				if w == "" {
					break
				}
			}
			if w == "" {
				continue
			}
			if cur == "" {
				cur = w
			} else if c.F.Measure(s, size, cur+" "+w) <= maxW {
				cur += " " + w
			} else {
				flush()
				cur = w
			}
		}
		flush()
	}
	return lines
}

// Ellipsize truncates str with "…" to fit maxW (size fixed).
func (c *Canvas) Ellipsize(str string, s fonts.Style, size float64, maxW int) string {
	if c.F.Measure(s, size, str) <= maxW {
		return str
	}
	for len(str) > 0 {
		str = str[:len(str)-1]
		for len(str) > 0 && (str[len(str)-1]&0xC0) == 0x80 {
			str = str[:len(str)-1]
		}
		if c.F.Measure(s, size, str+"…") <= maxW {
			return str + "…"
		}
	}
	return "…"
}

// lineStep returns the normal line advance (~1.2× size) for wrapped text.
// REVIEW_B #8: wrapped text everywhere must use normal line height, not a
// fixed 52px step that leaves a huge gap when the font was fitted smaller.
func lineStep(size float64) int {
	return int(math.Ceil(size * 1.2))
}

// --- Chrome: header band + footer + SAMPLE badge ---

const (
	headerH    = 150
	headerRule = 6
	footerY    = 986
)

// Chrome draws the header band (brand mark + "Sakuna Bantay" + card title +
// date/time PHT) and the footer (source attribution + unofficial line).
// sources lists only the relevant sources for the card. It returns the body
// rect available between header and footer.
func (c *Canvas) Chrome(title, dateLabel string, sources []string, sample bool) image.Rectangle {
	// Header.
	mark, err := brand.RenderMark(96)
	if err == nil {
		draw.Draw(c.Img, image.Rect(40, 27, 136, 123), mark, image.Point{}, draw.Over)
	}
	c.Text("Sakuna Bantay", fonts.Title, 40, navy, 152, 78)
	c.Text(title, fonts.Semi, 31, teal, 154, 120)
	if dateLabel != "" {
		w := c.F.Measure(fonts.NotoSemi, 26, dateLabel)
		c.Text(dateLabel, fonts.NotoSemi, 26, navy, Size-40-w, 82)
	}
	fillRect(c.Img, image.Rect(0, headerH, Size, headerH+headerRule), teal)
	if sample {
		c.sampleTag()
	}
	// Footer.
	fillRect(c.Img, image.Rect(0, footerY-14, Size, footerY-12), color.NRGBA{teal.R, teal.G, teal.B, 0x55})
	src := "Data: " + strings.Join(sources, ", ")
	src = c.Ellipsize(src, fonts.NotoReg, MinBodyPx, Size-80)
	c.Text(src, fonts.NotoReg, MinBodyPx, grayTx, 40, footerY+12)
	c.Text("Unofficial · Not an official warning · abalol.xyz/sakuna-bantay",
		fonts.NotoSemi, MinBodyPx, grayTx, 40, footerY+40)
	return image.Rect(0, headerH+headerRule, Size, footerY-14)
}

// sampleTag draws the SAMPLE corner badge. The diagonal watermark is NOT
// drawn here: map cards draw it over the map panel only (so it never sits
// behind the big M value/text), while non-map cards draw a full-width
// diagonal band at 12% alpha behind everything (see sampleBand).
// REVIEW_B #7.
func (c *Canvas) sampleTag() {
	const label = "SAMPLE"
	size := 28.0
	w := c.F.Measure(fonts.Title, size, label)
	x0 := Size - 40 - (w + 36)
	r := image.Rect(x0, headerH+18, Size-40, headerH+18+52)
	fillRoundRect(c.Img, r, 12, red)
	c.Text(label, fonts.Title, size, white, x0+18, headerH+18+38)
}

// sampleBand draws a full-width faint diagonal SAMPLE at 12% alpha behind
// everything, for non-map cards (daily, Taal). It must remain clearly
// visible: large red text at low alpha over the cream background.
func (c *Canvas) sampleBand() {
	const label = "SAMPLE"
	size := 190.0
	w := c.F.Measure(fonts.Title, size, label)
	_, _, lh := c.F.Metrics(fonts.Title, size)
	tmp := image.NewRGBA(image.Rect(0, 0, w+40, lh+40))
	c.F.DrawString(tmp, label, fonts.Title, size,
		color.NRGBA{red.R, red.G, red.B, 0x1F}, 20, 20+lh/2+20)
	rot := rotateImage(tmp, -30*math.Pi/180)
	off := image.Pt((Size-rot.Bounds().Dx())/2, (Size-rot.Bounds().Dy())/2)
	draw.Draw(c.Img, image.Rectangle{Min: off, Max: off.Add(rot.Bounds().Size())},
		rot, image.Point{}, draw.Over)
}

// rotateImage rotates src about its center (nearest neighbour; transparent bg).
func rotateImage(src *image.RGBA, angle float64) *image.RGBA {
	sb := src.Bounds()
	cx, cy := float64(sb.Dx())/2, float64(sb.Dy())/2
	sin, cos := math.Sin(angle), math.Cos(angle)
	corners := [][2]float64{{-cx, -cy}, {cx, -cy}, {cx, cy}, {-cx, cy}}
	maxX, maxY := 0.0, 0.0
	for _, p := range corners {
		x := math.Abs(p[0]*cos - p[1]*sin)
		y := math.Abs(p[0]*sin + p[1]*cos)
		maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
	}
	dw, dh := int(math.Ceil(maxX*2))+1, int(math.Ceil(maxY*2))+1
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	dcx, dcy := float64(dw)/2, float64(dh)/2
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			rx := float64(x) - dcx
			ry := float64(y) - dcy
			sx := rx*cos + ry*sin + cx
			sy := -rx*sin + ry*cos + cy
			ix, iy := int(math.Round(sx)), int(math.Round(sy))
			if image.Pt(ix, iy).In(sb) {
				dst.Set(x, y, src.RGBAAt(ix, iy))
			}
		}
	}
	return dst
}

// scaleImage high-quality scales src to w x h.
func scaleImage(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// --- Raster helpers ---

// fillRect fills r (clipped to dst) with c. Opaque colors take the fast
// draw.Src path; translucent colors are blended pixel by pixel in
// premultiplied space (Go's draw.Src of a non-opaque color must not be
// Over-composited afterwards — the values wrap around).
func fillRect(dst *image.RGBA, r image.Rectangle, c color.Color) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	if _, _, _, a := c.RGBA(); a == 0xffff {
		draw.Draw(dst, r, &image.Uniform{c}, image.Point{}, draw.Src)
		return
	}
	sr, sg, sb, sa := c.RGBA() // premultiplied 16-bit
	inv := 0xffff - sa
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := dst.PixOffset(x, y)
			dr := uint32(dst.Pix[i]) * 0x101
			dg := uint32(dst.Pix[i+1]) * 0x101
			db := uint32(dst.Pix[i+2]) * 0x101
			da := uint32(dst.Pix[i+3]) * 0x101
			dst.Pix[i] = uint8((sr + dr*inv/0xffff) >> 8)
			dst.Pix[i+1] = uint8((sg + dg*inv/0xffff) >> 8)
			dst.Pix[i+2] = uint8((sb + db*inv/0xffff) >> 8)
			dst.Pix[i+3] = uint8((sa + da*inv/0xffff) >> 8)
		}
	}
}

func fillRoundRect(dst *image.RGBA, r image.Rectangle, rad int, c color.Color) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	inner := image.Rect(r.Min.X+rad, r.Min.Y, r.Max.X-rad, r.Max.Y)
	fillRect(dst, inner, c)
	inner2 := image.Rect(r.Min.X, r.Min.Y+rad, r.Max.X, r.Max.Y-rad)
	fillRect(dst, inner2, c)
	for _, corner := range []image.Point{
		{r.Min.X + rad, r.Min.Y + rad},
		{r.Max.X - rad - 1, r.Min.Y + rad},
		{r.Min.X + rad, r.Max.Y - rad - 1},
		{r.Max.X - rad - 1, r.Max.Y - rad - 1},
	} {
		fillDisc(dst, corner.X, corner.Y, float64(rad), c)
	}
}

func strokeRoundRect(dst *image.RGBA, r image.Rectangle, rad int, thick int, c color.Color) {
	for i := 0; i < thick; i++ {
		rr := image.Rect(r.Min.X+i, r.Min.Y+i, r.Max.X-i, r.Max.Y-i)
		roundRectOutline(dst, rr, rad, c)
	}
}

func roundRectOutline(dst *image.RGBA, r image.Rectangle, rad int, c color.Color) {
	for x := r.Min.X + rad; x < r.Max.X-rad; x++ {
		setPx(dst, x, r.Min.Y, c)
		setPx(dst, x, r.Max.Y-1, c)
	}
	for y := r.Min.Y + rad; y < r.Max.Y-rad; y++ {
		setPx(dst, r.Min.X, y, c)
		setPx(dst, r.Max.X-1, y, c)
	}
	for _, corner := range []struct {
		cx, cy, qx, qy int
	}{
		{r.Min.X + rad, r.Min.Y + rad, -1, -1},
		{r.Max.X - rad - 1, r.Min.Y + rad, 1, -1},
		{r.Min.X + rad, r.Max.Y - rad - 1, -1, 1},
		{r.Max.X - rad - 1, r.Max.Y - rad - 1, 1, 1},
	} {
		for a := 0; a <= 90; a++ {
			t := float64(a) * math.Pi / 180
			var dx, dy int
			if corner.qx < 0 {
				dx = -int(math.Round(float64(rad) * math.Cos(t)))
			} else {
				dx = int(math.Round(float64(rad) * math.Cos(t)))
			}
			if corner.qy < 0 {
				dy = -int(math.Round(float64(rad) * math.Sin(t)))
			} else {
				dy = int(math.Round(float64(rad) * math.Sin(t)))
			}
			setPx(dst, corner.cx+dx, corner.cy+dy, c)
		}
	}
}

func setPx(dst *image.RGBA, x, y int, c color.Color) {
	if image.Pt(x, y).In(dst.Bounds()) {
		dst.Set(x, y, c)
	}
}

func fillDisc(dst *image.RGBA, cx, cy int, rad float64, c color.Color) {
	r := int(math.Ceil(rad))
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if math.Hypot(float64(x-cx), float64(y-cy)) <= rad {
				setPx(dst, x, y, c)
			}
		}
	}
}

func strokeCircle(dst *image.RGBA, cx, cy int, rad float64, thick int, c color.Color) {
	for i := 0; i < thick; i++ {
		circleOutline(dst, cx, cy, rad-float64(i), c)
	}
}

func circleOutline(dst *image.RGBA, cx, cy int, rad float64, c color.Color) {
	if rad <= 0 {
		return
	}
	steps := int(math.Max(64, 2*math.Pi*rad))
	for i := 0; i < steps; i++ {
		t := 2 * math.Pi * float64(i) / float64(steps)
		setPx(dst, cx+int(math.Round(rad*math.Cos(t))), cy+int(math.Round(rad*math.Sin(t))), c)
	}
}

// dashedCircle draws a dashed circle (for distance rings).
func dashedCircle(dst *image.RGBA, cx, cy int, rad float64, thick int, dash, gap float64, c color.Color) {
	if rad <= 0 {
		return
	}
	total := dash + gap
	steps := int(math.Max(128, 2*math.Pi*rad))
	for i := 0; i < steps; i++ {
		arc := 2 * math.Pi * rad * float64(i) / float64(steps)
		if math.Mod(arc, total) < dash {
			t := 2 * math.Pi * float64(i) / float64(steps)
			px := cx + int(math.Round(rad*math.Cos(t)))
			py := cy + int(math.Round(rad*math.Sin(t)))
			fillDisc(dst, px, py, float64(thick)/2, c)
		}
	}
}

// thickLine draws a line of width w (round caps).
func thickLine(dst *image.RGBA, x0, y0, x1, y1 int, w float64, c color.Color) {
	dx, dy := float64(x1-x0), float64(y1-y0)
	dist := math.Hypot(dx, dy)
	steps := int(math.Max(dist, 1)) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		fillDisc(dst, int(math.Round(float64(x0)+dx*t)), int(math.Round(float64(y0)+dy*t)), w/2, c)
	}
}

// polyPath strokes a polyline through pts.
func polyPath(dst *image.RGBA, pts []image.Point, w float64, c color.Color) {
	for i := 1; i < len(pts); i++ {
		thickLine(dst, pts[i-1].X, pts[i-1].Y, pts[i].X, pts[i].Y, w, c)
	}
}

// fillPoly fills a polygon (even-odd scanline). Intersections are sorted
// before pairing: without sorting, concave rings fill the wrong spans and
// leave long straight land/sea edges (REVIEW_B #3).
func fillPoly(dst *image.RGBA, pts []image.Point, c color.Color) {
	if len(pts) < 3 {
		return
	}
	minY, maxY := pts[0].Y, pts[0].Y
	for _, p := range pts {
		minY, maxY = min(minY, p.Y), max(maxY, p.Y)
	}
	b := dst.Bounds()
	minY, maxY = max(minY, b.Min.Y), min(maxY, b.Max.Y-1)
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
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			x0, x1 := xs[i], xs[i+1]
			for x := int(math.Ceil(x0)); x <= int(x1); x++ {
				setPx(dst, x, y, c)
			}
		}
	}
}

// starPath returns a 5-point star polygon.
func starPath(cx, cy int, rOuter, rInner float64) []image.Point {
	var pts []image.Point
	for i := 0; i < 10; i++ {
		rr := rOuter
		if i%2 == 1 {
			rr = rInner
		}
		t := -math.Pi/2 + float64(i)*math.Pi/5
		pts = append(pts, image.Pt(cx+int(math.Round(rr*math.Cos(t))), cy+int(math.Round(rr*math.Sin(t)))))
	}
	return pts
}
