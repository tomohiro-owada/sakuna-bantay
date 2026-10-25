// Package site generates the static Sakuna Bantay site.
//
// Plain HTML + one CSS file, no JS framework, mobile-first, brand colors.
// Pages: index (Today + summary), alerts.html, quakes.html, taal.html,
// sources.html. All nav links are relative so the site also works opened
// locally from disk; BasePath (default /sakuna-bantay/) is used only for
// absolute references (canonical/og:url) and is configurable.
package site

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/card"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/pagasa"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/posttext"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/snapshot"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/volcano"
)

// Options controls Generate.
type Options struct {
	Snap     *snapshot.Snapshot
	CardsDir string // PNGs to copy into the site
	BrandDir string // brand/ dir (sakuna-bantay-logo.svg + favicon/)
	OutDir   string
	BasePath string // default "/sakuna-bantay/"
	GAID     string // Google Analytics measurement ID; default DefaultGAID
}

// Config is the site-wide publish config (alias of Options so both
// site.Config and site.Options expose the GAID field).
type Config = Options

// DefaultGAID is the GA4 measurement ID shared with Presyo Bantay.
const DefaultGAID = "G-VL047YCMK8"

// CanonicalBase is the public Pages URL for this project.
const CanonicalBase = "https://abalol.xyz/sakuna-bantay/"

// SourceRepo is the public source repository linked from every footer.
const SourceRepo = "https://github.com/tomohiro-owada/sakuna-bantay"

func gaID(or string) string {
	if or == "" {
		return DefaultGAID
	}
	return or
}

func base(or string) string {
	if or == "" {
		return "/sakuna-bantay/"
	}
	if !strings.HasSuffix(or, "/") {
		or += "/"
	}
	return or
}

const css = `:root{--navy:#0E2A3B;--teal:#0F6B6B;--teal-lite:#2A9D8F;--amber:#F2A516;--cream:#F7F2E6;--red:#C0392B;--muted:#3D5566}
*{box-sizing:border-box}
body{margin:0;background:var(--cream);color:var(--navy);font-family:"Noto Sans",system-ui,sans-serif;line-height:1.5}
header{background:var(--navy);color:#fff;padding:12px 16px;display:flex;align-items:center;gap:12px}
header img.logo{height:44px;width:auto}
header .t{font-family:Montserrat,"Noto Sans",sans-serif;font-weight:800;font-size:20px}
header .sub{font-size:12px;opacity:.85}
nav{background:var(--teal);padding:8px 16px;display:flex;gap:12px;flex-wrap:wrap}
nav a{color:#fff;text-decoration:none;font-weight:600;font-size:14px}
main{max-width:860px;margin:0 auto;padding:16px}
.card{background:#fff;border:2px solid var(--teal);border-radius:12px;padding:16px;margin:16px 0}
.card img{max-width:100%;height:auto;border-radius:8px}
.badge{display:inline-block;background:var(--red);color:#fff;border-radius:8px;padding:2px 10px;font-weight:700;font-size:13px}
.badge.live{background:var(--teal)}
table{border-collapse:collapse;width:100%;font-size:14px}
th,td{text-align:left;padding:6px 8px;border-bottom:1px solid #d8cfba}
th{background:var(--navy);color:#fff}
tr.notable{background:#fdeeda}
.disclaimer{background:#fff8e6;border:2px solid var(--amber);border-radius:12px;padding:12px 16px;margin:16px 0;font-weight:600}
footer{padding:16px;font-size:12px;color:var(--muted);text-align:center}
.lang{color:var(--teal);font-weight:700;font-size:12px}
`

func esc(s string) string { return html.EscapeString(s) }

