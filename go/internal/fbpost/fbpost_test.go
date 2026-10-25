package fbpost

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMode(t *testing.T) {
	cases := []struct {
		name             string
		dry, envOK, kill bool
		wantLive         bool
	}{
		{"live needs all three", false, true, false, true},
		{"dry-run flag wins", true, true, false, false},
		{"kill switch wins", false, true, true, false},
		{"missing env wins", false, false, false, false},
		{"everything off is dry-run", true, false, true, false},
	}
	for _, tc := range cases {
		live, reason := Mode(tc.dry, tc.envOK, tc.kill)
		if live != tc.wantLive {
			t.Errorf("%s: live=%v, want %v (reason %q)", tc.name, live, tc.wantLive, reason)
		}
		if reason == "" {
			t.Errorf("%s: empty reason", tc.name)
		}
	}
}

func TestDryRunDefault(t *testing.T) {
	// Absence of -live never posts; -live + -dry-run → dry-run wins.
	if !DryRun(false, false) {
		t.Error("no -live must be dry-run")
	}
	if !DryRun(false, true) {
		t.Error("no -live + -dry-run must be dry-run")
	}
	if DryRun(true, false) {
		t.Error("-live alone must not be dry-run")
	}
	if !DryRun(true, true) {
		t.Error("-live + -dry-run must be dry-run (dry-run wins)")
	}
}

func TestChoosePoster(t *testing.T) {
	live := &GraphPoster{PageID: "p", Token: "t"}
	dry := &DryRunPoster{Dir: t.TempDir()}
	// Dangerous-looking config without -live → dry poster.
	p, isLive, _ := ChoosePoster(DryRun(false, false), true, false, live, dry)
	if isLive || p.Mode() != "dry-run" {
		t.Error("without -live must choose the dry-run poster")
	}
	// -live + -dry-run → dry poster (regression guard: -auto
	// -dry-run actually posted — impossible here by construction).
	p, isLive, _ = ChoosePoster(DryRun(true, true), true, false, live, dry)
	if isLive || p.Mode() != "dry-run" {
		t.Error("-live + -dry-run must choose the dry-run poster")
	}
	// Kill switch + -live → dry poster.
	p, isLive, _ = ChoosePoster(DryRun(true, false), true, true, live, dry)
	if isLive || p.Mode() != "dry-run" {
		t.Error("kill switch must force the dry-run poster")
	}
	// -live, usable env, no kill → live poster.
	p, isLive, _ = ChoosePoster(DryRun(true, false), true, false, live, dry)
	if !isLive || p.Mode() != "live" {
		t.Error("-live with env and no kill must choose the live poster")
	}
}

func TestLoadEnv(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := "SAKUNA_FB_PAGE_ID=12345\nSAKUNA_FB_PAGE_TOKEN=s3cr3t-token\n"
	if cfg := LoadEnv(write("good.env", good, 0o600)); !cfg.OK || cfg.PageID != "12345" || cfg.Token != "s3cr3t-token" {
		t.Errorf("good env not accepted: %+v", cfg)
	}
	if cfg := LoadEnv(write("open.env", good, 0o644)); cfg.OK {
		t.Error("0600 required: 0644 file must be refused")
	}
	if cfg := LoadEnv(filepath.Join(dir, "missing.env")); cfg.OK {
		t.Error("missing file must not be OK")
	}
	if cfg := LoadEnv(write("nokeys.env", "FOO=bar\n", 0o600)); cfg.OK {
		t.Error("missing keys must not be OK")
	}
	if cfg := LoadEnv(write("empty.env", "", 0o600)); cfg.OK {
		t.Error("empty file must not be OK")
	}
}

func TestLoadEnvOwnerMustBeCurrentUID(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "owned.env")
	content := "SAKUNA_FB_PAGE_ID=12345\nSAKUNA_FB_PAGE_TOKEN=s3cr3t-token\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadEnv(p)
	if !cfg.OK {
		t.Fatalf("current-user-owned 0600 file must pass (uid=%d): %+v", os.Getuid(), cfg)
	}
}

