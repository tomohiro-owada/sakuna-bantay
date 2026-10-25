package pagasa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func mustParse(t *testing.T, name string) *Alert {
	t.Helper()
	a, err := ParseCAP(load(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestParseFeed(t *testing.T) {
	entries, err := ParseFeed(load(t, "pagasa-feed.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries parsed")
	}
	// Count by product.
	counts := map[Product]int{}
	for _, e := range entries {
		counts[e.Product]++
		if e.CapURL == "" {
			t.Error("entry missing cap URL")
		}
		if e.Updated.IsZero() {
			t.Errorf("entry %q missing updated", e.Title)
		}
	}
	if counts[ProductWCP] == 0 {
		t.Error("want some wcp entries")
	}
	if counts[ProductACP] == 0 {
		t.Error("want some acp entries")
	}
	if counts[ProductGFA] == 0 {
		t.Error("want some gfa entries")
	}
	// First entry is a GFA.
	if entries[0].Product != ProductGFA {
		t.Errorf("first entry product = %s, want gfa", entries[0].Product)
	}
	if !strings.HasSuffix(entries[0].CapURL, ".cap") {
		t.Errorf("cap URL = %s", entries[0].CapURL)
	}
	// 72h filter: feed spans Oct 22-25; now = Oct 25 18:00 PHT.
	now, _ := time.Parse(time.RFC3339, "2026-10-25T18:00:00+08:00")
	recent := Recent(entries, now, 72*time.Hour)
	for _, e := range recent {
		if now.Sub(e.Updated) > 72*time.Hour {
			t.Errorf("stale entry in recent: %s %v", e.Title, e.Updated)
		}
	}
	// An entry from Oct 22 04:46 should be excluded (>72h).
	foundOld := false
	for _, e := range entries {
		if strings.Contains(e.Title, "Super Typhoon") && e.Updated.Day() == 22 && e.Updated.Hour() == 4 {
			foundOld = true
			for _, r := range recent {
				if r.CapURL == e.CapURL {
					t.Errorf("old entry %q should be excluded from 72h window", e.Title)
				}
			}
		}
	}
	if !foundOld {
		t.Error("expected an old Oct-22 entry to test 72h cutoff")
	}
}

func TestProductFromURL(t *testing.T) {
	cases := []struct {
		url  string
		want Product
	}{
		{"https://publicalert.pagasa.dost.gov.ph/output/wcp/abc.cap", ProductWCP},
		{"https://publicalert.pagasa.dost.gov.ph/output/acp/abc.cap", ProductACP},
		{"https://publicalert.pagasa.dost.gov.ph/output/gfa/abc.cap", ProductGFA},
		{"https://publicalert.pagasa.dost.gov.ph/output/rfa/abc.cap", ProductOther},
		{"https://example.com/x.cap", ProductOther},
	}
	for _, tc := range cases {
		if got := ProductFromURL(tc.url); got != tc.want {
			t.Errorf("ProductFromURL(%s) = %s, want %s", tc.url, got, tc.want)
		}
	}
}

func TestParseSignal1(t *testing.T) {
	a := mustParse(t, "pagasa-wcp-signal1.cap")
	if a.Identifier != "21b29a06-e297-47bd-a79d-20826e724a2c" {
		t.Errorf("identifier = %s", a.Identifier)
	}
	if a.Status != "Actual" || a.MsgType != "Alert" {
		t.Errorf("status/msg = %s/%s", a.Status, a.MsgType)
	}
	if a.Event != "Tropical Cyclone Warning" {
		t.Errorf("event = %q", a.Event)
	}
	if a.Signal() != 1 {
		t.Errorf("signal = %d, want 1", a.Signal())
	}
	if got := a.Cyclone(); got != "Typhoon Rosal (CHAMPI)" {
		t.Errorf("cyclone = %q", got)
	}
	if len(a.Areas) != 1 || a.Areas[0].Desc != "Batanes" {
		t.Errorf("areas = %+v", a.Areas)
	}
	if a.Areas[0].SAME() != "020900000" {
		t.Errorf("SAME = %q", a.Areas[0].SAME())
	}
	if a.Params["Public Storm Warning Signal Level"] != "1" {
		t.Errorf("signal param = %q", a.Params["Public Storm Warning Signal Level"])
	}
	if a.Expires.IsZero() {
		t.Error("expires not parsed")
	}
	// Batanes is not in the watch area.
	if a.InWatchArea() {
		t.Error("Batanes should not be in watch area")
	}
	if a.RainfallColor() != "" {
		t.Error("TC warning should not be a rainfall warning")
	}
	if a.IsTsunami() {
		t.Error("should not be tsunami")
	}
}

func TestParseSignal2FileIsGFA(t *testing.T) {
	// Fixture pagasa-wcp-signal2.cap actually holds a GFA payload
	// (identifier matches the Region-2 GFA feed entry).
	a := mustParse(t, "pagasa-wcp-signal2.cap")
	if !strings.Contains(a.Event, "Flood") {
		t.Errorf("expected flood event, got %q", a.Event)
	}
	if a.Signal() != 0 {
		t.Errorf("GFA signal = %d, want 0", a.Signal())
	}
	if len(a.Areas) != 2 {
		t.Errorf("areas = %d, want 2", len(a.Areas))
	}
	if len(a.ReferencedIDs()) != 1 || a.ReferencedIDs()[0] != "855536fb-a3b5-4721-9015-92cc1dd493dc" {
		t.Errorf("references = %q", a.ReferencedIDs())
	}
}

func TestParseGFA(t *testing.T) {
	a := mustParse(t, "pagasa-gfa.cap")
	if a.Identifier != "3c0fc291-74a3-414b-8ffc-61e3bc7e18fa" {
		t.Errorf("identifier = %s", a.Identifier)
	}
	if len(a.Areas) != 6 {
		t.Errorf("areas = %d, want 6", len(a.Areas))
	}
	if a.Areas[0].Polygon == "" {
		t.Error("polygon should be kept")
	}
}

func TestParseACP(t *testing.T) {
	a := mustParse(t, "pagasa-acp.cap")
	if a.Event != "Tropical Cyclone Alert" {
		t.Errorf("event = %q", a.Event)
	}
	if a.Signal() != 0 {
		t.Errorf("ACP has no signal number, got %d", a.Signal())
	}
	if got := a.Cyclone(); got != "Typhoon Rosal (CHAMPI)" {
		t.Errorf("cyclone = %q", got)
	}
}

func TestSignalLevelTable(t *testing.T) {
	cases := []struct {
		headline string
		want     int
	}{
		{"Tropical Cyclone Warning : Typhoon Rosal (CHAMPI) Signal #1", 1},
		{"Tropical Cyclone Warning : Super Typhoon Rosal (CHAMPI) Signal #5", 5},
		{"Tropical Cyclone Warning Signal #3", 3},
		{"signal #2", 2},
		{"Signal No. 4", 4},
		{"General Flood Advisory", 0},
		{"Tropical Cyclone Alert : Typhoon Rosal (CHAMPI)", 0},
		{"Signal #6", 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := SignalLevel(tc.headline, ""); got != tc.want {
			t.Errorf("SignalLevel(%q) = %d, want %d", tc.headline, got, tc.want)
		}
	}
}

func TestCycloneNameTable(t *testing.T) {
	cases := []struct {
		headline string
		want     string
	}{
		{"Tropical Cyclone Warning : Typhoon Rosal (CHAMPI) Signal #1", "Typhoon Rosal (CHAMPI)"},
		{"Tropical Cyclone Warning : Super Typhoon Rosal (CHAMPI) Signal #2", "Super Typhoon Rosal (CHAMPI)"},
		{"General Flood Advisory", ""},
	}
	for _, tc := range cases {
		if got := CycloneName(tc.headline); got != tc.want {
			t.Errorf("CycloneName(%q) = %q, want %q", tc.headline, got, tc.want)
		}
	}
}

func TestWatchAreaCodes(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"137600000", true},  // NCR 9d
		{"1380000000", true}, // NCR-ish 10d starting 13
		{"042100000", true},  // Cavite 9d
		{"0402100000", true}, // Cavite 10d
		{"043400000", true},  // Laguna 9d
		{"0403400000", true}, // Laguna 10d
		{"041000000", true},  // Batangas 9d
		{"0401000000", true}, // Batangas 10d
		{"045800000", true},  // Rizal 9d
		{"0405800000", true}, // Rizal 10d
		{"020900000", false}, // Batanes
		{"021500000", false}, // Cagayan
		{"082600000", false}, // Eastern Samar
		{"", false},
	}
	for _, tc := range cases {
		if got := IsWatchAreaCode(tc.code); got != tc.want {
			t.Errorf("IsWatchAreaCode(%q) = %v, want %v", tc.code, got, tc.want)
		}
	}
	if !IsNCRCode("137600000") || IsNCRCode("042100000") {
		t.Error("IsNCRCode wrong")
	}
}

func TestWatchAreaNames(t *testing.T) {
	for _, n := range []string{"Metro Manila", "NCR", "Las Piñas", "Las Pinas", "Cavite", "Laguna", "Batangas", "Rizal", "Province of Rizal"} {
		if !IsWatchAreaName(n) {
			t.Errorf("IsWatchAreaName(%q) = false, want true", n)
		}
	}
	for _, n := range []string{"Batanes", "Cagayan", "Eastern Samar", "Cebu"} {
		if IsWatchAreaName(n) {
			t.Errorf("IsWatchAreaName(%q) = true, want false", n)
		}
	}
}

func synthAlert(id, headline, event string, areas []Area, expires time.Time) *Alert {
	return &Alert{
		Identifier: id, Status: "Actual", MsgType: "Alert",
		Event: event, Headline: headline, Expires: expires,
		Areas:  areas,
		Params: map[string]string{},
	}
}

func TestProvinceSignalsAndWatch(t *testing.T) {
	now := time.Now()
	a1 := synthAlert("id1", "Tropical Cyclone Warning : Typhoon X Signal #1", "Tropical Cyclone Warning",
		[]Area{{Desc: "Cavite", Geocodes: map[string]string{"SAME": "042100000"}}}, now.Add(time.Hour))
	a2 := synthAlert("id2", "Tropical Cyclone Warning : Typhoon X Signal #2", "Tropical Cyclone Warning",
		[]Area{{Desc: "Cavite", Geocodes: map[string]string{"SAME": "042100000"}}}, now.Add(time.Hour))
	lvls, watch := ProvinceSignals([]*Alert{a1, a2})
	if lvls["Cavite"] != 2 {
		t.Errorf("Cavite max = %d, want 2", lvls["Cavite"])
	}
	if !watch {
		t.Error("watch should be affected")
	}
	batanes := synthAlert("id3", "Tropical Cyclone Warning : Typhoon X Signal #1", "Tropical Cyclone Warning",
		[]Area{{Desc: "Batanes", Geocodes: map[string]string{"SAME": "020900000"}}}, now.Add(time.Hour))
	lvls, watch = ProvinceSignals([]*Alert{batanes})
	if watch {
		t.Error("Batanes-only should not affect watch")
	}
	if MaxWatchSignal([]*Alert{a1, batanes}) != 1 {
		t.Errorf("MaxWatchSignal = %d, want 1", MaxWatchSignal([]*Alert{a1, batanes}))
	}
	if MaxWatchSignal([]*Alert{batanes}) != 0 {
		t.Error("MaxWatchSignal with no watch area should be 0")
	}
}

func TestRainfallDetection(t *testing.T) {
	mk := func(event, headline string, params map[string]string) *Alert {
		return &Alert{Identifier: "x", Status: "Actual", MsgType: "Alert",
			Event: event, Headline: headline, Params: params,
			Areas:   []Area{{Desc: "Metro Manila", Geocodes: map[string]string{"SAME": "137600000"}}},
			Expires: time.Now().Add(time.Hour),
		}
	}
	cases := []struct {
		name  string
		alert *Alert
		want  string
	}{
		{"orange", mk("Heavy Rainfall Warning", "Orange Rainfall Warning NCR", nil), "ORANGE"},
		{"red", mk("Heavy Rainfall Warning", "Red Rainfall Warning", nil), "RED"},
		{"yellow", mk("Rainfall Advisory", "Yellow Rainfall", nil), "YELLOW"},
		{"param", mk("Rainfall Warning", "Rainfall Warning", map[string]string{"color": "Red"}), "RED"},
		{"gfa", mk("General Flood Advisory", "General Flood Advisory", nil), ""},
		{"tc", mk("Tropical Cyclone Warning", "Signal #1", nil), ""},
	}
	for _, tc := range cases {
		if got := tc.alert.RainfallColor(); got != tc.want {
			t.Errorf("%s: RainfallColor = %q, want %q", tc.name, got, tc.want)
		}
	}
	if !mk("Heavy Rainfall Warning", "Orange", nil).InNCR() {
		t.Error("Metro Manila area should be InNCR")
	}
}

func TestTsunami(t *testing.T) {
	a := &Alert{Event: "Tsunami Warning"}
	if !a.IsTsunami() {
		t.Error("should detect tsunami")
	}
	b := &Alert{Event: "General Flood Advisory"}
	if b.IsTsunami() {
		t.Error("false tsunami")
	}
}

func TestActiveSet(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, "2026-10-25T18:00:00+08:00")
	old := &Alert{Identifier: "old", Status: "Actual", MsgType: "Alert", Expires: now.Add(time.Hour)}
	nw := &Alert{Identifier: "new", Status: "Actual", MsgType: "Alert", Expires: now.Add(time.Hour), References: "PAGASA-DOST,old,2026-10-25T10:00:00+08:00"}
	cancelled := &Alert{Identifier: "c", Status: "Actual", MsgType: "Cancel", Expires: now.Add(time.Hour)}
	expired := &Alert{Identifier: "e", Status: "Actual", MsgType: "Alert", Expires: now.Add(-time.Hour)}
	test := &Alert{Identifier: "t", Status: "Test", MsgType: "Alert", Expires: now.Add(time.Hour)}
	got := ActiveSet([]*Alert{old, nw, cancelled, expired, test}, now)
	if len(got) != 1 || got[0].Identifier != "new" {
		ids := []string{}
		for _, a := range got {
			ids = append(ids, a.Identifier)
		}
		t.Errorf("ActiveSet = %v, want [new]", ids)
	}
}

func TestReferencesParse(t *testing.T) {
	a := &Alert{References: "PAGASA-DOST,id1,2026-10-23T05:39:38+08:00 PAGASA-DOST,id2,2026-10-23T05:40:00+08:00"}
	ids := a.ReferencedIDs()
	if len(ids) != 2 || ids[0] != "id1" || ids[1] != "id2" {
		t.Errorf("ReferencedIDs = %v", ids)
	}
}

func stubCAPSleep(t *testing.T) {
	t.Helper()
	old := CAPSleep
	CAPSleep = func(time.Duration) {}
	t.Cleanup(func() { CAPSleep = old })
}

func TestFetchCAPsRetryOnce(t *testing.T) {
	stubCAPSleep(t)
	capBody := load(t, "pagasa-gfa.cap")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			// Transient failure: EOF-style reset via 500 then success on retry.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(capBody)
	}))
	defer srv.Close()
	c := fetch.New(t.TempDir(), "")
	c.HTTP = srv.Client()
	now, _ := time.Parse(time.RFC3339, "2026-10-25T18:00:00+08:00")
	entries := []FeedEntry{{ID: "e1", Title: "GFA", Updated: now, CapURL: srv.URL + "/a.cap"}}
	alerts, partial, err := FetchCAPs(context.Background(), c, entries, now)
	if err != nil || partial {
		t.Fatalf("retry should succeed without partial: alerts=%d partial=%v err=%v", len(alerts), partial, err)
	}
	if len(alerts) != 1 {
		t.Fatalf("want 1 alert after retry, got %d", len(alerts))
	}
	if hits.Load() != 2 {
		t.Errorf("want 2 fetches (1 retry), got %d", hits.Load())
	}
}

