// Package pagasa parses PAGASA public-alert Atom feeds and CAP 1.2 alerts.
package pagasa

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
)

// FeedURL is the PAGASA public alert Atom feed.
const FeedURL = "https://publicalert.pagasa.dost.gov.ph/feeds/"

// Product identifies the PAGASA product from the CAP URL path.
type Product string

const (
	ProductWCP   Product = "wcp" // Tropical Cyclone Warning
	ProductACP   Product = "acp" // Tropical Cyclone Alert
	ProductGFA   Product = "gfa" // General Flood Advisory
	ProductOther Product = "other"
)

// ProductFromURL maps a CAP/feed URL to its product via path segment.
func ProductFromURL(rawURL string) Product {
	u := strings.ToLower(rawURL)
	switch {
	case strings.Contains(u, "/output/wcp/"):
		return ProductWCP
	case strings.Contains(u, "/output/acp/"):
		return ProductACP
	case strings.Contains(u, "/output/gfa/"):
		return ProductGFA
	default:
		return ProductOther
	}
}

// FeedEntry is one Atom entry.
type FeedEntry struct {
	ID      string
	Title   string
	Updated time.Time
	CapURL  string
	Product Product
}

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}
type atomEntry struct {
	ID      string     `xml:"id"`
	Title   string     `xml:"title"`
	Updated string     `xml:"updated"`
	Links   []atomLink `xml:"link"`
}
type atomLink struct {
	Type string `xml:"type,attr"`
	Href string `xml:"href,attr"`
}

// ParseFeed parses the Atom feed XML.
func ParseFeed(data []byte) ([]FeedEntry, error) {
	var f atomFeed
	if err := xml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("pagasa: parse feed: %w", err)
	}
	var out []FeedEntry
	for _, e := range f.Entries {
		capURL := ""
		for _, l := range e.Links {
			if strings.EqualFold(l.Type, "application/cap+xml") {
				capURL = strings.TrimSpace(l.Href)
				break
			}
		}
		if capURL == "" {
			continue
		}
		var updated time.Time
		if e.Updated != "" {
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(e.Updated)); err == nil {
				updated = t
			}
		}
		out = append(out, FeedEntry{
			ID:      strings.TrimSpace(e.ID),
			Title:   strings.TrimSpace(e.Title),
			Updated: updated,
			CapURL:  capURL,
			Product: ProductFromURL(capURL),
		})
	}
	return out, nil
}

// Recent filters entries updated within the last maxAge relative to now.
func Recent(entries []FeedEntry, now time.Time, maxAge time.Duration) []FeedEntry {
	var out []FeedEntry
	for _, e := range entries {
		if e.Updated.IsZero() {
			continue
		}
		if now.Sub(e.Updated) <= maxAge && !e.Updated.After(now.Add(time.Minute)) {
			out = append(out, e)
		}
	}
	return out
}

// Area is one CAP <area>.
type Area struct {
	Desc     string
	Polygon  string
	Geocodes map[string]string // valueName -> value (SAME etc.)
}

// SAME returns the SAME (PSGC) geocode, or "".
func (a Area) SAME() string {
	if a.Geocodes == nil {
		return ""
	}
	return a.Geocodes["SAME"]
}

// Alert is a parsed CAP 1.2 message (single <info> supported; PAGASA uses one).
type Alert struct {
	Identifier string
	Sender     string
	Sent       time.Time
	SentRaw    string
	Status     string
	MsgType    string
	Scope      string
	References string

	Event       string
	Headline    string
	Severity    string
	Urgency     string
	Certainty   string
	Expires     time.Time
	ExpiresRaw  string
	Description string
	Instruction string
	Web         string
	Params      map[string]string
	Areas       []Area

	SourceURL string // CAP URL it was fetched from
	FeedTitle string // Atom entry title
}

type capAlert struct {
	Identifier string    `xml:"identifier"`
	Sender     string    `xml:"sender"`
	Sent       string    `xml:"sent"`
	Status     string    `xml:"status"`
	MsgType    string    `xml:"msgType"`
	Scope      string    `xml:"scope"`
	References string    `xml:"references"`
	Infos      []capInfo `xml:"info"`
}
type capInfo struct {
	Event       string     `xml:"event"`
	Headline    string     `xml:"headline"`
	Severity    string     `xml:"severity"`
	Urgency     string     `xml:"urgency"`
	Certainty   string     `xml:"certainty"`
	Expires     string     `xml:"expires"`
	Description string     `xml:"description"`
	Instruction string     `xml:"instruction"`
	Web         string     `xml:"web"`
	Params      []capParam `xml:"parameter"`
	Areas       []capArea  `xml:"area"`
}
type capParam struct {
	ValueName string `xml:"valueName"`
	Value     string `xml:"value"`
}
type capArea struct {
	Desc     string       `xml:"areaDesc"`
	Polygon  string       `xml:"polygon"`
	Geocodes []capGeocode `xml:"geocode"`
}
type capGeocode struct {
	ValueName string `xml:"valueName"`
	Value     string `xml:"value"`
}

