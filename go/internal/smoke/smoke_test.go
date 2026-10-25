// Package smoke holds the polite live smoke test for Phase A fetchers.
//
// Run with SAKUNA_LIVE=1 (otherwise skipped). It performs exactly one
// request per source: Open-Meteo forecast+AQ, PAGASA feed, PHIVOLCS list
// (via cache + conditional GET), USGS backup query, WOVODAT list.
// No files are written outside t.TempDir(); nothing is posted anywhere.
package smoke

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/openmeteo"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/pagasa"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/phivolcs"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/volcano"
)

func TestLiveSmoke(t *testing.T) {
	if os.Getenv("SAKUNA_LIVE") != "1" {
		t.Skip("set SAKUNA_LIVE=1 to run the polite live smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := fetch.New(t.TempDir(), "")

	// 1. Open-Meteo (forecast + air quality = the fetcher's normal two calls).
	td, err := openmeteo.Fetch(ctx, c, "", 0)
	if err != nil {
		t.Errorf("openmeteo live: %v", err)
	} else {
		t.Logf("openmeteo: %s Tmax=%.1f AQI=%d", td.Date, td.TMax, td.AQI)
	}

	// 2. PAGASA feed only (CAP bodies skipped to stay at one request).
	entries, err := pagasa.FetchFeed(ctx, c, 0)
	if err != nil {
		t.Errorf("pagasa feed live: %v", err)
	} else {
		t.Logf("pagasa: %d feed entries", len(entries))
	}

	// 3. PHIVOLCS list via cache + conditional GET (5-min min-refetch).
	evs, src, err := phivolcs.FetchWithBackup(ctx, c)
	if err != nil {
		t.Errorf("phivolcs live: %v", err)
	} else {
		t.Logf("phivolcs: %d events via %s", len(evs), src)
	}

	// 4. USGS backup query (one request; proves the backup path works live).
	us, err := phivolcs.FetchUSGS(ctx, c)
	if err != nil {
		t.Errorf("usgs live: %v", err)
	} else {
		t.Logf("usgs: %d events", len(us))
	}

	// 5. WOVODAT bulletin list.
	st, err := volcano.Fetch(ctx, c, 0)
	if err != nil {
		t.Errorf("volcano live: %v", err)
	} else {
		t.Logf("volcano: levels=%v taal=%d bid=%s", st.Levels, st.Taal.Level, st.Taal.BulletinID)
	}
}