func header(logo, title, sub string) string {
	// Header uses the mark only (favicon/favicon.svg): the full lockup
	// sakuna-bantay-logo.svg carries navy wordmark text that is invisible
	// on the navy header and would duplicate the text title next to it.
	mark := "favicon/favicon.svg"
	return `<header><img class="logo" src="` + esc(mark) + `" alt="Sakuna Bantay mark">` +
		`<div><div class="t">` + esc(title) + `</div><div class="sub">` + esc(sub) + `</div></div></header>` +
		`<nav><a href="index.html">Today · Ngayon · 今日</a>` +
		`<a href="alerts.html">Alerts · Alerto · 警報</a>` +
		`<a href="quakes.html">Quakes · Lindol · 地震</a>` +
		`<a href="taal.html">Taal · タール</a>` +
		`<a href="sources.html">Sources · Pinagmulan · 出典</a></nav>`
}

func footer(snap *snapshot.Snapshot, bp string) string {
	gen := snap.GeneratedAt.Format("2 Jan 2006, 3:04 PM PHT")
	_ = bp // base path kept for compatibility; public link uses CanonicalBase
	// The Generated line carries data-volatile on its own line so the
	// publisher can strip it (sed /data-volatile/d) when hashing HTML for
	// change detection; card PNGs use stable names so links don't churn.
	return `<div class="disclaimer">Unofficial, not an official source. For warnings follow DOST-PAGASA, PHIVOLCS, NDRRMC and your LGU.<br>` +
		`<span class="lang">Hindi opisyal na source.</span> Para sa babala, sundin ang DOST-PAGASA, PHIVOLCS, NDRRMC at ang inyong LGU.<br>` +
		`<span class="lang">非公式の情報源です。</span>警報についてはPAGASA・PHIVOLCS・NDRRMC・自治体に従ってください。</div>` +
		`<footer>` + "\n" +
		`<span data-volatile>Generated ` + esc(gen) + `</span>` + "\n" +
		` · <a href="` + CanonicalBase + `">abalol.xyz/sakuna-bantay</a> · ` +
		`Unofficial · Non-profit · Open data<br>` +
		`<span>Gumagamit ang site na ito ng Google Analytics para sa bilang ng bisita.<br>` +
		`This site uses Google Analytics to count visits.<br>` +
		`アクセス解析にGoogleアナリティクスを使っています。</span><br>` +
		`<a href="` + SourceRepo + `">Source code</a></footer>`
}

func gaHead(gaid string) string {
	g := esc(gaID(gaid))
	return `<script async src="https://www.googletagmanager.com/gtag/js?id=` + g + `"></script>` + "\n" +
		`<script>window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}gtag('js',new Date());gtag('config','` + g + `');</script>`
}

func page(title, sub, body, foot, gaid, canonical, ogURL, ogImage, ogDesc string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + esc(title) + ` — Sakuna Bantay</title>
<meta name="description" content="` + esc(ogDesc) + `">
<link rel="canonical" href="` + esc(canonical) + `">
<meta property="og:title" content="` + esc(title) + ` — Sakuna Bantay">
<meta property="og:description" content="` + esc(ogDesc) + `">
<meta property="og:type" content="website">
<meta property="og:url" content="` + esc(ogURL) + `">
<meta property="og:image" content="` + esc(ogImage) + `">
<link rel="stylesheet" href="style.css">
<link rel="icon" href="favicon/favicon.svg" type="image/svg+xml">
<link rel="icon" href="favicon/favicon-32.png" sizes="32x32" type="image/png">
<link rel="icon" href="favicon/favicon-16.png" sizes="16x16" type="image/png">
<link rel="apple-touch-icon" href="favicon/apple-touch-icon-180.png">
` + gaHead(gaid) + `
</head>
<body>
` + header("sakuna-bantay-logo.svg", "Sakuna Bantay", sub) + `
<main>
<h1>` + esc(title) + `</h1>
` + body + `
</main>
` + foot + `
</body>
</html>
`
}

