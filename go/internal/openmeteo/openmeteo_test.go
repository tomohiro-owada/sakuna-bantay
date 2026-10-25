package openmeteo

import (
	"context"
	"os"
	"testing"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseFixture(t *testing.T) {
	f := load(t, "openmeteo-forecast.json")
	a := load(t, "openmeteo-aq.json")
	td, err := Parse(f, a, "2026-10-25")
	if err != nil {
		t.Fatal(err)
	}
	if td.Date != "2026-10-25" {
		t.Errorf("date = %s", td.Date)
	}
	if td.TMax != 33.5 {
		t.Errorf("TMax = %v, want 33.5", td.TMax)
	}
	if td.ApparentMax != 38.3 {
		t.Errorf("ApparentMax = %v, want 38.3", td.ApparentMax)
	}
	if td.UVMax != 7.8 {
		t.Errorf("UVMax = %v, want 7.8", td.UVMax)
	}
	if td.UVCategory != "High" {
		t.Errorf("UVCategory = %s, want High", td.UVCategory)
	}
	if td.PrecipSum != 0.0 {
		t.Errorf("PrecipSum = %v", td.PrecipSum)
	}
	// REVIEW_C #3: single display number is the max hourly probability
	// over 06:00–23:00 (hourly max 6), not the daily aggregate 14.
	if td.PrecipProbMx != 6 {
		t.Errorf("PrecipProbMax = %d, want 6 (hourly 06-23 max)", td.PrecipProbMx)
	}
	if !td.TMinDerived {
		t.Error("fixture lacks temperature_2m_min; TMin should be derived")
	}
	// Hourly min for 2026-10-25: temps start 28.1 ... min is 26.5 at 06:00.
	if td.TMin != 26.5 {
		t.Errorf("derived TMin = %v, want 26.5", td.TMin)
	}
	if len(td.HourlyRain) != 18 {
		t.Fatalf("hourly rain entries = %d, want 18 (06-23)", len(td.HourlyRain))
	}
	if td.HourlyRain[0].Hour != 6 || td.HourlyRain[0].Prob != 6 {
		t.Errorf("first hourly = %+v, want 06:00/6%%", td.HourlyRain[0])
	}
	if td.HourlyRain[17].Hour != 23 || td.HourlyRain[17].Prob != 0 {
		t.Errorf("last hourly = %+v, want 23:00/0%%", td.HourlyRain[17])
	}
	if td.AQI != 150 {
		t.Errorf("AQI = %d, want 150", td.AQI)
	}
	if td.PM25 != 29.7 {
		t.Errorf("PM25 = %v, want 29.7", td.PM25)
	}
	if td.AQICategory != "Unhealthy for Sensitive Groups" {
		t.Errorf("AQICategory = %s", td.AQICategory)
	}
	if td.AQINote != "model estimate" {
		t.Errorf("AQINote = %q, must say model estimate", td.AQINote)
	}
	if td.AQIMax != 150 {
		t.Errorf("AQIMax = %d, want 150", td.AQIMax)
	}
	if td.PM25Max < 69.1 || td.PM25Max > 69.3 {
		t.Errorf("PM25Max = %v, want ~69.2", td.PM25Max)
	}
}

func TestAQIPM25SameDailyMaxBasis(t *testing.T) {
	// AQI and PM2.5 chips share the SAME time basis: the daily max of
	// hourly values for the target day. Both maxima must equal the max
	// over that day's hourly series (not the current-hour pair), and the
	// category must match the max AQI.
	f := load(t, "openmeteo-forecast.json")
	a := load(t, "openmeteo-aq.json")
	td, err := Parse(f, a, "2026-10-25")
	if err != nil {
		t.Fatal(err)
	}
	// Hourly max for 2026-10-25 from the fixture: AQI 150, PM2.5 69.2.
	if td.AQIMax != 150 {
		t.Errorf("AQIMax = %d, want hourly max 150", td.AQIMax)
	}
	if td.PM25Max < 69.1 || td.PM25Max > 69.3 {
		t.Errorf("PM25Max = %v, want hourly max ~69.2", td.PM25Max)
	}
	if td.AQICategory != AQICategory(td.AQIMax) {
		t.Errorf("AQICategory = %q, want category of max %d (%q)", td.AQICategory, td.AQIMax, AQICategory(td.AQIMax))
	}
	// Same basis: both are daily maxima, so a divergent current pair
	// (e.g. AQI 97 with PM2.5 75.4 at 02:00 on 2026-10-26) can never be
	// displayed side by side.
	if td.AQI == 97 && td.PM25 == 75.4 {
		t.Error("AQI/PM2.5 look like a current-hour pair, want daily maxima")
	}
}

func TestParseSecondDay(t *testing.T) {
	f := load(t, "openmeteo-forecast.json")
	a := load(t, "openmeteo-aq.json")
	td, err := Parse(f, a, "2026-10-26")
	if err != nil {
		t.Fatal(err)
	}
	if td.TMax != 34.0 || td.UVMax != 8.05 {
		t.Errorf("day2 = TMax %v UV %v", td.TMax, td.UVMax)
	}
	if td.UVCategory != "Very High" {
		t.Errorf("day2 UVCategory = %s, want Very High", td.UVCategory)
	}
	if len(td.HourlyRain) != 18 {
		t.Errorf("day2 hourly = %d, want 18", len(td.HourlyRain))
	}
}

func TestParseBadInput(t *testing.T) {
	if _, err := Parse([]byte("{"), load(t, "openmeteo-aq.json"), ""); err == nil {
		t.Error("bad forecast JSON should error")
	}
	if _, err := Parse(load(t, "openmeteo-forecast.json"), []byte("{"), ""); err == nil {
		t.Error("bad aq JSON should error")
	}
	if _, err := Parse([]byte(`{"daily":{"time":[]}}`), load(t, "openmeteo-aq.json"), ""); err == nil {
		t.Error("empty daily should error")
	}
	if _, err := Parse(load(t, "openmeteo-forecast.json"), load(t, "openmeteo-aq.json"), "2099-01-01"); err == nil {
		t.Error("unknown date should error")
	}
}

func TestUVCategoryBoundaries(t *testing.T) {
	cases := []struct {
		uv   float64
		want string
	}{
		{0, "Low"}, {2.9, "Low"},
		{3, "Moderate"}, {5.9, "Moderate"},
		{6, "High"}, {7.9, "High"},
		{8, "Very High"}, {10.9, "Very High"},
		{11, "Extreme"}, {15, "Extreme"},
	}
	for _, tc := range cases {
		if got := UVCategory(tc.uv); got != tc.want {
			t.Errorf("UVCategory(%v) = %s, want %s", tc.uv, got, tc.want)
		}
	}
}

func TestAQICategoryBoundaries(t *testing.T) {
	cases := []struct {
		aqi  int
		want string
	}{
		{0, "Good"}, {50, "Good"},
		{51, "Moderate"}, {100, "Moderate"},
		{101, "Unhealthy for Sensitive Groups"}, {150, "Unhealthy for Sensitive Groups"},
		{151, "Unhealthy"}, {200, "Unhealthy"},
		{201, "Very Unhealthy"}, {300, "Very Unhealthy"},
		{301, "Hazardous"}, {500, "Hazardous"},
	}
	for _, tc := range cases {
		if got := AQICategory(tc.aqi); got != tc.want {
			t.Errorf("AQICategory(%d) = %s, want %s", tc.aqi, got, tc.want)
		}
	}
}

func TestFetchViaFixtureClient(t *testing.T) {
	c := fetch.NewFixture("testdata", map[string]string{
		ForecastURL:   "openmeteo-forecast.json",
		AirQualityURL: "openmeteo-aq.json",
	})
	td, err := Fetch(context.Background(), c, "2026-10-25", 0)
	if err != nil {
		t.Fatal(err)
	}
	if td.Date != "2026-10-25" || td.TMax != 33.5 {
		t.Errorf("fetch via fixture = %+v", td)
	}
}
