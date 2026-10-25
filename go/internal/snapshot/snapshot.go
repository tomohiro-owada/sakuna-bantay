// Package snapshot aggregates all normalized data sources into one JSON
// Snapshot for the CLI (fetch → cards/decide/site).
//
// A failing source never aborts the run: the error is recorded in
// Errors and the rest of the snapshot is still built.
package snapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/openmeteo"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/pagasa"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/phivolcs"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/volcano"
)

// Rainfall is one rainfall warning affecting an area.
type Rainfall struct {
	CapID   string   `json:"cap_id"`
	Color   string   `json:"color"`
	Areas   []string `json:"areas"`
	InNCR   bool     `json:"in_ncr"`
	InWatch bool     `json:"in_watch"`
}

// Pagasa is the normalized PAGASA summary.
type Pagasa struct {
	Active          []*pagasa.Alert `json:"active"`
	ProvinceSignals map[string]int  `json:"province_signals"`
	WatchMax        int             `json:"watch_max_signal"`
	WatchAffected   bool            `json:"watch_affected"`
	WatchSummary    string          `json:"watch_summary"`
	NCRMax          int             `json:"ncr_max_signal"`
	RainNCR         string          `json:"rain_ncr"`
	Rainfall        []Rainfall      `json:"rainfall,omitempty"`
	Tsunami         []*pagasa.Alert `json:"tsunami,omitempty"`
	CycloneName     string          `json:"cyclone_name,omitempty"`
	CycloneIssued   string          `json:"cyclone_issued,omitempty"`
	FeedCount       int             `json:"feed_count"`
	// Partial is true when at least one fresh (<24h) CAP failed after a
	// retry but usable alerts from other CAPs were still parsed. Callers
	// may emit raises/new alerts but must not apply lowering/lifting
	// state changes in that pass.
	Partial bool `json:"partial,omitempty"`
}

// Quake is one earthquake with its notable flag per rules thresholds.
type Quake struct {
	phivolcs.Event
	Notable       bool   `json:"notable"`
	NotableReason string `json:"notable_reason,omitempty"`
}

// Snapshot is the normalized aggregation written by `sakuna fetch`.
type Snapshot struct {
	GeneratedAt  time.Time         `json:"generated_at"`
	Today        *openmeteo.Today  `json:"today,omitempty"`
	Pagasa       Pagasa            `json:"pagasa"`
	Quakes       []Quake           `json:"quakes"`
	QuakesSource string            `json:"quakes_source"`
	Taal         *volcano.Status   `json:"taal,omitempty"`
	Errors       map[string]string `json:"errors,omitempty"`
	FetchTimes   map[string]string `json:"fetch_times,omitempty"`
}

// Options controls Build.
type Options struct {
	// FixturesDir, when non-empty, reads from fixture files instead of
	// network (for tests/offline). Expected files:
	// openmeteo-forecast.json, openmeteo-aq.json, pagasa-feed.xml,
	// pagasa-wcp-signal1.cap, pagasa-acp.cap, pagasa-gfa.cap,
	// phivolcs-list.html, usgs-ph.geojson, wovodat-list.html.
	FixturesDir string
	// Now is the reference time (defaults to time.Now when zero).
	Now time.Time
	// SkipIntensities disables detail-page intensity fetching (tests).
	SkipIntensities bool
	// Per-source min-refetch intervals for the `run` loop (PHASE_D §3):
	// QuakesRefetch (default phivolcs.MinRefetch), PagasaFeedRefetch,
	// OpenMeteoRefetch, TaalRefetch (default 0 = always revalidate, as
	// before). Zero keeps the historical defaults so fetch/all behave
	// exactly as before.
	QuakesRefetch     time.Duration
	PagasaFeedRefetch time.Duration
	OpenMeteoRefetch  time.Duration
	TaalRefetch       time.Duration
}

func (o Options) now() time.Time {
	if o.Now.IsZero() {
		return time.Now()
	}
	return o.Now
}

