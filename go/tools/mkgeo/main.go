// Command mkgeo builds the compact embedded offline basemap
// go/internal/geo/basemap.bin from the source GeoJSON files in assets/geo/:
//
//   - geoBoundaries-PHL-ADM2_simplified.geojson (87 provinces/NCR districts,
//     property shapeName; CC BY 3.0 IGO)
//   - ne_50m_land.geojson (Natural Earth, public domain context land)
//
// Steps: clip to lon 114–130 / lat 3–23, simplify (Douglas–Peucker ~0.005°),
// quantize to uint16 over the clip box, and pack provinces (name + polygons)
// plus land polygons into a small binary file (< 1.5 MB, committed).
//
// Usage (from go/): go run ./tools/mkgeo -geo ../assets/geo -out internal/geo/basemap.bin
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const (
	clipMinLon = 114.0
	clipMaxLon = 130.0
	clipMinLat = 3.0
	clipMaxLat = 23.0
	// simplifyEps is the Douglas–Peucker tolerance in degrees (~0.5 km).
	simplifyEps = 0.005
	// maxBytes is the committed-file budget from the spec.
	maxBytes = 1536 * 1024 // 1.5 MB
)

type pt struct{ x, y float64 } // lon, lat

func main() {
	geoDir := flag.String("geo", "../assets/geo", "directory with source .geojson files")
	out := flag.String("out", "internal/geo/basemap.bin", "output path for basemap.bin")
	flag.Parse()

	provs, err := loadProvinces(filepath.Join(*geoDir, "geoBoundaries-PHL-ADM2_simplified.geojson"))
	die(err)
	land, err := loadLand(filepath.Join(*geoDir, "ne_50m_land.geojson"))
	die(err)

	// Clip + simplify + quantize.
	for i := range provs {
		provs[i].polys = processPolys(provs[i].polys)
	}
	land = processPolys(land)
	// Drop provinces left with no geometry (should not happen).
	kept := provs[:0]
	for _, p := range provs {
		if len(p.polys) > 0 {
			kept = append(kept, p)
		}
	}
	provs = kept

	var buf bytes.Buffer
	writeBasemap(&buf, provs, land)
	if buf.Len() > maxBytes {
		die(fmt.Errorf("basemap.bin is %d bytes, over the 1.5 MB budget", buf.Len()))
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		die(err)
	}
	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		die(err)
	}
	npts := 0
	for _, p := range provs {
		for _, r := range p.polys {
			npts += len(r)
		}
	}
	for _, r := range land {
		npts += len(r)
	}
	fmt.Printf("mkgeo: %d provinces, %d land polys, %d points, %d bytes -> %s\n",
		len(provs), len(land), npts, buf.Len(), *out)
}

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkgeo:", err)
		os.Exit(1)
	}
}

// --- GeoJSON loading ---

type province struct {
	name  string
	polys [][]pt
}

