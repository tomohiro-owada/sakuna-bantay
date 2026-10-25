// Package volcano parses the WOVODAT/PHIVOLCS bulletin list for Taal
// (and other volcano) alert levels plus the latest Taal bulletin link.
package volcano

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fetch"
	"golang.org/x/net/html"
)

// ListURL is the bulletin list page.
const ListURL = "https://wovodat.phivolcs.dost.gov.ph/bulletin/list-of-bulletin"

// BaseURL is the site root for resolving relative bulletin hrefs.
const BaseURL = "https://wovodat.phivolcs.dost.gov.ph"

// TaalLevel describes the PHIVOLCS Taal alert scheme.
func LevelDescription(level int) string {
	switch level {
	case 0:
		return "Normal"
	case 1:
		return "Low-level unrest"
	case 2:
		return "Increasing unrest"
	case 3:
		return "Magmatic unrest"
	case 4:
		return "Hazardous eruption imminent"
	case 5:
		return "Hazardous eruption in progress"
	default:
		return "Unknown"
	}
}

// TaalStatus is the latest Taal bulletin state.
type TaalStatus struct {
	Level         int    `json:"level"`
	LevelText     string `json:"level_text"`
	BulletinURL   string `json:"bulletin_url"`
	BulletinTitle string `json:"bulletin_title"`
	BulletinID    string `json:"bulletin_id"`
}

// Status is the parsed list page: all volcano levels + Taal detail.
type Status struct {
	Levels map[string]int `json:"levels"`
	Taal   TaalStatus     `json:"taal"`
}

var levelPairRe = regexp.MustCompile(`(?i)(Taal|Kanlaon|Bulusan|Pinatubo|Mayon|Hibok-?Hibok)\s*-\s*([0-5])`)
var bidRe = regexp.MustCompile(`[?&]bid=(\d+)`)

// Parse parses the bulletin list HTML.
func Parse(data []byte) (*Status, error) {
	text := extractText(data)
	levels := map[string]int{}
	for _, m := range levelPairRe.FindAllStringSubmatch(text, -1) {
		name := canonicalName(m[1])
		lvl, _ := strconv.Atoi(m[2])
		levels[name] = lvl
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("volcano: no alert levels found")
	}
	taalLvl := levels["Taal"]
	url, title, bid := latestTaalBulletin(data)
	st := &Status{
		Levels: levels,
		Taal: TaalStatus{
			Level:         taalLvl,
			LevelText:     LevelDescription(taalLvl),
			BulletinURL:   url,
			BulletinTitle: title,
			BulletinID:    bid,
		},
	}
	return st, nil
}

func canonicalName(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "-", ""))
	switch {
	case strings.Contains(s, "taal"):
		return "Taal"
	case strings.Contains(s, "kanlaon"):
		return "Kanlaon"
	case strings.Contains(s, "bulusan"):
		return "Bulusan"
	case strings.Contains(s, "pinatubo"):
		return "Pinatubo"
	case strings.Contains(s, "mayon"):
		return "Mayon"
	case strings.Contains(s, "hibok"):
		return "Hibok-Hibok"
	default:
		return s
	}
}

func extractText(data []byte) string {
	doc, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return string(data)
	}
	var b strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return b.String()
}

// latestTaalBulletin finds the newest /bulletin/activity-tvo?bid= link and
// its "Taal Volcano Summary ..." title text.
func latestTaalBulletin(data []byte) (string, string, string) {
	doc, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return "", "", ""
	}
	type hit struct {
		href  string
		bid   string
		title string
	}
	var hits []hit
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := ""
			for _, a := range n.Attr {
				if strings.EqualFold(a.Key, "href") {
					href = a.Val
				}
			}
			if strings.Contains(href, "activity-tvo") {
				m := bidRe.FindStringSubmatch(href)
				bid := ""
				if m != nil {
					bid = m[1]
				}
				title := findTaalTitle(n)
				hits = append(hits, hit{href: href, bid: bid, title: title})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if len(hits) == 0 {
		return "", "", ""
	}
	// Latest = max numeric bid; on ties prefer the English title
	// ("Taal Volcano" / lang=en).
	bestN := -1
	for _, h := range hits {
		if n := atoi(h.bid); n > bestN {
			bestN = n
		}
	}
	var cands []hit
	for _, h := range hits {
		if atoi(h.bid) == bestN {
			cands = append(cands, h)
		}
	}
	best := cands[0]
	for _, h := range cands[1:] {
		bestIsEN := strings.Contains(best.title, "Taal Volcano") || strings.Contains(best.href, "lang=en")
		hIsEN := strings.Contains(h.title, "Taal Volcano") || strings.Contains(h.href, "lang=en")
		if !bestIsEN && hIsEN {
			best = h
		} else if best.title == "" && h.title != "" {
			best = h
		}
	}
	abs := best.href
	if strings.HasPrefix(abs, "/") {
		abs = strings.TrimRight(BaseURL, "/") + abs
	}
	return abs, best.title, best.bid
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// findTaalTitle looks inside the anchor for text mentioning Taal Volcano
// (English) or BULKANG Taal (Filipino). English preferred by callers.
func findTaalTitle(a *html.Node) string {
	var cands []string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if len(cands) >= 2 {
			return
		}
		if n.Type == html.ElementNode && n.Data == "p" {
			t := strings.Join(strings.Fields(nodeText(n)), " ")
			if strings.Contains(t, "Taal Volcano") || strings.Contains(t, "BULKANG Taal") || strings.Contains(t, "Taal") && strings.Contains(strings.ToLower(t), "summary") {
				cands = append(cands, t)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(a)
	// Prefer the English "Taal Volcano" title.
	for _, c := range cands {
		if strings.Contains(c, "Taal Volcano") {
			return c
		}
	}
	if len(cands) > 0 {
		return cands[0]
	}
	return ""
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return b.String()
}

// Fetch downloads and parses the list page.
func Fetch(ctx context.Context, c *fetch.Client, minRefetch time.Duration) (*Status, error) {
	r, err := c.Get(ctx, ListURL, minRefetch)
	if err != nil {
		return nil, err
	}
	return Parse(r.Body)
}
