package site

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/snapshot"
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
