package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/card"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fbpost"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/geo"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/posttext"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/rules"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/site"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/snapshot"
)

// Per-source min-refetch intervals for `run` (PHASE_D §3): earthquakes
// every 2–3 min, everything else every 10–15 min. CAP bodies stay
// effectively-forever cached (pagasa.FetchCAPs, immutable).
const (
	runQuakesRefetch = 150 * time.Second // 2.5 min: PHIVOLCS (+USGS backup)
	runPagasaRefetch = 10 * time.Minute  // PAGASA feed
	runMeteoRefetch  = 15 * time.Minute  // Open-Meteo
	runTaalRefetch   = 15 * time.Minute  // WOVODAT
	runSiteMinAge    = 15 * time.Minute  // site rebuild throttle
)

// RunOptions is one `sakuna run` pass. Paths are resolved (no defaults
// here); HTTPClient/GraphBaseURL/Sleep/Stdout/Stderr are injectable for
// tests (nil = production defaults).
type RunOptions struct {
	StateDir, CacheDir, OutDir, SiteOutDir string
	BrandDir, BasePath                     string
	FbEnvPath, KillSwitchPath              string
	Live, DryRunFlag                       bool
	FixturesDir                            string
	Now                                    time.Time
	HTTPClient                             *http.Client
	GraphBaseURL                           string
	Sleep                                  func(time.Duration)
	Stdout, Stderr                         io.Writer
}

func (o *RunOptions) stdout() io.Writer {
	if o.Stdout != nil {
		return o.Stdout
	}
	return os.Stdout
}

func (o *RunOptions) stderr() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

func cmdRun(argv []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	home := fs.String("home", "", "home directory (default $HOME)")
	stateDir := fs.String("state", "", "state directory (default $HOME/state)")
	cacheDir := fs.String("cache", "", "disk cache directory (default $HOME/cache)")
	outDir := fs.String("out", "", "output directory (default $HOME/out)")
	fbEnv := fs.String("fb-env", "", "fb.env path (default $HOME/.config/sakuna/fb.env)")
	killSwitch := fs.String("kill-switch", "", "kill-switch path (default $HOME/.config/sakuna/KILL_SWITCH)")
	live := fs.Bool("live", false, "opt-in: actually post to Facebook (default is dry-run)")
	dryRun := fs.Bool("dry-run", false, "force dry-run (wins over -live)")
	fixtures := fs.String("fixtures", "", "fixtures directory (fixture mode, no network)")
	nowS := fs.String("now", "", "reference time RFC3339 (default: now)")
	siteOut := fs.String("site-out", "", "site output directory (empty = skip site rebuild)")
	base := fs.String("base", "/sakuna-bantay/", "site base path")
	brand := fs.String("brand", "../brand", "brand directory")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	h := *home
	realHome, _ := os.UserHomeDir()
	if h == "" {
		h = realHome
	}
	def := func(v, sub string) string {
		if v != "" {
			return v
		}
		return filepath.Join(h, sub)
	}
	now, err := parseNow(*nowS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sakuna run:", err)
		return 2
	}
	fbEnvP, killP := resolveRunConfigPaths(*fbEnv, *killSwitch)
	return runOnce(RunOptions{
		StateDir: def(*stateDir, "state"), CacheDir: def(*cacheDir, "cache"),
		OutDir: def(*outDir, "out"), SiteOutDir: *siteOut,
		BrandDir: *brand, BasePath: *base,
		FbEnvPath: fbEnvP, KillSwitchPath: killP,
		Live: *live, DryRunFlag: *dryRun,
		FixturesDir: *fixtures, Now: now,
	})
}

