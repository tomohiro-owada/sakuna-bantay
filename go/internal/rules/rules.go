// Package rules implements the pure posting rules (no I/O except an
// optional kill-switch file check).
package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/phivolcs"
)

// Kinds.
type Kind string

const (
	KindDaily   Kind = "Daily"
	KindSignal  Kind = "Signal"
	KindRain    Kind = "Rain"
	KindQuake   Kind = "Quake"
	KindTaal    Kind = "Taal"
	KindTsunami Kind = "Tsunami"
)

// Event is one candidate.
type Event struct {
	Kind         Kind
	SourceID     string
	DedupeKey    string
	SignalLevel  int     // Signal
	RainColor    string  // Rain: YELLOW/ORANGE/RED
	Magnitude    float64 // Quake
	DistanceKm   float64 // Quake
	Lat, Lon     float64 // Quake
	QuakeTime    time.Time
	QuakeCanon   string // canonical revision-stripped id (optional)
	IntensityMax int    // Quake: max reported intensity in watch area
	TaalLevel    int    // Taal
}

// Posted is one history record.
type Posted struct {
	Time       time.Time
	Kind       Kind
	SourceID   string
	DedupeKey  string
	QuakeTime  time.Time
	Lat, Lon   float64
	QuakeCanon string
	// PostID is the Facebook post id in live mode (empty in dry-run).
	PostID string `json:"post_id,omitempty"`
}

// State is the previous observed state. Has* marks whether the source was
// ever recorded: first observation baselines silently (no post).
type State struct {
	LastSignal    int
	HasSignal     bool
	LastTaal      int
	HasTaal       bool
	LastRainColor string // normalized upper ("", YELLOW, ORANGE, RED)
	HasRain       bool
}

// DeferredItem is a candidate deferred by caps (not dropped silently).
type DeferredItem struct {
	Event  Event
	Reason string
}

// SkippedItem is a candidate skipped by per-kind rules/dedupe.
type SkippedItem struct {
	Event  Event
	Reason string
}

// Decision is the Decide output.
type Decision struct {
	Post       []Event
	Deferred   []DeferredItem
	Skipped    []SkippedItem
	KillSwitch bool
}

// KillSwitchActive reports whether <dir>/KILL_SWITCH exists.
func KillSwitchActive(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "KILL_SWITCH"))
	return err == nil
}

func manila() *time.Location {
	loc, err := time.LoadLocation("Asia/Manila")
	if err != nil {
		return time.FixedZone("PHT", 8*3600)
	}
	return loc
}

func dedupeKey(e Event, phtDate string) string {
	if e.DedupeKey != "" {
		return e.DedupeKey
	}
	if e.Kind == KindDaily && phtDate != "" {
		return "daily:" + phtDate
	}
	return string(e.Kind) + ":" + e.SourceID
}

func priority(e Event) int {
	switch e.Kind {
	case KindTsunami:
		return 0
	case KindSignal:
		if e.SignalLevel >= 3 {
			return 1
		}
		return 4
	case KindRain:
		if strings.EqualFold(e.RainColor, "RED") {
			return 2
		}
		return 4
	case KindQuake:
		return 3
	case KindTaal:
		return 5
	case KindDaily:
		return 6
	default:
		return 7
	}
}

func bypassSpacing(e Event) bool {
	switch e.Kind {
	case KindTsunami:
		return true
	case KindRain:
		return strings.EqualFold(e.RainColor, "RED")
	case KindSignal:
		return e.SignalLevel >= 3
	default:
		return false
	}
}

// quakeHist is a minimal quake record for duplicate detection.
type quakeHist struct {
	t     time.Time
	lat   float64
	lon   float64
	canon string
}

