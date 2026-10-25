// Package fetch provides a shared polite HTTP client for Sakuna Bantay.
//
// Hard rules (SPEC): User-Agent "SakunaBantay/0.1
// (+https://abalol.xyz/sakuna-bantay/; non-profit)", 20s timeouts, at most
// 1 request per host per second, on-disk cache in cache/ keyed by URL
// storing body + ETag + Last-Modified + fetched-at, conditional GET
// (If-None-Match / If-Modified-Since), 304 reuses the cached body.
// Min refetch interval per source is configurable (e.g. PHIVOLCS 5 min).
// Fixture mode: when FixturesDir is set, reads from files instead of
// network (for tests/offline) via an explicit URL->filename map.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UserAgent is the polite UA required by the spec.
const UserAgent = "SakunaBantay/0.1 (+https://abalol.xyz/sakuna-bantay/; non-profit)"

// DefaultTimeout is the per-request timeout (20s per spec).
const DefaultTimeout = 20 * time.Second

// meta is the on-disk sidecar for a cached body.
type meta struct {
	URL          string `json:"url"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	FetchedAt    string `json:"fetched_at"`
}

// Result describes where a Get response came from.
type Result struct {
	Body        []byte
	FromCache   bool // true if no network was used (fresh cache or fixture)
	Revalidated bool // true if server answered 304
	StatusCode  int  // origin status (200, 304, or 0 for cache/fixture)
	URL         string
}

// Client is a polite cached HTTP client.
type Client struct {
	HTTP        *http.Client
	CacheDir    string
	FixturesDir string
	// Fixtures maps a full URL to a fixture filename relative to FixturesDir.
	// When FixturesDir != "", only mapped URLs are served; anything else
	// returns an error (no network in fixture mode).
	Fixtures map[string]string

	mu   sync.Mutex
	last map[string]time.Time // host -> last request time
	now  func() time.Time
}

// New creates a Client. cacheDir may be "" (no disk cache). fixturesDir may
// be "" (live network). Use NewFixture for tests.
func New(cacheDir, fixturesDir string) *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: DefaultTimeout},
		CacheDir:    cacheDir,
		FixturesDir: fixturesDir,
		Fixtures:    map[string]string{},
		last:        map[string]time.Time{},
		now:         time.Now,
	}
}

// NewFixture creates a fixture-only client for tests.
func NewFixture(dir string, mapping map[string]string) *Client {
	c := New("", dir)
	for k, v := range mapping {
		c.Fixtures[k] = v
	}
	return c
}

func cacheKey(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:])
}

func (c *Client) bodyPath(rawURL string) string {
	return filepath.Join(c.CacheDir, cacheKey(rawURL)+".body")
}

func (c *Client) metaPath(rawURL string) string {
	return filepath.Join(c.CacheDir, cacheKey(rawURL)+".meta.json")
}

func (c *Client) readCache(rawURL string) ([]byte, *meta) {
	if c.CacheDir == "" {
		return nil, nil
	}
	body, err := os.ReadFile(c.bodyPath(rawURL))
	if err != nil {
		return nil, nil
	}
	mb, err := os.ReadFile(c.metaPath(rawURL))
	if err != nil {
		return body, &meta{URL: rawURL}
	}
	var m meta
	if err := json.Unmarshal(mb, &m); err != nil {
		return body, &meta{URL: rawURL}
	}
	return body, &m
}

func (c *Client) writeCache(rawURL string, body []byte, m *meta) error {
	if c.CacheDir == "" {
		return nil
	}
	if err := os.MkdirAll(c.CacheDir, 0o755); err != nil {
		return err
	}
	m.URL = rawURL
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.bodyPath(rawURL), body, 0o644); err != nil {
		return err
	}
	return os.WriteFile(c.metaPath(rawURL), mb, 0o644)
}

// throttle enforces at most 1 request per host per second.
func (c *Client) throttle(ctx context.Context, host string) error {
	c.mu.Lock()
	last, ok := c.last[host]
	now := c.now()
	if ok {
		if d := now.Sub(last); d < time.Second {
			wait := time.Second - d
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			c.mu.Lock()
		}
	}
	c.last[host] = c.now()
	c.mu.Unlock()
	return nil
}

// Get fetches rawURL with cache + conditional GET.
// If minRefetch > 0 and the cached copy is fresher than minRefetch, the
// cached body is returned without network. In fixture mode the mapped
// fixture file is returned (also written to... no, fixtures bypass cache).
func (c *Client) Get(ctx context.Context, rawURL string, minRefetch time.Duration) (*Result, error) {
	// Fixture mode: no network.
	if c.FixturesDir != "" {
		name, ok := c.Fixtures[rawURL]
		if !ok {
			return nil, fmt.Errorf("fetch: no fixture mapped for %s (fixture mode, no network)", rawURL)
		}
		b, err := os.ReadFile(filepath.Join(c.FixturesDir, name))
		if err != nil {
			return nil, fmt.Errorf("fetch: read fixture %s: %w", name, err)
		}
		return &Result{Body: b, FromCache: true, URL: rawURL}, nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch: bad url %q: %w", rawURL, err)
	}

	cached, m := c.readCache(rawURL)
	if m != nil && minRefetch > 0 && cached != nil {
		if t, err := time.Parse(time.RFC3339, m.FetchedAt); err == nil {
			if c.now().Sub(t) < minRefetch {
				return &Result{Body: cached, FromCache: true, StatusCode: 200, URL: rawURL}, nil
			}
		}
	}

	if err := c.throttle(ctx, u.Host); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "*/*")
	if m != nil {
		if m.ETag != "" {
			req.Header.Set("If-None-Match", m.ETag)
		}
		if m.LastModified != "" {
			req.Header.Set("If-Modified-Since", m.LastModified)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if cached != nil {
			return nil, fmt.Errorf("fetch: %s: %w (have stale cache)", rawURL, err)
		}
		return nil, fmt.Errorf("fetch: %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && cached != nil {
		nm := &meta{ETag: m.ETag, LastModified: m.LastModified, FetchedAt: c.now().Format(time.RFC3339)}
		_ = c.writeCache(rawURL, cached, nm)
		return &Result{Body: cached, Revalidated: true, FromCache: true, StatusCode: 304, URL: rawURL}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch: %s: status %s", rawURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("fetch: read %s: %w", rawURL, err)
	}
	nm := &meta{
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		FetchedAt:    c.now().Format(time.RFC3339),
	}
	_ = c.writeCache(rawURL, body, nm)
	return &Result{Body: body, StatusCode: 200, URL: rawURL}, nil
}
