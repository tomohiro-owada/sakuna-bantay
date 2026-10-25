package snapshot

import (
	"context"
	"testing"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/phivolcs"
)

func fixtureTime() time.Time {
	// Matches the fixture feed (25 Oct 2026 evening PHT): GFA caps active,
	// WCP signal1 (expires 24 Oct 16:50) expired, ACP (expires 24 Oct 23:51)
	// expired.
	loc, _ := time.LoadLocation("Asia/Manila")
	return time.Date(2026, 10, 25, 18, 0, 0, 0, loc)
}

func TestBuildFixtures(t *testing.T) {
	snap, err := Build(context.Background(), fetch.New("", ""), Options{
		FixturesDir: "testdata",
		Now:         fixtureTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Today == nil {
		t.Fatal("today is nil")
	}
	if snap.Today.Date != "2026-10-25" {
		t.Errorf("today date = %q, want 2026-10-25", snap.Today.Date)
	}
	if snap.Pagasa.FeedCount == 0 {
		t.Error("feed count is 0")
	}
	// Expired WCP/ACP must not be active; GFA (expires 26 Oct) stays.
	for _, a := range snap.Pagasa.Active {
		if a.Expires.Before(fixtureTime()) {
			t.Errorf("expired alert in active set: %s expires %v", a.Identifier, a.Expires)
		}
	}
	if snap.Pagasa.WatchMax != 0 {
		t.Errorf("watch max = %d, want 0 (fixture signals expired)", snap.Pagasa.WatchMax)
	}
	if snap.Pagasa.WatchSummary != "NCR: no signal" {
		t.Errorf("watch summary = %q", snap.Pagasa.WatchSummary)
	}
	if len(snap.Quakes) == 0 {
		t.Fatal("no quakes")
	}
	if snap.QuakesSource != "PHIVOLCS" {
		t.Errorf("quakes source = %q, want PHIVOLCS", snap.QuakesSource)
	}
	for _, q := range snap.Quakes {
		if q.Notable {
			t.Errorf("fixture quake %s flagged notable (M%.1f dist %.0f)", q.ID, q.Mag, q.DistanceKm)
		}
		if q.DistanceKm == 0 {
			t.Errorf("quake %s missing distance", q.ID)
		}
	}
	if snap.Taal == nil {
		t.Fatal("taal is nil")
	}
	if snap.Taal.Taal.Level != 1 {
		t.Errorf("taal level = %d, want 1", snap.Taal.Taal.Level)
	}
	if len(snap.Errors) != 0 {
		t.Errorf("errors = %v, want none", snap.Errors)
	}
}

func TestNotableThresholds(t *testing.T) {
	mk := func(mag, dist, lat, lon float64) Quake {
		return Quake{Event: phivolcs.Event{Mag: mag, DistanceKm: dist, Lat: lat, Lon: lon}}
	}
	snap := &Snapshot{Quakes: []Quake{
		mk(5.0, 150, 14.0, 121.0),   // boundary: notable
		mk(4.99, 150, 14.0, 121.0),  // just below
		mk(5.0, 150.1, 14.0, 121.0), // just outside
		mk(6.5, 2000, 10.0, 120.0),  // M6.5 in PH bounds
	}}
	markNotable(snap)
	if !snap.Quakes[0].Notable {
		t.Error("M5.0 at 150km should be notable")
	}
	if snap.Quakes[1].Notable {
		t.Error("M4.99 should not be notable")
	}
	if snap.Quakes[2].Notable {
		t.Error("M5.0 beyond 150km should not be notable (no intensity/PH-M6.5)")
	}
	if !snap.Quakes[3].Notable {
		t.Error("M6.5 in PH should be notable")
	}
	// Intensity >= III in watch area.
	snap2 := &Snapshot{Quakes: []Quake{mk(3.0, 500, 0, 0)}}
	snap2.Quakes[0].Intensities = []phivolcs.Intensity{{Place: "City of Manila", Roman: "III", Level: 3}}
	markNotable(snap2)
	if !snap2.Quakes[0].Notable {
		t.Error("intensity III in Manila should be notable")
	}
}

func TestBuildFixtureErrorIsolation(t *testing.T) {
	// Missing dir: every source records an error, build still returns.
	snap, err := Build(context.Background(), fetch.New("", ""), Options{
		FixturesDir: "testdata-missing",
		Now:         fixtureTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Errors) == 0 {
		t.Error("expected errors for missing fixtures dir")
	}
	if snap.Today != nil || snap.Taal != nil || len(snap.Quakes) != 0 {
		t.Error("expected empty data with missing fixtures")
	}
}