// resolveRunConfigPaths resolves the fb.env and KILL_SWITCH paths. Explicit
// -fb-env / -kill-switch flags always win and are never derived from -home.
// When the flags are absent the defaults come from the REAL user home
// (os.UserHomeDir → $HOME/.config/sakuna), never from -home, so a -home
// pointing at /home/example/sakuna still reads config from
// /home/example/.config/sakuna.
func resolveRunConfigPaths(fbEnvFlag, killFlag string) (fbEnvP, killP string) {
	if fbEnvFlag != "" {
		fbEnvP = fbEnvFlag
	} else {
		realHome, _ := os.UserHomeDir()
		fbEnvP = fbpost.DefaultEnvPath(realHome)
	}
	if killFlag != "" {
		killP = killFlag
	} else {
		realHome, _ := os.UserHomeDir()
		killP = fbpost.DefaultKillSwitchPath(realHome)
	}
	return fbEnvP, killP
}

// historyName/stateName separate live and dry-run records so switching to
// live never inherits dry-run dedupe keys (PHASE_D §2).
func historyName(mode string) string {
	if mode == "live" {
		return "history.json"
	}
	return "history-dryrun.json"
}

func stateName(mode string) string {
	if mode == "live" {
		return "state.json"
	}
	return "state-dryrun.json"
}

func loadHistory(path string) []rules.Posted {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var h []rules.Posted
	if err := json.Unmarshal(b, &h); err != nil {
		return nil
	}
	return h
}

func saveHistory(path string, h []rules.Posted) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func loadRunState(path string) rules.State {
	b, err := os.ReadFile(path)
	if err != nil {
		return rules.State{}
	}
	var fs fileState
	if err := json.Unmarshal(b, &fs); err != nil {
		return rules.State{}
	}
	return rules.State{LastSignal: fs.LastSignal, HasSignal: fs.HasSignal, LastTaal: fs.LastTaal, HasTaal: fs.HasTaal, LastRainColor: fs.LastRainColor, HasRain: fs.HasRain}
}

