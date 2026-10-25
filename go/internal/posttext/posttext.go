// Package posttext builds the trilingual dry-run post text for each card.
//
// Layout per card (SPEC §posttext): Taglish block first, then a "———"
// separator line, English, "———", Japanese; each block is 2–5 short lines.
// Then a source line, the site link, and the unofficial line:
//
//	Source: ...
//	https://abalol.xyz/sakuna-bantay/
//	Unofficial · not an official warning — follow PAGASA, PHIVOLCS and your LGU.
//
// Tone is calm; no panic words, at most 2 emoji per post. Japanese is
// polite-plain (です/ます) throughout.
package posttext

import (
	"fmt"
	"sort"
	"strings"
)

// Separator between language blocks.
const Separator = "———"

// Link is the site link appended to every post.
const Link = "https://abalol.xyz/sakuna-bantay/"

// Unofficial is the closing disclaimer (English, shared by all cards).
const Unofficial = "Unofficial · not an official warning — follow PAGASA, PHIVOLCS and your LGU."

// join assembles the three language blocks plus footer.
func join(tl, en, ja []string, source string) string {
	var b strings.Builder
	b.WriteString(strings.Join(tl, "\n"))
	b.WriteString("\n" + Separator + "\n")
	b.WriteString(strings.Join(en, "\n"))
	b.WriteString("\n" + Separator + "\n")
	b.WriteString(strings.Join(ja, "\n"))
	b.WriteString("\n" + Separator + "\n")
	b.WriteString(source + "\n")
	b.WriteString(Link + "\n")
	b.WriteString(Unofficial)
	return b.String()
}

func fmtTemp(v float64) string {
	return fmt.Sprintf("%.0f°C", v)
}

// ---------------------------------------------------------------------------
// Daily
// ---------------------------------------------------------------------------

// DailyData is the plain input for the daily post text.
type DailyData struct {
	DateLabel     string // "25 Oct 2026"
	TMax          float64
	TMin          float64
	ApparentMax   float64
	PrecipProbMax int
	PrecipSum     float64
	UVMax         float64
	UVCategory    string
	AQI           int
	AQICategory   string
	TaalLevel     int
	TaalText      string
	Alerts        []string // active alert summaries (already filtered to ACTIVE)
	Sample        bool
}

// RainLines returns trilingual rain lines for one probability (the max
// hourly probability over the displayed window). Thresholds are shared
// with the card (REVIEW_C #3): <20% low, 20–49% some chance, >=50% likely.
// Exported so the site summary reuses the exact same wording as the card
// and post text.
func RainLines(p int) (tl, en, ja string) { return rainLines(p) }

// rainLines returns trilingual rain lines for one probability (the max
// hourly probability over the displayed 06:00–23:00 window). Thresholds
// are shared with the card (REVIEW_C #3): <20% low, 20–49% some chance,
// >=50% likely.
func rainLines(p int) (tl, en, ja string) {
	switch {
	case p >= 50:
		return fmt.Sprintf("Mataas ang tsansa ng ulan (hanggang %d%%). Magdala ng payong kung aalis.", p),
			fmt.Sprintf("Likely rain (up to %d%%). Bring an umbrella if heading out.", p),
			fmt.Sprintf("雨の可能性が高いです（最大%d%%）。お出かけの際は傘をお持ちください。", p)
	case p >= 20:
		return fmt.Sprintf("May tsansa ng ulan (hanggang %d%%). Magdala ng payong kung aalis.", p),
			fmt.Sprintf("Some chance of rain (up to %d%%). Bring an umbrella if heading out.", p),
			fmt.Sprintf("雨の可能性があります（最大%d%%）。お出かけの際は傘をお持ちください。", p)
	default:
		return fmt.Sprintf("Mababa ang tsansa ng ulan (pinakamataas %d%%).", p),
			fmt.Sprintf("Low chance of rain (max %d%%).", p),
			fmt.Sprintf("雨の可能性は低いです（最大%d%%）。", p)
	}
}

// uvCategoryTL keeps the EN term with a short Filipino gloss.
func uvCategoryTL(en string) string {
	switch en {
	case "Low":
		return "Low (mababa)"
	case "Moderate":
		return "Moderate (katamtaman)"
	case "High":
		return "High (mataas)"
	case "Very High":
		return "Very High (napakataas)"
	case "Extreme":
		return "Extreme (matindi)"
	case "":
		return "—"
	default:
		return en
	}
}

// uvCategoryJA translates UV categories to Japanese.
func uvCategoryJA(en string) string {
	switch en {
	case "Low":
		return "弱い"
	case "Moderate":
		return "中程度"
	case "High":
		return "強い"
	case "Very High":
		return "非常に強い"
	case "Extreme":
		return "極端に強い"
	case "":
		return "—"
	default:
		return en
	}
}

