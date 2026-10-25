package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/pagasa"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/rules"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/snapshot"
)

// failRoundTripper fails the test on ANY HTTP request. GraphPoster must
// never be called on dry-run paths; the only other HTTP user (fetch) is in
// fixture mode in these tests.
type failRoundTripper struct {
	t      *testing.T
	called *atomic.Bool
}

func (f *failRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	f.called.Store(true)
	f.t.Errorf("unexpected HTTP request in dry-run: %s %s", r.Method, r.URL.Redacted())
	return nil, errors.New("network disabled in test")
}

func fixtureNow() time.Time {
	// Matches the repo fixtures (25 Oct 2026 evening PHT).
	loc, _ := time.LoadLocation("Asia/Manila")
	if loc == nil {
		loc = time.FixedZone("PHT", 8*3600)
	}
	return time.Date(2026, 10, 25, 18, 0, 0, 0, loc)
}

func writeFbEnv(t *testing.T, path string) {
	t.Helper()
	content := "SAKUNA_FB_PAGE_ID=12345\nSAKUNA_FB_PAGE_TOKEN=test-token-xyz\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

type runHarness struct {
	t                       *testing.T
	home, state, cache, out string
	fbEnv, kill             string
	called                  *atomic.Bool
	stdout, stderr          *strings.Builder
}

func newHarness(t *testing.T) *runHarness {
	t.Helper()
	home := t.TempDir()
	called := &atomic.Bool{}
	return &runHarness{
		t:      t,
		home:   home,
		state:  filepath.Join(home, "state"),
		cache:  filepath.Join(home, "cache"),
		out:    filepath.Join(home, "out"),
		fbEnv:  filepath.Join(home, ".config", "sakuna", "fb.env"),
		kill:   filepath.Join(home, ".config", "sakuna", "KILL_SWITCH"),
		called: called,
		stdout: &strings.Builder{},
		stderr: &strings.Builder{},
	}
}

func (h *runHarness) opts(now time.Time) RunOptions {
	return RunOptions{
		StateDir: h.state, CacheDir: h.cache, OutDir: h.out,
		BrandDir: "../../../brand", BasePath: "/sakuna-bantay/",
		FbEnvPath: h.fbEnv, KillSwitchPath: h.kill,
		FixturesDir: "../../../fixtures", Now: now,
		HTTPClient: &http.Client{Transport: &failRoundTripper{t: h.t, called: h.called}},
		Stdout:     h.stdout, Stderr: h.stderr,
	}
}

// testFBServer is a fake Graph API: records posts, answers success.
type testFBServer struct {
	t     *testing.T
	posts *atomic.Int32
	srv   *httptest.Server
	URL   string
}

func newTestFBServer(t *testing.T, posts *atomic.Int32) *testFBServer {
	t.Helper()
	s := &testFBServer{t: t, posts: posts}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v21.0/") || !strings.HasSuffix(r.URL.Path, "/photos") {
			t.Errorf("fake graph: bad path %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("fake graph: query must be empty, got %q", r.URL.RawQuery)
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("fake graph: not multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var hasToken, hasImage, hasCaption bool
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			b, _ := io.ReadAll(part)
			switch part.FormName() {
			case "access_token":
				if string(b) != "test-token-xyz" {
					t.Errorf("fake graph: wrong token %q", string(b))
				}
				hasToken = true
			case "source":
				if len(b) > 0 {
					hasImage = true
				}
			case "caption":
				if len(b) > 0 {
					hasCaption = true
				}
			}
		}
		if !hasToken || !hasImage || !hasCaption {
			t.Errorf("fake graph: missing fields token=%v image=%v caption=%v", hasToken, hasImage, hasCaption)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		posts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fb_1","post_id":"page_fb_1"}`))
	}))
	s.URL = s.srv.URL
	return s
}

// Close shuts the fake graph down.
func (s *testFBServer) Close() { s.srv.Close() }

// Client returns an HTTP client routed to the fake graph.
func (s *testFBServer) Client() *http.Client { return s.srv.Client() }

func outboxFiles(t *testing.T, out string) (png, txt, jsn int) {
	t.Helper()
	dir := filepath.Join(out, "outbox")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, 0
	}
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".png"):
			png++
		case strings.HasSuffix(e.Name(), ".txt"):
			txt++
		case strings.HasSuffix(e.Name(), ".json"):
			jsn++
		}
	}
	return png, txt, jsn
}

