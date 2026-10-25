// Package fbpost posts Sakuna Bantay cards to Facebook (Graph API v21.0)
// or, by default, to a local outbox (dry-run).
//
// DEFAULT is dry-run: only `-live` (plus a usable fb.env and no kill
// switch) can post. The Poster interface separates DryRunPoster (writes
// PNG + caption + meta files to outbox/, never touches the network) from
// GraphPoster (HTTPS multipart POST). The choice is made by exactly one
// function (ChoosePoster), covered by tests — including a test that the
// whole `run` pipeline without -live never calls the HTTP transport.
package fbpost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Graph API constants.
const (
	GraphVersion   = "v21.0"
	DefaultBaseURL = "https://graph.facebook.com"
	// Timeout is the per-request HTTP timeout.
	Timeout = 60 * time.Second
	// RetryDelay is the wait before the single retry on 5xx/network errors.
	RetryDelay = 10 * time.Second
)

// Result is a successful post.
type Result struct {
	ID     string `json:"id"`
	PostID string `json:"post_id"`
}

// Poster posts one card image + trilingual caption.
type Poster interface {
	Post(ctx context.Context, png []byte, caption string) (Result, error)
	// Mode reports "live" or "dry-run" for history/state separation.
	Mode() string
}

// ---------------------------------------------------------------------------
// Mode decision (pure)
// ---------------------------------------------------------------------------

// Mode decides live vs dry-run: live only when !dryRunFlag && envFileOK &&
// !killSwitchPresent. Anything else is dry-run.
func Mode(dryRunFlag bool, envFileOK bool, killSwitchPresent bool) (live bool, reason string) {
	switch {
	case dryRunFlag:
		return false, "dry-run flag"
	case killSwitchPresent:
		return false, "dry-run (kill switch)"
	case !envFileOK:
		return false, "fb.env missing or unusable"
	default:
		return true, "live"
	}
}

// DryRun resolves the effective dry-run flag from the CLI flags. DEFAULT is
// dry-run: there is NO way that `-dry-run` or the absence of `-live` posts;
// -live + -dry-run together → dry-run wins.
func DryRun(liveFlag, dryRunFlag bool) bool {
	return !liveFlag || dryRunFlag
}

// ChoosePoster is the ONE code path that chooses the poster. livePoster is
// used only when Mode reports live; otherwise dryPoster is used.
func ChoosePoster(dryRunFlag bool, envFileOK bool, killSwitchPresent bool, livePoster, dryPoster Poster) (Poster, bool, string) {
	live, reason := Mode(dryRunFlag, envFileOK, killSwitchPresent)
	if live {
		return livePoster, true, reason
	}
	return dryPoster, false, reason
}

// ---------------------------------------------------------------------------
// Config: fb.env + kill switch paths
// ---------------------------------------------------------------------------

// DefaultEnvPath returns the default fb.env path under home.
func DefaultEnvPath(home string) string {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".config", "sakuna", "fb.env")
}

// DefaultKillSwitchPath returns the default KILL_SWITCH path under home.
func DefaultKillSwitchPath(home string) string {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".config", "sakuna", "KILL_SWITCH")
}

// Config is the parsed fb.env: page id + page token.
type Config struct {
	PageID string
	Token  string
	OK     bool
	Reason string // why not OK (never contains secrets)
}