// aqiCategoryJA translates US AQI categories to Japanese.
func aqiCategoryJA(en string) string {
	switch en {
	case "Good":
		return "良好"
	case "Moderate":
		return "普通"
	case "Unhealthy for Sensitive Groups":
		return "敏感な人には健康に良くない"
	case "Unhealthy":
		return "健康に良くない"
	case "Very Unhealthy":
		return "非常に健康に良くない"
	case "Hazardous":
		return "危険"
	case "":
		return "—"
	default:
		return en
	}
}

// taalNameJA returns the Japanese Taal level name.
func taalNameJA(level int) string {
	switch level {
	case 0:
		return "平常"
	case 1:
		return "低レベルの火山活動"
	case 2:
		return "活動の高まり"
	case 3:
		return "マグマ活動による不安"
	case 4:
		return "危険な噴火が切迫"
	case 5:
		return "危険な噴火が発生中"
	default:
		return "不明"
	}
}

// taalNameTL returns EN term + short Filipino gloss for Taglish.
func taalNameTL(level int, enText string) string {
	if enText == "" {
		enText = "Unknown"
	}
	var gloss string
	switch level {
	case 0:
		gloss = "normal"
	case 1:
		gloss = "mababang aktibidad"
	case 2:
		gloss = "tumataas na aktibidad"
	case 3:
		gloss = "pagkilos ng magma"
	case 4:
		gloss = "nakaambang mapanganib na pagputok"
	case 5:
		gloss = "nagaganap na mapanganib na pagputok"
	default:
		return enText
	}
	return fmt.Sprintf("%s (%s)", enText, gloss)
}

// Daily builds the daily Las Piñas post text.
//
// PHASE_D context decision: Taal and AQI stay as chips on the daily CARD
// but are NOT mentioned in the daily post TEXT (all 3 languages), so this
// text carries only temperature, rain, UV and the alerts line.
func Daily(d DailyData) string {
	rainTL, rainEN, rainJA := rainLines(d.PrecipProbMax)
	uvEN := d.UVCategory
	if uvEN == "" {
		uvEN = "—"
	}
	uvTL := uvCategoryTL(d.UVCategory)
	uvJA := uvCategoryJA(d.UVCategory)
	alertTL, alertEN, alertJA := dailyAlerts(d.Alerts)

	tl := []string{
		fmt.Sprintf("Init ngayon sa Las Piñas: hanggang %s, feels like %s.", fmtTemp(d.TMax), fmtTemp(d.ApparentMax)),
		rainTL,
		fmt.Sprintf("UV %s (%.1f).", uvTL, d.UVMax),
		alertTL,
	}
	en := []string{
		fmt.Sprintf("Hot day in Las Piñas: up to %s, feels like %s.", fmtTemp(d.TMax), fmtTemp(d.ApparentMax)),
		rainEN,
		fmt.Sprintf("UV %s (%.1f).", uvEN, d.UVMax),
		alertEN,
	}
	ja := []string{
		fmt.Sprintf("ラスピニャスは今日も暑いです。最高%s、体感%sです。", fmtTemp(d.TMax), fmtTemp(d.ApparentMax)),
		rainJA,
		fmt.Sprintf("UV指数 %s (%.1f)。", uvJA, d.UVMax),
		alertJA,
	}
	if d.Sample {
		tl[0] = "SAMPLE · " + tl[0]
		en[0] = "SAMPLE · " + en[0]
		ja[0] = "SAMPLE · " + ja[0]
	}
	return join(tl, en, ja, "Source: DOST-PAGASA (CC BY 4.0), PHIVOLCS, Open-Meteo (CC BY 4.0)")
}

// dailyAlerts picks the alert strip line in three languages.
// Input is already ACTIVE-only; empty (or the "no warnings" sentinel from
// the card pipeline) means no NCR-area warnings.
func dailyAlerts(alerts []string) (tl, en, ja string) {
	if len(alerts) == 0 || (len(alerts) == 1 && alerts[0] == "No active PAGASA warnings for NCR area") {
		return "Walang aktibong babala ng PAGASA para sa NCR area.",
			"No active PAGASA warnings for NCR area.",
			"NCR周辺にPAGASAの有効な警報はありません。"
	}
	// Keep the strip short: first item only (cards show max 2 lines).
	// Place strings from PHIVOLCS/PAGASA stay as-is (proper nouns).
	first := alerts[0]
	return first, first, first
}

// ---------------------------------------------------------------------------
// Quake
// ---------------------------------------------------------------------------

// QuakeData is the plain input for the earthquake post text.
type QuakeData struct {
	Mag          float64
	DepthKm      float64
	Place        string
	DistanceKm   float64
	Compass      string
	TimeLabel    string // "25 Oct 2026, 6:13 PM PHT"
	Source       string // "PHIVOLCS" or "USGS"
	FallbackNote bool   // true when no notable event: showing most relevant recent
	Sample       bool
}

