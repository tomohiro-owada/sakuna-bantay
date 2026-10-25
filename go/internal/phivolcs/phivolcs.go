// Package phivolcs parses the PHIVOLCS earthquake list, optional detail
// intensities, and the USGS backup feed, plus haversine/bearing helpers.
package phivolcs

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding/charmap"
)

// Endpoints and geography.
const (
	ListURL = "https://earthquake.phivolcs.dost.gov.ph/"
	USGSURL = "https://earthquake.usgs.gov/fdsnws/event/1/query?format=geojson&minlatitude=4&maxlatitude=22&minlongitude=116&maxlongitude=128&minmagnitude=4&orderby=time&limit=50"

	LasPinasLat = 14.44
	LasPinasLon = 121.01

	// MinRefetch is the polite minimum refetch for the heavy list page.
	MinRefetch = 5 * time.Minute
)

// Intensity is one reported intensity: place + roman level.
type Intensity struct {
	Place string `json:"place"`
	Roman string `json:"roman"`
	Level int    `json:"level"`
}

// Event is one earthquake.
type Event struct {
	ID          string      `json:"id"`
	CanonicalID string      `json:"canonical_id"`
	Time        time.Time   `json:"time"`
	Lat         float64     `json:"lat"`
	Lon         float64     `json:"lon"`
	DepthKm     float64     `json:"depth_km"`
	Mag         float64     `json:"mag"`
	Place       string      `json:"place"`
	URL         string      `json:"url"`
	Source      string      `json:"source"` // "PHIVOLCS" or "USGS"
	DistanceKm  float64     `json:"distance_km"`
	BearingDeg  float64     `json:"bearing_deg"`
	Compass     string      `json:"compass"`
	Intensities []Intensity `json:"intensities,omitempty"`
}

var manilaLoc = mustLoc()

func mustLoc() *time.Location {
	loc, err := time.LoadLocation("Asia/Manila")
	if err != nil {
		return time.FixedZone("PHT", 8*3600)
	}
	return loc
}

// Manila returns the Asia/Manila location.
func Manila() *time.Location { return manilaLoc }

// HaversineKm returns great-circle distance in km.
func HaversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}

// BearingDeg returns the initial bearing from (lat1,lon1) to (lat2,lon2),
// degrees clockwise from north (0-360).
func BearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	dλ := (lon2 - lon1) * math.Pi / 180
	y := math.Sin(dλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(dλ)
	br := math.Atan2(y, x) * 180 / math.Pi
	return math.Mod(br+360, 360)
}

// Compass16 maps a bearing to a 16-wind compass point.
func Compass16(bearing float64) string {
	pts := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	idx := int(math.Floor(bearing/22.5+0.5)) % 16
	return pts[idx]
}

// DistanceToLasPinas returns km from (lat,lon) to Las Piñas.
func DistanceToLasPinas(lat, lon float64) float64 {
	return HaversineKm(LasPinasLat, LasPinasLon, lat, lon)
}

func fillGeo(e *Event) {
	e.DistanceKm = DistanceToLasPinas(e.Lat, e.Lon)
	e.BearingDeg = BearingDeg(LasPinasLat, LasPinasLon, e.Lat, e.Lon)
	e.Compass = Compass16(e.BearingDeg)
}

var revisionSuffix = regexp.MustCompile(`_B\d+[A-Z]*$`)

// CanonicalID strips PHIVOLCS revision suffixes (_B1, _B1F, _B2) so
// revisions of the same event share an identity.
func CanonicalID(id string) string {
	base := strings.TrimSuffix(id, ".html")
	return revisionSuffix.ReplaceAllString(base, "")
}

// --- List parsing ---

func textOf(n *html.Node) string {
	var b strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

func findHref(n *html.Node) string {
	var href string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if href != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if strings.EqualFold(a.Key, "href") {
					href = a.Val
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return href
}

func cellsOf(tr *html.Node) []*html.Node {
	var out []*html.Node
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			out = append(out, c)
		}
	}
	return out
}

const dtLayout = "02 January 2006 - 03:04 PM"