// LoadEnv reads KEY=VALUE lines (SAKUNA_FB_PAGE_ID, SAKUNA_FB_PAGE_TOKEN).
// The file is REFUSED (treated as missing) unless its mode is exactly 0600
// and it is owned by the current user. Missing secrets are reported via
// Reason only — file contents are never logged.
func LoadEnv(path string) Config {
	st, err := os.Stat(path)
	if err != nil {
		return Config{Reason: "fb.env not present"}
	}
	if st.Mode().Perm() != 0o600 {
		log.Printf("fbpost: warning: refusing %s: mode %o, want 0600", path, st.Mode().Perm())
		return Config{Reason: "fb.env wrong mode (want 0600)"}
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		if int(sys.Uid) != os.Getuid() {
			log.Printf("fbpost: warning: refusing %s: not owned by current user", path)
			return Config{Reason: "fb.env not owned by current user"}
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("fbpost: warning: cannot read %s", path)
		return Config{Reason: "fb.env unreadable"}
	}
	vals := map[string]string{}
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(strings.Trim(strings.TrimSpace(v), `"'`))
		vals[k] = v
	}
	cfg := Config{PageID: vals["SAKUNA_FB_PAGE_ID"], Token: vals["SAKUNA_FB_PAGE_TOKEN"]}
	if cfg.PageID == "" || cfg.Token == "" {
		return Config{Reason: "fb.env missing SAKUNA_FB_PAGE_ID or SAKUNA_FB_PAGE_TOKEN"}
	}
	cfg.OK = true
	cfg.Reason = "fb.env ok"
	return cfg
}

// KillSwitchPresent reports whether the kill-switch file exists.
func KillSwitchPresent(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Redact replaces every occurrence of the token with "***" so errors and
// logs never leak it.
func Redact(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "***")
}

// ---------------------------------------------------------------------------
// DryRunPoster: outbox files, never the network
// ---------------------------------------------------------------------------

var dryRunCounter atomic.Uint64

// DryRunPoster writes each post to Dir as <stamp>.png + <stamp>.txt +
// <stamp>.json and never touches the network.
type DryRunPoster struct {
	Dir string
}

// Mode reports "dry-run".
func (d *DryRunPoster) Mode() string { return "dry-run" }

// Post writes the outbox files.
func (d *DryRunPoster) Post(ctx context.Context, png []byte, caption string) (Result, error) {
	if err := os.MkdirAll(d.Dir, 0o755); err != nil {
		return Result{}, err
	}
	base := fmt.Sprintf("%s-%04d",
		time.Now().UTC().Format("20060102T150405"), dryRunCounter.Add(1)%10000)
	if err := os.WriteFile(filepath.Join(d.Dir, base+".png"), png, 0o644); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(d.Dir, base+".txt"), []byte(caption+"\n"), 0o644); err != nil {
		return Result{}, err
	}
	meta, _ := json.MarshalIndent(map[string]any{
		"id":           "dryrun-" + base,
		"mode":         "dry-run",
		"time":         time.Now().UTC().Format(time.RFC3339),
		"caption_file": base + ".txt",
		"image_file":   base + ".png",
		"png_bytes":    len(png),
	}, "", "  ")
	meta = append(meta, '\n')
	if err := os.WriteFile(filepath.Join(d.Dir, base+".json"), meta, 0o644); err != nil {
		return Result{}, err
	}
	return Result{ID: "dryrun-" + base}, nil
}

// ---------------------------------------------------------------------------
// GraphPoster: HTTPS multipart POST to the Graph API
// ---------------------------------------------------------------------------

// GraphPoster posts photos via https://graph.facebook.com/v21.0/{page}/photos.
type GraphPoster struct {
	PageID  string
	Token   string
	BaseURL string // default DefaultBaseURL (override in tests)
	Client  *http.Client
	Sleep   func(time.Duration) // default time.Sleep (stubbed in tests)
}

func (g *GraphPoster) base() string {
	if g.BaseURL != "" {
		return g.BaseURL
	}
	return DefaultBaseURL
}

func (g *GraphPoster) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return &http.Client{Timeout: Timeout}
}

func (g *GraphPoster) sleep(d time.Duration) {
	if g.Sleep != nil {
		g.Sleep(d)
		return
	}
	time.Sleep(d)
}

// Mode reports "live".
func (g *GraphPoster) Mode() string { return "live" }

type graphOK struct {
	ID     string `json:"id"`
	PostID string `json:"post_id"`
}

type graphErrDetail struct {
	Message   string `json:"message"`
	Type      string `json:"type"`
	Code      int    `json:"code"`
	FbtraceID string `json:"fbtrace_id"`
}

type graphErrResp struct {
	Error *graphErrDetail `json:"error"`
}