// Decide applies the posting rules. Kill-switch enforcement lives in
// fbpost.Mode / the run loop (live vs dry-run), not here: Decide always
// evaluates the rules so dry-run passes simulate fully.
func Decide(now time.Time, events []Event, history []Posted, state State) Decision {
	loc := manila()
	pht := now.In(loc)
	phtDate := pht.Format("2006-01-02")

	seenDedupe := map[string]bool{}
	seenSource := map[[2]string]bool{} // (kind, sourceID)
	for _, h := range history {
		k := h.DedupeKey
		if k == "" {
			k = string(h.Kind) + ":" + h.SourceID
		}
		seenDedupe[k] = true
		seenSource[[2]string{string(h.Kind), h.SourceID}] = true
	}

	// Index history quakes for duplicate detection.
	var qh []quakeHist
	for _, h := range history {
		if h.Kind == KindQuake {
			qh = append(qh, quakeHist{t: h.QuakeTime, lat: h.Lat, lon: h.Lon, canon: h.QuakeCanon})
		}
	}

	var eligible []Event
	var skipped []SkippedItem
	skip := func(e Event, reason string) {
		skipped = append(skipped, SkippedItem{Event: e, Reason: reason})
	}

	for _, e := range events {
		dk := dedupeKey(e, phtDate)
		if seenDedupe[dk] {
			skip(e, "dedupe: already posted")
			continue
		}
		switch e.Kind {
		case KindDaily:
			{
				wantDK := "daily:" + phtDate
				// A daily event for another date is never postable now.
				if e.DedupeKey != "" && e.DedupeKey != wantDK {
					// Allow explicit keys but only today's date posts.
					// If the key date != today, skip.
					skip(e, "daily: not today's date")
					continue
				}
				hour := pht.Hour()
				min := pht.Minute()
				after6 := hour > 6 || (hour == 6 && min >= 0)
				beforeNoon := hour < 12
				if !after6 {
					skip(e, "daily: too early (before 06:00 PHT)")
					continue
				}
				if !beforeNoon {
					skip(e, "daily: missed window (after 11:59 PHT)")
					continue
				}
				if seenDedupe[wantDK] {
					skip(e, "daily: already posted today")
					continue
				}
				eligible = append(eligible, e)
			}
		case KindSignal:
			{
				if !state.HasSignal {
					skip(e, "signal: baseline (first observation)")
					continue
				}
				if e.SignalLevel != state.LastSignal {
					eligible = append(eligible, e)
				} else {
					skip(e, "signal: no change")
				}
			}
		case KindRain:
			{
				c := strings.ToUpper(strings.TrimSpace(e.RainColor))
				if c != "ORANGE" && c != "RED" {
					skip(e, "rain: not orange/red")
					continue
				}
				if !state.HasRain {
					skip(e, "rain: baseline (first observation)")
					continue
				}
				last := strings.ToUpper(strings.TrimSpace(state.LastRainColor))
				isNew := !seenSource[[2]string{string(KindRain), e.SourceID}]
				if !isNew && c == last {
					skip(e, "rain: no change (same id and color)")
					continue
				}
				eligible = append(eligible, e)
			}
		case KindQuake:
			{
				meets := false
				if e.Magnitude >= 5.0 && e.DistanceKm <= 150 {
					meets = true
				}
				if e.Magnitude >= 6.5 && phivolcs.InPHBounds(e.Lat, e.Lon) {
					meets = true
				}
				if e.IntensityMax >= 3 {
					meets = true
				}
				if !meets {
					skip(e, "quake: below threshold")
					continue
				}
				if seenSource[[2]string{string(KindQuake), e.SourceID}] {
					skip(e, "quake: duplicate source id")
					continue
				}
				if dupQuake(e, qh) {
					skip(e, "quake: duplicate event (60s/50km or revision)")
					continue
				}
				eligible = append(eligible, e)
			}
		case KindTaal:
			{
				if !state.HasTaal {
					skip(e, "taal: baseline (first observation)")
					continue
				}
				if e.TaalLevel == state.LastTaal {
					skip(e, "taal: no change")
					continue
				}
				eligible = append(eligible, e)
			}
		case KindTsunami:
			{
				eligible = append(eligible, e)
			}
		default:
			skip(e, "unknown kind")
		}
	}

	// Caps: max 6 posts per rolling 24h; >=30 min apart (bypass excepted).
	recent := 0
	var lastPost time.Time
	var haveLast bool
	for _, h := range history {
		if h.Time.After(now.Add(-24*time.Hour)) && !h.Time.After(now) {
			recent++
		}
		if !h.Time.After(now) && (!haveLast || h.Time.After(lastPost)) {
			lastPost = h.Time
			haveLast = true
		}
	}
	// Stable priority sort.
	idx := make([]int, len(eligible))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return priority(eligible[idx[a]]) < priority(eligible[idx[b]])
	})
	ordered := make([]Event, 0, len(eligible))
	for _, i := range idx {
		ordered = append(ordered, eligible[i])
	}

	var post []Event
	var deferred []DeferredItem
	// Track posted dedupe in-batch (avoid double-posting same key).
	batchDedupe := map[string]bool{}
	var batchQuakes []quakeHist
	for _, e := range ordered {
		dk := dedupeKey(e, phtDate)
		if batchDedupe[dk] {
			skipped = append(skipped, SkippedItem{Event: e, Reason: "dedupe: already selected this run"})
			continue
		}
		// In-batch quake duplicate check.
		if e.Kind == KindQuake {
			dup := false
			for _, bq := range batchQuakes {
				if quakeDupPair(e.QuakeTime, e.Lat, e.Lon, e.QuakeCanon, bq) {
					dup = true
					break
				}
			}
			if dup {
				skipped = append(skipped, SkippedItem{Event: e, Reason: "quake: duplicate event (60s/50km or revision)"})
				continue
			}
		}
		if recent+len(post) >= 6 {
			deferred = append(deferred, DeferredItem{Event: e, Reason: "24h cap exceeded (max 6 posts)"})
			continue
		}
		if !bypassSpacing(e) && haveLast {
			if now.Sub(lastPost) < 30*time.Minute {
				deferred = append(deferred, DeferredItem{Event: e, Reason: "too soon (30-min spacing)"})
				continue
			}
			// Also enforce spacing between posts in this batch.
			if len(post) > 0 {
				deferred = append(deferred, DeferredItem{Event: e, Reason: "too soon (30-min spacing)"})
				continue
			}
		} else if !bypassSpacing(e) && len(post) > 0 {
			deferred = append(deferred, DeferredItem{Event: e, Reason: "too soon (30-min spacing)"})
			continue
		}
		post = append(post, e)
		batchDedupe[dk] = true
		if e.Kind == KindQuake {
			batchQuakes = append(batchQuakes, quakeHist{t: e.QuakeTime, lat: e.Lat, lon: e.Lon, canon: e.QuakeCanon})
		}
		// Bypass posts still advance lastPost for subsequent non-bypass.
		lastPost = now
		haveLast = true
	}
	return Decision{Post: post, Deferred: deferred, Skipped: skipped}
}

func dupQuake(e Event, hist []quakeHist) bool {
	for _, h := range hist {
		if quakeDupPair(e.QuakeTime, e.Lat, e.Lon, e.QuakeCanon, h) {
			return true
		}
	}
	return false
}

func quakeDupPair(t time.Time, lat, lon float64, canon string, h quakeHist) bool {
	if canon != "" && h.canon != "" && canon == h.canon {
		return true
	}
	if t.IsZero() || h.t.IsZero() {
		return false
	}
	dt := t.Sub(h.t)
	if dt < 0 {
		dt = -dt
	}
	if dt <= 60*time.Second {
		if phivolcs.HaversineKm(lat, lon, h.lat, h.lon) <= 50 {
			return true
		}
	}
	return false
}