// compassJA maps English 16-wind compass points to Japanese.
func compassJA(c string) string {
	switch c {
	case "N":
		return "北"
	case "NNE":
		return "北北東"
	case "NE":
		return "北東"
	case "ENE":
		return "東北東"
	case "E":
		return "東"
	case "ESE":
		return "東南東"
	case "SE":
		return "南東"
	case "SSE":
		return "南南東"
	case "S":
		return "南"
	case "SSW":
		return "南南西"
	case "SW":
		return "南西"
	case "WSW":
		return "西南西"
	case "W":
		return "西"
	case "WNW":
		return "西北西"
	case "NW":
		return "北西"
	case "NNW":
		return "北北西"
	default:
		return c
	}
}

// Quake builds the earthquake post text.
func Quake(q QuakeData) string {
	src := q.Source
	if src == "" {
		src = "PHIVOLCS"
	}
	// Synthetic samples must not look like real events (REVIEW_C #2).
	srcDisplay := src
	srcJA := src
	if q.Sample {
		srcDisplay = "Synthetic sample — not a real event"
		srcJA = "Synthetic sample — not a real event"
	}
	distTL := fmt.Sprintf("%.0f km %s ng Las Piñas", q.DistanceKm, q.Compass)
	distEN := fmt.Sprintf("%.0f km %s of Las Piñas", q.DistanceKm, q.Compass)
	distJA := fmt.Sprintf("ラスピニャスから%sに約%.0f km", compassJA(q.Compass), q.DistanceKm)
	tl := []string{
		fmt.Sprintf("Lindol M%.1f, %s (%s).", q.Mag, q.Place, distTL),
		fmt.Sprintf("Lalim %.0f km, %s. Source: %s.", q.DepthKm, q.TimeLabel, srcDisplay),
		"Manatiling kalmado. Sundan ang PHIVOLCS para sa updates.",
	}
	en := []string{
		fmt.Sprintf("Earthquake M%.1f, %s (%s).", q.Mag, q.Place, distEN),
		fmt.Sprintf("Depth %.0f km, %s. Source: %s.", q.DepthKm, q.TimeLabel, srcDisplay),
		"Stay calm. Follow PHIVOLCS for updates.",
	}
	ja := []string{
		fmt.Sprintf("地震 M%.1f、%s（%s）。", q.Mag, q.Place, distJA),
		fmt.Sprintf("深さ%.0f km、%s。情報元: %s。", q.DepthKm, q.TimeLabel, srcJA),
		"落ち着いて行動してください。最新情報はPHIVOLCSをご確認ください。",
	}
	if q.FallbackNote {
		tl = append(tl, "Walang malakas na lindol malapit sa Las Piñas ngayon; ito ang pinakamalapit na kamakailangang lindol (para sa impormasyon lang).")
		en = append(en, "No significant quake near Las Piñas; this is the most relevant recent event (for information only).")
		ja = append(ja, "ラスピニャス周辺で大きな地震はありません。参考として最近の地震を表示しています。")
	}
	if q.Sample {
		tl[0] = "SAMPLE · " + tl[0]
		en[0] = "SAMPLE · " + en[0]
		ja[0] = "SAMPLE · " + ja[0]
	}
	source := "Source: PHIVOLCS"
	if src == "USGS" {
		source = "Source: USGS (public domain)"
	}
	if q.Sample {
		source = "SAMPLE ONLY — synthetic data"
	}
	return join(tl, en, ja, source)
}

// ---------------------------------------------------------------------------
// Signal
// ---------------------------------------------------------------------------

// SignalData is the plain input for the typhoon/signal post text.
type SignalData struct {
	Cyclone      string // e.g. "Typhoon Rosal (CHAMPI)"
	IssuedLabel  string // "25 Oct 2026, 11:00 AM PHT"
	Levels       map[string]int
	WatchSummary string // e.g. "NCR: no signal" / "NCR: Signal #2"
	Sample       bool
}

func topAreas(levels map[string]int) string {
	if len(levels) == 0 {
		return "—"
	}
	type kv struct {
		k string
		v int
	}
	var kvs []kv
	for k, v := range levels {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].v != kvs[j].v {
			return kvs[i].v > kvs[j].v
		}
		return kvs[i].k < kvs[j].k
	})
	n := 4
	if len(kvs) < n {
		n = len(kvs)
	}
	var parts []string
	for _, p := range kvs[:n] {
		parts = append(parts, fmt.Sprintf("#%d %s", p.v, p.k))
	}
	s := strings.Join(parts, "; ")
	if len(kvs) > n {
		s += fmt.Sprintf(" (+%d more)", len(kvs)-n)
	}
	return s
}

