package rules

import (
	"strings"
	"testing"
	"time"
)

func loc() *time.Location {
	l, _ := time.LoadLocation("Asia/Manila")
	if l == nil {
		return time.FixedZone("PHT", 8*3600)
	}
	return l
}

func pht(day, hour, min int) time.Time {
	return time.Date(2026, time.October, day, hour, min, 0, 0, loc())
}

func postedAt(t time.Time, kind Kind, id, dedupe string) Posted {
	return Posted{Time: t, Kind: kind, SourceID: id, DedupeKey: dedupe}
}

func decided(t *testing.T, now time.Time, evs []Event, hist []Posted, st State) Decision {
	t.Helper()
	return Decide(now, evs, hist, st)
}

func postedKinds(d Decision) []Kind {
	var out []Kind
	for _, e := range d.Post {
		out = append(out, e.Kind)
	}
	return out
}

func hasReason(items interface{}, substr string) bool {
	switch v := items.(type) {
	case []DeferredItem:
		for _, it := range v {
			if strings.Contains(it.Reason, substr) {
				return true
			}
		}
	case []SkippedItem:
		for _, it := range v {
			if strings.Contains(it.Reason, substr) {
				return true
			}
		}
	}
	return false
}

// --- Daily boundaries ---

func TestDailyWindow(t *testing.T) {
	mk := func() Event {
		return Event{Kind: KindDaily, SourceID: "x", DedupeKey: "daily:2026-10-25"}
	}
	cases := []struct {
		name string
		now  time.Time
		post bool
	}{
		{"05:59 too early", pht(25, 5, 59), false},
		{"06:00 open", pht(25, 6, 0), true},
		{"06:01 open", pht(25, 6, 1), true},
		{"11:59 last minute", pht(25, 11, 59), true},
		{"12:00 missed", pht(25, 12, 0), false},
		{"15:00 missed", pht(25, 15, 0), false},
		{"00:00 too early", pht(25, 0, 0), false},
	}
	for _, tc := range cases {
		d := decided(t, tc.now, []Event{mk()}, nil, State{})
		if got := len(d.Post) == 1; got != tc.post {
			t.Errorf("%s: posted=%v, want %v (skipped=%v)", tc.name, got, tc.post, d.Skipped)
		}
	}
}

func TestDailyDedupe(t *testing.T) {
	now := pht(25, 7, 0)
	ev := Event{Kind: KindDaily, SourceID: "x", DedupeKey: "daily:2026-10-25"}
	hist := []Posted{postedAt(pht(25, 6, 30), KindDaily, "old", "daily:2026-10-25")}
	d := decided(t, now, []Event{ev}, hist, State{HasTaal: true})
	if len(d.Post) != 0 {
		t.Error("daily already posted should not post")
	}
	// Different day posts fine.
	ev2 := Event{Kind: KindDaily, SourceID: "x", DedupeKey: "daily:2026-10-26"}
	d2 := decided(t, time.Date(2026, time.October, 26, 7, 0, 0, 0, loc()), []Event{ev2}, hist, State{HasTaal: true})
	if len(d2.Post) != 1 {
		t.Errorf("next-day daily should post, got %+v", d2)
	}
}

// --- Signal ---

func TestSignalChange(t *testing.T) {
	now := pht(25, 8, 0)
	cases := []struct {
		name  string
		last  int
		level int
		post  bool
	}{
		{"raised 0->1", 0, 1, true},
		{"raised 1->2", 1, 2, true},
		{"lowered 2->1", 2, 1, true},
		{"lifted 1->0", 1, 0, true},
		{"no change 1->1", 1, 1, false},
		{"no change 0->0", 0, 0, false},
		{"raised to 5", 2, 5, true},
	}
	for _, tc := range cases {
		ev := Event{Kind: KindSignal, SourceID: "cap-" + tc.name, DedupeKey: "signal:" + tc.name, SignalLevel: tc.level}
		d := decided(t, now, []Event{ev}, nil, State{LastSignal: tc.last, HasSignal: true, HasTaal: true, HasRain: true})
		if got := len(d.Post) == 1; got != tc.post {
			t.Errorf("%s: posted=%v want %v", tc.name, got, tc.post)
		}
	}
}