func TestFetchCAPsStaleSkip(t *testing.T) {
	stubCAPSleep(t)
	capBody := load(t, "pagasa-gfa.cap")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/stale.cap") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(capBody)
	}))
	defer srv.Close()
	c := fetch.New(t.TempDir(), "")
	c.HTTP = srv.Client()
	now, _ := time.Parse(time.RFC3339, "2026-10-25T18:00:00+08:00")
	entries := []FeedEntry{
		{ID: "fresh", Title: "GFA fresh", Updated: now, CapURL: srv.URL + "/fresh.cap"},
		{ID: "stale", Title: "old", Updated: now.Add(-25 * time.Hour), CapURL: srv.URL + "/stale.cap"},
	}
	alerts, partial, err := FetchCAPs(context.Background(), c, entries, now)
	if err != nil {
		t.Fatalf("stale skip should not error when usable alerts remain: %v", err)
	}
	if partial {
		t.Error("stale-only failure must not mark partial")
	}
	if len(alerts) != 1 {
		t.Errorf("want 1 usable alert, got %d", len(alerts))
	}
}

func TestFetchCAPsPartialFresh(t *testing.T) {
	stubCAPSleep(t)
	capBody := load(t, "pagasa-gfa.cap")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/bad.cap") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(capBody)
	}))
	defer srv.Close()
	c := fetch.New(t.TempDir(), "")
	c.HTTP = srv.Client()
	now, _ := time.Parse(time.RFC3339, "2026-10-25T18:00:00+08:00")
	entries := []FeedEntry{
		{ID: "good", Title: "GFA", Updated: now, CapURL: srv.URL + "/good.cap"},
		{ID: "bad", Title: "fresh bad", Updated: now.Add(-time.Hour), CapURL: srv.URL + "/bad.cap"},
	}
	alerts, partial, err := FetchCAPs(context.Background(), c, entries, now)
	if err == nil || !partial {
		t.Fatalf("fresh failure must be partial with error: partial=%v err=%v", partial, err)
	}
	if len(alerts) != 1 {
		t.Errorf("want 1 usable alert in partial mode, got %d", len(alerts))
	}
}