// Signal builds the typhoon / wind-signal post text.
func Signal(s SignalData) string {
	cyc := s.Cyclone
	if cyc == "" {
		cyc = "No active cyclone"
	}
	areas := topAreas(s.Levels)
	watch := s.WatchSummary
	if watch == "" {
		watch = "NCR: no signal"
	}
	tl := []string{
		fmt.Sprintf("%s. %s.", cyc, watch),
		fmt.Sprintf("Issued %s.", s.IssuedLabel),
		fmt.Sprintf("May signal: %s.", areas),
		"Sundan ang PAGASA at LGU para sa updates.",
	}
	en := []string{
		fmt.Sprintf("%s. %s.", cyc, watch),
		fmt.Sprintf("Issued %s.", s.IssuedLabel),
		fmt.Sprintf("Areas with signals: %s.", areas),
		"Follow PAGASA and your LGU for updates.",
	}
	ja := []string{
		fmt.Sprintf("%s。%s。", cyc, watch),
		fmt.Sprintf("発表: %s。", s.IssuedLabel),
		fmt.Sprintf("シグナル発令中: %s。", areas),
		"PAGASAと自治体の最新情報をご確認ください。",
	}
	if s.Sample {
		tl[0] = "SAMPLE · " + tl[0]
		en[0] = "SAMPLE · " + en[0]
		ja[0] = "SAMPLE · " + ja[0]
	}
	source := "Source: DOST-PAGASA (CC BY 4.0)"
	if s.Sample {
		source = "SAMPLE ONLY — synthetic data"
	}
	return join(tl, en, ja, source)
}

// ---------------------------------------------------------------------------
// Generic alert (rain / tsunami — PHASE_D §3)
// ---------------------------------------------------------------------------

// AlertData is the plain input for the generic trilingual alert text.
type AlertData struct {
	Title  string   // e.g. "Orange rainfall warning — Metro Manila"
	Lines  []string // short detail lines (areas, headline)
	Source string   // defaults to "Source: DOST-PAGASA (CC BY 4.0)"
	Sample bool
}

// Alert builds the generic trilingual alert post text.
func Alert(a AlertData) string {
	title := a.Title
	if title == "" {
		title = "Weather alert"
	}
	body := a.Lines
	if len(body) > 3 {
		body = body[:3]
	}
	tl := append([]string{title}, body...)
	tl = append(tl, "Sundan ang PAGASA at LGU para sa updates.")
	en := append([]string{title}, body...)
	en = append(en, "Follow PAGASA and your LGU for updates.")
	ja := append([]string{title}, body...)
	ja = append(ja, "PAGASAと自治体の最新情報をご確認ください。")
	if a.Sample {
		tl[0] = "SAMPLE · " + tl[0]
		en[0] = "SAMPLE · " + en[0]
		ja[0] = "SAMPLE · " + ja[0]
	}
	source := a.Source
	if source == "" {
		source = "Source: DOST-PAGASA (CC BY 4.0)"
	}
	if a.Sample {
		source = "SAMPLE ONLY — synthetic data"
	}
	return join(tl, en, ja, source)
}

// ---------------------------------------------------------------------------
// Taal
// ---------------------------------------------------------------------------

// TaalData is the plain input for the Taal post text.
type TaalData struct {
	Level         int
	LevelText     string
	BulletinTitle string
	BulletinDate  string
	Sample        bool
}

// Taal builds the Taal volcano post text.
func Taal(t TaalData) string {
	taalEN := t.LevelText
	if taalEN == "" {
		taalEN = "—"
	}
	taalTL := taalNameTL(t.Level, t.LevelText)
	taalJA := taalNameJA(t.Level)
	tl := []string{
		fmt.Sprintf("Taal Alert Level %d — %s.", t.Level, taalTL),
		fmt.Sprintf("%s.", t.BulletinTitle),
		"Bawal pumasok sa Taal Volcano Island (PDZ). Sundan ang PHIVOLCS.",
	}
	en := []string{
		fmt.Sprintf("Taal Alert Level %d — %s.", t.Level, taalEN),
		fmt.Sprintf("%s.", t.BulletinTitle),
		"Entry to Taal Volcano Island (PDZ) not allowed. Follow PHIVOLCS.",
	}
	ja := []string{
		fmt.Sprintf("タール火山 アラートレベル%d — %s。", t.Level, taalJA),
		fmt.Sprintf("%s。", t.BulletinTitle),
		"タール火山島(PDZ)への立ち入りは禁止です。PHIVOLCSの情報をご確認ください。",
	}
	if t.Sample {
		tl[0] = "SAMPLE · " + tl[0]
		en[0] = "SAMPLE · " + en[0]
		ja[0] = "SAMPLE · " + ja[0]
	}
	return join(tl, en, ja, "Source: PHIVOLCS")
}