// Post uploads one photo. The access token travels as a multipart form field
// only — never in the URL, never in logs. One retry on 5xx/network errors
// after RetryDelay; no retry on 4xx. Returned errors have the token redacted.
func (g *GraphPoster) Post(ctx context.Context, png []byte, caption string) (Result, error) {
	endpoint := g.base() + "/" + GraphVersion + "/" + g.PageID + "/photos"
	res, code, netErr, err := g.attempt(ctx, endpoint, png, caption)
	if err != nil {
		if netErr || code >= 500 {
			g.sleep(RetryDelay)
			res, _, _, err2 := g.attempt(ctx, endpoint, png, caption)
			if err2 != nil {
				return Result{}, fmt.Errorf("fbpost: %s", Redact(err2.Error(), g.Token))
			}
			return res, nil
		}
		return Result{}, fmt.Errorf("fbpost: %s", Redact(err.Error(), g.Token))
	}
	return res, nil
}

// attempt performs a single upload. It returns the result, the HTTP status
// (0 on network error), whether the failure was a network error, and the
// (unredacted — redacted by the caller) error.
func (g *GraphPoster) attempt(ctx context.Context, endpoint string, png []byte, caption string) (Result, int, bool, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="source"; filename="card.png"`)
	h.Set("Content-Type", "image/png")
	fw, err := w.CreatePart(h)
	if err != nil {
		return Result{}, 0, false, err
	}
	if _, err := fw.Write(png); err != nil {
		return Result{}, 0, false, err
	}
	if err := w.WriteField("caption", caption); err != nil {
		return Result{}, 0, false, err
	}
	if err := w.WriteField("published", "true"); err != nil {
		return Result{}, 0, false, err
	}
	if err := w.WriteField("access_token", g.Token); err != nil {
		return Result{}, 0, false, err
	}
	if err := w.Close(); err != nil {
		return Result{}, 0, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return Result{}, 0, false, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", "SakunaBantay/0.1 (+https://abalol.xyz/sakuna-bantay/; non-profit)")
	resp, err := g.client().Do(req)
	if err != nil {
		return Result{}, 0, true, fmt.Errorf("post %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, resp.StatusCode, resp.StatusCode >= 500, fmt.Errorf("post %s: read: %w", endpoint, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, resp.StatusCode, false, graphError(endpoint, resp.StatusCode, respBody)
	}
	var ok graphOK
	if err := json.Unmarshal(respBody, &ok); err != nil || ok.ID == "" {
		var ge graphErrResp
		if jerr := json.Unmarshal(respBody, &ge); jerr == nil && ge.Error != nil {
			return Result{}, resp.StatusCode, false, fmt.Errorf("post %s: graph error: %s (type %s code %d fbtrace %s)",
				endpoint, ge.Error.Message, ge.Error.Type, ge.Error.Code, ge.Error.FbtraceID)
		}
		return Result{}, resp.StatusCode, false, fmt.Errorf("post %s: bad success payload %q", endpoint, string(respBody))
	}
	// A 2xx with an error payload is still a failure.
	var ge graphErrResp
	if jerr := json.Unmarshal(respBody, &ge); jerr == nil && ge.Error != nil {
		return Result{}, resp.StatusCode, false, fmt.Errorf("post %s: graph error: %s (type %s code %d fbtrace %s)",
			endpoint, ge.Error.Message, ge.Error.Type, ge.Error.Code, ge.Error.FbtraceID)
	}
	return Result{ID: ok.ID, PostID: ok.PostID}, resp.StatusCode, false, nil
}

func graphError(endpoint string, code int, body []byte) error {
	var ge graphErrResp
	if err := json.Unmarshal(body, &ge); err == nil && ge.Error != nil {
		return fmt.Errorf("post %s: status %d: %s (type %s code %d fbtrace %s)",
			endpoint, code, ge.Error.Message, ge.Error.Type, ge.Error.Code, ge.Error.FbtraceID)
	}
	return fmt.Errorf("post %s: status %d: %q", endpoint, code, string(body))
}