func saveRunState(path string, st rules.State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	fs := fileState{LastSignal: st.LastSignal, HasSignal: st.HasSignal, LastTaal: st.LastTaal, HasTaal: st.HasTaal, LastRainColor: st.LastRainColor, HasRain: st.HasRain}
	data, err := json.MarshalIndent(fs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// tryLock takes a non-blocking flock on path. held=true means another pass
// is running.
func tryLock(path string) (fh *os.File, held bool, err error) {
	fh, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(fh.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fh.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return fh, false, nil
}

// siteFresh reports whether the site was rebuilt less than runSiteMinAge
// ago (marker written with the pass's own clock, so -now stays coherent).
func siteFresh(stateDir string, now time.Time) bool {
	b, err := os.ReadFile(filepath.Join(stateDir, "site-last-build"))
	if err != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	if err != nil {
		return false
	}
	d := now.Sub(t)
	return d >= 0 && d < runSiteMinAge
}

func markSiteBuilt(stateDir string, now time.Time) {
	_ = os.WriteFile(filepath.Join(stateDir, "site-last-build"),
		[]byte(now.Format(time.RFC3339)+"\n"), 0o644)
}

// touchAlertDirty creates $state/site-alert-dirty so the site publisher
// takes the fast path (alert change → push within minutes, throttled).
func touchAlertDirty(stateDir string, now time.Time) {
	_ = os.WriteFile(filepath.Join(stateDir, "site-alert-dirty"),
		[]byte(now.Format(time.RFC3339)+"\n"), 0o644)
}

// watchAlertIDs returns the sorted identifiers of the currently active
// watch-area alerts (used to detect set changes across passes).
func watchAlertIDs(snap *snapshot.Snapshot) []string {
	if snap == nil {
		return nil
	}
	var ids []string
	for _, a := range snap.Pagasa.Active {
		if a.InWatchArea() {
			ids = append(ids, a.Identifier)
		}
	}
	sort.Strings(ids)
	return ids
}

// updateWatchAlerts records the current watch-area alert set in
// $state/watch-alerts.json. It reports whether the set changed since the
// last pass (missing file counts as empty, so a first pass with active
// alerts reports a change).
func updateWatchAlerts(stateDir string, ids []string) bool {
	path := filepath.Join(stateDir, "watch-alerts.json")
	var prev []string
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &prev)
	}
	if equalStringSlices(prev, ids) {
		return false
	}
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return true
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o644)
	return true
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runOnce performs one cron pass: fetch → decide → post → snapshot/site.
// It never loops.
func runOnce(o RunOptions) int {
	stdout, stderr := o.stdout(), o.stderr()
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	if err := os.MkdirAll(o.StateDir, 0o755); err != nil {
		fmt.Fprintln(stderr, "run: state dir:", err)
		return 1
	}
	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		fmt.Fprintln(stderr, "run: out dir:", err)
		return 1
	}
	lock, held, err := tryLock(filepath.Join(o.StateDir, "run.lock"))
	if err != nil {
		fmt.Fprintln(stderr, "run: lock:", err)
		return 1
	}
	if held {
		fmt.Fprintln(stdout, "run: already running")
		return 0
	}
	defer func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}()

	// --- Mode (default dry-run; NEVER log secrets) ---
	cfg := fbpost.LoadEnv(o.FbEnvPath)
	killPresent := fbpost.KillSwitchPresent(o.KillSwitchPath) || rules.KillSwitchActive(o.StateDir)
	effDry := fbpost.DryRun(o.Live, o.DryRunFlag)
	live, modeReason := fbpost.Mode(effDry, cfg.OK, killPresent)
	mode := "dry-run"
	if live {
		mode = "live"
	}

	hist := loadHistory(filepath.Join(o.StateDir, historyName(mode)))
	st := loadRunState(filepath.Join(o.StateDir, stateName(mode)))

	dryPoster := &fbpost.DryRunPoster{Dir: filepath.Join(o.OutDir, "outbox")}
	httpClient := o.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: fbpost.Timeout}
	}
	graphPoster := &fbpost.GraphPoster{
		PageID: cfg.PageID, Token: cfg.Token,
		Client: httpClient, BaseURL: o.GraphBaseURL, Sleep: o.Sleep,
	}
	poster, _, _ := fbpost.ChoosePoster(effDry, cfg.OK, killPresent, graphPoster, dryPoster)

	// --- Fetch (per-source errors never abort the pass) ---
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	c := fetch.New(o.CacheDir, o.FixturesDir)
	snap, err := snapshot.Build(ctx, c, snapshot.Options{
		FixturesDir: o.FixturesDir, Now: now,
		QuakesRefetch: runQuakesRefetch, PagasaFeedRefetch: runPagasaRefetch,
		OpenMeteoRefetch: runMeteoRefetch, TaalRefetch: runTaalRefetch,
	})
	if err != nil {
		fmt.Fprintln(stderr, "run: fetch:", err)
		return 1
	}
	var okS, errS []string
	srcErr := func(keys ...string) bool {
		for _, k := range keys {
			if _, bad := snap.Errors[k]; bad {
				return true
			}
		}
		return false
	}
	if srcErr("openmeteo") {
		errS = append(errS, "openmeteo")
	} else {
		okS = append(okS, "openmeteo")
	}
	if srcErr("pagasa_feed", "pagasa_caps") {
		errS = append(errS, "pagasa")
	} else {
		okS = append(okS, "pagasa")
	}
	if srcErr("quakes") {
		errS = append(errS, "quakes")
	} else {
		okS = append(okS, "quakes")
	}
	if srcErr("taal") {
		errS = append(errS, "taal")
	} else {
		okS = append(okS, "taal")
	}

	// --- Decide (kill switch blocks LIVE only via Mode: dry-run still
	// simulates fully — decide → render → outbox + history-dryrun) ---
	events := candidatesFromSnapshot(snap, now, st, hist)
	dec := rules.Decide(now, events, hist, st)

	// --- Post each event (card PNG + trilingual caption, always an image) ---
	posted := 0
	for _, ev := range dec.Post {
		pngBytes, caption, rerr := renderRunEvent(snap, now, ev)
		if rerr != nil {
			fmt.Fprintf(stderr, "run: render %s %s: %v\n", ev.Kind, ev.SourceID, rerr)
			continue
		}
		res, perr := poster.Post(ctx, pngBytes, caption)
		if perr != nil {
			// Not marked as posted: retries next pass (subject to caps).
			fmt.Fprintf(stderr, "run: post %s %s failed: %v\n", ev.Kind, ev.SourceID, perr)
			continue
		}
		pid := res.PostID
		if pid == "" {
			pid = res.ID
		}
		hist = append(hist, rules.Posted{
			Time: now, Kind: ev.Kind, SourceID: ev.SourceID,
			DedupeKey: dedupeOf(ev, now), QuakeTime: ev.QuakeTime,
			Lat: ev.Lat, Lon: ev.Lon, QuakeCanon: ev.QuakeCanon,
			PostID: pid,
		})
		posted++
	}

	// --- State tracks last-seen values per source (separately per mode) ---
	// A failed fetch never moves state. When PAGASA is partial only
	// raises apply; lowering/lifting and first baselines wait for a full pass.
	partial := snap.Pagasa.Partial
	pagasaOK := !srcErr("pagasa_feed") && (!srcErr("pagasa_caps") || partial)
	applyPagasaState(&st, snap, pagasaOK)
	if !srcErr("taal") && snap.Taal != nil {
		st.LastTaal = snap.Taal.Taal.Level
		st.HasTaal = true
	}
	if err := saveHistory(filepath.Join(o.StateDir, historyName(mode)), hist); err != nil {
		fmt.Fprintln(stderr, "run: save history:", err)
	}
	if err := saveRunState(filepath.Join(o.StateDir, stateName(mode)), st); err != nil {
		fmt.Fprintln(stderr, "run: save state:", err)
	}

	// --- Alert-dirty flag for site-publish fast path (shared, not per-mode) ---
	// Touch $state/site-alert-dirty whenever an alert event (non-Daily) is
	// posted or dry-run-planned, or the set of active watch-area alerts
	// changes. site-publish.sh pushes immediately on this flag (throttled to
	// 10 min) instead of waiting for the hourly cadence.
	hasAlertPost := false
	for _, ev := range dec.Post {
		if ev.Kind != rules.KindDaily {
			hasAlertPost = true
			break
		}
	}
	if hasAlertPost {
		touchAlertDirty(o.StateDir, now)
	}
	if updateWatchAlerts(o.StateDir, watchAlertIDs(snap)) {
		touchAlertDirty(o.StateDir, now)
	}

	// --- latest/ + site ---
	latestDir := filepath.Join(o.OutDir, "latest")
	if err := os.MkdirAll(latestDir, 0o755); err != nil {
		fmt.Fprintln(stderr, "run: latest dir:", err)
	}
	writeRunJSON(filepath.Join(latestDir, "snapshot.json"), snap, stderr)
	writeRunJSON(filepath.Join(latestDir, "decision.json"), &dec, stderr)
	siteStatus := "skipped"
	if o.SiteOutDir != "" {
		switch {
		case siteFresh(o.StateDir, now):
			siteStatus = "fresh-skipped"
		default:
			cardsDir := filepath.Join(latestDir, "cards")
			if rerr := renderCards(snap, cardsDir); rerr != nil {
				fmt.Fprintln(stderr, "run: cards:", rerr)
				siteStatus = "cards-error"
			} else if serr := site.Generate(site.Options{
				Snap: snap, CardsDir: cardsDir, BrandDir: o.BrandDir,
				OutDir: o.SiteOutDir, BasePath: o.BasePath,
			}); serr != nil {
				fmt.Fprintln(stderr, "run: site:", serr)
				siteStatus = "site-error"
			} else {
				markSiteBuilt(o.StateDir, now)
				siteStatus = "rebuilt"
			}
		}
	}

	errList := "none"
	if len(errS) > 0 {
		errList = strings.Join(errS, ",")
	}
	planned := ""
	if mode == "dry-run" {
		parts := make([]string, 0, len(dec.Post))
		for _, e := range dec.Post {
			parts = append(parts, string(e.Kind)+":"+dedupeOf(e, now))
		}
		planned = " planned=[" + strings.Join(parts, ",") + "]"
	}
	fmt.Fprintf(stdout, "run %s mode=%s sources=ok[%s] err[%s] events=%d posted=%d deferred=%d site=%s fb=%s%s\n",
		now.In(manila()).Format("2 Jan 2006, 3:04 PM PHT"), mode,
		strings.Join(okS, ","), errList, len(events), posted, len(dec.Deferred),
		siteStatus, modeReason, planned)
	return 0
}