func TestSignalDedupeIncludesCAPID(t *testing.T) {
	now := pht(25, 8, 0)
	ev := Event{Kind: KindSignal, SourceID: "capA", DedupeKey: "signal:capA", SignalLevel: 1}
	hist := []Posted{postedAt(now.Add(-time.Hour), KindSignal, "capA", "signal:capA")}
	d := decided(t, now, []Event{ev}, hist, State{LastSignal: 0, HasSignal: true, HasTaal: true, HasRain: true})
	if len(d.Post) != 0 {
		t.Error("same CAP dedupe should not repost")
	}
}

// --- Rain ---

func TestRainColors(t *testing.T) {
	now := pht(25, 8, 0)
	cases := []struct {
		color string
		post  bool
	}{
		{"ORANGE", true}, {"RED", true},
		{"orange", true}, {"red", true},
		{"YELLOW", false}, {"", false}, {"GREEN", false},
	}
	for _, tc := range cases {
		ev := Event{Kind: KindRain, SourceID: "r-" + tc.color, DedupeKey: "rain:" + tc.color, RainColor: tc.color}
		d := decided(t, now, []Event{ev}, nil, State{HasTaal: true, HasSignal: true, HasRain: true})
		if got := len(d.Post) == 1; got != tc.post {
			t.Errorf("color %q: posted=%v want %v", tc.color, got, tc.post)
		}
	}
}

func TestRainNewIDOrColorChange(t *testing.T) {
	now := pht(25, 8, 0)
	st := State{LastRainColor: "ORANGE", HasTaal: true, HasSignal: true, HasRain: true}
	// Same id + same color -> skip.
	ev := Event{Kind: KindRain, SourceID: "cap1", DedupeKey: "rain:cap1a", RainColor: "ORANGE"}
	hist := []Posted{postedAt(now.Add(-time.Hour), KindRain, "cap1", "rain:cap1")}
	if d := decided(t, now, []Event{ev}, hist, st); len(d.Post) != 0 {
		t.Error("same id+color should skip")
	}
	// New id, same color -> post.
	ev2 := Event{Kind: KindRain, SourceID: "cap2", DedupeKey: "rain:cap2", RainColor: "ORANGE"}
	if d := decided(t, now, []Event{ev2}, hist, st); len(d.Post) != 1 {
		t.Error("new CAP id should post")
	}
	// Same id, color change ORANGE->RED -> post.
	ev3 := Event{Kind: KindRain, SourceID: "cap1", DedupeKey: "rain:cap1b", RainColor: "RED"}
	if d := decided(t, now, []Event{ev3}, hist, st); len(d.Post) != 1 {
		t.Error("color change should post")
	}
}

// --- Quake thresholds & boundaries ---

func qev(id string, mag, dist, lat, lon float64, imax int) Event {
	return Event{
		Kind: KindQuake, SourceID: id, DedupeKey: "quake:" + id,
		Magnitude: mag, DistanceKm: dist, Lat: lat, Lon: lon,
		QuakeTime:    pht(25, 6, 0),
		IntensityMax: imax,
	}
}

func TestQuakeThresholds(t *testing.T) {
	now := pht(25, 8, 0)
	st := State{HasTaal: true}
	// M>=5.0 and <=150km posts; M4.99 or >150 does not.
	cases := []struct {
		name string
		ev   Event
		post bool
	}{
		{"M5.0 d150 posts", qev("a1", 5.0, 150, 14.0, 121.0, 0), true},
		{"M4.99 d100 skips", qev("a2", 4.99, 100, 14.0, 121.0, 0), false},
		{"M5.0 d150.01 skips (over)", qev("a3", 5.0, 150.01, 14.0, 121.0, 0), false},
		{"M6.0 d150 posts", qev("a4", 6.0, 150, 14.0, 121.0, 0), true},
		{"M6.0 d151 skips unless PH", qev("a5", 6.0, 151, 14.0, 121.0, 0), false},
		{"M6.5 in bounds posts", qev("a6", 6.5, 5000, 10, 120, 0), true},
		{"M6.49 in bounds skips", qev("a7", 6.49, 5000, 10, 120, 0), false},
		{"M6.5 out of bounds skips", qev("a8", 6.5, 5000, 30, 130, 0), false},
		{"M4.0 intensity III posts", qev("a9", 4.0, 5000, 0, 0, 3), true},
		{"M4.0 intensity II skips", qev("a10", 4.0, 5000, 0, 0, 2), false},
		{"M7 far in-bounds posts", qev("a11", 7.0, 900, 19.8, 122.0, 0), true},
	}
	for _, tc := range cases {
		d := decided(t, now, []Event{tc.ev}, nil, st)
		if got := len(d.Post) == 1; got != tc.post {
			t.Errorf("%s: posted=%v want %v (skip=%v)", tc.name, got, tc.post, d.Skipped)
		}
	}
}