func TestGraphPosterMultipart(t *testing.T) {
	const pageID = "999"
	const token = "test-token-abc123"
	png := []byte("\x89PNG\r\n\x1a\nfakepng-bytes")
	const caption = "Taglish line\n———\nEnglish line"
	var gotMethod, gotPath, gotQuery, gotCT string
	var gotCaption, gotPublished, gotToken, gotFilename, gotFileCT string
	var gotFile []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		gotCT = r.Header.Get("Content-Type")
		full := "http://" + r.Host + r.URL.RequestURI()
		if strings.Contains(full, token) {
			t.Errorf("token leaked into request URL %q", full)
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("not multipart: %v", err)
			return
		}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("multipart read: %v", err)
				return
			}
			b, _ := io.ReadAll(part)
			switch part.FormName() {
			case "source":
				gotFilename = part.FileName()
				gotFileCT = part.Header.Get("Content-Type")
				gotFile = b
			case "caption":
				gotCaption = string(b)
			case "published":
				gotPublished = string(b)
			case "access_token":
				gotToken = string(b)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"999_111","post_id":"999_111"}`))
	}))
	defer srv.Close()

	g := &GraphPoster{PageID: pageID, Token: token, BaseURL: srv.URL, Sleep: func(time.Duration) {}}
	res, err := g.Post(context.Background(), png, caption)
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "999_111" || res.PostID != "999_111" {
		t.Errorf("result = %+v, want ids 999_111", res)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/v21.0/"+pageID+"/photos" {
		t.Errorf("path = %s, want /v21.0/%s/photos", gotPath, pageID)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty (token never in URL)", gotQuery)
	}
	mt, _, _ := mime.ParseMediaType(gotCT)
	if !strings.HasPrefix(mt, "multipart/form-data") {
		t.Errorf("content-type = %q, want multipart", gotCT)
	}
	if string(gotFile) != string(png) {
		t.Error("source file bytes mismatch")
	}
	if gotFilename == "" {
		t.Error("source part missing filename")
	}
	if gotFileCT != "image/png" {
		t.Errorf("source content-type = %q, want image/png", gotFileCT)
	}
	if gotCaption != caption {
		t.Errorf("caption = %q, want %q", gotCaption, caption)
	}
	if gotPublished != "true" {
		t.Errorf("published = %q, want true", gotPublished)
	}
	if gotToken != token {
		t.Error("access_token field missing/wrong")
	}
}

func TestGraphPosterRedactsToken(t *testing.T) {
	const token = "super-secret-token-xyz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		// Echo the token inside the error message (nasty server).
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token ` + token + ` rejected","type":"OAuthException","code":190,"fbtrace_id":"abc"}}`))
	}))
	defer srv.Close()
	g := &GraphPoster{PageID: "1", Token: token, BaseURL: srv.URL, Sleep: func(time.Duration) {}}
	_, err := g.Post(context.Background(), []byte("png"), "cap")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("error leaks token: %v", err)
	}
	if !strings.Contains(err.Error(), "***") {
		t.Errorf("error should contain redaction marker: %v", err)
	}
}

func TestGraphPosterRetry(t *testing.T) {
	var calls atomic.Int32
	var slept atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"boom","type":"X","code":1,"fbtrace_id":"t"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"a","post_id":"b"}`))
	}))
	defer srv.Close()
	g := &GraphPoster{PageID: "1", Token: "tok", BaseURL: srv.URL,
		Sleep: func(d time.Duration) { slept.Add(int64(d)) }}
	res, err := g.Post(context.Background(), []byte("png"), "cap")
	if err != nil || res.ID != "a" {
		t.Fatalf("retry should succeed: res=%+v err=%v", res, err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2 (one retry on 5xx)", calls.Load())
	}
	if time.Duration(slept.Load()) != RetryDelay {
		t.Errorf("slept = %v, want %v", time.Duration(slept.Load()), RetryDelay)
	}

	// No retry on 4xx.
	calls.Store(0)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad","type":"OAuthException","code":100,"fbtrace_id":"t"}}`))
	}))
	defer srv2.Close()
	g2 := &GraphPoster{PageID: "1", Token: "tok", BaseURL: srv2.URL, Sleep: func(time.Duration) {}}
	if _, err := g2.Post(context.Background(), []byte("png"), "cap"); err == nil {
		t.Fatal("expected 4xx error")
	}
	if calls.Load() != 1 {
		t.Errorf("4xx calls = %d, want 1 (no retry)", calls.Load())
	}
}

func TestDryRunPosterWrites(t *testing.T) {
	dir := t.TempDir()
	d := &DryRunPoster{Dir: filepath.Join(dir, "outbox")}
	png := []byte("\x89PNGfakepng")
	res, err := d.Post(context.Background(), png, "hello caption")
	if err != nil {
		t.Fatal(err)
	}
	if res.ID == "" {
		t.Error("empty result id")
	}
	if d.Mode() != "dry-run" {
		t.Errorf("mode = %q, want dry-run", d.Mode())
	}
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		t.Fatal(err)
	}
	var hasPNG, hasTXT, hasJSON bool
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".png"):
			hasPNG = true
			b, _ := os.ReadFile(filepath.Join(d.Dir, e.Name()))
			if string(b) != string(png) {
				t.Error("outbox png bytes mismatch")
			}
		case strings.HasSuffix(e.Name(), ".txt"):
			hasTXT = true
			b, _ := os.ReadFile(filepath.Join(d.Dir, e.Name()))
			if !strings.Contains(string(b), "hello caption") {
				t.Error("outbox txt missing caption")
			}
		case strings.HasSuffix(e.Name(), ".json"):
			hasJSON = true
		}
	}
	if !hasPNG || !hasTXT || !hasJSON {
		t.Errorf("outbox files missing (png=%v txt=%v json=%v)", hasPNG, hasTXT, hasJSON)
	}
}