// applyPagasaState folds the PAGASA snapshot into the run state.
// Full pass: record baseline/level. Partial pass: only raises, never
// lowering/lifting and never a first baseline.
func applyPagasaState(st *rules.State, snap *snapshot.Snapshot, pagasaOK bool) {
	if !pagasaOK {
		return
	}
	if snap.Pagasa.Partial {
		if st.HasSignal && snap.Pagasa.WatchMax > st.LastSignal {
			st.LastSignal = snap.Pagasa.WatchMax
		}
		if st.HasRain && rainRank(snap.Pagasa.RainNCR) > rainRank(st.LastRainColor) {
			st.LastRainColor = snap.Pagasa.RainNCR
		}
		return
	}
	st.LastSignal = snap.Pagasa.WatchMax
	st.HasSignal = true
	st.LastRainColor = snap.Pagasa.RainNCR
	st.HasRain = true
}

func dedupeOf(ev rules.Event, now time.Time) string {
	if ev.DedupeKey != "" {
		return ev.DedupeKey
	}
	if ev.Kind == rules.KindDaily {
		return "daily:" + now.In(manila()).Format("2006-01-02")
	}
	return string(ev.Kind) + ":" + ev.SourceID
}

func writeRunJSON(path string, v any, stderr io.Writer) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "run: encode", path+":", err)
		return
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(stderr, "run: write", path+":", err)
	}
}