// leadingZeros strips PHIVOLCS zero-padded distances ("073 km N 59° E ..."
// → "73 km N 59° E ..."). Only the leading integer is unpadded.
var leadingZerosRe = regexp.MustCompile(`^0+(\d)`)

// CleanPlace removes zero padding from the leading distance of a PHIVOLCS
// location string. Exported for card rendering + tests.
func CleanPlace(s string) string {
	s = strings.TrimSpace(s)
	if leadingZerosRe.MatchString(s) {
		s = leadingZerosRe.ReplaceAllString(s, `$1`)
	}
	return s
}

// ParseList parses the PHIVOLCS list HTML into events (skips header/month rows).
func ParseList(data []byte) ([]Event, error) {
	doc, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("phivolcs: parse html: %w", err)
	}
	var out []Event
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			if ev, ok := parseRow(n); ok {
				out = append(out, ev)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if len(out) == 0 {
		return nil, fmt.Errorf("phivolcs: no events found")
	}
	return out, nil
}

func parseRow(tr *html.Node) (Event, bool) {
	cells := cellsOf(tr)
	if len(cells) < 6 {
		return Event{}, false
	}
	href := findHref(cells[0])
	if href == "" {
		return Event{}, false
	}
	dtRaw := textOf(cells[0])
	t, err := time.ParseInLocation(dtLayout, dtRaw, manilaLoc)
	if err != nil {
		return Event{}, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(textOf(cells[1])), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(textOf(cells[2])), 64)
	depth, err3 := strconv.ParseFloat(strings.TrimSpace(textOf(cells[3])), 64)
	mag, err4 := strconv.ParseFloat(strings.TrimSpace(textOf(cells[4])), 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return Event{}, false
	}
	place := CleanPlace(textOf(cells[5]))
	// Absolute URL: backslashes -> slashes, under site root.
	fixed := strings.ReplaceAll(href, "\\", "/")
	abs := fixed
	if !strings.HasPrefix(fixed, "http") {
		base, _ := url.Parse(ListURL)
		ref, err := url.Parse(strings.TrimLeft(fixed, "/"))
		if err == nil {
			abs = base.ResolveReference(ref).String()
		} else {
			abs = strings.TrimRight(ListURL, "/") + "/" + strings.TrimLeft(fixed, "/")
		}
	}
	id := strings.TrimSuffix(path.Base(strings.ReplaceAll(fixed, "\\", "/")), ".html")
	if id == "" || id == "." || id == "/" {
		id = strings.TrimSuffix(path.Base(abs), ".html")
	}
	ev := Event{
		ID:          id,
		CanonicalID: CanonicalID(id),
		Time:        t,
		Lat:         lat,
		Lon:         lon,
		DepthKm:     depth,
		Mag:         mag,
		Place:       place,
		URL:         abs,
		Source:      "PHIVOLCS",
	}
	fillGeo(&ev)
	return ev, true
}

// --- Detail intensities ---

// DecodeDetailBody normalizes a PHIVOLCS event detail page body.
//
// The detail pages are served gzip-compressed WITHOUT a Content-Encoding
// header (so the HTTP client must not / does not decompress them), and the
// HTML itself is windows-1252 encoded (degree signs as 0xB0, smart quotes,
// ...). This helper:
//  1. sniffs the gzip magic bytes (0x1f 0x8b) and decompresses when present;
//  2. decodes windows-1252 to UTF-8 when the body is not valid UTF-8.
//
// It is idempotent on plain UTF-8 HTML (plain fixtures pass through).
func DecodeDetailBody(data []byte) []byte {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		if gr, err := gzip.NewReader(bytes.NewReader(data)); err == nil {
			if out, err := io.ReadAll(io.LimitReader(gr, 32<<20)); err == nil {
				gr.Close()
				data = out
			} else {
				gr.Close()
			}
		}
	}
	if utf8.Valid(data) {
		return data
	}
	out, err := charmap.Windows1252.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return out
}

