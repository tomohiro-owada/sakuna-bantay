package site

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/card"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/pagasa"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/posttext"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/snapshot"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/volcano"
)

func testSnap(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	loc, _ := time.LoadLocation("Asia/Manila")
	now := time.Date(2026, 10, 25, 18, 0, 0, 0, loc)
	snap, err := snapshot.Build(context.Background(), fetch.New("", ""), snapshot.Options{
		FixturesDir: "../snapshot/testdata",
		Now:         now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestGenerate(t *testing.T) {
	snap := testSnap(t)
	out := t.TempDir()
	cards := t.TempDir()
	// fake card PNGs (1x1) so copying is exercised.
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for _, n := range []string{"daily-20261025.png", "taal-16933.png", "quake-synth-near.png"} {
		if err := os.WriteFile(filepath.Join(cards, n), png, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	brandDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(brandDir, "sakuna-bantay-logo.svg"), []byte(`<svg></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(brandDir, "favicon"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brandDir, "favicon", "favicon.svg"), []byte(`<svg></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Generate(Options{
		Snap: snap, CardsDir: cards, BrandDir: brandDir, OutDir: out, BasePath: "/sakuna-bantay/",
	}); err != nil {
		t.Fatal(err)
	}
	// Stable names only: today.png + taal.png; dated names and synth
	// cards are never published.
	for _, n := range []string{"index.html", "alerts.html", "quakes.html", "taal.html", "sources.html", "style.css",
		"sakuna-bantay-logo.svg", "today.png", "taal.png"} {
		if _, err := os.Stat(filepath.Join(out, n)); err != nil {
			t.Errorf("missing %s: %v", n, err)
		}
	}
	for _, n := range []string{"daily-20261025.png", "quake-synth-near.png", "signal-synth.png", "quake-latest.png"} {
		if _, err := os.Stat(filepath.Join(out, n)); !os.IsNotExist(err) {
			t.Errorf("must not publish %s", n)
		}
	}
	idx, _ := os.ReadFile(filepath.Join(out, "index.html"))
	s := string(idx)
	for _, want := range []string{"Unofficial, not an official source", "Las Piñas", "alerts.html", "quakes.html"} {
		if !strings.Contains(s, want) {
			t.Errorf("index.html missing %q", want)
		}
	}
	if strings.Contains(s, "/sakuna-bantay/alerts.html") {
		t.Errorf("nav links must be relative (local disk support), got absolute base-prefixed link")
	}
	src, _ := os.ReadFile(filepath.Join(out, "sources.html"))
	for _, want := range []string{"CC BY 4.0", "public domain", "OFL", "Unofficial, not an official source"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("sources.html missing %q", want)
		}
	}
}

func TestGeneratePublishFields(t *testing.T) {
	snap := testSnap(t)
	out := t.TempDir()
	cards := t.TempDir()
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for _, n := range []string{"daily-20261025.png", "taal-16933.png"} {
		if err := os.WriteFile(filepath.Join(cards, n), png, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Generate(Options{
		Snap: snap, CardsDir: cards, BrandDir: "", OutDir: out, BasePath: "/sakuna-bantay/",
	}); err != nil {
		t.Fatal(err)
	}
	pages := []string{"index.html", "alerts.html", "quakes.html", "taal.html", "sources.html"}
	for _, p := range pages {
		b, err := os.ReadFile(filepath.Join(out, p))
		if err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
		s := string(b)
		// GA4 tag exactly like Presyo Bantay (default ID).
		for _, want := range []string{
			`<script async src="https://www.googletagmanager.com/gtag/js?id=G-VL047YCMK8"></script>`,
			`gtag('config','G-VL047YCMK8')`,
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%s missing GA tag %q", p, want)
			}
		}
		// Trilingual privacy line.
		for _, want := range []string{
			"Gumagamit ang site na ito ng Google Analytics para sa bilang ng bisita.",
			"This site uses Google Analytics to count visits.",
			"アクセス解析にGoogleアナリティクスを使っています。",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%s missing privacy line %q", p, want)
			}
		}
		// Source repo link + unofficial disclaimer.
		if !strings.Contains(s, "https://github.com/tomohiro-owada/sakuna-bantay") {
			t.Errorf("%s missing source repo link", p)
		}
		if !strings.Contains(s, "Unofficial, not an official source") {
			t.Errorf("%s missing unofficial disclaimer", p)
		}
		// Favicons.
		for _, want := range []string{
			`href="favicon/favicon.svg"`,
			`favicon-32.png`,
			`favicon-16.png`,
			`apple-touch-icon`,
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%s missing favicon %q", p, want)
			}
		}
		// Canonical + og.
		if !strings.Contains(s, "https://abalol.xyz/sakuna-bantay/") {
			t.Errorf("%s missing canonical/base URL", p)
		}
		if !strings.Contains(s, "og:image") || !strings.Contains(s, "today.png") {
			t.Errorf("%s missing og:image Today card (today.png)", p)
		}
		if strings.Contains(s, "daily-20261025.png") || strings.Contains(strings.ToLower(s), "synth") {
			t.Errorf("%s must not reference dated/synth card names", p)
		}
		// Volatile Generated timestamp carries data-volatile so the
		// publisher hash can strip it.
		if !strings.Contains(s, "data-volatile") {
			t.Errorf("%s missing data-volatile marker", p)
		}
		if !strings.Contains(s, "og:title") || !strings.Contains(s, "og:description") {
			t.Errorf("%s missing og:title/description", p)
		}
		// Asset links stay relative (disk + /sakuna-bantay/ base).
		// Canonical/og URLs are absolute https://..., but href/src for
		// assets (nav, css, images) must not be root-absolute.
		if strings.Contains(s, `href="/sakuna-bantay/`) || strings.Contains(s, `src="/sakuna-bantay/`) ||
			strings.Contains(s, `href="/style.css"`) {
			t.Errorf("%s has absolute asset links", p)
		}
	}

	// Custom GA id flows through to every page.
	out2 := t.TempDir()
	if err := Generate(Options{
		Snap: snap, CardsDir: cards, BrandDir: "", OutDir: out2, BasePath: "/sakuna-bantay/",
		GAID: "G-TEST123",
	}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out2, "index.html"))
	if !strings.Contains(string(b), "G-TEST123") {
		t.Errorf("custom GAID not rendered")
	}
	if strings.Contains(string(b), "G-VL047YCMK8") {
		t.Errorf("default GAID leaked into custom build")
	}
	// site.Config exposes the same GAID field.
	var cfg Config = Options{GAID: "G-TEST123"}
	if cfg.GAID != "G-TEST123" {
		t.Errorf("site.Config GAID field missing")
	}
	if DefaultGAID != "G-VL047YCMK8" {
		t.Errorf("DefaultGAID = %q, want G-VL047YCMK8", DefaultGAID)
	}
}

func TestGenerateStableNamesNoSynth(t *testing.T) {
	snap := testSnap(t)
	out := t.TempDir()
	cards := t.TempDir()
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	// Live dated cards plus synth SAMPLEs: only the live ones publish,
	// renamed to stable names.
	for _, n := range []string{
		"daily-20261025.png", "taal-16933.png",
		"signal-synth.png", "quake-synth-near.png", "daily-synth-foo.png",
	} {
		if err := os.WriteFile(filepath.Join(cards, n), png, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Generate(Options{
		Snap: snap, CardsDir: cards, BrandDir: "", OutDir: out, BasePath: "/sakuna-bantay/",
	}); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ents {
		if strings.HasSuffix(strings.ToLower(e.Name()), ".png") {
			got = append(got, e.Name())
		}
	}
	sort.Strings(got)
	want := []string{"taal.png", "today.png"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("published PNGs = %v, want %v", got, want)
	}
	idx, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if !strings.Contains(string(idx), `src="today.png"`) {
		t.Errorf("index.html must reference today.png")
	}
	taal, _ := os.ReadFile(filepath.Join(out, "taal.html"))
	if !strings.Contains(string(taal), `src="taal.png"`) {
		t.Errorf("taal.html must reference taal.png")
	}
	// The publisher strips volatile lines with sed /data-volatile/d:
	// every HTML file must keep its volatile timestamp on such a line,
	// and stripping must leave the privacy + source lines intact.
	for _, p := range []string{"index.html", "alerts.html", "quakes.html", "taal.html", "sources.html"} {
		b, err := os.ReadFile(filepath.Join(out, p))
		if err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
		var kept []string
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "data-volatile") {
				continue
			}
			kept = append(kept, line)
		}
		j := strings.Join(kept, "\n")
		if strings.Contains(j, "data-volatile") {
			t.Errorf("%s: stripped output still has data-volatile", p)
		}
		// Static footer content survives the strip.
		if !strings.Contains(j, "This site uses Google Analytics to count visits.") {
			t.Errorf("%s: strip removed static privacy line", p)
		}
	}
}

func buildSiteHTML(t *testing.T, snap *snapshot.Snapshot) (string, string) {
	t.Helper()
	out := t.TempDir()
	cards := t.TempDir()
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for _, n := range []string{"daily-20261025.png", "taal-16933.png"} {
		if err := os.WriteFile(filepath.Join(cards, n), png, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Generate(Options{
		Snap: snap, CardsDir: cards, BrandDir: "", OutDir: out, BasePath: "/sakuna-bantay/",
	}); err != nil {
		t.Fatal(err)
	}
	idx, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	al, err := os.ReadFile(filepath.Join(out, "alerts.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(idx), string(al)
}

// TestIndexSummaryMatchesCardInputs ensures the index summary reuses the
// SAME values as the card/posttext inputs for the same snapshot: rain is
// the displayed-window max with shared thresholds, AQI is the daily max
// labelled model estimate, Taal level comes from snapshot.taal.level.
func TestIndexSummaryMatchesCardInputs(t *testing.T) {
	snap := testSnap(t)
	if snap.Today == nil {
		t.Fatal("fixture snapshot has no Today")
	}
	// Force divergences that caught the live bug: stale daily aggregate
	// (14) vs window max (80), current AQI (102) vs daily max (121),
	// stale Taal text vs canonical LevelDescription, NCRMax vs Taal level.
	snap.Today.PrecipProbMx = 14
	for i, h := range snap.Today.HourlyRain {
		if h.Hour == 19 {
			snap.Today.HourlyRain[i].Prob = 80
		}
	}
	snap.Today.AQI = 102
	snap.Today.AQIMax = 121
	snap.Today.AQICategory = "Unhealthy for Sensitive Groups"
	snap.Today.AQINote = "model estimate"
	if snap.Taal != nil {
		snap.Taal.Taal.Level = 2
		snap.Taal.Taal.LevelText = "STALE"
	}
	snap.Pagasa.NCRMax = 0
	snap.Pagasa.WatchSummary = "NCR: no signal"

	_, _, windowMax := card.DailyDisplay(*snap.Today, snap.GeneratedAt)
	if windowMax != 80 {
		t.Fatalf("fixture windowMax = %d, want 80 (hour 19 in displayed window)", windowMax)
	}
	rainTL, rainEN, _ := posttext.RainLines(windowMax)
	_ = rainTL
	if !strings.Contains(rainEN, "Likely rain") {
		t.Fatalf("RainLines(80) should be likely: %q", rainEN)
	}

	idx, _ := buildSiteHTML(t, snap)
	// Rain: window max number + likely wording, not stale low wording.
	if !strings.Contains(idx, "80%") {
		t.Errorf("index summary missing window max 80%%:\n%s", idx)
	}
	if !strings.Contains(idx, "Likely rain") && !strings.Contains(idx, "Mataas ang tsansa") {
		t.Errorf("index summary missing likely-rain wording for 80%% (shared thresholds)")
	}
	if strings.Contains(idx, "Mababa ang tsansa ng ulan (max 80%)") {
		t.Errorf("index still uses stale low wording for 80%%")
	}
	// AQI: daily max with model-estimate label, not current value.
	if !strings.Contains(idx, "121") {
		t.Errorf("index summary missing AQI max 121")
	}
	if !strings.Contains(idx, "model estimate") {
		t.Errorf("index summary missing model-estimate label for AQI")
	}
	if strings.Contains(idx, "AQI (US, max) 102") || strings.Contains(idx, "AQI 102") {
		t.Errorf("index summary must show AQI max (121), not current (102)")
	}
	// Taal: level number from snapshot.taal.level with canonical text.
	wantLevel := snap.Taal.Taal.Level
	wantDesc := volcano.LevelDescription(wantLevel)
	if !strings.Contains(idx, fmt.Sprintf("Alert Level %d", wantLevel)) {
		t.Errorf("index summary missing Taal Alert Level %d", wantLevel)
	}
	if !strings.Contains(idx, wantDesc) {
		t.Errorf("index summary missing canonical Taal text %q", wantDesc)
	}
	if strings.Contains(idx, "STALE") {
		t.Errorf("index summary must not use stale stored Taal LevelText")
	}
}

// TestIndexTaalLevel1 pins the live bug: NCRMax 0 with Taal level 1 must
// render "Alert Level 1 · Low-level unrest" (not "Alert Level 0"), on
// separate lines from the watch summary.
func TestIndexTaalLevel1(t *testing.T) {
	snap := testSnap(t)
	if snap.Taal == nil {
		t.Fatal("fixture snapshot has no Taal")
	}
	snap.Taal.Taal.Level = 1
	snap.Taal.Taal.LevelText = "Normal" // stale: must be recomputed
	snap.Pagasa.NCRMax = 0
	snap.Pagasa.WatchSummary = "NCR: no signal"

	idx, _ := buildSiteHTML(t, snap)
	if !strings.Contains(idx, "Alert Level 1") {
		t.Errorf("index summary missing Alert Level 1:\n%s", idx)
	}
	if !strings.Contains(idx, "Low-level unrest") {
		t.Errorf("index summary missing Low-level unrest for level 1")
	}
	if strings.Contains(idx, "Alert Level 0") {
		t.Errorf("index summary must not show Alert Level 0 for Taal level 1")
	}
	// Taal line and watch summary must be on separate lines, not run on.
	if strings.Contains(idx, "Low-level unrest NCR: no signal") {
		t.Errorf("Taal description and watch summary must be on separate lines")
	}
}

// TestHeaderUsesMarkOnly ensures the header uses the mark (favicon) rather
// than the full lockup SVG whose navy wordmark is invisible on navy.
func TestHeaderUsesMarkOnly(t *testing.T) {
	snap := testSnap(t)
	idx, _ := buildSiteHTML(t, snap)
	if !strings.Contains(idx, `src="favicon/favicon.svg"`) {
		t.Errorf("header must use the mark favicon/favicon.svg")
	}
	if strings.Contains(idx, `<img class="logo" src="sakuna-bantay-logo.svg"`) {
		t.Errorf("header must not use the full lockup sakuna-bantay-logo.svg (navy-on-navy)")
	}
	if !strings.Contains(idx, "Sakuna Bantay") {
		t.Errorf("header must keep the text title")
	}
}

// TestAlertsActiveOnlyGrouped covers the alerts page: only ACTIVE (not
// expired, not superseded) alerts, each with headline/event, areas,
// severity, issued/expires PHT and a CAP link; watch-area items grouped
// first, with a trilingual notice when the watch area has none.
func TestAlertsActiveOnlyGrouped(t *testing.T) {
	snap := testSnap(t)
	now := snap.GeneratedAt
	mk := func(id, headline, event, sev string, sentH, expH int, areas []string, refs string) *pagasa.Alert {
		var al pagasa.Alert
		al.Identifier = id
		al.Status = "Actual"
		al.MsgType = "Alert"
		al.Sent = now.Add(time.Duration(sentH) * time.Hour)
		al.Expires = now.Add(time.Duration(expH) * time.Hour)
		al.Headline = headline
		al.Event = event
		al.Severity = sev
		al.SourceURL = "https://example.test/cap/" + id + ".cap"
		for _, d := range areas {
			al.Areas = append(al.Areas, pagasa.Area{Desc: d})
		}
		al.References = refs
		return &al
	}
	watchAlert := mk("watch-1", "Orange Rainfall Warning — Metro Manila", "Rainfall", "Severe", -1, 5,
		[]string{"Metro Manila", "Cavite", "Laguna"}, "")
	outerAlert := mk("outer-1", "General Flood Advisory — Region 8", "Flood Advisory", "Moderate", -2, 6,
		[]string{"Eastern Samar", "Northern Samar", "Southern Leyte"}, "")
	expired := mk("expired-1", "Expired Advisory", "Flood Advisory", "Minor", -30, -1,
		[]string{"Metro Manila"}, "")
	superseded := mk("old-1", "Superseded Advisory", "Flood Advisory", "Minor", -3, 5,
		[]string{"Laguna"}, "")
	superseder := mk("new-1", "Updated Advisory", "Flood Advisory", "Moderate", -1, 5,
		[]string{"Laguna"}, "")
	// References are "sender,identifier,sent" triples (space-separated).
	superseder.References = "sender,old-1," + superseded.Sent.Format(time.RFC3339)
	snap.Pagasa.Active = []*pagasa.Alert{watchAlert, outerAlert, expired, superseded, superseder}

	_, alertsHTML := buildSiteHTML(t, snap)
	// Active only.
	if !strings.Contains(alertsHTML, "Orange Rainfall Warning") {
		t.Errorf("alerts page missing active watch alert")
	}
	if !strings.Contains(alertsHTML, "General Flood Advisory") {
		t.Errorf("alerts page missing active outer alert")
	}
	if strings.Contains(alertsHTML, "Expired Advisory") {
		t.Errorf("alerts page must not list expired alerts")
	}
	if strings.Contains(alertsHTML, "Superseded Advisory") {
		t.Errorf("alerts page must not list superseded alerts")
	}
	if strings.Contains(alertsHTML, "expired-1") || strings.Contains(alertsHTML, "old-1 —") {
		t.Errorf("alerts page must not expose raw identifiers")
	}
	// Required fields per item.
	for _, want := range []string{"Severity", "Issued", "Expires", "Areas:", "CAP source", "https://example.test/cap/watch-1.cap"} {
		if !strings.Contains(alertsHTML, want) {
			t.Errorf("alerts page item missing %q", want)
		}
	}
	// Grouping: watch first.
	watchIdx := strings.Index(alertsHTML, "Para sa NCR area / For the NCR area")
	elseIdx := strings.Index(alertsHTML, "Iba pang lugar / Elsewhere in PH")
	if watchIdx < 0 || elseIdx < 0 {
		t.Fatalf("alerts page missing group headings")
	}
	if watchIdx > elseIdx {
		t.Errorf("watch-area group must come first")
	}
	if strings.Index(alertsHTML, "Orange Rainfall Warning") < watchIdx ||
		strings.Index(alertsHTML, "Orange Rainfall Warning") > elseIdx {
		t.Errorf("watch alert must be under the watch-area heading")
	}
	if strings.Index(alertsHTML, "General Flood Advisory") < elseIdx {
		t.Errorf("outer alert must be under the Elsewhere heading")
	}
	// PHT label.
	if !strings.Contains(alertsHTML, "PHT") {
		t.Errorf("alerts page must label times PHT")
	}
}

// TestAlertsWatchEmptyTrilingual ensures the watch-area section says so
// trilingually when no watch-area alert is active.
func TestAlertsWatchEmptyTrilingual(t *testing.T) {
	snap := testSnap(t)
	now := snap.GeneratedAt
	outer := &pagasa.Alert{
		Identifier: "outer-only",
		Status:     "Actual",
		MsgType:    "Alert",
		Sent:       now.Add(-time.Hour),
		Expires:    now.Add(5 * time.Hour),
		Headline:   "General Flood Advisory",
		Event:      "Flood Advisory",
		Severity:   "Moderate",
		SourceURL:  "https://example.test/cap/outer-only.cap",
		Areas:      []pagasa.Area{{Desc: "Eastern Samar"}},
	}
	snap.Pagasa.Active = []*pagasa.Alert{outer}
	_, alertsHTML := buildSiteHTML(t, snap)
	for _, want := range []string{
		"Walang aktibong babala ng PAGASA para sa NCR area.",
		"No active PAGASA warnings for NCR area.",
		"NCR周辺にPAGASAの有効な警報はありません。",
	} {
		if !strings.Contains(alertsHTML, want) {
			t.Errorf("watch-empty notice missing trilingual line %q", want)
		}
	}
}
