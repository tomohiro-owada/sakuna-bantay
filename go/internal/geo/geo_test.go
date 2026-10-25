package geo

import (
	"math"
	"os"
	"testing"
)

func TestLoadBasemap(t *testing.T) {
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Provinces) != 87 {
		t.Errorf("provinces = %d, want 87", len(b.Provinces))
	}
	if len(b.Land) == 0 {
		t.Error("no land polys")
	}
	// Spot-check names.
	found := map[string]bool{}
	for _, p := range b.Provinces {
		found[p.Name] = true
		if len(p.Polys) == 0 {
			t.Errorf("province %q has no polygons", p.Name)
		}
		for _, r := range p.Polys {
			if len(r) < 3 {
				t.Errorf("province %q ring has %d points", p.Name, len(r))
			}
			for _, pt := range r {
				if pt.Lon < ClipMinLon || pt.Lon > ClipMaxLon || pt.Lat < ClipMinLat || pt.Lat > ClipMaxLat {
					t.Errorf("province %q point out of clip box: %+v", p.Name, pt)
				}
			}
		}
	}
	for _, want := range []string{"Abra", "Batanes", "Cavite", "NCR, Second District", "Quezon"} {
		if !found[want] {
			t.Errorf("missing province %q", want)
		}
	}
	// Batanes is the northern tip.
	for _, p := range b.Provinces {
		if p.Name == "Batanes" {
			maxLat := -90.0
			for _, r := range p.Polys {
				for _, pt := range r {
					maxLat = math.Max(maxLat, pt.Lat)
				}
			}
			if maxLat < 20 {
				t.Errorf("Batanes maxLat = %v, want > 20", maxLat)
			}
		}
	}
}

func TestBasemapSizeBudget(t *testing.T) {
	st, err := os.Stat("basemap.bin")
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() >= 1536*1024 {
		t.Errorf("basemap.bin = %d bytes, over the 1.5 MB budget", st.Size())
	}
	t.Logf("basemap.bin = %d bytes", st.Size())
}

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"Batanes":                             "batanes",
		"Province of Cavite":                  "cavite",
		"Cavite Province":                     "cavite",
		"Las Piñas":                           "las pinas",
		"Metropolitan Manila":                 "metropolitan manila", // not a known alias: unchanged
		"Metro Manila":                        "metro manila",
		"NCR, Second District":                "metro manila",
		"NCR, Third District":                 "metro manila",
		"NCR, Fourth District":                "metro manila",
		"NCR, City of Manila, First District": "metro manila",
		"National Capital Region":             "metro manila",
		"NCR":                                 "metro manila",
		"NCR area":                            "metro manila",
		"Metro Manila area":                   "metro manila",
		"CITY OF MANILA":                      "city of manila",
		"Davao Oriental":                      "davao oriental",
	}
	for in, want := range cases {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
	// Every basemap province resolves through the index.
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	idx := b.Index()
	for _, p := range b.Provinces {
		if idx[NormalizeName(p.Name)] == nil {
			t.Errorf("province %q missing from index", p.Name)
		}
	}
	if idx["batanes"] == nil || idx["metro manila"] == nil {
		t.Error("index missing batanes/metro manila")
	}
}

func TestProjectionFit(t *testing.T) {
	bb := BBox{MinLon: 116, MinLat: 9, MaxLon: 127, MaxLat: 21} // Luzon–Visayas
	p := bb.Fit(60, 200, 800, 600, 0)
	// No distortion: equal scales.
	if math.Abs(p.SX-p.SY)/p.SX > 1e-9 {
		t.Errorf("SX != SY: %v vs %v", p.SX, p.SY)
	}
	// Corners map onto rect corners (bbox was expanded to match aspect).
	x0, y0 := p.Project(p.MinLon, p.MaxLat)
	x1, y1 := p.Project(p.MaxLon, p.MinLat)
	if math.Abs(x0-60) > 1e-6 || math.Abs(y0-200) > 1e-6 || math.Abs(x1-860) > 1e-6 || math.Abs(y1-800) > 1e-6 {
		t.Errorf("corners = (%v,%v)-(%v,%v), want rect corners", x0, y0, x1, y1)
	}
	// Round-trip.
	lon, lat := p.Unproject(400, 500)
	x, y := p.Project(lon, lat)
	if math.Abs(x-400) > 1e-6 || math.Abs(y-500) > 1e-6 {
		t.Errorf("unproject round-trip failed: (%v,%v)", x, y)
	}
	// Mid lat increases x spacing vs equator-ish... sanity: 1° lon < 1° lat in px.
	dx, _ := p.Project(121, 14)
	dx2, _ := p.Project(122, 14)
	_, dy := p.Project(121, 14)
	_, dy2 := p.Project(121, 15)
	if (dx2 - dx) >= (dy - dy2) {
		t.Error("cos(lat) correction missing: 1° lon should be narrower than 1° lat")
	}
}

func TestLasPinasInBasemap(t *testing.T) {
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Las Piñas must lie inside the clip box (else every quake map breaks).
	if 121.01 < b.MinLon || 121.01 > b.MaxLon || 14.44 < b.MinLat || 14.44 > b.MaxLat {
		t.Error("Las Piñas outside basemap clip box")
	}
}