// Generate builds the static site.
//
// The site is built into a temp dir next to OutDir and then atomically
// swapped into place (rename), so OutDir contains only files from the
// current build — stale files from older builds never survive.
func Generate(o Options) error {
	if o.OutDir == "" {
		return fmt.Errorf("site: empty out dir")
	}
	clean := filepath.Clean(o.OutDir)
	parent := filepath.Dir(clean)
	if parent != "" && parent != "." {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	} else {
		parent = "."
		if clean == "." {
			return fmt.Errorf("site: empty out dir")
		}
	}
	tmp, err := os.MkdirTemp(parent, ".site-tmp-*")
	if err != nil {
		return err
	}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(tmp)
		}
	}()
	oc := o
	oc.OutDir = tmp
	if err := buildInto(oc); err != nil {
		return err
	}
	// Atomic swap: tmp -> clean. os.Rename cannot replace a non-empty
	// dir, so move the old dir aside first when it exists.
	if _, err := os.Lstat(clean); os.IsNotExist(err) {
		if err := os.Rename(tmp, clean); err != nil {
			return err
		}
	} else {
		backup := fmt.Sprintf("%s.backup-%d", clean, time.Now().UnixNano())
		for i := 0; i < 10; i++ {
			if _, err := os.Lstat(backup); os.IsNotExist(err) {
				break
			}
			backup = fmt.Sprintf("%s.backup-%d-%d", clean, time.Now().UnixNano(), i)
		}
		if err := os.Rename(clean, backup); err != nil {
			return err
		}
		if err := os.Rename(tmp, clean); err != nil {
			// Best-effort restore.
			_ = os.Rename(backup, clean)
			return err
		}
		_ = os.RemoveAll(backup)
	}
	failed = false
	return nil
}

