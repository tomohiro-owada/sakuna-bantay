package posttext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/openmeteo"
)

func fixtureToday(t *testing.T) DailyData {
	t.Helper()
	fc, err := os.ReadFile(filepath.Join("testdata", "openmeteo-forecast.json"))
	if err != nil {
		t.Fatal(err)
	}
	aq, err := os.ReadFile(filepath.Join("testdata", "openmeteo-aq.json"))
	if err != nil {
		t.Fatal(err)
	}
	day, err := openmeteo.Parse(fc, aq, "")
	if err != nil {
		t.Fatal(err)
	}
	return DailyData{
		DateLabel:     "25 Oct 2026",
		TMax:          day.TMax,
		TMin:          day.TMin,
		ApparentMax:   day.ApparentMax,
		PrecipProbMax: day.PrecipProbMx,
		PrecipSum:     day.PrecipSum,
		UVMax:         day.UVMax,
		UVCategory:    day.UVCategory,
		AQI:           day.AQI,
		AQICategory:   day.AQICategory,
		TaalLevel:     1,
		TaalText:      "Low-level unrest",
		Alerts:        []string{"Signal #1: Batanes, Cagayan"},
	}
}

func splitBlocks(s string) []string {
	var parts []string
	var cur []string
	flush := func() {
		parts = append(parts, strings.Join(cur, "\n"))
		cur = nil
	}
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) == Separator {
			flush()
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	return parts
}

func checkStructure(t *testing.T, name, post string, wantSource string) {
	t.Helper()
	parts := splitBlocks(post)
	if len(parts) != 4 {
		t.Fatalf("%s: split into %d parts, want 4 (TL,EN,JA,footer)", name, len(parts))
	}
	for i, p := range parts[:3] {
		lines := strings.Split(strings.Trim(p, "\n"), "\n")
		if len(lines) < 2 || len(lines) > 5 {
			t.Errorf("%s: block %d has %d lines, want 2-5: %q", name, i, len(lines), p)
		}
		for _, ln := range lines {
			if len(ln) > 160 {
				t.Errorf("%s: block %d line too long (%d chars): %q", name, i, len(ln), ln)
			}
		}
	}
	footer := parts[3]
	if !strings.Contains(footer, wantSource) {
		t.Errorf("%s: footer missing source %q:\n%s", name, wantSource, footer)
	}
	if !strings.Contains(footer, Link) {
		t.Errorf("%s: footer missing link %s", name, Link)
	}
	if !strings.Contains(footer, Unofficial) {
		t.Errorf("%s: footer missing unofficial line", name)
	}
	// No panic words.
	lower := strings.ToLower(post)
	for _, w := range []string{"panic", "deadly", "catastroph", "evacuate now", "doom"} {
		if strings.Contains(lower, w) {
			t.Errorf("%s: post contains panic word %q", name, w)
		}
	}
	// Max 2 emoji (count common emoji runes; our texts use none).
	emoji := 0
	for _, r := range post {
		if r > 0x2500 && (r >= 0x1F300 && r <= 0x1FAFF || r >= 0x2600 && r <= 0x27BF) {
			emoji++
		}
	}
	if emoji > 2 {
		t.Errorf("%s: %d emoji, want max 2", name, emoji)
	}
}

