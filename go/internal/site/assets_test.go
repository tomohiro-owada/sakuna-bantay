package site

import (
	"os"
	"path/filepath"
	"testing"
)

// repoBrandDir locates the repo brand/ directory when present (local
// checkout). Returns "" when absent (e.g. server without brand/).
func repoBrandDir() string {
	// Package dir is go/internal/site; brand is ../../../brand.
	candidates := []string{
		filepath.Join("..", "..", "..", "brand"),
		filepath.Join("..", "..", "..", "..", "brand"),
		"../../../brand",
	}
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(c, "sakuna-bantay-logo.svg")); err == nil && !st.IsDir() {
			return c
		}
	}
	// Absolute fallback for this repo layout.
	if st, err := os.Stat("/workspace/sakuna-bantay/brand/sakuna-bantay-logo.svg"); err == nil && !st.IsDir() {
		return "/workspace/sakuna-bantay/brand"
	}
	return ""
}

// TestEmbeddedAssetsSyncWithBrand keeps go/internal/site/assets/ in sync
// with the repo brand/ source of truth. Skipped when brand/ is absent.
func TestEmbeddedAssetsSyncWithBrand(t *testing.T) {
	brandDir := repoBrandDir()
	if brandDir == "" {
		t.Skip("brand/ not present; embedded assets used as-is")
	}
	// Logo.
	wantLogo, err := os.ReadFile(filepath.Join(brandDir, "sakuna-bantay-logo.svg"))
	if err != nil {
		t.Fatalf("read brand logo: %v", err)
	}
	gotLogo, err := embeddedLogo()
	if err != nil {
		t.Fatalf("read embedded logo: %v", err)
	}
	if string(gotLogo) != string(wantLogo) {
		t.Errorf("embedded sakuna-bantay-logo.svg differs from %s/sakuna-bantay-logo.svg (re-copy brand/ into internal/site/assets/)", brandDir)
	}
	// Favicons: same file set, byte-identical.
	wantEnts, err := os.ReadDir(filepath.Join(brandDir, "favicon"))
	if err != nil {
		t.Fatalf("read brand favicon: %v", err)
	}
	gotFavs, err := embeddedFavicons()
	if err != nil {
		t.Fatalf("read embedded favicons: %v", err)
	}
	if len(gotFavs) != len(wantEnts) {
		t.Errorf("embedded favicon count = %d, brand count = %d", len(gotFavs), len(wantEnts))
	}
	for _, e := range wantEnts {
		if e.IsDir() {
			continue
		}
		want, err := os.ReadFile(filepath.Join(brandDir, "favicon", e.Name()))
		if err != nil {
			t.Fatalf("read brand favicon %s: %v", e.Name(), err)
		}
		got, ok := gotFavs[e.Name()]
		if !ok {
			t.Errorf("embedded favicon missing %s", e.Name())
			continue
		}
		if string(got) != string(want) {
			t.Errorf("embedded favicon/%s differs from brand/favicon/%s", e.Name(), e.Name())
		}
	}
	for name := range gotFavs {
		if _, err := os.Stat(filepath.Join(brandDir, "favicon", name)); err != nil {
			t.Errorf("embedded favicon/%s has no counterpart in brand/favicon/", name)
		}
	}
}

// TestGenerateEmbeddedFallbackNoBrandDir ensures a missing/empty BrandDir
// still ships logo + favicons from the embedded copies.
func TestGenerateEmbeddedFallbackNoBrandDir(t *testing.T) {
	snap := testSnap(t)
	cards := t.TempDir()
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for _, n := range []string{"daily-20261025.png", "taal-16933.png"} {
		if err := os.WriteFile(filepath.Join(cards, n), png, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, brandDir := range []string{"", filepath.Join(t.TempDir(), "does-not-exist")} {
		out := filepath.Join(t.TempDir(), "site")
		if err := Generate(Options{
			Snap: snap, CardsDir: cards, BrandDir: brandDir, OutDir: out, BasePath: "/sakuna-bantay/",
		}); err != nil {
			t.Fatalf("BrandDir %q: %v", brandDir, err)
		}
		for _, n := range []string{
			"sakuna-bantay-logo.svg",
			"favicon/favicon.svg", "favicon/favicon-32.png", "favicon/apple-touch-icon-180.png",
			"today.png", "taal.png",
		} {
			if _, err := os.Stat(filepath.Join(out, n)); err != nil {
				t.Errorf("BrandDir %q: missing %s: %v", brandDir, n, err)
			}
		}
	}
}

// TestGenerateAtomicReplacesStale ensures Generate leaves only the current
// build's files: stale dated/synth files from an older build disappear.
func TestGenerateAtomicReplacesStale(t *testing.T) {
	snap := testSnap(t)
	cards := t.TempDir()
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for _, n := range []string{"daily-20261025.png", "taal-16933.png", "signal-synth.png", "quake-synth-near.png"} {
		if err := os.WriteFile(filepath.Join(cards, n), png, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	parent := t.TempDir()
	out := filepath.Join(parent, "site")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	// Stale files from an older build.
	for _, n := range []string{"daily-20200101.png", "signal-synth.png", "quake-synth-near.png", "stale.txt"} {
		if err := os.WriteFile(filepath.Join(out, n), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Generate(Options{
		Snap: snap, CardsDir: cards, BrandDir: "", OutDir: out, BasePath: "/sakuna-bantay/",
	}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"daily-20200101.png", "signal-synth.png", "quake-synth-near.png", "stale.txt", "daily-20261025.png"} {
		if _, err := os.Stat(filepath.Join(out, n)); !os.IsNotExist(err) {
			t.Errorf("stale file %s must not survive atomic rebuild", n)
		}
	}
	for _, n := range []string{"today.png", "taal.png", "sakuna-bantay-logo.svg", "favicon/favicon.svg"} {
		if _, err := os.Stat(filepath.Join(out, n)); err != nil {
			t.Errorf("missing %s after rebuild: %v", n, err)
		}
	}
	// No temp/backup debris next to the output.
	ents, _ := os.ReadDir(parent)
	for _, e := range ents {
		name := e.Name()
		if name == "site" {
			continue
		}
		t.Errorf("debris next to site dir after atomic swap: %s", name)
	}
}