// writeBrandAssets copies the brand logo + favicons into dst. BrandDir is
// tried first; any missing piece falls back to the embedded copies so a
// server without a brand/ directory still ships logo + favicons.
func writeBrandAssets(dst, brandDir string) error {
	logoWritten := false
	favWritten := false
	if brandDir != "" {
		if data, err := os.ReadFile(filepath.Join(brandDir, "sakuna-bantay-logo.svg")); err == nil {
			if err := os.WriteFile(filepath.Join(dst, "sakuna-bantay-logo.svg"), data, 0o644); err != nil {
				return err
			}
			logoWritten = true
		}
		favSrc := filepath.Join(brandDir, "favicon")
		favDst := filepath.Join(dst, "favicon")
		if st, err := os.Stat(favSrc); err == nil && st.IsDir() {
			if ents, err := os.ReadDir(favSrc); err == nil {
				n := 0
				for _, e := range ents {
					if e.IsDir() {
						continue
					}
					b, err := os.ReadFile(filepath.Join(favSrc, e.Name()))
					if err != nil {
						continue
					}
					if n == 0 {
						if err := os.MkdirAll(favDst, 0o755); err != nil {
							return err
						}
					}
					if err := os.WriteFile(filepath.Join(favDst, e.Name()), b, 0o644); err != nil {
						return err
					}
					n++
				}
				if n > 0 {
					favWritten = true
				}
			}
		}
	}
	if !logoWritten {
		data, err := embeddedLogo()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, "sakuna-bantay-logo.svg"), data, 0o644); err != nil {
			return err
		}
	}
	if !favWritten {
		favs, err := embeddedFavicons()
		if err != nil {
			return err
		}
		favDst := filepath.Join(dst, "favicon")
		if err := os.MkdirAll(favDst, 0o755); err != nil {
			return err
		}
		for name, b := range favs {
			if err := os.WriteFile(filepath.Join(favDst, name), b, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildInto(o Options) error {
	bp := base(o.BasePath)
	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		return err
	}
	// style.css
	if err := os.WriteFile(filepath.Join(o.OutDir, "style.css"), []byte(css), 0o644); err != nil {
		return err
	}
	// brand logo + favicons (disk first, embedded fallback).
	if err := writeBrandAssets(o.OutDir, o.BrandDir); err != nil {
		return err
	}
	// Card PNGs: copy ONLY the images actually referenced by pages, under
	// stable names so links don't churn. Synthetic SAMPLE cards (any
	// *-synth*.png) are never published to the public site.
	var pngs []string
	if o.CardsDir != "" {
		ents, _ := os.ReadDir(o.CardsDir)
		var names []string
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".png") {
				continue
			}
			names = append(names, e.Name())
		}
		sort.Strings(names)
		pick := func(prefix string) string {
			for _, n := range names {
				lower := strings.ToLower(n)
				if strings.Contains(lower, "synth") {
					continue
				}
				if strings.HasPrefix(lower, prefix) && strings.HasSuffix(lower, ".png") {
					return n
				}
			}
			return ""
		}
		if src := pick("daily-"); src != "" {
			if b, err := os.ReadFile(filepath.Join(o.CardsDir, src)); err == nil {
				_ = os.WriteFile(filepath.Join(o.OutDir, "today.png"), b, 0o644)
				pngs = append(pngs, "today.png")
			}
		}
		if src := pick("taal-"); src != "" {
			if b, err := os.ReadFile(filepath.Join(o.CardsDir, src)); err == nil {
				_ = os.WriteFile(filepath.Join(o.OutDir, "taal.png"), b, 0o644)
				pngs = append(pngs, "taal.png")
			}
		}
		// NOTE: quake-latest.png would go here if a quake card were shown;
		// the quakes page is currently a table, so no quake image is
		// referenced and none is copied.
		sort.Strings(pngs)
	}
	foot := footer(o.Snap, bp)
	manila, _ := time.LoadLocation("Asia/Manila")
	gaid := gaID(o.GAID)

	// Today card drives og:image on every page (stable name).
	daily := ""
	for _, p := range pngs {
		if p == "today.png" {
			daily = p
			break
		}
	}
	ogImage := CanonicalBase + daily
	if daily == "" {
		ogImage = CanonicalBase
	}

	mkpage := func(file, title, sub, desc, body string) error {
		canonical := CanonicalBase
		if file != "index.html" {
			canonical = CanonicalBase + file
		}
		return os.WriteFile(filepath.Join(o.OutDir, file),
			[]byte(page(title, sub, body, foot, gaid, canonical, canonical, ogImage, desc)), 0o644)
	}

	// --- index ---
	todayBody := todayHTML(o.Snap, pngs, manila)
	if err := mkpage("index.html", "Today · Las Piñas", "Weather & disaster watch · Las Piñas",
		"Sakuna Bantay — unofficial weather & disaster watch for Las Piñas from official/open data.", todayBody); err != nil {
		return err
	}
	// --- alerts ---
	alertsBody := alertsHTML(o.Snap, manila)
	if err := mkpage("alerts.html", "Alerts", "PAGASA alerts · Alerto · 警報",
		"Active PAGASA wind, rain and tsunami alerts for the Sakuna Bantay watch area.", alertsBody); err != nil {
		return err
	}
	// --- quakes ---
	quakesBody := quakesHTML(o.Snap)
	if err := mkpage("quakes.html", "Quakes", "Earthquakes · Lindol · 地震",
		"Latest earthquakes near Las Piñas from PHIVOLCS and USGS.", quakesBody); err != nil {
		return err
	}
	// --- taal ---
	taalBody := taalHTML(o.Snap, pngs)
	if err := mkpage("taal.html", "Taal", "Taal Volcano · Bulkang Taal · タール火山",
		"Taal Volcano alert level and latest bulletin.", taalBody); err != nil {
		return err
	}
	// --- sources ---
	sourcesBody := sourcesHTML()
	if err := mkpage("sources.html", "Sources & licenses", "Pinagmulan · 出典",
		"Data sources and licenses for Sakuna Bantay.", sourcesBody); err != nil {
		return err
	}
	return nil
}

func phtStr(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "—"
	}
	if loc != nil {
		t = t.In(loc)
	}
	return t.Format("2 Jan 2006, 3:04 PM PHT")
}

// siteNow is the reference time for window/active filtering: the snapshot
// fetch time (GeneratedAt), falling back to now.
func siteNow(snap *snapshot.Snapshot) time.Time {
	if snap != nil && !snap.GeneratedAt.IsZero() {
		return snap.GeneratedAt
	}
	return time.Now()
}