func TestQuakeDedupe(t *testing.T) {
	now := pht(25, 8, 0)
	st := State{HasTaal: true}
	// Same source id.
	ev := qev("same", 5.5, 100, 14.0, 121.0, 0)
	hist := []Posted{{Time: now.Add(-time.Hour), Kind: KindQuake, SourceID: "same", DedupeKey: "quake:other",
		QuakeTime: pht(25, 6, 0), Lat: 10, Lon: 120}}
	if d := decided(t, now, []Event{ev}, hist, st); len(d.Post) != 0 {
		t.Error("same source id should dedupe")
	}
	// USGS/PHIVOLCS duplicate: within 60s and 50km.
	base := pht(25, 6, 0)
	cand := Event{Kind: KindQuake, SourceID: "us1", DedupeKey: "quake:us1", Magnitude: 5.5, DistanceKm: 100,
		Lat: 14.0, Lon: 121.0, QuakeTime: base}
	hist2 := []Posted{{Time: now.Add(-time.Hour), Kind: KindQuake, SourceID: "ph1", DedupeKey: "quake:ph1",
		QuakeTime: base.Add(60 * time.Second), Lat: 14.1, Lon: 121.1}}
	if d := decided(t, now, []Event{cand}, hist2, st); len(d.Post) != 0 {
		t.Error("within 60s+50km should dedupe")
	}
	// 61s apart -> not duplicate.
	hist3 := []Posted{{Time: now.Add(-time.Hour), Kind: KindQuake, SourceID: "ph1", DedupeKey: "quake:ph1",
		QuakeTime: base.Add(61 * time.Second), Lat: 14.0, Lon: 121.0}}
	if d := decided(t, now, []Event{cand}, hist3, st); len(d.Post) != 1 {
		t.Error("61s apart should NOT dedupe")
	}
	// >50km apart -> not duplicate.
	hist4 := []Posted{{Time: now.Add(-time.Hour), Kind: KindQuake, SourceID: "ph1", DedupeKey: "quake:ph1",
		QuakeTime: base, Lat: 10.0, Lon: 125.0}}
	if d := decided(t, now, []Event{cand}, hist4, st); len(d.Post) != 1 {
		t.Errorf(">50km should NOT dedupe (dist ok)")
	}
	// Revision: same canonical id.
	candR := Event{Kind: KindQuake, SourceID: "2026_1025_1013_B1F", DedupeKey: "quake:rev2",
		Magnitude: 5.5, DistanceKm: 100, Lat: 14.0, Lon: 121.0, QuakeTime: base, QuakeCanon: "2026_1025_1013"}
	histR := []Posted{{Time: now.Add(-time.Hour), Kind: KindQuake, SourceID: "2026_1025_1013_B1",
		DedupeKey: "quake:rev1", QuakeTime: base.Add(30 * 24 * time.Hour), Lat: 0, Lon: 0, QuakeCanon: "2026_1025_1013"}}
	_ = histR
	if d := decided(t, now, []Event{candR}, []Posted{
		{Time: now.Add(-time.Hour), Kind: KindQuake, SourceID: "2026_1025_1013_B1", DedupeKey: "quake:rev1",
			QuakeTime: base.Add(500 * time.Second), Lat: 0, Lon: 0, QuakeCanon: "2026_1025_1013"},
	}, st); len(d.Post) != 0 {
		t.Error("same canonical revision should dedupe")
	}
}

// --- Taal / Tsunami ---

func TestTaalChange(t *testing.T) {
	now := pht(25, 8, 0)
	known := State{LastTaal: 1, HasTaal: true, HasSignal: true, HasRain: true}
	if d := decided(t, now, []Event{{Kind: KindTaal, SourceID: "t1", DedupeKey: "taal:t1", TaalLevel: 2}},
		nil, known); len(d.Post) != 1 {
		t.Error("level change should post")
	}
	if d := decided(t, now, []Event{{Kind: KindTaal, SourceID: "t1", DedupeKey: "taal:t1", TaalLevel: 1}},
		nil, known); len(d.Post) != 0 {
		t.Error("same level should skip")
	}
	// Unknown prior state baselines silently — no post.
	d := decided(t, now, []Event{{Kind: KindTaal, SourceID: "t1", DedupeKey: "taal:t1", TaalLevel: 1}},
		nil, State{})
	if len(d.Post) != 0 {
		t.Error("unknown state should baseline, not post")
	}
	if !hasReason(d.Skipped, "baseline") {
		t.Errorf("unknown taal should skip with baseline reason, got %v", d.Skipped)
	}
}