// ParseCAP parses CAP 1.2 XML. Namespace prefixes are ignored by encoding/xml
// as long as we match local names (the default namespace is handled).
func ParseCAP(data []byte) (*Alert, error) {
	var c capAlert
	if err := xml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("pagasa: parse cap: %w", err)
	}
	if c.Identifier == "" {
		return nil, fmt.Errorf("pagasa: cap missing identifier")
	}
	a := &Alert{
		Identifier: strings.TrimSpace(c.Identifier),
		Sender:     strings.TrimSpace(c.Sender),
		SentRaw:    strings.TrimSpace(c.Sent),
		Status:     strings.TrimSpace(c.Status),
		MsgType:    strings.TrimSpace(c.MsgType),
		Scope:      strings.TrimSpace(c.Scope),
		References: strings.TrimSpace(c.References),
		Params:     map[string]string{},
	}
	if t, err := time.Parse(time.RFC3339, a.SentRaw); err == nil {
		a.Sent = t
	}
	if len(c.Infos) > 0 {
		in := c.Infos[0]
		a.Event = strings.TrimSpace(in.Event)
		a.Headline = strings.TrimSpace(in.Headline)
		a.Severity = strings.TrimSpace(in.Severity)
		a.Urgency = strings.TrimSpace(in.Urgency)
		a.Certainty = strings.TrimSpace(in.Certainty)
		a.ExpiresRaw = strings.TrimSpace(in.Expires)
		if t, err := time.Parse(time.RFC3339, a.ExpiresRaw); err == nil {
			a.Expires = t
		}
		a.Description = strings.TrimSpace(in.Description)
		a.Instruction = strings.TrimSpace(in.Instruction)
		a.Web = strings.TrimSpace(in.Web)
		for _, p := range in.Params {
			a.Params[strings.TrimSpace(p.ValueName)] = strings.TrimSpace(p.Value)
		}
		for _, ar := range in.Areas {
			area := Area{
				Desc:     strings.TrimSpace(ar.Desc),
				Polygon:  strings.TrimSpace(ar.Polygon),
				Geocodes: map[string]string{},
			}
			for _, g := range ar.Geocodes {
				area.Geocodes[strings.TrimSpace(g.ValueName)] = strings.TrimSpace(g.Value)
			}
			a.Areas = append(a.Areas, area)
		}
	}
	return a, nil
}

var signalRe = regexp.MustCompile(`(?i)signal\s*#\s*([1-5])`)
var signalNoRe = regexp.MustCompile(`(?i)signal\s*no\.?\s*([1-5])`)

// SignalLevel extracts the wind signal level (1-5) from headline/title.
// Returns 0 when absent.
func SignalLevel(headline, title string) int {
	for _, s := range []string{headline, title} {
		if m := signalRe.FindStringSubmatch(s); m != nil {
			return int(m[1][0] - '0')
		}
		if m := signalNoRe.FindStringSubmatch(s); m != nil {
			return int(m[1][0] - '0')
		}
	}
	return 0
}

// Signal returns the alert's wind signal level from its headline.
func (a *Alert) Signal() int { return SignalLevel(a.Headline, a.FeedTitle) }

var cycloneRe = regexp.MustCompile(`(?i)(Super Typhoon|Typhoon|Tropical Storm|Severe Tropical Storm|Tropical Depression)\s+[A-Za-z]+\s*(?:\([^)]+\))?`)

// CycloneName extracts e.g. "Typhoon Rosal (CHAMPI)" from a headline.
func CycloneName(headline string) string {
	return strings.TrimSpace(cycloneRe.FindString(headline))
}

// Cyclone returns the cyclone name for this alert, or "".
func (a *Alert) Cyclone() string { return CycloneName(a.Headline) }

// ReferencedIDs parses the <references> field into CAP identifiers.
// Format: "sender,identifier,sent" triples separated by spaces.
func (a *Alert) ReferencedIDs() []string {
	var out []string
	for _, part := range strings.Fields(a.References) {
		segs := strings.Split(part, ",")
		if len(segs) >= 2 && segs[1] != "" {
			out = append(out, segs[1])
		}
	}
	return out
}