func loadProvinces(path string) ([]province, error) {
	fc, err := readFeatures(path)
	if err != nil {
		return nil, err
	}
	var out []province
	for _, f := range fc {
		name := ""
		if props, ok := f.props["shapeName"].(string); ok {
			name = props
		}
		if name == "" {
			return nil, fmt.Errorf("province feature without shapeName")
		}
		out = append(out, province{name: name, polys: extractPolys(f.geom)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no provinces in %s", path)
	}
	return out, nil
}

func loadLand(path string) ([][]pt, error) {
	fc, err := readFeatures(path)
	if err != nil {
		return nil, err
	}
	var out [][]pt
	for _, f := range fc {
		out = append(out, extractPolys(f.geom)...)
	}
	return out, nil
}

type feature struct {
	props map[string]any
	geom  geom
}

type geom struct {
	typ    string
	coords any
}

func readFeatures(path string) ([]feature, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Type       string         `json:"type"`
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Type        string `json:"type"`
				Coordinates any    `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&fc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var out []feature
	for _, f := range fc.Features {
		out = append(out, feature{props: f.Properties, geom: geom{typ: f.Geometry.Type, coords: f.Geometry.Coordinates}})
	}
	return out, nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case float64:
		return n, true
	}
	return 0, false
}

// extractPolys flattens Polygon/MultiPolygon coordinates into rings of pts.
func extractPolys(g geom) [][]pt {
	var rings []any
	switch g.typ {
	case "Polygon":
		if polys, ok := g.coords.([]any); ok {
			rings = append(rings, polys...)
		}
	case "MultiPolygon":
		if mpolys, ok := g.coords.([]any); ok {
			for _, p := range mpolys {
				if polys, ok := p.([]any); ok {
					rings = append(rings, polys...)
				}
			}
		}
	}
	var out [][]pt
	for _, r := range rings {
		pts, ok := r.([]any)
		if !ok {
			continue
		}
		var ring []pt
		for _, c := range pts {
			pair, ok := c.([]any)
			if !ok || len(pair) < 2 {
				continue
			}
			x, ok1 := toFloat(pair[0])
			y, ok2 := toFloat(pair[1])
			if !ok1 || !ok2 {
				continue
			}
			ring = append(ring, pt{x, y})
		}
		if len(ring) >= 4 {
			out = append(out, ring)
		}
	}
	return out
}

// --- Clip + simplify + quantize ---

func processPolys(polys [][]pt) [][]pt {
	var out [][]pt
	for _, ring := range polys {
		r := clipRing(ring)
		if len(r) < 3 {
			continue
		}
		r = douglasPeucker(r, simplifyEps)
		if len(r) < 3 {
			continue
		}
		q := quantizeDedupe(r)
		if len(q) < 3 {
			continue
		}
		out = append(out, q)
	}
	return out
}

// clipRing clips a ring to the clip box (Sutherland–Hodgman). Points exactly
// on the border are kept (inside).
func clipRing(ring []pt) []pt {
	out := ring
	out = clipAgainst(out, func(p pt) bool { return p.x >= clipMinLon }, func(a, b pt) pt { return intersectV(a, b, clipMinLon) })
	out = clipAgainst(out, func(p pt) bool { return p.x <= clipMaxLon }, func(a, b pt) pt { return intersectV(a, b, clipMaxLon) })
	out = clipAgainst(out, func(p pt) bool { return p.y >= clipMinLat }, func(a, b pt) pt { return intersectH(a, b, clipMinLat) })
	out = clipAgainst(out, func(p pt) bool { return p.y <= clipMaxLat }, func(a, b pt) pt { return intersectH(a, b, clipMaxLat) })
	return out
}

func clipAgainst(ring []pt, inside func(pt) bool, cross func(a, b pt) pt) []pt {
	if len(ring) == 0 {
		return nil
	}
	var out []pt
	prev := ring[len(ring)-1]
	prevIn := inside(prev)
	for _, cur := range ring {
		curIn := inside(cur)
		if curIn {
			if !prevIn {
				out = append(out, cross(prev, cur))
			}
			out = append(out, cur)
		} else if prevIn {
			out = append(out, cross(prev, cur))
		}
		prev, prevIn = cur, curIn
	}
	return out
}

func intersectV(a, b pt, x float64) pt {
	if b.x == a.x {
		return pt{x, a.y}
	}
	t := (x - a.x) / (b.x - a.x)
	return pt{x, a.y + t*(b.y-a.y)}
}

func intersectH(a, b pt, y float64) pt {
	if b.y == a.y {
		return pt{a.x, y}
	}
	t := (y - a.y) / (b.y - a.y)
	return pt{a.x + t*(b.x-a.x), y}
}

// douglasPeucker simplifies a ring (treated as an open polyline; endpoints
// kept).
func douglasPeucker(ring []pt, eps float64) []pt {
	if len(ring) < 3 {
		return ring
	}
	keep := make([]bool, len(ring))
	keep[0], keep[len(ring)-1] = true, true
	var stack [][2]int
	stack = append(stack, [2]int{0, len(ring) - 1})
	for len(stack) > 0 {
		seg := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		i, j := seg[0], seg[1]
		if j <= i+1 {
			continue
		}
		maxD, maxK := 0.0, -1
		for k := i + 1; k < j; k++ {
			if d := perpDist(ring[k], ring[i], ring[j]); d > maxD {
				maxD, maxK = d, k
			}
		}
		if maxD > eps && maxK >= 0 {
			keep[maxK] = true
			stack = append(stack, [2]int{i, maxK}, [2]int{maxK, j})
		}
	}
	var out []pt
	for i, k := range keep {
		if k {
			out = append(out, ring[i])
		}
	}
	return out
}

func perpDist(p, a, b pt) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	den := math.Hypot(dx, dy)
	if den == 0 {
		return math.Hypot(p.x-a.x, p.y-a.y)
	}
	return math.Abs(dy*p.x-dx*p.y+b.x*a.y-b.y*a.x) / den
}

// quantizeDedupe maps lon/lat to uint16 over the clip box and drops
// consecutive duplicates from quantization.
func quantizeDedupe(ring []pt) []pt {
	out := make([]pt, 0, len(ring))
	var px, py uint16
	have := false
	for _, p := range ring {
		qx := uint16(math.Round((p.x - clipMinLon) / (clipMaxLon - clipMinLon) * 65535))
		qy := uint16(math.Round((p.y - clipMinLat) / (clipMaxLat - clipMinLat) * 65535))
		if have && qx == px && qy == py {
			continue
		}
		px, py, have = qx, qy, true
		out = append(out, pt{float64(qx), float64(qy)})
	}
	return out
}

// --- Binary encoding ---
//
//	basemap.bin = magic "SBGEO1\n"
//	  + bbox float64 x4 (minLon minLat maxLon maxLat)
//	  + uvarint nProvinces
//	    + uvarint nameLen + name bytes
//	    + uvarint nPolys + polys (uvarint nPts + uvarint qx,qy pairs)
//	  + uvarint nLandPolys + polys (same encoding, no names)
func writeBasemap(buf *bytes.Buffer, provs []province, land [][]pt) {
	buf.WriteString("SBGEO1\n")
	for _, v := range []float64{clipMinLon, clipMinLat, clipMaxLon, clipMaxLat} {
		_ = binary.Write(buf, binary.LittleEndian, v)
	}
	var tmp [binary.MaxVarintLen64]byte
	uvarint := func(v uint64) {
		n := binary.PutUvarint(tmp[:], v)
		buf.Write(tmp[:n])
	}
	uvarint(uint64(len(provs)))
	for _, p := range provs {
		uvarint(uint64(len(p.name)))
		buf.WriteString(p.name)
		uvarint(uint64(len(p.polys)))
		for _, r := range p.polys {
			writeRing(buf, uvarint, r)
		}
	}
	uvarint(uint64(len(land)))
	for _, r := range land {
		writeRing(buf, uvarint, r)
	}
}

func writeRing(buf *bytes.Buffer, uvarint func(uint64), r []pt) {
	uvarint(uint64(len(r)))
	for _, p := range r {
		uvarint(uint64(p.x))
		uvarint(uint64(p.y))
	}
}