func readHist(t *testing.T, path string) []rules.Posted {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var h []rules.Posted
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return h
}

// seedDryState writes a baselined dry-run state where Taal 0->1 is a change
// (so the fixture Taal level 1 posts instead of baselining silently).
func seedDryState(t *testing.T, stateDir string) {
	t.Helper()
	st := `{"last_signal":0,"has_signal":true,"last_taal":0,"has_taal":true,"last_rain_color":"","has_rain":true}`
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "state-dryrun.json"), []byte(st), 0o644); err != nil {
		t.Fatal(err)
	}
}

func seedLiveState(t *testing.T, stateDir string) {
	t.Helper()
	st := `{"last_signal":0,"has_signal":true,"last_taal":0,"has_taal":true,"last_rain_color":"","has_rain":true}`
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte(st), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunDryRunNoNetwork is the PHASE_D §2 pipeline test: fixtures + fake
// env (0600) + no kill switch (the most dangerous configuration) WITHOUT
// -live → the HTTP transport is never called and outbox files exist.
func TestRunDryRunNoNetwork(t *testing.T) {
	h := newHarness(t)
	writeFbEnv(t, h.fbEnv)
	seedDryState(t, h.state)
	o := h.opts(fixtureNow())
	o.Live = false
	if code := runOnce(o); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, h.stderr.String())
	}
	if h.called.Load() {
		t.Error("HTTP transport was called on dry-run")
	}
	png, txt, jsn := outboxFiles(t, h.out)
	if png == 0 || txt == 0 || jsn == 0 {
		t.Errorf("outbox files missing (png=%d txt=%d json=%d)", png, txt, jsn)
	}
	hist := readHist(t, filepath.Join(h.state, "history-dryrun.json"))
	if len(hist) == 0 {
		t.Error("history-dryrun.json has no entries (taal post expected)")
	}
	if _, err := os.Stat(filepath.Join(h.state, "history.json")); !os.IsNotExist(err) {
		t.Error("history.json must not exist after a dry-run")
	}
	if _, err := os.Stat(filepath.Join(h.state, "state-dryrun.json")); err != nil {
		t.Errorf("state-dryrun.json missing: %v", err)
	}
	if !strings.Contains(h.stdout.String(), "mode=dry-run") {
		t.Errorf("summary missing mode=dry-run:\n%s", h.stdout.String())
	}
	if !strings.Contains(h.stdout.String(), "planned=[Taal:") {
		t.Errorf("dry-run summary must list planned posts (kind + dedupe):\n%s", h.stdout.String())
	}
	if strings.Contains(h.stdout.String(), "test-token") || strings.Contains(h.stderr.String(), "test-token") {
		t.Error("logs leak the token")
	}
}