func TestBaselineFirstRun(t *testing.T) {
	now := pht(25, 8, 0)
	evs := []Event{
		{Kind: KindTaal, SourceID: "t1", DedupeKey: "taal:t1", TaalLevel: 1},
		{Kind: KindSignal, SourceID: "s1", DedupeKey: "signal:s1", SignalLevel: 1},
		{Kind: KindRain, SourceID: "r1", DedupeKey: "rain:r1", RainColor: "ORANGE"},
	}
	d := decided(t, now, evs, nil, State{})
	if len(d.Post) != 0 {
		t.Errorf("first run with unknown state must not post, got %v", postedKinds(d))
	}
	if len(d.Skipped) != 3 {
		t.Errorf("want 3 baseline skips, got %v", d.Skipped)
	}
	// After baselining, a real change posts (one kind at a time to avoid
	// 30-min spacing between non-bypass posts in the same pass).
	st := State{LastTaal: 1, HasTaal: true, LastSignal: 1, HasSignal: true, LastRainColor: "ORANGE", HasRain: true}
	evTaal := []Event{{Kind: KindTaal, SourceID: "t2", DedupeKey: "taal:t2", TaalLevel: 2}}
	if d := decided(t, now, evTaal, nil, st); len(d.Post) != 1 {
		t.Errorf("taal change after baseline should post, got %v", d)
	}
	evSig := []Event{{Kind: KindSignal, SourceID: "s2", DedupeKey: "signal:s2", SignalLevel: 2}}
	if d := decided(t, now, evSig, nil, st); len(d.Post) != 1 {
		t.Errorf("signal change after baseline should post, got %v", d)
	}
	evRain := []Event{{Kind: KindRain, SourceID: "r2", DedupeKey: "rain:r2", RainColor: "RED"}}
	if d := decided(t, now, evRain, nil, st); len(d.Post) != 1 {
		t.Errorf("rain change after baseline should post, got %v", d)
	}
}

func TestTsunamiAlways(t *testing.T) {
	now := pht(25, 8, 0)
	d := decided(t, now, []Event{{Kind: KindTsunami, SourceID: "ts1", DedupeKey: "tsunami:ts1"}}, nil, State{HasTaal: true})
	if len(d.Post) != 1 {
		t.Error("tsunami should post")
	}
}

// --- Spacing boundaries ---

func TestSpacing29vs30(t *testing.T) {
	mk := func(id string) Event {
		return Event{Kind: KindQuake, SourceID: id, DedupeKey: "quake:" + id, Magnitude: 5.5, DistanceKm: 100, Lat: 14.0, Lon: 121.0, QuakeTime: pht(25, 6, 0)}
	}
	now := pht(25, 8, 0)
	st := State{HasTaal: true}
	// Last post 29 min ago -> non-bypass deferred.
	hist29 := []Posted{postedAt(now.Add(-29*time.Minute), KindQuake, "old", "quake:old")}
	d := decided(t, now, []Event{mk("new")}, hist29, st)
	if len(d.Post) != 0 || !hasReason(d.Deferred, "too soon") {
		t.Errorf("29min: should defer too soon, got post=%d deferred=%v", len(d.Post), d.Deferred)
	}
	// Exactly 30 min ago -> allowed.
	hist30 := []Posted{postedAt(now.Add(-30*time.Minute), KindQuake, "old", "quake:old")}
	d2 := decided(t, now, []Event{mk("new")}, hist30, st)
	if len(d2.Post) != 1 {
		t.Errorf("30min: should post, got %+v", d2)
	}
	// Bypass (tsunami) ignores spacing.
	tsu := Event{Kind: KindTsunami, SourceID: "ts", DedupeKey: "tsunami:ts"}
	d3 := decided(t, now, []Event{tsu}, hist29, st)
	if len(d3.Post) != 1 {
		t.Errorf("tsunami bypass should post at 29min, got %+v", d3)
	}
	// Bypass: Rain RED and Signal>=3 ignore spacing.
	red := Event{Kind: KindRain, SourceID: "r", DedupeKey: "rain:r", RainColor: "RED"}
	if d := decided(t, now, []Event{red}, hist29, State{HasTaal: true, HasSignal: true, HasRain: true}); len(d.Post) != 1 {
		t.Error("rain RED bypass should post")
	}
	sig3 := Event{Kind: KindSignal, SourceID: "s", DedupeKey: "signal:s", SignalLevel: 3}
	if d := decided(t, now, []Event{sig3}, hist29, State{LastSignal: 0, HasSignal: true, HasTaal: true, HasRain: true}); len(d.Post) != 1 {
		t.Error("signal>=3 bypass should post")
	}
	// Non-bypass: Signal 2 and Rain ORANGE respect spacing.
	sig2 := Event{Kind: KindSignal, SourceID: "s2", DedupeKey: "signal:s2", SignalLevel: 2}
	if d := decided(t, now, []Event{sig2}, hist29, State{LastSignal: 0, HasSignal: true, HasTaal: true, HasRain: true}); len(d.Post) != 0 {
		t.Error("signal 2 should defer at 29min")
	}
}