func TestDailyGolden(t *testing.T) {
	d := fixtureToday(t)
	got := Daily(d)
	checkStructure(t, "daily", got, "Source: DOST-PAGASA")
	// Natural Taglish first line (fixture: 33.5→34°C, 38.3→38°C).
	if !strings.Contains(got, "Init ngayon sa Las Piñas: hanggang 34°C, feels like 38°C.") {
		t.Errorf("daily Taglish first line unnatural or wrong:\n%s", got)
	}
	if !strings.Contains(got, "Mababa ang tsansa ng ulan") {
		t.Errorf("daily Taglish rain line missing:\n%s", got)
	}
	// Japanese consistency: polite です/ます.
	if !strings.Contains(got, "です") {
		t.Errorf("daily Japanese should use です/ます:\n%s", got)
	}
	// REVIEW_C #3: hourly 06-23 max is 6 (not daily 14).
	// REVIEW_C #4: translated UV in TL/JA blocks.
	// PHASE_D: Taal and AQI are card chips only, never in the daily text.
	want := `Init ngayon sa Las Piñas: hanggang 34°C, feels like 38°C.
Mababa ang tsansa ng ulan (pinakamataas 6%).
UV High (mataas) (7.8).
Signal #1: Batanes, Cagayan
———
Hot day in Las Piñas: up to 34°C, feels like 38°C.
Low chance of rain (max 6%).
UV High (7.8).
Signal #1: Batanes, Cagayan
———
ラスピニャスは今日も暑いです。最高34°C、体感38°Cです。
雨の可能性は低いです（最大6%）。
UV指数 強い (7.8)。
Signal #1: Batanes, Cagayan
———
Source: DOST-PAGASA (CC BY 4.0), PHIVOLCS, Open-Meteo (CC BY 4.0)
https://abalol.xyz/sakuna-bantay/
Unofficial · not an official warning — follow PAGASA, PHIVOLCS and your LGU.`
	if got != want {
		t.Errorf("daily golden mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestDailyNoTaalAQI(t *testing.T) {
	// PHASE_D context: Taal and AQI are card chips only — never in the
	// daily post text (all 3 languages). UV stays.
	d := fixtureToday(t)
	got := Daily(d)
	parts := splitBlocks(got)
	if len(parts) != 4 {
		t.Fatalf("split into %d parts, want 4", len(parts))
	}
	for i, p := range parts[:3] {
		for _, bad := range []string{
			"Taal", "タール", "AQI", "model estimate", "モデル推定",
			"Low-level unrest", "Unhealthy", "mababang aktibidad",
		} {
			if strings.Contains(p, bad) {
				t.Errorf("block %d contains %q, want no Taal/AQI:\n%s", i, bad, p)
			}
		}
	}
	if !strings.Contains(got, "UV High (mataas) (7.8).") {
		t.Errorf("TL UV line missing:\n%s", got)
	}
	if !strings.Contains(got, "UV指数 強い (7.8)。") {
		t.Errorf("JA UV line missing:\n%s", got)
	}
}

func TestDailyNoAlerts(t *testing.T) {
	d := fixtureToday(t)
	d.Alerts = nil
	got := Daily(d)
	checkStructure(t, "daily-empty", got, "Source: DOST-PAGASA")
	if !strings.Contains(got, "Walang aktibong babala ng PAGASA para sa NCR area.") {
		t.Errorf("daily empty Taglish line missing:\n%s", got)
	}
	if !strings.Contains(got, "No active PAGASA warnings for NCR area.") {
		t.Errorf("daily empty English line missing:\n%s", got)
	}
	if !strings.Contains(got, "NCR周辺にPAGASAの有効な警報はありません。") {
		t.Errorf("daily empty Japanese line missing:\n%s", got)
	}
	// Sentinel from the card pipeline must translate too.
	d.Alerts = []string{"No active PAGASA warnings for NCR area"}
	got2 := Daily(d)
	if !strings.Contains(got2, "Walang aktibong babala ng PAGASA para sa NCR area.") {
		t.Errorf("daily sentinel Taglish missing:\n%s", got2)
	}
	if !strings.Contains(got2, "NCR周辺にPAGASAの有効な警報はありません。") {
		t.Errorf("daily sentinel Japanese missing:\n%s", got2)
	}
}

func TestQuakeGolden(t *testing.T) {
	q := QuakeData{
		Mag: 5.1, DepthKm: 2, Place: "73 km N 59° E of Baganga (Davao Oriental)",
		DistanceKm: 789, Compass: "SE", TimeLabel: "16 Oct 2026, 5:13 PM PHT",
		Source: "PHIVOLCS",
	}
	got := Quake(q)
	checkStructure(t, "quake", got, "Source: PHIVOLCS")
	if !strings.Contains(got, "Lindol M5.1") {
		t.Errorf("quake Taglish missing Lindol line:\n%s", got)
	}
	if !strings.Contains(got, "地震 M5.1") {
		t.Errorf("quake Japanese missing 地震 line:\n%s", got)
	}
	if strings.Contains(got, "latest notable") {
		t.Errorf("quake live notable should not carry fallback note:\n%s", got)
	}
	// REVIEW_C #6: Japanese compass uses Japanese directions.
	if !strings.Contains(got, "ラスピニャスから南東に約789 km") {
		t.Errorf("quake Japanese compass not translated:\n%s", got)
	}

	// Fallback variant carries trilingual lines (REVIEW_C #5), no debug.
	q.FallbackNote = true
	got2 := Quake(q)
	if strings.Contains(got2, "latest notable") {
		t.Errorf("quake fallback must not contain debug line:\n%s", got2)
	}
	for _, want := range []string{
		"Walang malakas na lindol malapit sa Las Piñas ngayon; ito ang pinakamalapit na kamakailangang lindol (para sa impormasyon lang).",
		"No significant quake near Las Piñas; this is the most relevant recent event (for information only).",
		"ラスピニャス周辺で大きな地震はありません。参考として最近の地震を表示しています。",
	} {
		if !strings.Contains(got2, want) {
			t.Errorf("quake fallback missing %q:\n%s", want, got2)
		}
	}
	checkStructure(t, "quake-fallback", got2, "Source: PHIVOLCS")

	// Sample marks first line and uses synthetic source (REVIEW_C #2).
	q.Sample = true
	got3 := Quake(q)
	if !strings.Contains(strings.Split(got3, "\n")[0], "SAMPLE") {
		t.Errorf("quake sample missing SAMPLE mark:\n%s", got3)
	}
	if !strings.Contains(got3, "Synthetic sample — not a real event") {
		t.Errorf("quake sample missing synthetic source:\n%s", got3)
	}
	if strings.Contains(got3, "Source: PHIVOLCS") {
		t.Errorf("quake sample must not claim PHIVOLCS:\n%s", got3)
	}
	checkStructure(t, "quake-sample", got3, "SAMPLE ONLY — synthetic data")
}

func TestSignalGolden(t *testing.T) {
	s := SignalData{
		Cyclone:      "Typhoon Rosal (CHAMPI)",
		IssuedLabel:  "25 Oct 2026, 11:00 AM PHT",
		Levels:       map[string]int{"Batanes": 2, "Cagayan": 1},
		WatchSummary: "NCR: no signal",
	}
	got := Signal(s)
	checkStructure(t, "signal", got, "Source: DOST-PAGASA")
	if !strings.Contains(got, "Typhoon Rosal (CHAMPI). NCR: no signal.") {
		t.Errorf("signal first line wrong:\n%s", got)
	}
	if !strings.Contains(got, "Sundan ang PAGASA") {
		t.Errorf("signal Taglish closing missing:\n%s", got)
	}

	empty := Signal(SignalData{Cyclone: "", IssuedLabel: "25 Oct 2026, 11:00 AM PHT", Levels: map[string]int{}, WatchSummary: "NCR: no signal"})
	if !strings.Contains(empty, "No active cyclone") {
		t.Errorf("signal empty cyclone missing:\n%s", empty)
	}
}

func TestTaalGolden(t *testing.T) {
	ta := TaalData{
		Level: 1, LevelText: "Low-level unrest",
		BulletinTitle: "Taal Volcano Summary of 24Hr Observation 25 October 2026 12:00 AM",
		BulletinDate:  "25 Oct 2026",
	}
	got := Taal(ta)
	checkStructure(t, "taal", got, "Source: PHIVOLCS")
	if !strings.Contains(got, "Taal Alert Level 1 — Low-level unrest.") {
		t.Errorf("taal EN first line wrong:\n%s", got)
	}
	if !strings.Contains(got, "Taal Alert Level 1 — Low-level unrest (mababang aktibidad).") {
		t.Errorf("taal Taglish gloss missing:\n%s", got)
	}
	if !strings.Contains(got, "タール火山 アラートレベル1 — 低レベルの火山活動。") {
		t.Errorf("taal Japanese level name missing:\n%s", got)
	}
	if !strings.Contains(got, "Bawal pumasok sa Taal Volcano Island (PDZ).") {
		t.Errorf("taal Taglish reminder missing:\n%s", got)
	}
	if !strings.Contains(got, "タール火山 アラートレベル1") {
		t.Errorf("taal Japanese missing:\n%s", got)
	}
}

func TestRainThresholds(t *testing.T) {
	// REVIEW_C #3: shared thresholds <20 low, 20-49 some, >=50 likely.
	for _, tc := range []struct {
		p    int
		want string
	}{
		{0, "Mababa ang tsansa"},
		{19, "Mababa ang tsansa"},
		{20, "May tsansa ng ulan"},
		{31, "May tsansa ng ulan"},
		{49, "May tsansa ng ulan"},
		{50, "Mataas ang tsansa"},
		{88, "Mataas ang tsansa"},
	} {
		tl, en, ja := rainLines(tc.p)
		if !strings.Contains(tl, tc.want) {
			t.Errorf("rainLines(%d) TL missing %q: %q", tc.p, tc.want, tl)
		}
		if tc.p < 20 && !strings.Contains(en, "Low chance") {
			t.Errorf("rainLines(%d) EN should be low: %q", tc.p, en)
		}
		if tc.p >= 20 && tc.p < 50 && !strings.Contains(en, "Some chance") {
			t.Errorf("rainLines(%d) EN should be some: %q", tc.p, en)
		}
		if tc.p >= 50 && !strings.Contains(en, "Likely rain") {
			t.Errorf("rainLines(%d) EN should be likely: %q", tc.p, en)
		}
		if !strings.Contains(ja, "雨") {
			t.Errorf("rainLines(%d) JA missing rain: %q", tc.p, ja)
		}
	}
}

func TestSignalSampleSource(t *testing.T) {
	s := SignalData{
		Cyclone:      "Bagyong SAMPLE (sample only)",
		IssuedLabel:  "25 Oct 2026, 11:00 AM PHT",
		Levels:       map[string]int{"Batanes": 3},
		WatchSummary: "NCR: Signal #2",
		Sample:       true,
	}
	got := Signal(s)
	checkStructure(t, "signal-sample", got, "SAMPLE ONLY — synthetic data")
	if strings.Contains(got, "Rosal (CHAMPI)") {
		t.Errorf("synthetic signal must not use real cyclone name:\n%s", got)
	}
	if !strings.Contains(got, "Bagyong SAMPLE (sample only)") {
		t.Errorf("synthetic signal missing fictional name:\n%s", got)
	}
}

func TestCompassJA(t *testing.T) {
	cases := map[string]string{
		"N": "北", "NNE": "北北東", "NE": "北東", "ENE": "東北東",
		"E": "東", "ESE": "東南東", "SE": "南東", "SSE": "南南東",
		"S": "南", "SSW": "南南西", "SW": "南西", "WSW": "西南西",
		"W": "西", "WNW": "西北西", "NW": "北西", "NNW": "北北西",
	}
	for en, ja := range cases {
		if got := compassJA(en); got != ja {
			t.Errorf("compassJA(%q) = %q, want %q", en, got, ja)
		}
	}
	// Full quake JA line uses translated compass.
	q := QuakeData{Mag: 4.2, DepthKm: 22, Place: "16 km N 77° W of Dinalungan (Aurora)", DistanceKm: 211, Compass: "NNE", TimeLabel: "19 Oct 2026, 7:51 PM PHT", Source: "PHIVOLCS"}
	if got := Quake(q); !strings.Contains(got, "ラスピニャスから北北東に約211 km") {
		t.Errorf("quake NNE not translated to 北北東:\n%s", got)
	}
}

func TestNoUntranslatedEnglish(t *testing.T) {
	// REVIEW_C #4: Taglish/Japanese blocks must not contain leftover English
	// except proper nouns / place strings.
	d := fixtureToday(t)
	d.Alerts = nil
	got := Daily(d)
	parts := splitBlocks(got)
	tl, ja := parts[0], parts[2]
	for _, bad := range []string{
		"Walang active na PAGASA warning",
		"No active PAGASA warnings for NCR area",
		"NCRエリアに有効なPAGASA警報はありません",
	} {
		if strings.Contains(tl, bad) || strings.Contains(ja, bad) {
			// The old untranslated strings must be gone (new strings differ).
			if bad == "Walang active na PAGASA warning" && strings.Contains(tl, "Walang aktibong babala") {
				continue
			}
			t.Errorf("found stale untranslated line %q", bad)
		}
	}
	if !strings.Contains(tl, "Walang aktibong babala") {
		t.Errorf("TL alerts not translated:\n%s", tl)
	}
	if strings.Contains(ja, "No active PAGASA") {
		t.Errorf("JA block contains English alerts line:\n%s", ja)
	}
	if strings.Contains(ja, "Unhealthy for Sensitive Groups") {
		t.Errorf("JA AQI not translated:\n%s", ja)
	}
	if strings.Contains(ja, "Low-level unrest") {
		t.Errorf("JA Taal not translated:\n%s", ja)
	}
}

func TestAlertGeneric(t *testing.T) {
	// PHASE_D §3: generic rain/tsunami alert text keeps the trilingual
	// structure.
	a := AlertData{
		Title: "Orange rainfall warning — Metro Manila",
		Lines: []string{"Metro Manila, Rizal, Laguna"},
	}
	got := Alert(a)
	checkStructure(t, "alert", got, "Source: DOST-PAGASA")
	if !strings.Contains(got, "Sundan ang PAGASA at LGU") {
		t.Errorf("alert TL closing missing:\n%s", got)
	}
	if !strings.Contains(got, "PAGASAと自治体") {
		t.Errorf("alert JA closing missing:\n%s", got)
	}
	empty := Alert(AlertData{})
	checkStructure(t, "alert-empty", empty, "Source: DOST-PAGASA")
	if !strings.Contains(empty, "Weather alert") {
		t.Errorf("alert default title missing:\n%s", empty)
	}
}