// Build aggregates all sources. Errors per source are recorded, never fatal
// (an entirely empty snapshot still returns nil error unless nothing at all
// could be fetched — even then it returns the snapshot with Errors set).
func Build(ctx context.Context, c *fetch.Client, opts Options) (*Snapshot, error) {
	now := opts.now()
	snap := &Snapshot{
		GeneratedAt: now,
		Errors:      map[string]string{},
		FetchTimes:  map[string]string{},
		Pagasa: Pagasa{
			ProvinceSignals: map[string]int{},
		},
	}
	if opts.FixturesDir != "" {
		buildFixtures(snap, opts)
		// Intensities skipped in fixture mode (no detail URLs mapped).
		finishPagasa(snap, now)
		finishQuakes(snap)
		return snap, nil
	}

	// --- Open-Meteo ---
	if td, err := openmeteo.Fetch(ctx, c, "", opts.OpenMeteoRefetch); err != nil {
		snap.Errors["openmeteo"] = err.Error()
	} else {
		snap.Today = td
		snap.FetchTimes["openmeteo"] = now.Format(time.RFC3339)
	}

	// --- PAGASA ---
	entries, err := pagasa.FetchFeed(ctx, c, opts.PagasaFeedRefetch)
	if err != nil {
		snap.Errors["pagasa_feed"] = err.Error()
	} else {
		snap.Pagasa.FeedCount = len(entries)
		snap.FetchTimes["pagasa_feed"] = now.Format(time.RFC3339)
		recent := pagasa.Recent(entries, now, 72*time.Hour)
		if len(recent) > 0 {
			alerts, isPartial, ferr := pagasa.FetchCAPs(ctx, c, recent, now)
			if ferr != nil {
				snap.Errors["pagasa_caps"] = ferr.Error()
			}
			if len(alerts) > 0 {
				snap.FetchTimes["pagasa_caps"] = now.Format(time.RFC3339)
				snap.Pagasa.Active = pagasa.ActiveSet(alerts, now)
			}
			if isPartial && len(alerts) > 0 {
				snap.Pagasa.Partial = true
			}
		}
	}
	finishPagasa(snap, now)

	// --- PHIVOLCS + USGS backup ---
	qRef := opts.QuakesRefetch
	if qRef <= 0 {
		qRef = phivolcs.MinRefetch
	}
	evs, src, err := phivolcs.FetchWithBackupRefetch(ctx, c, qRef)
	if err != nil {
		snap.Errors["quakes"] = err.Error()
	} else {
		snap.QuakesSource = src
		snap.FetchTimes["quakes_"+strings.ToLower(src)] = now.Format(time.RFC3339)
		snap.Quakes = toQuakes(evs)
		if !opts.SkipIntensities {
			fillIntensities(ctx, c, snap)
		}
	}
	markNotable(snap)
	sortQuakes(snap)

	// --- Taal ---
	if st, err := volcano.Fetch(ctx, c, opts.TaalRefetch); err != nil {
		snap.Errors["taal"] = err.Error()
	} else {
		snap.Taal = st
		snap.FetchTimes["taal"] = now.Format(time.RFC3339)
	}
	return snap, nil
}

// finishPagasa derives ProvinceSignals, WatchMax, rainfall, tsunami, cyclone.
func finishPagasa(snap *Snapshot, now time.Time) {
	active := snap.Pagasa.Active
	levels, watch := pagasa.ProvinceSignals(active)
	snap.Pagasa.ProvinceSignals = levels
	snap.Pagasa.WatchAffected = watch
	snap.Pagasa.WatchMax = pagasa.MaxWatchSignal(active)
	snap.Pagasa.NCRMax = maxNCRSignal(active)
	if snap.Pagasa.NCRMax > 0 {
		snap.Pagasa.WatchSummary = fmt.Sprintf("NCR: Signal #%d", snap.Pagasa.NCRMax)
	} else {
		snap.Pagasa.WatchSummary = "NCR: no signal"
	}
	// Rainfall.
	best := ""
	rank := map[string]int{"": 0, "YELLOW": 1, "ORANGE": 2, "RED": 3}
	for _, a := range active {
		col := a.RainfallColor()
		if col == "" {
			continue
		}
		var areas []string
		for _, ar := range a.Areas {
			areas = append(areas, ar.Desc)
		}
		r := Rainfall{CapID: a.Identifier, Color: col, Areas: areas, InNCR: a.InNCR(), InWatch: a.InWatchArea()}
		snap.Pagasa.Rainfall = append(snap.Pagasa.Rainfall, r)
		if a.InNCR() && rank[col] > rank[best] {
			best = col
		}
	}
	snap.Pagasa.RainNCR = best
	// Tsunami.
	for _, a := range active {
		if a.IsTsunami() {
			snap.Pagasa.Tsunami = append(snap.Pagasa.Tsunami, a)
		}
	}
	// Cyclone: prefer the highest-signal active warning with a name,
	// tie-break by latest sent.
	bestSig, bestTime := -1, time.Time{}
	for _, a := range active {
		name := a.Cyclone()
		if name == "" {
			continue
		}
		sig := a.Signal()
		if sig > bestSig || (sig == bestSig && a.Sent.After(bestTime)) {
			bestSig = sig
			bestTime = a.Sent
			snap.Pagasa.CycloneName = name
			if !a.Sent.IsZero() {
				snap.Pagasa.CycloneIssued = a.Sent.Format(time.RFC3339)
			}
		}
	}
}