// --- 24h cap, priority, bypass counts ---

func histRecent(n int, now time.Time) []Posted {
	var out []Posted
	for i := 0; i < n; i++ {
		out = append(out, Posted{Time: now.Add(-time.Duration(i+1) * time.Hour), Kind: KindQuake, SourceID: string(rune('a'+i)) + "x", DedupeKey: string(rune('a'+i)) + "q"})
	}
	return out
}

func TestCap6th7th(t *testing.T) {
	now := pht(25, 8, 0)
	st := State{HasTaal: true}
	q := func(id string, secOff int) Event {
		return Event{Kind: KindQuake, SourceID: id, DedupeKey: "quake:" + id, Magnitude: 5.5, DistanceKm: 100, Lat: 14 + float64(secOff)*0.01, Lon: 121, QuakeTime: now.Add(time.Duration(secOff) * time.Hour)}
	}
	// 5 recent -> 1 slot: first of two posts, second deferred for cap.
	d := decided(t, now, []Event{q("n1", 1), q("n2", 5)}, histRecent(5, now), st)
	if len(d.Post) != 1 || len(d.Deferred) != 1 || !hasReason(d.Deferred, "24h cap") {
		t.Errorf("5 recent + 2 candidates: post=%d deferred=%v skipped=%v", len(d.Post), d.Deferred, d.Skipped)
	}
	// 6 recent -> 0 slots.
	d2 := decided(t, now, []Event{q("n1", 1)}, histRecent(6, now), st)
	if len(d2.Post) != 0 || !hasReason(d2.Deferred, "24h cap") {
		t.Errorf("6 recent: should defer cap, got %+v", d2)
	}
	// Old posts outside 24h don't count.
	old := []Posted{postedAt(now.Add(-25*time.Hour), KindQuake, "o", "quake:o")}
	if d := decided(t, now, []Event{q("n1", 1)}, old, st); len(d.Post) != 1 {
		t.Error("25h-old post should not count toward cap")
	}
}

func TestPriorityWins(t *testing.T) {
	now := pht(25, 8, 0)
	st := State{HasTaal: true, HasSignal: true, HasRain: true, LastSignal: 0}
	daily := Event{Kind: KindDaily, SourceID: "d", DedupeKey: "daily:2026-10-25"}
	tsu := Event{Kind: KindTsunami, SourceID: "ts", DedupeKey: "tsunami:ts"}
	// 5 recent -> 1 slot; tsunami outranks daily even though daily listed first.
	d := decided(t, now, []Event{daily, tsu}, histRecent(5, now), st)
	if len(d.Post) != 1 || d.Post[0].Kind != KindTsunami {
		t.Errorf("priority: want tsunami, got %v", postedKinds(d))
	}
	if len(d.Deferred) != 1 || d.Deferred[0].Event.Kind != KindDaily {
		t.Errorf("daily should defer, got %v", d.Deferred)
	}
	// Full order: Tsunami > Signal>=3 > Rain RED > Quake > Signal other > Taal > Daily.
	evs := []Event{
		{Kind: KindDaily, SourceID: "d", DedupeKey: "daily:2026-10-25"},
		{Kind: KindTaal, SourceID: "t", DedupeKey: "taal:t", TaalLevel: 2},
		{Kind: KindSignal, SourceID: "s1", DedupeKey: "signal:s1", SignalLevel: 1},
		{Kind: KindQuake, SourceID: "q", DedupeKey: "quake:q", Magnitude: 5.5, DistanceKm: 100, Lat: 14, Lon: 121, QuakeTime: now},
		{Kind: KindRain, SourceID: "r", DedupeKey: "rain:r", RainColor: "RED"},
		{Kind: KindSignal, SourceID: "s3", DedupeKey: "signal:s3", SignalLevel: 3},
		{Kind: KindTsunami, SourceID: "ts", DedupeKey: "tsunami:ts"},
	}
	d2 := decided(t, now, evs, histRecent(5, now), State{LastSignal: 0, HasSignal: true, LastTaal: 1, HasTaal: true, HasRain: true})
	if len(d2.Post) != 1 || d2.Post[0].Kind != KindTsunami {
		t.Errorf("order: want tsunami first, got %v", postedKinds(d2))
	}
}

