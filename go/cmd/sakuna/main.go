// Command sakuna is the Sakuna Bantay CLI (SPEC §CLI).
//
// Subcommands:
//
//	sakuna fetch [-fixtures DIR] [-cache DIR] [-out FILE] [-now RFC3339]
//	sakuna cards [-snapshot FILE] -out DIR
//	sakuna decide [-snapshot FILE] [-state DIR] [-now RFC3339]
//	sakuna site [-snapshot FILE] [-cards DIR] [-out DIR] [-base PATH] [-brand DIR]
//	sakuna all [-fixtures DIR] [-cache DIR] [-out DIR] [-snapshot FILE]
//	  [-samples-out DIR] [-site-out DIR] [-state DIR] [-now RFC3339]
//	  [-base PATH] [-brand DIR]
//	sakuna run [-home DIR] [-state DIR] [-cache DIR] [-out DIR]
//	  [-fb-env FILE] [-kill-switch FILE] [-live] [-dry-run] [-fixtures DIR]
//	  [-now RFC3339] [-site-out DIR] [-base PATH] [-brand DIR]
//
// Dry-run only (fetch/cards/decide/all): decide prints JSON and never posts.
// `run` posts via go/internal/fbpost but DEFAULTS to dry-run (outbox files);
// only -live with a usable fb.env and no kill switch goes live. No network
// posting of any kind happens without -live.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/card"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/geo"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/phivolcs"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/posttext"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/rules"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/site"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/snapshot"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "fetch":
		code = cmdFetch(os.Args[2:])
	case "cards":
		code = cmdCards(os.Args[2:])
	case "decide":
		code = cmdDecide(os.Args[2:])
	case "site":
		code = cmdSite(os.Args[2:])
	case "all":
		code = cmdAll(os.Args[2:])
	case "run":
		code = cmdRun(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "sakuna: unknown subcommand %q\n", os.Args[1])
		usage()
		code = 2
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: sakuna <fetch|cards|decide|site|all|run> [flags]")
}

func manila() *time.Location {
	loc, err := time.LoadLocation("Asia/Manila")
	if err != nil {
		return time.FixedZone("PHT", 8*3600)
	}
	return loc
}

func phtLabel(t time.Time) string {
	return t.In(manila()).Format("2 Jan 2006, 3:04 PM") + " PHT"
}

