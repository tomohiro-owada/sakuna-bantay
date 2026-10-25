package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUserAgentAndTimeout(t *testing.T) {
	if UserAgent == "" || len(UserAgent) < 10 {
		t.Fatal("UserAgent must be set")
	}
	c := New("", "")
	if c.HTTP.Timeout != DefaultTimeout {
		t.Errorf("timeout = %v, want %v", c.HTTP.Timeout, DefaultTimeout)
	}
	if DefaultTimeout != 20*time.Second {
		t.Errorf("DefaultTimeout = %v, want 20s", DefaultTimeout)
	}
}

func TestCacheKeyStable(t *testing.T) {
	a := cacheKey("https://example.com/a")
	b := cacheKey("https://example.com/a")
	c := cacheKey("https://example.com/b")
	if a != b {
		t.Error("same URL must give same key")
	}
	if a == c {
		t.Error("different URLs must give different keys")
	}
	if len(a) != 64 {
		t.Errorf("sha256 hex len = %d, want 64", len(a))
	}
}

func TestConditionalGet304(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if ua := r.Header.Get("User-Agent"); ua != UserAgent {
			t.Errorf("UA = %q, want %q", ua, UserAgent)
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Wed, 25 Oct 2026 00:00:00 GMT")
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := New(dir, "")
	ctx := context.Background()

	r1, err := c.Get(ctx, srv.URL+"/x", 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(r1.Body) != "hello" || r1.FromCache {
		t.Errorf("first fetch = %q fromCache=%v", r1.Body, r1.FromCache)
	}
	// Second fetch sends If-None-Match, server answers 304.
	r2, err := c.Get(ctx, srv.URL+"/x", 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(r2.Body) != "hello" {
		t.Errorf("304 body = %q, want hello", r2.Body)
	}
	if !r2.Revalidated {
		t.Error("second fetch should be Revalidated (304)")
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
	// Meta sidecar exists and holds validators.
	mb, err := os.ReadFile(filepath.Join(dir, cacheKey(srv.URL+"/x")+".meta.json"))
	if err != nil {
		t.Fatal("meta sidecar missing:", err)
	}
	if string(mb) == "" {
		t.Error("empty meta")
	}
}

func TestMinRefetchUsesCache(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte("v"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := New(dir, "")
	ctx := context.Background()
	if _, err := c.Get(ctx, srv.URL, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	r2, err := c.Get(ctx, srv.URL, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.FromCache {
		t.Error("fresh cache should be served without network")
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1 (second served from cache)", hits)
	}
	// minRefetch=0 forces revalidation.
	if _, err := c.Get(ctx, srv.URL, 0); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2 after forced refetch", hits)
	}
}

func TestPerHostLimiter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := New(dir, "")
	// Fix clock for deterministic throttle test.
	base := time.Now()
	c.now = func() time.Time { return base }
	ctx := context.Background()
	start := time.Now()
	// Different URLs same host; minRefetch=0 forces network each time.
	if _, err := c.Get(ctx, srv.URL+"/a", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, srv.URL+"/b", 0); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("two rapid requests finished in %v, want >= ~1s (1 req/s/host)", elapsed)
	}
}

func TestFixtureMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.json"), []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewFixture(dir, map[string]string{"https://example.com/api": "f.json"})
	r, err := c.Get(context.Background(), "https://example.com/api", 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Body) != `{"a":1}` || !r.FromCache {
		t.Errorf("fixture body = %q fromCache=%v", r.Body, r.FromCache)
	}
	if _, err := c.Get(context.Background(), "https://example.com/other", 0); err == nil {
		t.Error("unmapped URL in fixture mode should error (no network)")
	}
}

func TestNon200Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(t.TempDir(), "")
	if _, err := c.Get(context.Background(), srv.URL, 0); err == nil {
		t.Error("500 should return error")
	}
}