func TestBypassCountsTowardCap(t *testing.T) {
	now := pht(25, 8, 0)
	st := State{HasTaal: true}
	tsu := func(id string) Event { return Event{Kind: KindTsunami, SourceID: id, DedupeKey: "tsunami:" + id} }
	// 5 recent + 2 bypass tsunamis: only 1 slot -> 1 posts, 1 deferred for cap.
	d := decided(t, now, []Event{tsu("a"), tsu("b")}, histRecent(5, now), st)
	if len(d.Post) != 1 || !hasReason(d.Deferred, "24h cap") {
		t.Errorf("bypass must still count: post=%d deferred=%v", len(d.Post), d.Deferred)
	}
}

// --- Dedupe ---

func TestNeverPostSameDedupe(t *testing.T) {
	now := pht(25, 8, 0)
	ev := Event{Kind: KindQuake, SourceID: "x", DedupeKey: "quake:x", Magnitude: 5.5, DistanceKm: 50, Lat: 14, Lon: 121, QuakeTime: now}
	hist := []Posted{postedAt(now.Add(-2*time.Hour), KindQuake, "other-id", "quake:x")}
	d := decided(t, now, []Event{ev}, hist, State{HasTaal: true})
	if len(d.Post) != 0 {
		t.Error("same dedupe key must never repost")
	}
}

// --- Kill switch (enforced via Mode, not Decide) ---

func TestKillSwitchActive(t *testing.T) {
	dir := t.TempDir()
	if KillSwitchActive(dir) {
		t.Error("empty dir should not have kill switch")
	}
	// Decide always evaluates: KILL_SWITCH file does not suppress posts here.
	// Live blocking is enforced by fbpost.Mode / the run loop choosing the
	// dry-run poster, covered by run tests.
	now := pht(25, 8, 0)
	evs := []Event{{Kind: KindTsunami, SourceID: "ts", DedupeKey: "tsunami:ts"}}
	d := Decide(now, evs, nil, State{HasTaal: true, HasSignal: true, HasRain: true})
	if d.KillSwitch || len(d.Post) != 1 {
		t.Error("Decide should post normally (kill enforced in Mode)")
	}
	if !KillSwitchActive(t.TempDir()) {
		// empty temp dir: no file
	} else {
		t.Error("KillSwitchActive wrong")
	}
}

// --- Mixed regression: spacing across batch ---

func TestBatchSpacing(t *testing.T) {
	now := pht(25, 8, 0)
	// Two non-bypass quakes at once with distinct times/locations:
	// only one posts (30-min spacing within batch).
	mkQ := func(id string, lat float64, off time.Duration) Event {
		return Event{Kind: KindQuake, SourceID: id, DedupeKey: "quake:" + id, Magnitude: 5.5,
			DistanceKm: 100, Lat: lat, Lon: 121, QuakeTime: now.Add(off)}
	}
	d := decided(t, now, []Event{mkQ("aa", 14.0, -3*time.Hour), mkQ("bb", 10.0, -6*time.Hour)}, nil, State{HasTaal: true})
	if len(d.Post) != 1 || !hasReason(d.Deferred, "too soon") {
		t.Errorf("batch spacing: post=%d deferred=%v skipped=%v", len(d.Post), d.Deferred, d.Skipped)
	}
}