// activeAlertsForSite returns the ACTIVE-only, de-duplicated alerts for
// rendering: expired and superseded items are dropped (via
// pagasa.ActiveSet at the snapshot time), and repeated identifiers (as in
// fixture builds mapping many feed entries to one CAP) collapse to one.
func activeAlertsForSite(snap *snapshot.Snapshot) []*pagasa.Alert {
	if snap == nil {
		return nil
	}
	now := siteNow(snap)
	filtered := pagasa.ActiveSet(snap.Pagasa.Active, now)
	seen := map[string]bool{}
	var out []*pagasa.Alert
	for _, a := range filtered {
		if a == nil {
			continue
		}
		if seen[a.Identifier] {
			continue
		}
		seen[a.Identifier] = true
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sent.After(out[j].Sent) })
	return out
}

// alertAreasText joins areaDesc values, truncated to at most maxAreas with
// a (+N more) suffix; empty areas render as "—".
func alertAreasText(a *pagasa.Alert, maxAreas int) string {
	if a == nil || len(a.Areas) == 0 {
		return "—"
	}
	var descs []string
	for _, ar := range a.Areas {
		if ar.Desc == "" {
			continue
		}
		descs = append(descs, ar.Desc)
	}
	if len(descs) == 0 {
		return "—"
	}
	if maxAreas > 0 && len(descs) > maxAreas {
		extra := len(descs) - maxAreas
		return strings.Join(descs[:maxAreas], "; ") + fmt.Sprintf(" (+%d more)", extra)
	}
	return strings.Join(descs, "; ")
}

// capLink returns the CAP URL for an alert (SourceURL, falling back to Web).
func capLink(a *pagasa.Alert) string {
	if a == nil {
		return ""
	}
	if a.SourceURL != "" {
		return a.SourceURL
	}
	return a.Web
}

// taalLevelForSite returns the Taal level + canonical description, derived
// from snapshot.taal.level via volcano.LevelDescription (not the stored
// LevelText, which may be stale).
func taalLevelForSite(snap *snapshot.Snapshot) (int, string, bool) {
	if snap == nil || snap.Taal == nil {
		return 0, "", false
	}
	lvl := snap.Taal.Taal.Level
	return lvl, volcano.LevelDescription(lvl), true
}

func todayHTML(snap *snapshot.Snapshot, pngs []string, loc *time.Location) string {
	var b strings.Builder
	// daily card image (stable name today.png)
	daily := ""
	for _, p := range pngs {
		if p == "today.png" {
			daily = p
			break
		}
	}
	if daily != "" {
		fmt.Fprintf(&b, `<div class="card"><img src="%s" alt="Daily Las Piñas card"></div>`+"\n", esc(daily))
	}
	if snap.Today != nil {
		t := snap.Today
		// SAME values as the card/posttext inputs: rain is the max over
		// the displayed window (card.DailyDisplay with the snapshot time),
		// wording uses the shared <20/20–49/>=50 thresholds
		// (posttext.RainLines); AQI is the daily max labelled as a model
		// estimate; Taal level comes from snapshot.taal.level with
		// volcano.LevelDescription.
		_, _, windowMax := card.DailyDisplay(*t, siteNow(snap))
		rainTL, rainEN, rainJA := posttext.RainLines(windowMax)
		uv := t.UVCategory
		if uv == "" {
			uv = "—"
		}
		aqiCat := t.AQICategory
		if aqiCat == "" {
			aqiCat = "—"
		}
		fmt.Fprintf(&b, `<div class="card"><h2>Las Piñas — %s</h2>
<p><span class="lang">Taglish:</span> Init hanggang %.0f°C, feels like %.0f°C. %s</p>
<p><span class="lang">EN:</span> Up to %.0f°C, feels like %.0f°C. %s UV %s (%.1f). AQI (US, max) %d (%s · model estimate).</p>
<p><span class="lang">JP:</span> 最高%.0f°C、体感%.0f°C。%s UV %s、AQI（最大）%d。</p>`+"\n",
			esc(t.Date), t.TMax, t.ApparentMax, esc(rainTL),
			t.TMax, t.ApparentMax, esc(rainEN), esc(uv), t.UVMax, t.AQIMax, esc(aqiCat),
			t.TMax, t.ApparentMax, esc(rainJA), esc(uv), t.AQIMax)
		if lvl, desc, ok := taalLevelForSite(snap); ok {
			fmt.Fprintf(&b, "<p>Taal Alert Level %d · %s</p>\n<p>%s</p></div>\n",
				lvl, esc(desc), esc(snap.Pagasa.WatchSummary))
		} else {
			fmt.Fprintf(&b, "<p>Taal status unavailable.</p>\n<p>%s</p></div>\n",
				esc(snap.Pagasa.WatchSummary))
		}
	}
	alerts := activeAlertsForSite(snap)
	if len(alerts) > 0 {
		b.WriteString(`<div class="card"><h2>Active alerts</h2><ul>`)
		for _, a := range alerts {
			title := a.Headline
			if title == "" {
				title = a.Event
			}
			if title == "" {
				title = "Weather alert"
			}
			link := capLink(a)
			if link != "" {
				fmt.Fprintf(&b, `<li><a href="%s">%s</a> — %s</li>`, esc(link), esc(title), esc(a.Event))
			} else {
				fmt.Fprintf(&b, "<li>%s — %s</li>", esc(title), esc(a.Event))
			}
		}
		b.WriteString(`</ul><p><a href="alerts.html">All alerts · Lahat ng alerto · 警報一覧</a></p></div>`)
	} else {
		b.WriteString(`<div class="card"><p>Walang aktibong babala ng PAGASA para sa NCR area.<br>No active PAGASA warnings for NCR area.<br>NCR周辺にPAGASAの有効な警報はありません。</p></div>`)
	}
	return b.String()
}