// Active reports whether the alert is currently active:
// status Actual, msgType != Cancel, expires > now.
// Supersession is handled by ActiveSet, not here.
func (a *Alert) Active(now time.Time) bool {
	if !strings.EqualFold(a.Status, "Actual") {
		return false
	}
	if strings.EqualFold(a.MsgType, "Cancel") {
		return false
	}
	if a.Expires.IsZero() {
		return false
	}
	return a.Expires.After(now)
}

// ActiveSet filters to active alerts and drops superseded ones
// (an alert whose identifier is referenced by another alert in the set).
func ActiveSet(alerts []*Alert, now time.Time) []*Alert {
	superseded := map[string]bool{}
	for _, a := range alerts {
		for _, id := range a.ReferencedIDs() {
			superseded[id] = true
		}
	}
	var out []*Alert
	for _, a := range alerts {
		if !a.Active(now) {
			continue
		}
		if superseded[a.Identifier] {
			continue
		}
		out = append(out, a)
	}
	return out
}

// --- Watch area (PSGC SAME + name fallback) ---

// digitsOnly strips non-digits.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsWatchAreaCode reports whether a SAME/PSGC code is in the watch area:
// NCR, Cavite, Laguna, Batangas, Rizal. Handles 9- and 10-digit PSGC.
func IsWatchAreaCode(code string) bool {
	d := digitsOnly(code)
	if d == "" {
		return false
	}
	prefixes := []string{
		"13",            // NCR (9d starts 13, 10d starts 13)
		"0421", "04021", // Cavite
		"0434", "04034", // Laguna
		"0410", "04010", // Batangas
		"0458", "04058", // Rizal
	}
	for _, p := range prefixes {
		if strings.HasPrefix(d, p) {
			return true
		}
	}
	return false
}

// IsNCRCode reports whether a SAME code is in NCR.
func IsNCRCode(code string) bool {
	d := digitsOnly(code)
	return d != "" && strings.HasPrefix(d, "13")
}

func normName(s string) string {
	s = strings.ToLower(s)
	// rough ñ handling
	s = strings.ReplaceAll(s, "ñ", "n")
	return s
}

// IsWatchAreaName reports whether an areaDesc matches the watch area.
func IsWatchAreaName(name string) bool {
	n := normName(name)
	for _, k := range []string{
		"metro manila", "ncr", "national capital",
		"las pinas", "las pi", "cavite", "laguna", "batangas", "rizal",
	} {
		if strings.Contains(n, k) {
			return true
		}
	}
	return false
}

// IsNCRName reports whether an areaDesc matches NCR specifically.
func IsNCRName(name string) bool {
	n := normName(name)
	for _, k := range []string{"metro manila", "ncr", "national capital", "las pinas", "las pi"} {
		if strings.Contains(n, k) {
			return true
		}
	}
	return false
}

// InWatchArea reports whether any area touches the watch area.
func (a *Alert) InWatchArea() bool {
	for _, ar := range a.Areas {
		if IsWatchAreaCode(ar.SAME()) || IsWatchAreaName(ar.Desc) {
			return true
		}
	}
	return false
}

// InNCR reports whether any area touches NCR specifically.
func (a *Alert) InNCR() bool {
	for _, ar := range a.Areas {
		if IsNCRCode(ar.SAME()) || IsNCRName(ar.Desc) {
			return true
		}
	}
	return false
}

// ProvinceSignals aggregates max signal level per province name and reports
// whether any watch-area province is affected.
func ProvinceSignals(alerts []*Alert) (map[string]int, bool) {
	levels := map[string]int{}
	watch := false
	for _, a := range alerts {
		lvl := a.Signal()
		if lvl == 0 {
			continue
		}
		if a.InWatchArea() {
			watch = true
		}
		for _, ar := range a.Areas {
			if ar.Desc == "" {
				continue
			}
			if lvl > levels[ar.Desc] {
				levels[ar.Desc] = lvl
			}
		}
	}
	return levels, watch
}

// MaxWatchSignal returns the max signal level over the watch area (0 = none).
func MaxWatchSignal(alerts []*Alert) int {
	max := 0
	for _, a := range alerts {
		if lvl := a.Signal(); lvl > 0 && a.InWatchArea() && lvl > max {
			max = lvl
		}
	}
	return max
}

