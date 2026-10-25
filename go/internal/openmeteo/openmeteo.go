// Package openmeteo fetches and normalizes Open-Meteo forecast +
// air-quality data for Las Piñas (14.44N, 121.01E, Asia/Manila).
package openmeteo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
)

// Las Piñas constants and API endpoints.
const (
	Lat = 14.44
	Lon = 121.01
	TZ  = "Asia/Manila"
)

// ForecastURL and AirQualityURL are the exact SPEC queries.
var (
	ForecastURL   = "https://api.open-meteo.com/v1/forecast?latitude=14.44&longitude=121.01&timezone=Asia%2FManila&daily=temperature_2m_max%2Ctemperature_2m_min%2Capparent_temperature_max%2Cuv_index_max%2Cprecipitation_sum%2Cprecipitation_probability_max&hourly=precipitation_probability%2Ctemperature_2m%2Capparent_temperature%2Cuv_index&forecast_days=2"
	AirQualityURL = "https://air-quality-api.open-meteo.com/v1/air-quality?latitude=14.44&longitude=121.01&timezone=Asia%2FManila&current=us_aqi%2Cpm2_5&hourly=us_aqi%2Cpm2_5&forecast_days=1"
)

// HourRain is the hourly rain probability for one clock hour.
type HourRain struct {
	Time string `json:"time"`
	Hour int    `json:"hour"`
	Prob int    `json:"prob"`
}

// Today is the normalized day summary.
type Today struct {
	Date         string     `json:"date"`
	TMax         float64    `json:"t_max"`
	TMin         float64    `json:"t_min"`
	TMinDerived  bool       `json:"t_min_derived,omitempty"`
	ApparentMax  float64    `json:"apparent_max"`
	UVMax        float64    `json:"uv_max"`
	UVCategory   string     `json:"uv_category"`
	PrecipSum    float64    `json:"precip_sum"`
	PrecipProbMx int        `json:"precip_prob_max"`
	HourlyRain   []HourRain `json:"hourly_rain_06_23"`
	AQI          int        `json:"aqi"`
	PM25         float64    `json:"pm25"`
	AQIMax       int        `json:"aqi_max"`
	PM25Max      float64    `json:"pm25_max"`
	AQICategory  string     `json:"aqi_category"`
	AQINote      string     `json:"aqi_note"`
}

// UVCategory maps the UV index to Low/Moderate/High/Very High/Extreme.
func UVCategory(uv float64) string {
	switch {
	case uv < 3:
		return "Low"
	case uv < 6:
		return "Moderate"
	case uv < 8:
		return "High"
	case uv < 11:
		return "Very High"
	default:
		return "Extreme"
	}
}

// AQICategory maps US AQI to its standard band.
func AQICategory(aqi int) string {
	switch {
	case aqi <= 50:
		return "Good"
	case aqi <= 100:
		return "Moderate"
	case aqi <= 150:
		return "Unhealthy for Sensitive Groups"
	case aqi <= 200:
		return "Unhealthy"
	case aqi <= 300:
		return "Very Unhealthy"
	default:
		return "Hazardous"
	}
}

type forecastResp struct {
	Daily struct {
		Time          []string  `json:"time"`
		TMax          []float64 `json:"temperature_2m_max"`
		TMin          []float64 `json:"temperature_2m_min"`
		ApparentMax   []float64 `json:"apparent_temperature_max"`
		UVMax         []float64 `json:"uv_index_max"`
		PrecipSum     []float64 `json:"precipitation_sum"`
		PrecipProbMax []int     `json:"precipitation_probability_max"`
	} `json:"daily"`
	Hourly struct {
		Time       []string  `json:"time"`
		PrecipProb []*int    `json:"precipitation_probability"`
		Temp       []float64 `json:"temperature_2m"`
		Apparent   []float64 `json:"apparent_temperature"`
		UV         []float64 `json:"uv_index"`
	} `json:"hourly"`
}

type aqResp struct {
	Current struct {
		AQI  *int     `json:"us_aqi"`
		PM25 *float64 `json:"pm2_5"`
	} `json:"current"`
	Hourly struct {
		Time []string   `json:"time"`
		AQI  []*int     `json:"us_aqi"`
		PM25 []*float64 `json:"pm2_5"`
	} `json:"hourly"`
}