// TestRunFirstRunBaselinesSilently: empty state + fixtures → no
// Taal/Signal/Rain change post; current values recorded silently as baseline.
func TestRunFirstRunBaselinesSilently(t *testing.T) {
	h := newHarness(t)
	writeFbEnv(t, h.fbEnv)
	o := h.opts(fixtureNow())
	o.Live = false
	if code := runOnce(o); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, h.stderr.String())
	}
	if h.called.Load() {
		t.Error("HTTP transport was called on dry-run")
	}
	for _, p := range readHist(t, filepath.Join(h.state, "history-dryrun.json")) {
		if p.Kind == rules.KindTaal || p.Kind == rules.KindSignal || p.Kind == rules.KindRain {
			t.Errorf("first run must baseline, not post: %+v", p)
		}
	}
	b, err := os.ReadFile(filepath.Join(h.state, "state-dryrun.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fs fileState
	if err := json.Unmarshal(b, &fs); err != nil {
		t.Fatal(err)
	}
	if !fs.HasTaal || !fs.HasSignal || !fs.HasRain {
		t.Errorf("baseline state must record Has flags: %+v", fs)
	}
	if fs.LastTaal != 1 {
		t.Errorf("last_taal = %d, want 1 (fixture baseline)", fs.LastTaal)
	}
}

// TestRunLiveKillSwitchNoNetwork: -live but kill switch present → no network,
// yet the pipeline still simulates fully (dry-run outbox + history-dryrun).
func TestRunLiveKillSwitchNoNetwork(t *testing.T) {
	h := newHarness(t)
	writeFbEnv(t, h.fbEnv)
	if err := os.WriteFile(h.kill, []byte("stop"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedDryState(t, h.state)
	o := h.opts(fixtureNow())
	o.Live = true
	if code := runOnce(o); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, h.stderr.String())
	}
	if h.called.Load() {
		t.Error("HTTP transport was called despite kill switch")
	}
	if png, txt, jsn := outboxFiles(t, h.out); png == 0 || txt == 0 || jsn == 0 {
		t.Errorf("kill switch dry-run must still simulate (outbox png=%d txt=%d json=%d)", png, txt, jsn)
	}
	if _, err := os.Stat(filepath.Join(h.state, "history-dryrun.json")); err != nil {
		t.Errorf("history-dryrun.json missing despite kill-switch dry-run: %v", err)
	} else if len(readHist(t, filepath.Join(h.state, "history-dryrun.json"))) == 0 {
		t.Error("history-dryrun.json empty despite kill-switch dry-run")
	}
	if _, err := os.Stat(filepath.Join(h.state, "history.json")); !os.IsNotExist(err) {
		t.Error("history.json must not exist when kill switch forces dry-run")
	}
	if !strings.Contains(h.stdout.String(), "mode=dry-run") {
		t.Errorf("summary missing mode=dry-run:\n%s", h.stdout.String())
	}
	if !strings.Contains(h.stdout.String(), "fb=dry-run (kill switch)") {
		t.Errorf("summary missing fb=dry-run (kill switch):\n%s", h.stdout.String())
	}
	if !strings.Contains(h.stdout.String(), "planned=[Taal:") {
		t.Errorf("kill-switch dry-run summary must list planned posts (kind + dedupe):\n%s", h.stdout.String())
	}
}

// TestRunLiveDryRunWins: -live + -dry-run together → dry-run wins.
func TestRunLiveDryRunWins(t *testing.T) {
	h := newHarness(t)
	writeFbEnv(t, h.fbEnv)
	seedDryState(t, h.state)
	o := h.opts(fixtureNow())
	o.Live = true
	o.DryRunFlag = true
	if code := runOnce(o); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, h.stderr.String())
	}
	if h.called.Load() {
		t.Error("HTTP transport was called despite -dry-run")
	}
	if png, _, _ := outboxFiles(t, h.out); png == 0 {
		t.Error("expected outbox files in dry-run-wins mode")
	}
	if !strings.Contains(h.stdout.String(), "mode=dry-run") {
		t.Errorf("summary missing mode=dry-run:\n%s", h.stdout.String())
	}
	if !strings.Contains(h.stdout.String(), "planned=[Taal:") {
		t.Errorf("dry-run summary must list planned posts (kind + dedupe):\n%s", h.stdout.String())
	}
}

// TestRunLivePostsViaHTTP: -live without kill switch posts through the
// injected HTTP client (httptest, never the real graph) and records the FB
// post id in history.json.
func TestRunLivePostsViaHTTP(t *testing.T) {
	h := newHarness(t)
	writeFbEnv(t, h.fbEnv)
	seedLiveState(t, h.state)
	var posts atomic.Int32
	srv := newTestFBServer(t, &posts)
	defer srv.Close()
	o := h.opts(fixtureNow())
	o.Live = true
	o.GraphBaseURL = srv.URL
	o.HTTPClient = srv.Client()
	if code := runOnce(o); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, h.stderr.String())
	}
	if posts.Load() == 0 {
		t.Fatal("live run posted nothing to the fake graph")
	}
	hist := readHist(t, filepath.Join(h.state, "history.json"))
	if len(hist) == 0 {
		t.Fatal("history.json empty after live run")
	}
	if hist[0].PostID == "" {
		t.Errorf("live history missing FB post id: %+v", hist[0])
	}
	if _, err := os.Stat(filepath.Join(h.state, "history-dryrun.json")); !os.IsNotExist(err) {
		t.Error("history-dryrun.json must not exist after a live run")
	}
	if _, err := os.Stat(filepath.Join(h.state, "state.json")); err != nil {
		t.Errorf("state.json missing: %v", err)
	}
	if !strings.Contains(h.stdout.String(), "mode=live") {
		t.Errorf("summary missing mode=live:\n%s", h.stdout.String())
	}
	if png, _, _ := outboxFiles(t, h.out); png != 0 {
		t.Error("live run must not write outbox files")
	}
}