func parseNow(s string) (time.Time, error) {
	if s == "" {
		return time.Now(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Also accept "2006-01-02 15:04:05 +0800" etc.
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.In(manila()), nil
	}
	return time.Time{}, fmt.Errorf("bad -now %q (want RFC3339)", s)
}

func loadSnapshot(path string) (*snapshot.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap snapshot.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// ---------------------------------------------------------------------------
// fetch
// ---------------------------------------------------------------------------

func cmdFetch(argv []string) int {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	fixtures := fs.String("fixtures", "", "fixtures directory (fixture mode, no network)")
	cache := fs.String("cache", "../cache", "disk cache directory")
	out := fs.String("out", "../out/snapshot.json", "output snapshot JSON path")
	nowS := fs.String("now", "", "reference time RFC3339 (default: now)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	now, err := parseNow(*nowS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sakuna fetch:", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	c := fetch.New(*cache, *fixtures)
	snap, err := snapshot.Build(ctx, c, snapshot.Options{FixturesDir: *fixtures, Now: now})
	if err != nil {
		fmt.Fprintln(os.Stderr, "sakuna fetch:", err)
		return 1
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sakuna fetch:", err)
		return 1
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "sakuna fetch:", err)
		return 1
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "sakuna fetch:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "fetch: wrote %s (today=%s quakes=%d via %s active=%d errors=%d)\n",
		*out, todayDate(snap), len(snap.Quakes), snap.QuakesSource, len(snap.Pagasa.Active), len(snap.Errors))
	return 0
}

func todayDate(snap *snapshot.Snapshot) string {
	if snap.Today != nil {
		return snap.Today.Date
	}
	return "-"
}

// ---------------------------------------------------------------------------
// cards
// ---------------------------------------------------------------------------

// ManifestEntry is one rendered card in manifest.json.
type ManifestEntry struct {
	PNG      string `json:"png"`
	TXT      string `json:"txt"`
	Card     string `json:"card"`
	Live     bool   `json:"live"`
	Sample   bool   `json:"sample"`
	SourceID string `json:"source_id,omitempty"`
	Note     string `json:"note,omitempty"`
}

// Manifest lists all rendered cards.
type Manifest struct {
	GeneratedAt string          `json:"generated_at"`
	Files       []ManifestEntry `json:"files"`
}

func cmdCards(argv []string) int {
	fs := flag.NewFlagSet("cards", flag.ContinueOnError)
	snapPath := fs.String("snapshot", "../out/snapshot.json", "input snapshot JSON")
	out := fs.String("out", "../samples", "output samples directory")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	snap, err := loadSnapshot(*snapPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sakuna cards:", err)
		return 1
	}
	if err := renderCards(snap, *out); err != nil {
		fmt.Fprintln(os.Stderr, "sakuna cards:", err)
		return 1
	}
	return 0
}

func dateCompact(ymd string) string {
	return strings.ReplaceAll(ymd, "-", "")
}

func renderCards(snap *snapshot.Snapshot, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	f := fonts.LoadEmbedded()
	b, err := geo.Load()
	if err != nil {
		return err
	}
	now := snap.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	timeLabel := phtLabel(now)
	var mf Manifest
	mf.GeneratedAt = now.Format(time.RFC3339)

	// ---- Daily (always live) ----
	if snap.Today != nil {
		day := snap.Today
		dateLabel := day.Date
		if t, err := time.Parse("2006-01-02", day.Date); err == nil {
			dateLabel = t.In(manila()).Format("2 Jan 2006")
		}
		taalLvl, taalTxt := 0, ""
		if snap.Taal != nil {
			taalLvl = snap.Taal.Taal.Level
			taalTxt = snap.Taal.Taal.LevelText
		}
		alerts := dailyAlertLines(snap)
		_, _, windowMax := card.DailyDisplay(*day, now)
		in := card.DailyInput{
			Day:       *day,
			DateLabel: dateLabel,
			TimeLabel: timeLabel,
			TaalLevel: taalLvl,
			TaalText:  taalTxt,
			Alerts:    alerts,
			Now:       now,
		}
		img, _, err := card.RenderDaily(f, in)
		if err != nil {
			return fmt.Errorf("daily: %w", err)
		}
		pngName := "daily-" + dateCompact(day.Date) + ".png"
		if err := writePNGFile(outDir, pngName, img); err != nil {
			return err
		}
		txt := posttext.Daily(posttext.DailyData{
			DateLabel: dateLabel, TMax: day.TMax, TMin: day.TMin,
			ApparentMax: day.ApparentMax, PrecipProbMax: windowMax,
			PrecipSum: day.PrecipSum, UVMax: day.UVMax, UVCategory: day.UVCategory,
			AQI: day.AQI, AQICategory: day.AQICategory,
			TaalLevel: taalLvl, TaalText: taalTxt, Alerts: alerts,
		})
		txtName := strings.TrimSuffix(pngName, ".png") + ".txt"
		if err := os.WriteFile(filepath.Join(outDir, txtName), []byte(txt+"\n"), 0o644); err != nil {
			return err
		}
		mf.Files = append(mf.Files, ManifestEntry{PNG: pngName, TXT: txtName, Card: "daily", Live: true, SourceID: "daily:" + day.Date})
	}

	// ---- Signal ----
	if snap.Pagasa.WatchMax > 0 || len(snap.Pagasa.ProvinceSignals) > 0 {
		issued := snap.Pagasa.CycloneIssued
		issuedLabel := timeLabel
		if t, err := time.Parse(time.RFC3339, issued); err == nil {
			issuedLabel = phtLabel(t)
		}
		in := card.SignalInput{
			Cyclone:      snap.Pagasa.CycloneName,
			IssuedLabel:  issuedLabel,
			Levels:       snap.Pagasa.ProvinceSignals,
			WatchSummary: snap.Pagasa.WatchSummary,
		}
		img, _, err := card.RenderSignal(f, b, in)
		if err != nil {
			return fmt.Errorf("signal: %w", err)
		}
		date := "unknown"
		if snap.Today != nil {
			date = dateCompact(snap.Today.Date)
		} else {
			date = now.In(manila()).Format("20060102")
		}
		pngName := "signal-" + date + ".png"
		if err := writePNGFile(outDir, pngName, img); err != nil {
			return err
		}
		txt := posttext.Signal(posttext.SignalData{
			Cyclone: in.Cyclone, IssuedLabel: issuedLabel,
			Levels: in.Levels, WatchSummary: in.WatchSummary,
		})
		txtName := strings.TrimSuffix(pngName, ".png") + ".txt"
		if err := os.WriteFile(filepath.Join(outDir, txtName), []byte(txt+"\n"), 0o644); err != nil {
			return err
		}
		mf.Files = append(mf.Files, ManifestEntry{PNG: pngName, TXT: txtName, Card: "signal", Live: true, SourceID: snap.Pagasa.CycloneName})
	} else {
		// Synthetic signal SAMPLE (no live wind signal).
		in := card.SignalInput{
			Cyclone:     "Bagyong SAMPLE (sample only)",
			IssuedLabel: timeLabel,
			Levels: map[string]int{
				"Batanes": 3, "Cagayan": 2, "Ilocos Norte": 2,
				"Metro Manila": 2, "Cavite": 2, "Laguna": 1,
				"Batangas": 3, "Rizal": 1, "Quezon": 2,
			},
			WatchSummary: "NCR: Signal #2",
			Sample:       true,
		}
		img, _, err := card.RenderSignal(f, b, in)
		if err != nil {
			return fmt.Errorf("signal-synth: %w", err)
		}
		pngName := "signal-synth.png"
		if err := writePNGFile(outDir, pngName, img); err != nil {
			return err
		}
		txt := posttext.Signal(posttext.SignalData{
			Cyclone: in.Cyclone, IssuedLabel: in.IssuedLabel,
			Levels: in.Levels, WatchSummary: in.WatchSummary, Sample: true,
		})
		txtName := strings.TrimSuffix(pngName, ".png") + ".txt"
		if err := os.WriteFile(filepath.Join(outDir, txtName), []byte(txt+"\n"), 0o644); err != nil {
			return err
		}
		mf.Files = append(mf.Files, ManifestEntry{PNG: pngName, TXT: txtName, Card: "signal", Sample: true, SourceID: "synth-signal", Note: "no active wind signal; synthetic SAMPLE"})
	}

	// ---- Quakes ----
	var notables []snapshot.Quake
	for _, q := range snap.Quakes {
		if q.Notable {
			notables = append(notables, q)
		}
	}
	sort.Slice(notables, func(i, j int) bool { return notables[i].Time.After(notables[j].Time) })
	if len(notables) > 0 {
		n := len(notables)
		if n > 5 {
			n = 5
		}
		for _, q := range notables[:n] {
			tl := phtLabel(q.Time)
			img, _, err := card.RenderQuake(f, b, card.QuakeInput{Event: q.Event, TimeLabel: tl})
			if err != nil {
				return fmt.Errorf("quake %s: %w", q.ID, err)
			}
			pngName := "quake-" + sanitizeID(q.ID) + ".png"
			if err := writePNGFile(outDir, pngName, img); err != nil {
				return err
			}
			txt := posttext.Quake(posttext.QuakeData{
				Mag: q.Mag, DepthKm: q.DepthKm, Place: q.Place,
				DistanceKm: q.DistanceKm, Compass: q.Compass,
				TimeLabel: tl, Source: q.Source,
			})
			txtName := strings.TrimSuffix(pngName, ".png") + ".txt"
			if err := os.WriteFile(filepath.Join(outDir, txtName), []byte(txt+"\n"), 0o644); err != nil {
				return err
			}
			mf.Files = append(mf.Files, ManifestEntry{PNG: pngName, TXT: txtName, Card: "quake", Live: true, SourceID: q.ID, Note: q.NotableReason})
		}
	} else {
		// No notable quake: most relevant real recent event as LIVE with
		// fallback label, PLUS a synthetic near-quake SAMPLE.
		if rel := mostRelevant(snap, now); rel != nil {
			tl := phtLabel(rel.Time)
			img, _, err := card.RenderQuake(f, b, card.QuakeInput{Event: rel.Event, TimeLabel: tl})
			if err != nil {
				return fmt.Errorf("quake fallback: %w", err)
			}
			pngName := "quake-" + sanitizeID(rel.ID) + ".png"
			if err := writePNGFile(outDir, pngName, img); err != nil {
				return err
			}
			txt := posttext.Quake(posttext.QuakeData{
				Mag: rel.Mag, DepthKm: rel.DepthKm, Place: rel.Place,
				DistanceKm: rel.DistanceKm, Compass: rel.Compass,
				TimeLabel: tl, Source: rel.Source, FallbackNote: true,
			})
			txtName := strings.TrimSuffix(pngName, ".png") + ".txt"
			if err := os.WriteFile(filepath.Join(outDir, txtName), []byte(txt+"\n"), 0o644); err != nil {
				return err
			}
			mf.Files = append(mf.Files, ManifestEntry{PNG: pngName, TXT: txtName, Card: "quake", Live: true, SourceID: rel.ID, Note: "latest notable: none; showing most relevant recent event"})
		}
		// Synthetic near-quake SAMPLE.
		nt := now.In(manila())
		near := phivolcs.Event{
			ID: "synth-nasugbu-m56", CanonicalID: "synth-nasugbu-m56",
			Time: nt, Lat: 13.88, Lon: 120.44, DepthKm: 10, Mag: 5.6,
			Place: "30 km SW of Nasugbu, Batangas", Source: "PHIVOLCS",
		}
		near.DistanceKm = phivolcs.DistanceToLasPinas(near.Lat, near.Lon)
		near.BearingDeg = phivolcs.BearingDeg(phivolcs.LasPinasLat, phivolcs.LasPinasLon, near.Lat, near.Lon)
		near.Compass = phivolcs.Compass16(near.BearingDeg)
		tl := phtLabel(nt)
		img, _, err := card.RenderQuake(f, b, card.QuakeInput{Event: near, TimeLabel: tl, Sample: true})
		if err != nil {
			return fmt.Errorf("quake synth: %w", err)
		}
		pngName := "quake-synth-near.png"
		if err := writePNGFile(outDir, pngName, img); err != nil {
			return err
		}
		txt := posttext.Quake(posttext.QuakeData{
			Mag: near.Mag, DepthKm: near.DepthKm, Place: near.Place,
			DistanceKm: near.DistanceKm, Compass: near.Compass,
			TimeLabel: tl, Source: near.Source, Sample: true,
		})
		txtName := strings.TrimSuffix(pngName, ".png") + ".txt"
		if err := os.WriteFile(filepath.Join(outDir, txtName), []byte(txt+"\n"), 0o644); err != nil {
			return err
		}
		mf.Files = append(mf.Files, ManifestEntry{PNG: pngName, TXT: txtName, Card: "quake", Sample: true, SourceID: near.ID, Note: "synthetic near-quake SAMPLE"})
	}

	// ---- Taal (always live) ----
	if snap.Taal != nil {
		bdate := ""
		if t, err := parseBulletinDate(snap.Taal.Taal.BulletinTitle); err == nil {
			bdate = t.Format("2 Jan 2006")
		} else {
			bdate = now.In(manila()).Format("2 Jan 2006")
		}
		in := card.TaalInput{
			Level: snap.Taal.Taal.Level, LevelText: snap.Taal.Taal.LevelText,
			BulletinTitle: snap.Taal.Taal.BulletinTitle, BulletinDate: bdate,
		}
		img, _, err := card.RenderTaal(f, in)
		if err != nil {
			return fmt.Errorf("taal: %w", err)
		}
		pngName := "taal-" + snap.Taal.Taal.BulletinID + ".png"
		if snap.Taal.Taal.BulletinID == "" {
			date := now.In(manila()).Format("20060102")
			if snap.Today != nil {
				date = dateCompact(snap.Today.Date)
			}
			pngName = "taal-" + date + ".png"
		}
		if err := writePNGFile(outDir, pngName, img); err != nil {
			return err
		}
		txt := posttext.Taal(posttext.TaalData{
			Level: in.Level, LevelText: in.LevelText,
			BulletinTitle: in.BulletinTitle, BulletinDate: bdate,
		})
		txtName := strings.TrimSuffix(pngName, ".png") + ".txt"
		if err := os.WriteFile(filepath.Join(outDir, txtName), []byte(txt+"\n"), 0o644); err != nil {
			return err
		}
		mf.Files = append(mf.Files, ManifestEntry{PNG: pngName, TXT: txtName, Card: "taal", Live: true, SourceID: snap.Taal.Taal.BulletinID})
	}

	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "cards: wrote %d cards to %s\n", len(mf.Files), outDir)
	return nil
}

func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	s := strings.Trim(b.String(), "-_")
	if s == "" {
		return "event"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// dailyAlertLines builds the daily card alerts strip from ACTIVE items only.
func dailyAlertLines(snap *snapshot.Snapshot) []string {
	var lines []string
	// Wind signals affecting the watch area first.
	if snap.Pagasa.WatchMax > 0 {
		// Summarize top provinces.
		type kv struct {
			k string
			v int
		}
		var kvs []kv
		for k, v := range snap.Pagasa.ProvinceSignals {
			kvs = append(kvs, kv{k, v})
		}
		sort.Slice(kvs, func(i, j int) bool {
			if kvs[i].v != kvs[j].v {
				return kvs[i].v > kvs[j].v
			}
			return kvs[i].k < kvs[j].k
		})
		n := 3
		if len(kvs) < n {
			n = len(kvs)
		}
		var parts []string
		for _, p := range kvs[:n] {
			parts = append(parts, fmt.Sprintf("%s (#%d)", p.k, p.v))
		}
		lines = append(lines, fmt.Sprintf("Signal #%d: %s", snap.Pagasa.WatchMax, strings.Join(parts, ", ")))
	}
	// NCR rainfall.
	if snap.Pagasa.RainNCR == "ORANGE" || snap.Pagasa.RainNCR == "RED" {
		color := strings.Title(strings.ToLower(snap.Pagasa.RainNCR))
		lines = append(lines, fmt.Sprintf("%s rainfall warning in effect for Metro Manila", color))
	}
	// Latest notable quake.
	for _, q := range snap.Quakes {
		if q.Notable {
			lines = append(lines, fmt.Sprintf("Latest quake: M%.1f, %s (%.0f km %s)", q.Mag, q.Place, q.DistanceKm, q.Compass))
			break
		}
	}
	if len(lines) == 0 {
		return []string{"No active PAGASA warnings for NCR area"}
	}
	if len(lines) > 2 {
		lines = lines[:2]
	}
	return lines
}

// mostRelevant picks the most relevant real recent event when nothing is
// notable: largest M within 300 km in the last 7 days, else latest M>=4 in PH.
func mostRelevant(snap *snapshot.Snapshot, now time.Time) *snapshot.Quake {
	var within []*snapshot.Quake
	for i := range snap.Quakes {
		q := &snap.Quakes[i]
		if now.Sub(q.Time) <= 7*24*time.Hour && q.DistanceKm <= 300 {
			within = append(within, q)
		}
	}
	if len(within) > 0 {
		best := within[0]
		for _, q := range within[1:] {
			if q.Mag > best.Mag {
				best = q
			}
		}
		return best
	}
	var cand []*snapshot.Quake
	for i := range snap.Quakes {
		q := &snap.Quakes[i]
		if q.Mag >= 4.0 && phivolcs.InPHBounds(q.Lat, q.Lon) {
			cand = append(cand, q)
		}
	}
	if len(cand) == 0 {
		// Fall back to the latest real event so the card always has data.
		if len(snap.Quakes) == 0 {
			return nil
		}
		best := &snap.Quakes[0]
		for i := range snap.Quakes {
			if snap.Quakes[i].Time.After(best.Time) {
				best = &snap.Quakes[i]
			}
		}
		return best
	}
	best := cand[0]
	for _, q := range cand[1:] {
		if q.Time.After(best.Time) {
			best = q
		}
	}
	return best
}

func parseBulletinDate(title string) (time.Time, error) {
	// "Taal Volcano Summary of 24Hr Observation 25 October 2026 12:00 AM"
	loc := manila()
	for _, layout := range []string{"2 January 2006 03:04 PM", "2 January 2006 15:04", "2 January 2006"} {
		for i := len(title); i > 0; i-- {
			// try suffixes
			_ = i
			break
		}
		_ = layout
	}
	// Find a "<day> <Month> <year>" triple.
	fields := strings.Fields(title)
	for i := 0; i+2 < len(fields); i++ {
		triple := strings.Join(fields[i:i+3], " ")
		if t, err := time.ParseInLocation("2 January 2006", triple, loc); err == nil {
			// Optional time suffix.
			if i+5 < len(fields) {
				if tt, err := time.ParseInLocation("2 January 2006 03:04 PM",
					strings.Join(fields[i:i+5], " "), loc); err == nil {
					return tt, nil
				}
			}
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("no date in %q", title)
}

func writePNGFile(dir, name string, img image.Image) error {
	fh, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer fh.Close()
	return png.Encode(fh, img)
}

// ---------------------------------------------------------------------------
// decide
// ---------------------------------------------------------------------------

type fileState struct {
	LastSignal    int    `json:"last_signal"`
	HasSignal     bool   `json:"has_signal"`
	LastTaal      int    `json:"last_taal"`
	HasTaal       bool   `json:"has_taal"`
	LastRainColor string `json:"last_rain_color"`
	HasRain       bool   `json:"has_rain"`
}

func cmdDecide(argv []string) int {
	fs := flag.NewFlagSet("decide", flag.ContinueOnError)
	snapPath := fs.String("snapshot", "../out/snapshot.json", "input snapshot JSON")
	stateDir := fs.String("state", "", "state directory (state.json + history.json + KILL_SWITCH)")
	nowS := fs.String("now", "", "reference time RFC3339 (default: now)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	now, err := parseNow(*nowS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sakuna decide:", err)
		return 2
	}
	dec, err := runDecide(*snapPath, *stateDir, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sakuna decide:", err)
		return 1
	}
	data, _ := json.MarshalIndent(dec, "", "  ")
	data = append(data, '\n')
	os.Stdout.Write(data)
	return 0
}

func runDecide(snapPath, stateDir string, now time.Time) (*rules.Decision, error) {
	snap, err := loadSnapshot(snapPath)
	if err != nil {
		return nil, err
	}
	var st rules.State
	var hist []rules.Posted
	if stateDir != "" {
		if b, err := os.ReadFile(filepath.Join(stateDir, "state.json")); err == nil {
			var fs fileState
			if jerr := json.Unmarshal(b, &fs); jerr == nil {
				st = rules.State{LastSignal: fs.LastSignal, HasSignal: fs.HasSignal, LastTaal: fs.LastTaal, HasTaal: fs.HasTaal, LastRainColor: fs.LastRainColor, HasRain: fs.HasRain}
			}
		}
		if b, err := os.ReadFile(filepath.Join(stateDir, "history.json")); err == nil {
			_ = json.Unmarshal(b, &hist)
		}
	}
	events := candidatesFromSnapshot(snap, now, st, hist)
	dec := rules.Decide(now, events, hist, st)
	return &dec, nil
}

func rainRank(color string) int {
	switch color {
	case "RED":
		return 3
	case "ORANGE":
		return 2
	case "YELLOW":
		return 1
	default:
		return 0
	}
}

func candidatesFromSnapshot(snap *snapshot.Snapshot, now time.Time, st rules.State, hist []rules.Posted) []rules.Event {
	loc := manila()
	phtDate := now.In(loc).Format("2006-01-02")
	failed := func(key string) bool {
		if snap.Errors == nil {
			return false
		}
		_, bad := snap.Errors[key]
		return bad
	}
	// PHASE_D §3: a failed source emits NO events — a failed fetch is never
	// treated as "signal lifted" / "level changed". The previous state is
	// kept for that source. When PAGASA is partial (some fresh CAPs failed
	// after retry but usable alerts remain), only raises/new alerts are
	// emitted; lowering/lifting is suppressed here and in the state update.
	partial := snap.Pagasa.Partial
	pagasaOK := !failed("pagasa_feed") && (!failed("pagasa_caps") || partial)
	quakesOK := !failed("quakes")
	taalOK := !failed("taal") && snap.Taal != nil
	openmeteoOK := !failed("openmeteo") && snap.Today != nil
	var out []rules.Event
	// Daily.
	if openmeteoOK {
		out = append(out, rules.Event{
			Kind: rules.KindDaily, SourceID: "daily:" + phtDate,
			DedupeKey: "daily:" + phtDate,
		})
	}
	// Signal (always emitted so lifting to 0 is detected; suppressed to
	// raises only when partial).
	if pagasaOK {
		emitSignal := true
		if partial {
			if !st.HasSignal {
				emitSignal = false
			} else if snap.Pagasa.WatchMax <= st.LastSignal {
				emitSignal = false
			}
		}
		if emitSignal {
			sigID := "signal-none"
			for _, a := range snap.Pagasa.Active {
				if a.Signal() > 0 {
					sigID = a.Identifier
					break
				}
			}
			out = append(out, rules.Event{
				Kind: rules.KindSignal, SourceID: sigID,
				DedupeKey: "signal:" + sigID, SignalLevel: snap.Pagasa.WatchMax,
			})
		}
	}
	// Rain (NCR orange/red; raises/new only when partial).
	if pagasaOK && (snap.Pagasa.RainNCR == "ORANGE" || snap.Pagasa.RainNCR == "RED") {
		emitRain := true
		if partial {
			if !st.HasRain {
				emitRain = false
			} else if rainRank(snap.Pagasa.RainNCR) > rainRank(st.LastRainColor) {
				emitRain = true
			} else {
				// Same or lower color: only a never-seen CAP id counts as new.
				capProbe := ""
				for _, r := range snap.Pagasa.Rainfall {
					if r.InNCR && r.Color == snap.Pagasa.RainNCR {
						capProbe = r.CapID
						break
					}
				}
				seen := false
				for _, h := range hist {
					if h.Kind == rules.KindRain && h.SourceID == capProbe {
						seen = true
						break
					}
				}
				emitRain = capProbe != "" && !seen
			}
		}
		if emitRain {
			capID := "rain-ncr"
			for _, r := range snap.Pagasa.Rainfall {
				if r.InNCR && r.Color == snap.Pagasa.RainNCR {
					capID = r.CapID
					break
				}
			}
			out = append(out, rules.Event{
				Kind: rules.KindRain, SourceID: capID,
				DedupeKey: "rain:" + capID + ":" + snap.Pagasa.RainNCR,
				RainColor: snap.Pagasa.RainNCR,
			})
		}
	}
	// Quakes (notable only).
	if quakesOK {
		for _, q := range snap.Quakes {
			if !q.Notable {
				continue
			}
			maxI := 0
			for _, in := range q.Intensities {
				if intensityWatch(in.Place) && in.Level > maxI {
					maxI = in.Level
				}
			}
			out = append(out, rules.Event{
				Kind: rules.KindQuake, SourceID: q.ID,
				DedupeKey: "quake:" + q.ID,
				Magnitude: q.Mag, DistanceKm: q.DistanceKm,
				Lat: q.Lat, Lon: q.Lon, QuakeTime: q.Time,
				QuakeCanon: q.CanonicalID, IntensityMax: maxI,
			})
		}
	}
	// Taal.
	if taalOK {
		bid := snap.Taal.Taal.BulletinID
		if bid == "" {
			bid = fmt.Sprintf("level-%d", snap.Taal.Taal.Level)
		}
		out = append(out, rules.Event{
			Kind: rules.KindTaal, SourceID: bid,
			DedupeKey: "taal:" + bid, TaalLevel: snap.Taal.Taal.Level,
		})
	}
	// Tsunami (new alerts only when partial; Decide dedupes the rest).
	if pagasaOK {
		seenTsunami := map[string]bool{}
		if partial {
			for _, h := range hist {
				if h.Kind == rules.KindTsunami {
					seenTsunami[h.DedupeKey] = true
					seenTsunami[string(h.Kind)+":"+h.SourceID] = true
				}
			}
		}
		for _, a := range snap.Pagasa.Tsunami {
			dk := "tsunami:" + a.Identifier
			if partial && seenTsunami[dk] {
				continue
			}
			out = append(out, rules.Event{
				Kind: rules.KindTsunami, SourceID: a.Identifier,
				DedupeKey: dk,
			})
		}
	}
	return out
}

func intensityWatch(place string) bool {
	n := strings.ToLower(place)
	if strings.Contains(n, "metro manila") || strings.Contains(n, "ncr") ||
		strings.Contains(n, "cavite") || strings.Contains(n, "laguna") ||
		strings.Contains(n, "batangas") || strings.Contains(n, "rizal") ||
		strings.Contains(n, "manila") || strings.Contains(n, "quezon city") ||
		strings.Contains(n, "makati") || strings.Contains(n, "pasig") {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// site
// ---------------------------------------------------------------------------

func cmdSite(argv []string) int {
	fs := flag.NewFlagSet("site", flag.ContinueOnError)
	snapPath := fs.String("snapshot", "../out/snapshot.json", "input snapshot JSON")
	cards := fs.String("cards", "../samples", "cards directory (PNGs)")
	out := fs.String("out", "../out/site", "output site directory")
	base := fs.String("base", "/sakuna-bantay/", "site base path")
	brand := fs.String("brand", "../brand", "brand directory")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	snap, err := loadSnapshot(*snapPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sakuna site:", err)
		return 1
	}
	if err := site.Generate(site.Options{
		Snap: snap, CardsDir: *cards, BrandDir: *brand, OutDir: *out, BasePath: *base,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "sakuna site:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "site: wrote %s\n", *out)
	return 0
}

// ---------------------------------------------------------------------------
// all
// ---------------------------------------------------------------------------

func cmdAll(argv []string) int {
	fs := flag.NewFlagSet("all", flag.ContinueOnError)
	fixtures := fs.String("fixtures", "", "fixtures directory (fixture mode)")
	cache := fs.String("cache", "../cache", "disk cache directory")
	out := fs.String("out", "../out", "base output directory")
	snapPath := fs.String("snapshot", "", "snapshot path override (default <out>/snapshot.json)")
	samplesOut := fs.String("samples-out", "", "samples dir override (default <out>/samples)")
	siteOut := fs.String("site-out", "", "site dir override (default <out>/site)")
	stateDir := fs.String("state", "", "state directory")
	nowS := fs.String("now", "", "reference time RFC3339 (default: now)")
	base := fs.String("base", "/sakuna-bantay/", "site base path")
	brand := fs.String("brand", "../brand", "brand directory")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	now, err := parseNow(*nowS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sakuna all:", err)
		return 2
	}
	snapFile := *snapPath
	if snapFile == "" {
		snapFile = filepath.Join(*out, "snapshot.json")
	}
	samplesDir := *samplesOut
	if samplesDir == "" {
		samplesDir = filepath.Join(*out, "samples")
	}
	siteDir := *siteOut
	if siteDir == "" {
		siteDir = filepath.Join(*out, "site")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	c := fetch.New(*cache, *fixtures)
	snap, err := snapshot.Build(ctx, c, snapshot.Options{FixturesDir: *fixtures, Now: now})
	if err != nil {
		fmt.Fprintln(os.Stderr, "sakuna all (fetch):", err)
		return 1
	}
	data, _ := json.MarshalIndent(snap, "", "  ")
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(snapFile), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "sakuna all:", err)
		return 1
	}
	if err := os.WriteFile(snapFile, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "sakuna all:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "fetch: wrote %s\n", snapFile)

	if err := renderCards(snap, samplesDir); err != nil {
		fmt.Fprintln(os.Stderr, "sakuna all (cards):", err)
		return 1
	}
	// decide (dry-run, print JSON).
	var st rules.State
	var hist []rules.Posted
	if *stateDir != "" {
		if b, err := os.ReadFile(filepath.Join(*stateDir, "state.json")); err == nil {
			var fs fileState
			if jerr := json.Unmarshal(b, &fs); jerr == nil {
				st = rules.State{LastSignal: fs.LastSignal, HasSignal: fs.HasSignal, LastTaal: fs.LastTaal, HasTaal: fs.HasTaal, LastRainColor: fs.LastRainColor, HasRain: fs.HasRain}
			}
		}
		if b, err := os.ReadFile(filepath.Join(*stateDir, "history.json")); err == nil {
			_ = json.Unmarshal(b, &hist)
		}
	}
	dec := rules.Decide(now, candidatesFromSnapshot(snap, now, st, hist), hist, st)
	decData, _ := json.MarshalIndent(dec, "", "  ")
	decData = append(decData, '\n')
	os.Stdout.Write(decData)
	_ = os.WriteFile(filepath.Join(*out, "decide.json"), decData, 0o644)

	if err := site.Generate(site.Options{
		Snap: snap, CardsDir: samplesDir, BrandDir: *brand, OutDir: siteDir, BasePath: *base,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "sakuna all (site):", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "site: wrote %s\n", siteDir)
	return 0
}