func intOr0(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func floatOr0(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// Parse combines decoded forecast + AQ payloads into a Today for date.
// If date == "", the first daily entry is used.
func Parse(fcast []byte, aq []byte, date string) (*Today, error) {
	var f forecastResp
	if err := json.Unmarshal(fcast, &f); err != nil {
		return nil, fmt.Errorf("openmeteo: parse forecast: %w", err)
	}
	var a aqResp
	if err := json.Unmarshal(aq, &a); err != nil {
		return nil, fmt.Errorf("openmeteo: parse aq: %w", err)
	}
	if len(f.Daily.Time) == 0 {
		return nil, fmt.Errorf("openmeteo: empty daily")
	}
	idx := 0
	if date != "" {
		found := false
		for i, d := range f.Daily.Time {
			if d == date {
				idx = i
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("openmeteo: date %s not in forecast", date)
		}
	}
	day := f.Daily.Time[idx]
	get := func(fs []float64) float64 {
		if idx < len(fs) {
			return fs[idx]
		}
		return 0
	}
	getI := func(is []int) int {
		if idx < len(is) {
			return is[idx]
		}
		return 0
	}
	t := &Today{
		Date:         day,
		TMax:         get(f.Daily.TMax),
		ApparentMax:  get(f.Daily.ApparentMax),
		UVMax:        get(f.Daily.UVMax),
		PrecipSum:    get(f.Daily.PrecipSum),
		PrecipProbMx: getI(f.Daily.PrecipProbMax),
		AQINote:      "model estimate",
	}
	if idx < len(f.Daily.TMin) {
		t.TMin = f.Daily.TMin[idx]
	} else {
		// Fixture lacks temperature_2m_min: derive from hourly temps.
		t.TMin, t.TMinDerived = deriveMin(f.Hourly.Time, f.Hourly.Temp, day)
	}
	t.UVCategory = UVCategory(t.UVMax)
	t.AQI = intOr0(a.Current.AQI)
	t.PM25 = floatOr0(a.Current.PM25)
	// Category is derived from the daily max below (chips show max); set
	// here as a fallback and overwritten after the hourly max is known.
	t.AQICategory = AQICategory(t.AQI)
	// Hourly rain 06:00-23:00 of the target day.
	for i, ts := range f.Hourly.Time {
		if len(ts) < 13 {
			continue
		}
		d := ts[:10]
		if d != day {
			continue
		}
		hour := 0
		fmt.Sscanf(ts[11:13], "%d", &hour)
		if hour < 6 || hour > 23 {
			continue
		}
		prob := 0
		if i < len(f.Hourly.PrecipProb) && f.Hourly.PrecipProb[i] != nil {
			prob = *f.Hourly.PrecipProb[i]
		}
		t.HourlyRain = append(t.HourlyRain, HourRain{Time: ts, Hour: hour, Prob: prob})
	}
	// REVIEW_C #3: PrecipProbMx is the single display number for header,
	// chart note and post text: the max hourly probability over the
	// displayed 06:00–23:00 window (from hourly data), not the daily
	// aggregate (which covers 00:00–23:00 and diverges from the chart).
	if len(t.HourlyRain) > 0 {
		mx := 0
		for _, h := range t.HourlyRain {
			if h.Prob > mx {
				mx = h.Prob
			}
		}
		t.PrecipProbMx = mx
	}
	// Daily max of hourly AQ for the target day: both chips share this
	// same time basis (max over hourly values) so a single hour's
	// PM2.5/AQI pair can never mix inconsistent instants. Initialized from
	// zero so only target-day hourly values count; falls back to current
	// when hourly data is missing.
	maxAQI := 0
	maxPM := 0.0
	for i, ts := range a.Hourly.Time {
		if len(ts) >= 10 && ts[:10] == day {
			if i < len(a.Hourly.AQI) && a.Hourly.AQI[i] != nil && *a.Hourly.AQI[i] > maxAQI {
				maxAQI = *a.Hourly.AQI[i]
			}
			if i < len(a.Hourly.PM25) && a.Hourly.PM25[i] != nil && *a.Hourly.PM25[i] > maxPM {
				maxPM = *a.Hourly.PM25[i]
			}
		}
	}
	if maxAQI == 0 {
		maxAQI = t.AQI
	}
	if maxPM == 0 {
		maxPM = t.PM25
	}
	t.AQIMax = maxAQI
	t.PM25Max = maxPM
	// Chips display the daily max, so the category must match the max.
	t.AQICategory = AQICategory(t.AQIMax)
	return t, nil
}

func deriveMin(times []string, temps []float64, day string) (float64, bool) {
	found := false
	min := 0.0
	for i, ts := range times {
		if len(ts) >= 10 && ts[:10] == day && i < len(temps) {
			if !found || temps[i] < min {
				min = temps[i]
				found = true
			}
		}
	}
	return min, found
}

// Fetch downloads both payloads via c and returns Today for date
// ("" = first day). minRefetch is passed to fetch (0 = always revalidate).
func Fetch(ctx context.Context, c *fetch.Client, date string, minRefetch time.Duration) (*Today, error) {
	fr, err := c.Get(ctx, ForecastURL, minRefetch)
	if err != nil {
		// Accept URL variants (unescaped timezone slash) in fixture maps.
		var alt error
		_ = alt
		return nil, fmt.Errorf("openmeteo: forecast: %w", err)
	}
	ar, err := c.Get(ctx, AirQualityURL, minRefetch)
	if err != nil {
		return nil, fmt.Errorf("openmeteo: air quality: %w", err)
	}
	return Parse(fr.Body, ar.Body, date)
}

// CanonicalURL matches ForecastURL modulo escaping so tests can map either.
func CanonicalURL(u string) string {
	return strings.ReplaceAll(u, "%2F", "/")
}