// RomanToInt converts I..X (and beyond) to int.
func RomanToInt(s string) int {
	s = strings.ToUpper(strings.TrimSpace(s))
	vals := map[byte]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}
	total, prev := 0, 0
	for i := len(s) - 1; i >= 0; i-- {
		v := vals[s[i]]
		if v < prev {
			total -= v
		} else {
			total += v
		}
		prev = v
	}
	return total
}

// ParseIntensities extracts "Intensity III - Place; ..." reports from a
// detail page. Best effort: returns empty slice when none found.
//
// The input may be the raw HTTP body: DecodeDetailBody is applied first so
// gzip-without-Content-Encoding + windows-1252 pages (the live format, see
// testdata/phivolcs-detail-raw-gzip.html) parse the same as decoded HTML.
// Both "Reported Intensities :" and "Instrumental Intensity:" report lines
// are matched; each match is "Intensity <ROMAN> - <places>".
//
// Parsing is scoped to the enclosing block element (td/p/div/...) of each
// "ntensit..." text node, so a place list cannot bleed into neighbouring
// table cells ("Expecting Damage : NO ..." lives in the next <td>).
func ParseIntensities(data []byte) []Intensity {
	doc, err := html.Parse(strings.NewReader(string(DecodeDetailBody(data))))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Intensity
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode && strings.Contains(strings.ToLower(n.Data), "ntensit") {
			if sc := scopeText(n); sc != "" && !seen[sc] {
				seen[sc] = true
				out = append(out, parseIntensityScope(sc)...)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// scopeText returns the text of the nearest enclosing block-level element
// (td/th/p/div/li/...) of a text node.
func scopeText(t *html.Node) string {
	for p := t.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode {
			switch p.Data {
			case "td", "th", "p", "div", "li", "tr", "body":
				return textOf(p)
			}
		}
	}
	return ""
}

// intensityMarkerRe matches one "Intensity <ROMAN> -" report head. A label
// like "Reported Intensities :" or "Instrumental Intensity:" has no roman
// numeral after "Intensity" and never matches.
var intensityMarkerRe = regexp.MustCompile(`(?i)Intensity\s+([IVX]+)\s*[–—:\-]`)

// parseIntensityScope extracts intensities from one block of text: split on
// ';', then each "Intensity ROMAN - places" marker runs to the next marker
// (or the segment end).
func parseIntensityScope(text string) []Intensity {
	var out []Intensity
	for _, seg := range strings.Split(text, ";") {
		idxs := intensityMarkerRe.FindAllStringSubmatchIndex(seg, -1)
		for i, m := range idxs {
			roman := strings.ToUpper(seg[m[2]:m[3]])
			end := len(seg)
			if i+1 < len(idxs) {
				end = idxs[i+1][0]
			}
			place := strings.Trim(strings.TrimSpace(seg[m[1]:end]), " .:\t\r\n")
			if place == "" {
				continue
			}
			lvl := RomanToInt(roman)
			for _, q := range splitPlaces(place) {
				out = append(out, Intensity{Place: q, Roman: roman, Level: lvl})
			}
		}
	}
	return out
}

// splitPlaces splits a place list on commas, except a comma followed by an
// all-uppercase fragment is a "City of Bislig, SURIGAO DEL SUR" city+province
// pair and stays together. ("Quezon City, Makati City" still splits: the
// second fragment has lowercase letters.)
func splitPlaces(p string) []string {
	var out []string
	for _, frag := range strings.Split(p, ",") {
		q := strings.TrimSpace(strings.Trim(frag, " ."))
		if q == "" {
			continue
		}
		if len(out) > 0 && !strings.ContainsAny(q, "abcdefghijklmnopqrstuvwxyzñáéíóúü") {
			out[len(out)-1] += ", " + q
			continue
		}
		out = append(out, q)
	}
	return out
}

// --- USGS backup ---

type usgsCollection struct {
	Features []usgsFeature `json:"features"`
}
type usgsFeature struct {
	ID         string `json:"id"`
	Properties struct {
		Mag   *float64 `json:"mag"`
		Place *string  `json:"place"`
		Time  *int64   `json:"time"`
		URL   *string  `json:"url"`
	} `json:"properties"`
	Geometry struct {
		Coordinates []float64 `json:"coordinates"`
	} `json:"geometry"`
}

// ParseUSGS converts a USGS FDSN GeoJSON payload into events (Source "USGS").
func ParseUSGS(data []byte) ([]Event, error) {
	var c usgsCollection
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("phivolcs: parse usgs: %w", err)
	}
	var out []Event
	for _, f := range c.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		ev := Event{ID: f.ID, CanonicalID: f.ID, Source: "USGS"}
		if f.Properties.Mag != nil {
			ev.Mag = *f.Properties.Mag
		}
		if f.Properties.Place != nil {
			ev.Place = *f.Properties.Place
		}
		if f.Properties.Time != nil {
			ev.Time = time.UnixMilli(*f.Properties.Time).In(manilaLoc)
		}
		if f.Properties.URL != nil {
			ev.URL = *f.Properties.URL
		}
		ev.Lon = f.Geometry.Coordinates[0]
		ev.Lat = f.Geometry.Coordinates[1]
		if len(f.Geometry.Coordinates) >= 3 {
			ev.DepthKm = f.Geometry.Coordinates[2]
		}
		fillGeo(&ev)
		out = append(out, ev)
	}
	return out, nil
}