func maxNCRSignal(active []*pagasa.Alert) int {
	max := 0
	for _, a := range active {
		if lvl := a.Signal(); lvl > 0 && a.InNCR() && lvl > max {
			max = lvl
		}
	}
	return max
}

func toQuakes(evs []phivolcs.Event) []Quake {
	out := make([]Quake, 0, len(evs))
	for _, e := range evs {
		out = append(out, Quake{Event: e})
	}
	return out
}

func sortQuakes(snap *Snapshot) {
	sort.Slice(snap.Quakes, func(i, j int) bool {
		return snap.Quakes[i].Time.After(snap.Quakes[j].Time)
	})
}

// finishQuakes sorts fixture quakes (no intensities in fixture mode).
func finishQuakes(snap *Snapshot) {
	markNotable(snap)
	sortQuakes(snap)
}

// markNotable flags quakes per rules thresholds:
// (M>=5.0 and distance <=150km) OR (M>=6.5 in PH bounds) OR
// (any reported intensity >= III in the watch area).
func markNotable(snap *Snapshot) {
	for i := range snap.Quakes {
		q := &snap.Quakes[i]
		if q.Mag >= 5.0 && q.DistanceKm <= 150 {
			q.Notable = true
			q.NotableReason = "M>=5 within 150km"
			continue
		}
		if q.Mag >= 6.5 && phivolcs.InPHBounds(q.Lat, q.Lon) {
			q.Notable = true
			q.NotableReason = "M>=6.5 in PH"
			continue
		}
		for _, in := range q.Intensities {
			if in.Level >= 3 && intensityInWatch(in.Place) {
				q.Notable = true
				q.NotableReason = "intensity>=III in watch area"
				break
			}
		}
	}
}

// intensityInWatch reports whether an intensity place is in the watch area
// (NCR cities + Cavite/Laguna/Batangas/Rizal). Intensity places are city
// names ("City of Manila", "Quezon City"), so we match city names beyond
// pagasa.IsWatchAreaName's areaDesc keywords.
func intensityInWatch(place string) bool {
	if pagasa.IsWatchAreaName(place) {
		return true
	}
	n := strings.ToLower(place)
	cities := []string{
		"manila", "quezon city", "makati", "pasig", "taguig", "caloocan",
		"pasay", "paranaque", "parañaque", "las pinas", "las piñas",
		"muntinlupa", "marikina", "valenzuela", "malabon", "navotas",
		"san juan", "mandaluyong", "pateros",
	}
	for _, c := range cities {
		if strings.Contains(n, c) {
			return true
		}
	}
	return false
}

// fillIntensities fetches detail pages for M>=4 within 300km candidates.
// Best effort: failures ignored.
func fillIntensities(ctx context.Context, c *fetch.Client, snap *Snapshot) {
	for i := range snap.Quakes {
		q := &snap.Quakes[i]
		if q.Source != "PHIVOLCS" {
			continue
		}
		if q.Mag < 4.0 || q.DistanceKm > 300 {
			continue
		}
		if q.URL == "" {
			continue
		}
		ints, err := phivolcs.FetchIntensities(ctx, c, q.URL)
		if err != nil || len(ints) == 0 {
			continue
		}
		q.Intensities = ints
	}
}

