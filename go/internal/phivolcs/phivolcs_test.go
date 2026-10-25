package phivolcs

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseListCount(t *testing.T) {
	evs, err := ParseList(load(t, "phivolcs-list.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 38 {
		t.Errorf("events = %d, want 38", len(evs))
	}
}

func TestParseListPHTTimezone(t *testing.T) {
	// REVIEW_B #1: list times are ALREADY PHT (header says
	// "Date - Time (Philippine Time)"). The row "16 October 2026 - 05:13 PM"
	// must parse to 17:13 +08:00 = 09:13 UTC, matching href id 2026_1016_0913.
	html := `<html><body><table>
<tr><th>Date - Time</th><th>Lat</th><th>Lon</th><th>Depth</th><th>Mag</th><th>Location</th></tr>
<tr><td><a href="2026_Earthquake_Information\October\2026_1016_0913_B1.html">16 October 2026 - 05:13 PM</a></td><td>7.92</td><td>127.13</td><td>010</td><td>5.1</td><td>073 km N 59° E of Baganga (Davao Oriental)</td></tr>
</table></body></html>`
	evs, err := ParseList([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	e := evs[0]
	// Wall clock must stay 17:13 in Manila.
	if h, m := e.Time.In(Manila()).Hour(), e.Time.In(Manila()).Minute(); h != 17 || m != 13 {
		t.Errorf("PHT wall time = %02d:%02d, want 17:13 (got %v)", h, m, e.Time)
	}
	// Same instant is 09:13 UTC.
	if hu := e.Time.UTC().Hour(); hu != 9 || e.Time.UTC().Minute() != 13 {
		t.Errorf("UTC time = %v, want 09:13 UTC", e.Time.UTC().Format("15:04"))
	}
	if _, off := e.Time.In(Manila()).Zone(); off != 8*3600 {
		t.Errorf("Manila offset = %d, want 28800", off)
	}
}

func TestCleanPlaceStripsLeadingZeros(t *testing.T) {
	// REVIEW_B #4.
	cases := map[string]string{
		"073 km N 59° E of Baganga (Davao Oriental)": "73 km N 59° E of Baganga (Davao Oriental)",
		"004 km S 45° W of Naic (Cavite)":            "4 km S 45° W of Naic (Cavite)",
		"012 km S 45° W of Naic (Cavite)":            "12 km S 45° W of Naic (Cavite)",
		"77 km ENE of Calayan, Cagayan":              "77 km ENE of Calayan, Cagayan",
	}
	for in, want := range cases {
		if got := CleanPlace(in); got != want {
			t.Errorf("CleanPlace(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseFirstRow(t *testing.T) {
	evs, err := ParseList(load(t, "phivolcs-list.html"))
	if err != nil {
		t.Fatal(err)
	}
	e := evs[0]
	if e.ID != "2026_1025_1013_B1" {
		t.Errorf("ID = %q, want 2026_1025_1013_B1", e.ID)
	}
	if e.CanonicalID != "2026_1025_1013" {
		t.Errorf("CanonicalID = %q, want 2026_1025_1013", e.CanonicalID)
	}
	want, _ := time.ParseInLocation("02 January 2006 - 03:04 PM", "25 October 2026 - 06:13 PM", Manila())
	if !e.Time.Equal(want) {
		t.Errorf("time = %v, want %v", e.Time, want)
	}
	if e.Lat != 8.01 || e.Lon != 122.38 {
		t.Errorf("lat/lon = %v/%v", e.Lat, e.Lon)
	}
	if e.DepthKm != 36 || e.Mag != 1.6 {
		t.Errorf("depth/mag = %v/%v", e.DepthKm, e.Mag)
	}
	if e.Source != "PHIVOLCS" {
		t.Errorf("source = %s", e.Source)
	}
	// Backslashes converted to forward slashes, absolute under site root.
	wantURL := "https://earthquake.phivolcs.dost.gov.ph/2026_Earthquake_Information/October/2026_1025_1013_B1.html"
	if e.URL != wantURL {
		t.Errorf("URL = %s, want %s", e.URL, wantURL)
	}
	if e.DistanceKm <= 0 {
		t.Error("distance should be > 0")
	}
}

func TestBackslashAndRevisionIDs(t *testing.T) {
	evs, err := ParseList(load(t, "phivolcs-list.html"))
	if err != nil {
		t.Fatal(err)
	}
	foundF := false
	for _, e := range evs {
		if e.ID == "2026_1025_1008_B1F" {
			foundF = true
			if e.CanonicalID != "2026_1025_1008" {
				t.Errorf("CanonicalID(%q) = %q", e.ID, e.CanonicalID)
			}
		}
		if len(e.URL) == 0 || e.URL[:8] != "https://" {
			t.Errorf("non-absolute URL %q", e.URL)
		}
	}
	if !foundF {
		t.Error("expected a _B1F revision id in fixture")
	}
	cases := map[string]string{
		"2026_1025_1013_B1":  "2026_1025_1013",
		"2026_1025_1008_B1F": "2026_1025_1008",
		"2026_1025_0900_B2":  "2026_1025_0900",
		"us6000u4m9":         "us6000u4m9",
	}
	for in, want := range cases {
		if got := CanonicalID(in + ".html"); got != want {
			t.Errorf("CanonicalID(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestHaversineAndBearing(t *testing.T) {
	// Las Piñas to itself.
	if d := HaversineKm(LasPinasLat, LasPinasLon, LasPinasLat, LasPinasLon); d != 0 {
		t.Errorf("self distance = %v, want 0", d)
	}
	// Known: Manila (14.5995,120.9842) to Las Piñas ~18 km.
	d := HaversineKm(14.5995, 120.9842, LasPinasLat, LasPinasLon)
	if d < 15 || d > 22 {
		t.Errorf("Manila-LP distance = %v, want ~18", d)
	}
	// Bearing north: same lon, higher lat -> ~0 (N).
	if b := BearingDeg(0, 0, 1, 0); math.Abs(b-0) > 0.5 {
		t.Errorf("bearing north = %v", b)
	}
	// Due east -> 90.
	if b := BearingDeg(0, 0, 0, 1); math.Abs(b-90) > 0.5 {
		t.Errorf("bearing east = %v", b)
	}
	// Due south -> 180, west -> 270.
	if b := BearingDeg(0, 0, -1, 0); math.Abs(b-180) > 0.5 {
		t.Errorf("bearing south = %v", b)
	}
	if b := BearingDeg(0, 0, 0, -1); math.Abs(b-270) > 0.5 {
		t.Errorf("bearing west = %v", b)
	}
	cases := map[float64]string{0: "N", 45: "NE", 90: "E", 135: "SE", 180: "S", 225: "SW", 270: "W", 315: "NW", 22.5: "NNE"}
	for b, want := range cases {
		if got := Compass16(b); got != want {
			t.Errorf("Compass16(%v) = %s, want %s", b, got, want)
		}
	}
	// Distance symmetry.
	a := HaversineKm(14.44, 121.01, 8.01, 122.38)
	b2 := HaversineKm(8.01, 122.38, 14.44, 121.01)
	if math.Abs(a-b2) > 1e-9 {
		t.Error("haversine should be symmetric")
	}
}

func TestParseUSGS(t *testing.T) {
	evs, err := ParseUSGS(load(t, "usgs-ph.geojson"))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 20 {
		t.Errorf("usgs events = %d, want 20", len(evs))
	}
	e := evs[0]
	if e.ID != "us6000u4m9" || e.Source != "USGS" {
		t.Errorf("first = %+v", e)
	}
	if e.Mag != 4.7 {
		t.Errorf("mag = %v, want 4.7", e.Mag)
	}
	if e.Lat != 19.805 || e.Lon != 122.0872 {
		t.Errorf("lat/lon = %v/%v", e.Lat, e.Lon)
	}
	if e.DistanceKm <= 0 || e.Compass == "" {
		t.Errorf("geo not filled: %+v", e)
	}
	if !InPHBounds(e.Lat, e.Lon) {
		t.Error("first event should be in PH bounds")
	}
	if InPHBounds(0, 0) || InPHBounds(23, 120) || InPHBounds(10, 115) {
		t.Error("InPHBounds false positives")
	}
	// Edges inclusive.
	if !InPHBounds(4, 116) || !InPHBounds(22, 128) {
		t.Error("bounds edges should be inclusive")
	}
}

func TestParseIntensities(t *testing.T) {
	html := `<html><body><p>Reported Intensities: Intensity III - City of Manila; Intensity IV - Quezon City, Makati City. Intensity II - City of Pasay.</p></body></html>`
	got := ParseIntensities([]byte(html))
	if len(got) < 3 {
		t.Fatalf("intensities = %+v, want >=3", got)
	}
	levels := map[string]int{}
	for _, in := range got {
		levels[in.Place] = in.Level
		if in.Roman == "" || in.Level == 0 {
			t.Errorf("bad intensity %+v", in)
		}
	}
	if levels["City of Manila"] != 3 {
		t.Errorf("Manila level = %d", levels["City of Manila"])
	}
	if levels["Quezon City"] != 4 {
		t.Errorf("QC level = %d", levels["Quezon City"])
	}
	if RomanToInt("III") != 3 || RomanToInt("IV") != 4 || RomanToInt("VIII") != 8 || RomanToInt("X") != 10 {
		t.Error("RomanToInt wrong")
	}
	if len(ParseIntensities([]byte("<html><body>no data</body></html>"))) != 0 {
		t.Error("no intensities expected")
	}
}

func TestParseListSkipsHeaders(t *testing.T) {
	// Header-only HTML must error (no events).
	if _, err := ParseList([]byte("<html><body><table><tr><td>Date - Time</td></tr></table></body></html>")); err == nil {
		t.Error("header-only should error")
	}
	if _, err := ParseUSGS([]byte("nope")); err == nil {
		t.Error("bad geojson should error")
	}
}

func TestDecodeDetailBodyGzip(t *testing.T) {
	raw := load(t, "phivolcs-detail-raw-gzip.html")
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		t.Fatal("raw fixture does not start with gzip magic bytes 1f 8b")
	}
	dec := DecodeDetailBody(raw)
	if len(dec) == len(raw) {
		t.Error("decoded body should differ in length from gzipped raw")
	}
	s := string(dec)
	for _, want := range []string{
		"Reported",
		"Intensities",
		"Instrumental Intensity:",
		"Intensity I - City of Bislig, SURIGAO DEL SUR",
		"Baganga",
		"07.92°N, 127.13°E", // windows-1252 0xB0 decoded to U+00B0
	} {
		if !strings.Contains(s, want) {
			t.Errorf("decoded body missing %q", want)
		}
	}
}

func TestParseIntensitiesDetailFixture(t *testing.T) {
	for _, name := range []string{"phivolcs-detail-raw-gzip.html", "phivolcs-detail.html"} {
		got := ParseIntensities(load(t, name))
		if len(got) != 1 {
			t.Fatalf("%s: intensities = %+v, want exactly 1", name, got)
		}
		in := got[0]
		if in.Place != "City of Bislig, SURIGAO DEL SUR" {
			t.Errorf("%s: place = %q, want city+province kept together", name, in.Place)
		}
		if in.Roman != "I" || in.Level != 1 {
			t.Errorf("%s: roman/level = %q/%d, want I/1", name, in.Roman, in.Level)
		}
	}
}

func TestSplitPlacesKeepsProvince(t *testing.T) {
	got := splitPlaces("City of Bislig, SURIGAO DEL SUR")
	if len(got) != 1 || got[0] != "City of Bislig, SURIGAO DEL SUR" {
		t.Errorf("splitPlaces province pair = %q", got)
	}
	got = splitPlaces("Quezon City, Makati City")
	if len(got) != 2 {
		t.Errorf("splitPlaces city list = %q, want 2", got)
	}
}