// RainfallColor detects rainfall warnings: event names containing
// "Rainfall" with color Yellow/Orange/Red from event/headline/parameters.
// Returns "YELLOW", "ORANGE", "RED", or "".
func (a *Alert) RainfallColor() string {
	hayEvent := strings.ToLower(a.Event)
	if !strings.Contains(hayEvent, "rainfall") {
		return ""
	}
	hay := strings.ToLower(a.Event + " " + a.Headline)
	for _, v := range a.Params {
		hay += " " + strings.ToLower(v)
	}
	switch {
	case strings.Contains(hay, "red"):
		return "RED"
	case strings.Contains(hay, "orange"):
		return "ORANGE"
	case strings.Contains(hay, "yellow"):
		return "YELLOW"
	default:
		return ""
	}
}

// IsTsunami reports whether the CAP event mentions tsunami.
func (a *Alert) IsTsunami() bool {
	return strings.Contains(strings.ToLower(a.Event), "tsunami")
}

// FetchFeed downloads and parses the Atom feed.
func FetchFeed(ctx context.Context, c *fetch.Client, minRefetch time.Duration) ([]FeedEntry, error) {
	r, err := c.Get(ctx, FeedURL, minRefetch)
	if err != nil {
		return nil, err
	}
	return ParseFeed(r.Body)
}

// CAPRetryDelay is the wait before the single retry per CAP fetch.
// Stubbed to 0 in tests.
var CAPRetryDelay = 2 * time.Second

// CAPSleep sleeps between CAP retries (defaults to time.Sleep; stub in tests).
var CAPSleep = time.Sleep

// StaleCAPAge is the feed-entry age above which a still-failing CAP is
// skipped with a warning instead of marking PAGASA partial.
const StaleCAPAge = 24 * time.Hour

// FetchCAPs downloads CAP XML for entries (callers should pre-filter with
// Recent(..., 72h)). CAPs are immutable: cached effectively forever.
//
// One retry (CAPRetryDelay later) per CAP. A CAP that still fails is
// skipped with a warning when its feed entry is older than StaleCAPAge;
// otherwise PAGASA is partial: successfully parsed CAPs are still
// returned along with partial=true so callers may emit raises/new alerts
// but must not apply lowering/lifting state changes.
func FetchCAPs(ctx context.Context, c *fetch.Client, entries []FeedEntry, now time.Time) (alerts []*Alert, partial bool, err error) {
	const forever = 365 * 24 * time.Hour
	var failed []string
	for _, e := range entries {
		r, ferr := c.Get(ctx, e.CapURL, forever)
		if ferr != nil {
			select {
			case <-ctx.Done():
				failed = append(failed, e.CapURL)
				if isStaleCAP(e, now) {
					log.Printf("pagasa: warning: skipping stale CAP %s (feed entry %v)", e.CapURL, e.Updated.Format(time.RFC3339))
					continue
				}
				partial = true
				continue
			default:
			}
			CAPSleep(CAPRetryDelay)
			r, ferr = c.Get(ctx, e.CapURL, forever)
			if ferr != nil {
				failed = append(failed, e.CapURL)
				if isStaleCAP(e, now) {
					log.Printf("pagasa: warning: skipping stale CAP %s (feed entry %v)", e.CapURL, e.Updated.Format(time.RFC3339))
					continue
				}
				partial = true
				continue
			}
		}
		a, perr := ParseCAP(r.Body)
		if perr != nil {
			failed = append(failed, e.CapURL)
			if isStaleCAP(e, now) {
				log.Printf("pagasa: warning: skipping stale CAP %s (feed entry %v)", e.CapURL, e.Updated.Format(time.RFC3339))
				continue
			}
			partial = true
			continue
		}
		a.SourceURL = e.CapURL
		a.FeedTitle = e.Title
		alerts = append(alerts, a)
	}
	if len(alerts) == 0 && len(failed) > 0 {
		if partial {
			return nil, true, fmt.Errorf("pagasa: %d/%d CAPs failed (partial, no usable alerts)", len(failed), len(entries))
		}
		return nil, false, fmt.Errorf("pagasa: %d/%d stale CAPs skipped", len(failed), len(entries))
	}
	if partial {
		return alerts, true, fmt.Errorf("pagasa: partial (%d/%d CAPs failed, %d usable)", len(failed), len(entries), len(alerts))
	}
	return alerts, false, nil
}

func isStaleCAP(e FeedEntry, now time.Time) bool {
	if e.Updated.IsZero() || now.IsZero() {
		return false
	}
	return now.Sub(e.Updated) > StaleCAPAge
}