// --- Fixture mode ---

func readFixture(dir, name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, name))
}

// capFixtureFor picks the local CAP fixture for a feed entry by product.
func capFixtureFor(e pagasa.FeedEntry) string {
	switch e.Product {
	case pagasa.ProductWCP:
		return "pagasa-wcp-signal1.cap"
	case pagasa.ProductACP:
		return "pagasa-acp.cap"
	case pagasa.ProductGFA:
		return "pagasa-gfa.cap"
	default:
		return ""
	}
}

func buildFixtures(snap *Snapshot, opts Options) {
	dir := opts.FixturesDir
	now := opts.now()
	// Open-Meteo.
	if fc, err := readFixture(dir, "openmeteo-forecast.json"); err != nil {
		snap.Errors["openmeteo"] = err.Error()
	} else if aq, err := readFixture(dir, "openmeteo-aq.json"); err != nil {
		snap.Errors["openmeteo"] = err.Error()
	} else if td, err := openmeteo.Parse(fc, aq, ""); err != nil {
		snap.Errors["openmeteo"] = err.Error()
	} else {
		snap.Today = td
		snap.FetchTimes["openmeteo"] = now.Format(time.RFC3339)
	}
	// PAGASA feed + CAPs.
	feedData, err := readFixture(dir, "pagasa-feed.xml")
	if err != nil {
		snap.Errors["pagasa_feed"] = err.Error()
		return
	}
	entries, err := pagasa.ParseFeed(feedData)
	if err != nil {
		snap.Errors["pagasa_feed"] = err.Error()
		return
	}
	snap.Pagasa.FeedCount = len(entries)
	snap.FetchTimes["pagasa_feed"] = now.Format(time.RFC3339)
	recent := pagasa.Recent(entries, now, 72*time.Hour)
	var alerts []*pagasa.Alert
	for _, e := range recent {
		name := capFixtureFor(e)
		if name == "" {
			continue
		}
		// Prefer a same-basename fixture when present (lets tests drop
		// exact CAP files into the fixtures dir); fall back to the
		// product-type fixture otherwise.
		candidates := []string{filepath.Base(e.CapURL), name}
		var data []byte
		var rerr error
		for _, cand := range candidates {
			data, rerr = readFixture(dir, cand)
			if rerr == nil {
				break
			}
		}
		if rerr != nil {
			continue
		}
		a, perr := pagasa.ParseCAP(data)
		if perr != nil {
			continue
		}
		a.SourceURL = e.CapURL
		a.FeedTitle = e.Title
		alerts = append(alerts, a)
	}
	snap.FetchTimes["pagasa_caps"] = now.Format(time.RFC3339)
	snap.Pagasa.Active = pagasa.ActiveSet(alerts, now)

	// PHIVOLCS list; USGS file is only the backup (used when the list is
	// missing/unparsable, mirroring live FetchWithBackup).
	if listData, err := readFixture(dir, "phivolcs-list.html"); err != nil {
		if usData, uerr := readFixture(dir, "usgs-ph.geojson"); uerr != nil {
			snap.Errors["quakes"] = err.Error()
		} else if evs, perr := phivolcs.ParseUSGS(usData); perr != nil {
			snap.Errors["quakes"] = perr.Error()
		} else {
			snap.QuakesSource = "USGS"
			snap.Quakes = toQuakes(evs)
			snap.FetchTimes["quakes_usgs"] = now.Format(time.RFC3339)
		}
	} else if evs, perr := phivolcs.ParseList(listData); perr != nil {
		snap.Errors["quakes"] = perr.Error()
	} else {
		snap.QuakesSource = "PHIVOLCS"
		snap.Quakes = toQuakes(evs)
		snap.FetchTimes["quakes_phivolcs"] = now.Format(time.RFC3339)
	}

	// Taal.
	if wov, err := readFixture(dir, "wovodat-list.html"); err != nil {
		snap.Errors["taal"] = err.Error()
	} else if st, perr := volcano.Parse(wov); perr != nil {
		snap.Errors["taal"] = perr.Error()
	} else {
		snap.Taal = st
		snap.FetchTimes["taal"] = now.Format(time.RFC3339)
	}
}