func alertsHTML(snap *snapshot.Snapshot, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="card"><p>Watch area: %s. Max signal over watch area: %d.</p></div>`+"\n",
		esc(snap.Pagasa.WatchSummary), snap.Pagasa.WatchMax)
	if snap.Pagasa.CycloneName != "" {
		fmt.Fprintf(&b, `<div class="card"><h2>%s</h2><p>Issued %s</p></div>`+"\n",
			esc(snap.Pagasa.CycloneName), esc(snap.Pagasa.CycloneIssued))
	}
	if len(snap.Pagasa.ProvinceSignals) > 0 {
		b.WriteString(`<div class="card"><h2>Wind signals by province</h2><table><tr><th>Province</th><th>Signal</th></tr>`)
		names := make([]string, 0, len(snap.Pagasa.ProvinceSignals))
		for k := range snap.Pagasa.ProvinceSignals {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>#%d</td></tr>", esc(n), snap.Pagasa.ProvinceSignals[n])
		}
		b.WriteString(`</table></div>`)
	}
	alerts := activeAlertsForSite(snap)
	var watch, other []*pagasa.Alert
	for _, a := range alerts {
		if a.InWatchArea() {
			watch = append(watch, a)
		} else {
			other = append(other, a)
		}
	}
	if len(alerts) == 0 {
		b.WriteString(`<div class="card"><p>No active alerts.</p></div>`)
		return b.String()
	}
	b.WriteString(`<div class="card"><h2>Para sa NCR area / For the NCR area</h2>`)
	if len(watch) == 0 {
		b.WriteString(`<p>Walang aktibong babala ng PAGASA para sa NCR area.<br>No active PAGASA warnings for NCR area.<br>NCR周辺にPAGASAの有効な警報はありません。</p></div>`)
	} else {
		b.WriteString(`</div>`)
		for _, a := range watch {
			b.WriteString(alertCardHTML(a, loc, true))
		}
	}
	if len(other) > 0 {
		b.WriteString(`<div class="card"><h2>Iba pang lugar / Elsewhere in PH</h2></div>`)
		for _, a := range other {
			b.WriteString(alertCardHTML(a, loc, false))
		}
	}
	return b.String()
}

// alertCardHTML renders one ACTIVE alert with headline/event, areas,
// severity, issued/expires PHT and a CAP link.
func alertCardHTML(a *pagasa.Alert, loc *time.Location, inWatch bool) string {
	title := a.Headline
	if title == "" {
		title = a.Event
	}
	if title == "" {
		title = "Weather alert"
	}
	event := a.Event
	if event == "" {
		event = "—"
	}
	sev := a.Severity
	if sev == "" {
		sev = "—"
	}
	watch := "outside watch area"
	if inWatch {
		watch = "in watch area"
	}
	detail := fmt.Sprintf("Event: %s · Severity: %s", event, sev)
	if sig := a.Signal(); sig > 0 {
		detail += fmt.Sprintf(" · Signal #%d", sig)
	}
	if rain := a.RainfallColor(); rain != "" {
		detail += fmt.Sprintf(" · Rain %s", rain)
	}
	detail += " · " + watch
	var link string
	if u := capLink(a); u != "" {
		link = fmt.Sprintf(`<p><a href="%s">CAP source · CAP pinagmulan · CAPソース</a></p>`, esc(u))
	}
	return fmt.Sprintf(`<div class="card"><h3>%s</h3>
<p>%s</p>
<p>Issued %s · Expires %s</p>
<p>Areas: %s</p>
%s</div>`+"\n",
		esc(title), esc(detail),
		esc(phtStr(a.Sent, loc)), esc(phtStr(a.Expires, loc)),
		esc(alertAreasText(a, 8)), link)
}

func quakesHTML(snap *snapshot.Snapshot) string {
	var b strings.Builder
	n := len(snap.Quakes)
	if n > 20 {
		n = 20
	}
	fmt.Fprintf(&b, `<div class="card"><p>Latest %d events (distance from Las Piñas). Source: %s.</p></div>`+"\n", n, esc(snap.QuakesSource))
	b.WriteString(`<div class="card"><table><tr><th>Time (PHT)</th><th>M</th><th>Place</th><th>Dist</th><th>Src</th></tr>`)
	for _, q := range snap.Quakes[:n] {
		cls := ""
		star := ""
		if q.Notable {
			cls = ` class="notable"`
			star = " ★"
		}
		fmt.Fprintf(&b, `<tr%s><td>%s</td><td>M%.1f%s</td><td>%s</td><td>%.0f km %s</td><td>%s</td></tr>`+"\n",
			cls, esc(q.Time.Format("2 Jan 15:04")), q.Mag, star, esc(q.Place), q.DistanceKm, esc(q.Compass), esc(q.Source))
	}
	b.WriteString(`</table></div>`)
	return b.String()
}

func taalHTML(snap *snapshot.Snapshot, pngs []string) string {
	var b strings.Builder
	taal := ""
	for _, p := range pngs {
		if p == "taal.png" {
			taal = p
			break
		}
	}
	if taal != "" {
		fmt.Fprintf(&b, `<div class="card"><img src="%s" alt="Taal gauge card"></div>`+"\n", esc(taal))
	}
	if snap.Taal != nil {
		fmt.Fprintf(&b, `<div class="card"><h2>Alert Level %d — %s</h2><p>%s</p><p><a href="%s">Bulletin %s</a></p></div>`+"\n",
			snap.Taal.Taal.Level, esc(snap.Taal.Taal.LevelText),
			esc(snap.Taal.Taal.BulletinTitle), esc(snap.Taal.Taal.BulletinURL), esc(snap.Taal.Taal.BulletinID))
	} else {
		b.WriteString(`<div class="card"><p>Taal status unavailable.</p></div>`)
	}
	return b.String()
}

func sourcesHTML() string {
	return `<div class="card"><h2>Sources &amp; licenses</h2><ul>
<li>DOST-PAGASA CAP alerts — CC BY 4.0</li>
<li>PHIVOLCS earthquake information — PHIVOLCS</li>
<li>Open-Meteo forecast + air quality — CC BY 4.0 (AQI labelled "model estimate")</li>
<li>USGS earthquake feed (backup) — public domain</li>
<li>geoBoundaries ADM2 — CC BY 3.0 IGO</li>
<li>Natural Earth land — public domain</li>
<li>Noto fonts — SIL Open Font License (OFL)</li>
</ul></div>
<div class="disclaimer">Unofficial, not an official source. For warnings follow DOST-PAGASA, PHIVOLCS, NDRRMC and your LGU.</div>`
}