// InPHBounds reports whether (lat,lon) is within the PH box
// lat 4–22, lon 116–128 (inclusive).
func InPHBounds(lat, lon float64) bool {
	return lat >= 4 && lat <= 22 && lon >= 116 && lon <= 128
}

// --- Fetching ---

// FetchList downloads and parses the PHIVOLCS list (conditional GET, 5-min refetch).
func FetchList(ctx context.Context, c *fetch.Client) ([]Event, error) {
	return FetchListRefetch(ctx, c, MinRefetch)
}

// FetchListRefetch is FetchList with an explicit min-refetch interval
// (the `run` loop polls faster than the default).
func FetchListRefetch(ctx context.Context, c *fetch.Client, minRefetch time.Duration) ([]Event, error) {
	r, err := c.Get(ctx, ListURL, minRefetch)
	if err != nil {
		return nil, err
	}
	return ParseList(r.Body)
}

// FetchUSGS downloads and parses the USGS backup feed.
func FetchUSGS(ctx context.Context, c *fetch.Client) ([]Event, error) {
	return FetchUSGSRefetch(ctx, c, MinRefetch)
}

// FetchUSGSRefetch is FetchUSGS with an explicit min-refetch interval.
func FetchUSGSRefetch(ctx context.Context, c *fetch.Client, minRefetch time.Duration) ([]Event, error) {
	r, err := c.Get(ctx, USGSURL, minRefetch)
	if err != nil {
		return nil, err
	}
	return ParseUSGS(r.Body)
}

// FetchWithBackup tries PHIVOLCS, falling back to USGS on failure.
func FetchWithBackup(ctx context.Context, c *fetch.Client) ([]Event, string, error) {
	return FetchWithBackupRefetch(ctx, c, MinRefetch)
}

// FetchWithBackupRefetch is FetchWithBackup with an explicit min-refetch
// interval for both sources.
func FetchWithBackupRefetch(ctx context.Context, c *fetch.Client, minRefetch time.Duration) ([]Event, string, error) {
	evs, err := FetchListRefetch(ctx, c, minRefetch)
	if err == nil {
		return evs, "PHIVOLCS", nil
	}
	us, uerr := FetchUSGSRefetch(ctx, c, minRefetch)
	if uerr != nil {
		return nil, "", fmt.Errorf("phivolcs: %v; usgs backup: %w", err, uerr)
	}
	return us, "USGS", nil
}

// FetchIntensities fetches a detail page and parses intensities.
// Best effort: the raw body may be gzip-compressed without Content-Encoding
// and windows-1252 encoded; ParseIntensities normalizes both. Network
// failures return the error (callers ignore it: intensities are optional).
func FetchIntensities(ctx context.Context, c *fetch.Client, detailURL string) ([]Intensity, error) {
	r, err := c.Get(ctx, detailURL, 24*time.Hour) // CAP-like: immutable
	if err != nil {
		return nil, err
	}
	return ParseIntensities(r.Body), nil
}