// renderRunEvent renders one decided event to PNG bytes + trilingual caption.
// Every post includes an image; rain & tsunami use the generic alert card.
func renderRunEvent(snap *snapshot.Snapshot, now time.Time, ev rules.Event) ([]byte, string, error) {
	f := fonts.LoadEmbedded()
	b, err := geo.Load()
	if err != nil {
		return nil, "", err
	}
	timeLabel := phtLabel(now)
	switch ev.Kind {
	case rules.KindDaily:
		if snap.Today == nil {
			return nil, "", fmt.Errorf("no daily data")
		}
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
		img, _, err := card.RenderDaily(f, card.DailyInput{
			Day: *day, DateLabel: dateLabel, TimeLabel: timeLabel,
			TaalLevel: taalLvl, TaalText: taalTxt, Alerts: alerts, Now: now,
		})
		if err != nil {
			return nil, "", err
		}
		caption := posttext.Daily(posttext.DailyData{
			DateLabel: dateLabel, TMax: day.TMax, TMin: day.TMin,
			ApparentMax: day.ApparentMax, PrecipProbMax: windowMax,
			PrecipSum: day.PrecipSum, UVMax: day.UVMax, UVCategory: day.UVCategory,
			AQI: day.AQI, AQICategory: day.AQICategory,
			TaalLevel: taalLvl, TaalText: taalTxt, Alerts: alerts,
		})
		return pngBytes(img, caption)
	case rules.KindSignal:
		issuedLabel := timeLabel
		if t, err := time.Parse(time.RFC3339, snap.Pagasa.CycloneIssued); err == nil {
			issuedLabel = phtLabel(t)
		}
		in := card.SignalInput{
			Cyclone: snap.Pagasa.CycloneName, IssuedLabel: issuedLabel,
			Levels: snap.Pagasa.ProvinceSignals, WatchSummary: snap.Pagasa.WatchSummary,
		}
		img, _, err := card.RenderSignal(f, b, in)
		if err != nil {
			return nil, "", err
		}
		caption := posttext.Signal(posttext.SignalData{
			Cyclone: in.Cyclone, IssuedLabel: in.IssuedLabel,
			Levels: in.Levels, WatchSummary: in.WatchSummary,
		})
		return pngBytes(img, caption)
	case rules.KindQuake:
		for _, q := range snap.Quakes {
			if q.ID == ev.SourceID {
				img, _, err := card.RenderQuake(f, b, card.QuakeInput{Event: q.Event, TimeLabel: phtLabel(q.Time)})
				if err != nil {
					return nil, "", err
				}
				caption := posttext.Quake(posttext.QuakeData{
					Mag: q.Mag, DepthKm: q.DepthKm, Place: q.Place,
					DistanceKm: q.DistanceKm, Compass: q.Compass,
					TimeLabel: phtLabel(q.Time), Source: q.Source,
				})
				return pngBytes(img, caption)
			}
		}
		// Event left the feed: still post something with an image.
		title := fmt.Sprintf("Earthquake M%.1f", ev.Magnitude)
		img, _, err := card.RenderAlert(f, card.AlertInput{
			Title:     title,
			Lines:     []string{fmt.Sprintf("%.0f km from Las Piñas", ev.DistanceKm), "See PHIVOLCS for details."},
			TimeLabel: timeLabel, Sources: []string{"PHIVOLCS"},
		})
		if err != nil {
			return nil, "", err
		}
		return pngBytes(img, posttext.Alert(posttext.AlertData{Title: title, Source: "Source: PHIVOLCS"}))
	case rules.KindTaal:
		if snap.Taal == nil {
			return nil, "", fmt.Errorf("no taal data")
		}
		bdate := now.In(manila()).Format("2 Jan 2006")
		if t, err := parseBulletinDate(snap.Taal.Taal.BulletinTitle); err == nil {
			bdate = t.Format("2 Jan 2006")
		}
		in := card.TaalInput{
			Level: snap.Taal.Taal.Level, LevelText: snap.Taal.Taal.LevelText,
			BulletinTitle: snap.Taal.Taal.BulletinTitle, BulletinDate: bdate,
		}
		img, _, err := card.RenderTaal(f, in)
		if err != nil {
			return nil, "", err
		}
		caption := posttext.Taal(posttext.TaalData{
			Level: in.Level, LevelText: in.LevelText,
			BulletinTitle: in.BulletinTitle, BulletinDate: bdate,
		})
		return pngBytes(img, caption)
	case rules.KindRain:
		color := ev.RainColor
		if color == "" {
			color = snap.Pagasa.RainNCR
		}
		titleColor := strings.Title(strings.ToLower(color))
		title := fmt.Sprintf("%s Rainfall Warning — Metro Manila (NCR)", titleColor)
		var lines []string
		for _, r := range snap.Pagasa.Rainfall {
			if r.Color == color && (r.InNCR || r.InWatch) {
				lines = append(lines, "Areas: "+strings.Join(r.Areas, "; "))
				break
			}
		}
		img, _, err := card.RenderAlert(f, card.AlertInput{Title: title, Lines: lines, TimeLabel: timeLabel})
		if err != nil {
			return nil, "", err
		}
		return pngBytes(img, posttext.Alert(posttext.AlertData{Title: title, Lines: lines}))
	case rules.KindTsunami:
		title := "Tsunami Advisory"
		var lines []string
		for _, a := range snap.Pagasa.Tsunami {
			if a.Identifier == ev.SourceID && a.Headline != "" {
				lines = append(lines, a.Headline)
				break
			}
		}
		if len(lines) == 0 {
			for _, a := range snap.Pagasa.Tsunami {
				if a.Headline != "" {
					lines = append(lines, a.Headline)
					break
				}
			}
		}
		img, _, err := card.RenderAlert(f, card.AlertInput{Title: title, Lines: lines, TimeLabel: timeLabel})
		if err != nil {
			return nil, "", err
		}
		return pngBytes(img, posttext.Alert(posttext.AlertData{Title: title, Lines: lines}))
	default:
		return nil, "", fmt.Errorf("unknown kind %q", ev.Kind)
	}
}

// pngBytes encodes img to PNG bytes, paired with its caption.
func pngBytes(img image.Image, caption string) ([]byte, string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), caption, nil
}