// TestRunSourceFailureKeepsState: a failed PAGASA fetch is never treated as
// "signal lifted" — no signal post, previous state kept.
func TestRunSourceFailureKeepsState(t *testing.T) {
	h := newHarness(t)
	writeFbEnv(t, h.fbEnv)
	// Fixtures without the feed: pagasa fails, everything else parses.
	broken := t.TempDir()
	copyDir(t, "../../../fixtures", broken)
	if err := os.Remove(filepath.Join(broken, "pagasa-feed.xml")); err != nil {
		t.Fatal(err)
	}
	// Previous state saw Signal #2 and Taal 1.
	st := `{"last_signal":2,"has_signal":true,"last_taal":1,"has_taal":true,"last_rain_color":"","has_rain":true}`
	if err := os.MkdirAll(h.state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.state, "state-dryrun.json"), []byte(st), 0o644); err != nil {
		t.Fatal(err)
	}
	o := h.opts(fixtureNow())
	o.Live = false
	o.FixturesDir = broken
	if code := runOnce(o); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, h.stderr.String())
	}
	if h.called.Load() {
		t.Error("HTTP transport was called on dry-run")
	}
	for _, p := range readHist(t, filepath.Join(h.state, "history-dryrun.json")) {
		if p.Kind == rules.KindSignal {
			t.Errorf("failed fetch treated as signal change: %+v", p)
		}
	}
	b, err := os.ReadFile(filepath.Join(h.state, "state-dryrun.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fs fileState
	if err := json.Unmarshal(b, &fs); err != nil {
		t.Fatal(err)
	}
	if fs.LastSignal != 2 {
		t.Errorf("last_signal = %d, want 2 (kept across failed fetch)", fs.LastSignal)
	}
}

// TestRunLockHeld: an already-running pass exits 0 without posting.
func TestRunLockHeld(t *testing.T) {
	h := newHarness(t)
	writeFbEnv(t, h.fbEnv)
	if err := os.MkdirAll(h.state, 0o755); err != nil {
		t.Fatal(err)
	}
	lock, held, err := tryLock(filepath.Join(h.state, "run.lock"))
	if err != nil || held {
		t.Fatalf("pre-lock failed: %v held=%v", err, held)
	}
	defer func() {
		_ = lock.Close()
	}()
	o := h.opts(fixtureNow())
	if code := runOnce(o); code != 0 {
		t.Fatalf("locked run must exit 0, got %d", code)
	}
	if !strings.Contains(h.stdout.String(), "already running") {
		t.Errorf("missing already-running log:\n%s", h.stdout.String())
	}
	if png, _, _ := outboxFiles(t, h.out); png != 0 {
		t.Error("locked run must not post")
	}
}

func TestSiteThrottle(t *testing.T) {
	dir := t.TempDir()
	now := fixtureNow()
	if siteFresh(dir, now) {
		t.Error("no marker → not fresh")
	}
	markSiteBuilt(dir, now)
	if !siteFresh(dir, now.Add(14*time.Minute)) {
		t.Error("14 min → still fresh")
	}
	if siteFresh(dir, now.Add(16*time.Minute)) {
		t.Error("16 min → stale, rebuild due")
	}
	if siteFresh(dir, now.Add(-time.Minute)) {
		t.Error("marker in the future → not fresh")
	}
}

// TestRunSiteRebuildThrottle: second pass within 15 min skips the rebuild.
func TestRunSiteRebuildThrottle(t *testing.T) {
	h := newHarness(t)
	writeFbEnv(t, h.fbEnv)
	siteOut := filepath.Join(h.home, "site")
	now := fixtureNow()
	o := h.opts(now)
	o.SiteOutDir = siteOut
	if code := runOnce(o); code != 0 {
		t.Fatalf("first run exit %d:\n%s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "site=rebuilt") {
		t.Errorf("first run should rebuild site:\n%s", h.stdout.String())
	}
	st1, err := os.Stat(filepath.Join(siteOut, "index.html"))
	if err != nil {
		t.Fatalf("site index missing: %v", err)
	}
	h.stdout.Reset()
	h.stderr.Reset()
	o2 := h.opts(now.Add(time.Minute))
	o2.SiteOutDir = siteOut
	if code := runOnce(o2); code != 0 {
		t.Fatalf("second run exit %d:\n%s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "site=fresh-skipped") {
		t.Errorf("second run should skip site rebuild:\n%s", h.stdout.String())
	}
	st2, err := os.Stat(filepath.Join(siteOut, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !st2.ModTime().Equal(st1.ModTime()) {
		t.Error("site index rewritten despite throttle")
	}
}

func partialSnap(watchMax int, rainColor, rainCap string, tsunamiIDs []string) *snapshot.Snapshot {
	snap := &snapshot.Snapshot{Errors: map[string]string{"pagasa_caps": "pagasa: partial (1/2 CAPs failed, 1 usable)"}}
	snap.Pagasa.Partial = true
	snap.Pagasa.WatchMax = watchMax
	snap.Pagasa.RainNCR = rainColor
	if rainColor != "" {
		snap.Pagasa.Rainfall = []snapshot.Rainfall{{CapID: rainCap, Color: rainColor, Areas: []string{"Metro Manila"}, InNCR: true, InWatch: true}}
	}
	for _, id := range tsunamiIDs {
		snap.Pagasa.Tsunami = append(snap.Pagasa.Tsunami, &pagasa.Alert{Identifier: id, Event: "Tsunami Warning", Headline: "Tsunami"})
	}
	return snap
}

func TestPartialSuppressesLowering(t *testing.T) {
	now := fixtureNow()
	// Previous: Signal 2, rain ORANGE. Partial snapshot lowers to 1/none:
	// no signal/rain events, state must not move.
	st := rules.State{LastSignal: 2, HasSignal: true, LastRainColor: "ORANGE", HasRain: true, HasTaal: true}
	snap := partialSnap(1, "", "", nil)
	if evs := candidatesFromSnapshot(snap, now, st, nil); len(evs) != 0 {
		t.Errorf("partial lowering must emit no events, got %v", evs)
	}
	st2 := st
	applyPagasaState(&st2, snap, true)
	if st2.LastSignal != 2 || st2.LastRainColor != "ORANGE" {
		t.Errorf("partial must not lower state: %+v", st2)
	}
}

func TestPartialEmitsRaiseAndNew(t *testing.T) {
	now := fixtureNow()
	st := rules.State{LastSignal: 1, HasSignal: true, LastRainColor: "ORANGE", HasRain: true, HasTaal: true}
	// Raise: signal 1->2 and rain ORANGE->RED.
	snap := partialSnap(2, "RED", "cap-red", nil)
	evs := candidatesFromSnapshot(snap, now, st, nil)
	var kinds []string
	for _, e := range evs {
		kinds = append(kinds, string(e.Kind))
	}
	joined := strings.Join(kinds, ",")
	if !strings.Contains(joined, "Signal") || !strings.Contains(joined, "Rain") {
		t.Errorf("partial raise must emit signal+rain, got %v", kinds)
	}
	st2 := st
	applyPagasaState(&st2, snap, true)
	if st2.LastSignal != 2 || st2.LastRainColor != "RED" {
		t.Errorf("partial raise must update state: %+v", st2)
	}
	// New alert with same level: new rain CAP id posts even without a raise.
	hist := []rules.Posted{{Kind: rules.KindRain, SourceID: "cap-old", DedupeKey: "rain:cap-old:ORANGE"}}
	snapSame := partialSnap(1, "ORANGE", "cap-new", nil)
	if evs := candidatesFromSnapshot(snapSame, now, st, hist); len(evs) == 0 {
		t.Error("partial new CAP id should still emit")
	}
	// Same id + same color: no event.
	histSame := []rules.Posted{{Kind: rules.KindRain, SourceID: "cap-new", DedupeKey: "rain:cap-new:ORANGE"}}
	_ = histSame
	snapDup := partialSnap(1, "ORANGE", "cap-old", nil)
	if evs := candidatesFromSnapshot(snapDup, now, st, hist); len(evs) != 0 {
		t.Errorf("partial duplicate must emit nothing, got %v", evs)
	}
	// Tsunami: only unseen ids emit when partial.
	histTsu := []rules.Posted{{Kind: rules.KindTsunami, SourceID: "seen", DedupeKey: "tsunami:seen"}}
	snapTsu := partialSnap(1, "", "", []string{"seen", "fresh"})
	evsTsu := candidatesFromSnapshot(snapTsu, now, st, histTsu)
	if len(evsTsu) != 1 || evsTsu[0].SourceID != "fresh" {
		t.Errorf("partial tsunami must emit only new ids, got %v", evsTsu)
	}
}

// TestResolveRunConfigPathsExplicit: with -home X and explicit -fb-env /
// -kill-switch the explicit paths must be used verbatim (never derived from
// -home). With flags absent the defaults must come from the real user home
// ($HOME/.config/sakuna via os.UserHomeDir), not from -home.
func TestResolveRunConfigPathsExplicit(t *testing.T) {
	fb, kill := resolveRunConfigPaths("/home/example/.config/sakuna/fb.env", "/home/example/.config/sakuna/KILL_SWITCH")
	if fb != "/home/example/.config/sakuna/fb.env" {
		t.Errorf("explicit fb-env not preserved: %q", fb)
	}
	if kill != "/home/example/.config/sakuna/KILL_SWITCH" {
		t.Errorf("explicit kill-switch not preserved: %q", kill)
	}
}

func TestResolveRunConfigPathsDefaultFromRealHome(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	fb, kill := resolveRunConfigPaths("", "")
	wantFb := filepath.Join(fakeHome, ".config", "sakuna", "fb.env")
	wantKill := filepath.Join(fakeHome, ".config", "sakuna", "KILL_SWITCH")
	if fb != wantFb {
		t.Errorf("default fb-env = %q, want %q (real $HOME, not -home)", fb, wantFb)
	}
	if kill != wantKill {
		t.Errorf("default kill-switch = %q, want %q (real $HOME, not -home)", kill, wantKill)
	}
	// A -home pointing elsewhere must not influence the config defaults.
	otherHome := filepath.Join(fakeHome, "elsewhere")
	if strings.HasPrefix(fb, otherHome) || strings.HasPrefix(kill, otherHome) {
		t.Errorf("config defaults must not derive from -home: fb=%q kill=%q", fb, kill)
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRunAlertDirtyOnAlertPost: a dry-run pass that plans an alert event
// (non-Daily) must create $state/site-alert-dirty for the publisher fast path.
func TestRunAlertDirtyOnAlertPost(t *testing.T) {
	h := newHarness(t)
	writeFbEnv(t, h.fbEnv)
	seedDryState(t, h.state)
	o := h.opts(fixtureNow())
	o.Live = false
	if code := runOnce(o); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, h.stderr.String())
	}
	if _, err := os.Stat(filepath.Join(h.state, "site-alert-dirty")); err != nil {
		t.Errorf("site-alert-dirty missing after alert dry-run plan: %v", err)
	}
}

// TestRunAlertDirtyOnWatchSetChange: changing the set of active watch-area
// alerts between passes touches the dirty flag even when nothing posts.
func TestRunAlertDirtyOnWatchSetChange(t *testing.T) {
	dir := t.TempDir()
	now := fixtureNow()
	// First: empty set → no flag.
	if updateWatchAlerts(dir, nil) {
		t.Error("empty initial set should not count as a change")
	}
	if _, err := os.Stat(filepath.Join(dir, "site-alert-dirty")); !os.IsNotExist(err) {
		t.Error("dirty flag must not exist after empty no-change")
	}
	// New watch-area alert appears → change.
	if !updateWatchAlerts(dir, []string{"cap-new-1"}) {
		t.Error("new alert set should count as a change")
	}
	touchAlertDirty(dir, now)
	if _, err := os.Stat(filepath.Join(dir, "site-alert-dirty")); err != nil {
		t.Errorf("dirty flag missing after set change: %v", err)
	}
	// Same set again → no change.
	if updateWatchAlerts(dir, []string{"cap-new-1"}) {
		t.Error("identical set must not count as a change")
	}
	// Different set → change.
	if !updateWatchAlerts(dir, []string{"cap-new-1", "cap-new-2"}) {
		t.Error("expanded set should count as a change")
	}
}
