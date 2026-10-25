// Command preview renders the four Sakuna Bantay cards from fixtures plus
// two synthetic samples into an output directory for visual review.
//
// Usage (from go/): go run ./tools/preview -out /tmp/sakuna-cards [-fixtures ../fixtures]
//
// Outputs (all 1080x1080 PNG):
//
//	daily.png            – daily card from openmeteo + wovodat fixtures (live)
//	quake.png            – far quake from PHIVOLCS detail fixture (Baganga M5.1, live)
//	signal.png           – typhoon card from PAGASA fixture levels (live)
//	taal.png             – Taal gauge from wovodat fixture (live)
//	signal-synth.png     – synthetic signal SAMPLE (NCR/Cavite/Laguna/Batangas/Rizal 1–3)
//	quake-near-synth.png – synthetic near quake SAMPLE (M5.6 30 km SW of Nasugbu)
//
// Header "as-of" times (REVIEW_B #10): daily = forecast fetch time PHT,
// quake = event time PHT, signal = CAP sent time PHT, Taal = bulletin date.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/tomohiro-owada/sakuna-bantay/go/internal/card"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/geo"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/openmeteo"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/phivolcs"
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/volcano"
)

func main() {
	out := flag.String("out", "", "output directory for PNG cards")
	fixDir := flag.String("fixtures", "../fixtures", "fixtures directory")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "preview: -out DIR is required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "preview:", err)
		os.Exit(1)
	}
	f := fonts.LoadEmbedded()
	b, err := geo.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview: basemap:", err)
		os.Exit(1)
	}
	manila := phivolcs.Manila()
	read := func(name string) []byte {
		bb, err := os.ReadFile(filepath.Join(*fixDir, name))
		if err != nil {
			fmt.Fprintln(os.Stderr, "preview:", err)
			os.Exit(1)
		}
		return bb
	}
	save := func(name string, img image.Image) {
		p := filepath.Join(*out, name)
		fh, err := os.Create(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "preview:", err)
			os.Exit(1)
		}
		if err := png.Encode(fh, img); err != nil {
			fh.Close()
			fmt.Fprintln(os.Stderr, "preview:", err)
			os.Exit(1)
		}
		fh.Close()
		fmt.Println("wrote", p)
	}

	// --- Daily (live): openmeteo fixtures + Taal from wovodat + alerts.
	fcast := read("openmeteo-forecast.json")
	aq := read("openmeteo-aq.json")
	day, err := openmeteo.Parse(fcast, aq, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview: openmeteo:", err)
		os.Exit(1)
	}
	vol, err := volcano.Parse(read("wovodat-list.html"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview: volcano:", err)
		os.Exit(1)
	}
	dayDate := "25 Oct 2026"
	if t, err := time.Parse("2006-01-02", day.Date); err == nil {
		dayDate = t.In(manila).Format("2 Jan 2006")
	}
	dailyIn := card.DailyInput{
		Day:       *day,
		DateLabel: dayDate,
		TimeLabel: "25 Oct 2026, 6:13 PM PHT",
		TaalLevel: vol.Taal.Level,
		TaalText:  vol.Taal.LevelText,
		Alerts: []string{
			"Signal #1: Batanes, Cagayan",
			"Orange rainfall warning in effect for Metro Manila",
		},
	}
	img, _, err := card.RenderDaily(f, dailyIn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview: daily:", err)
		os.Exit(1)
	}
	save("daily.png", img)

	// --- Quake far (live): Baganga detail fixture (16 Oct 2026 05:13:36 PM PHT).
	evTime := time.Date(2026, 10, 16, 17, 13, 36, 0, manila)
	far := phivolcs.Event{
		ID: "2026_1016_0913", CanonicalID: "2026_1016_0913",
		Time: evTime, Lat: 7.92, Lon: 127.13, DepthKm: 2, Mag: 5.1,
		Place:  phivolcs.CleanPlace("073 km N 59° E of Baganga (Davao Oriental)"),
		Source: "PHIVOLCS",
		URL:    "https://earthquake.phivolcs.dost.gov.ph/2026_Earthquake_Information/October/2026_1016_0913_B1.html",
	}
	far.DistanceKm = phivolcs.DistanceToLasPinas(far.Lat, far.Lon)
	far.BearingDeg = phivolcs.BearingDeg(phivolcs.LasPinasLat, phivolcs.LasPinasLon, far.Lat, far.Lon)
	far.Compass = phivolcs.Compass16(far.BearingDeg)
	img, _, err = card.RenderQuake(f, b, card.QuakeInput{
		Event:     far,
		TimeLabel: evTime.Format("2 Jan 2006, 3:04 PM") + " PHT",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview: quake:", err)
		os.Exit(1)
	}
	save("quake.png", img)

	// --- Signal (live): PAGASA fixture (Rosal).
	sent, _ := time.Parse(time.RFC3339, "2026-10-24T04:50:38+08:00")
	img, _, err = card.RenderSignal(f, b, card.SignalInput{
		Cyclone:      "Typhoon Rosal (CHAMPI)",
		IssuedLabel:  sent.In(manila).Format("2 Jan 2006, 3:04 PM") + " PHT",
		Levels:       map[string]int{"Batanes": 2, "Cagayan": 1, "Ilocos Norte": 1},
		WatchSummary: "NCR: no signal",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview: signal:", err)
		os.Exit(1)
	}
	save("signal.png", img)

	// --- Taal (live): bulletin date as header as-of.
	taalIn := card.TaalInput{
		Level:         vol.Taal.Level,
		LevelText:     vol.Taal.LevelText,
		BulletinTitle: vol.Taal.BulletinTitle,
		BulletinDate:  "25 Oct 2026",
	}
	img, _, err = card.RenderTaal(f, taalIn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview: taal:", err)
		os.Exit(1)
	}
	save("taal.png", img)

	// --- Synthetic signal SAMPLE: watch-area provinces at levels 1–3.
	img, _, err = card.RenderSignal(f, b, card.SignalInput{
		Cyclone:     "Typhoon Rosal (CHAMPI)",
		IssuedLabel: "25 Oct 2026, 11:00 AM PHT",
		Levels: map[string]int{
			"Batanes": 3, "Cagayan": 2, "Ilocos Norte": 2,
			"Metro Manila": 2, "Cavite": 2, "Laguna": 1,
			"Batangas": 3, "Rizal": 1, "Quezon": 2,
		},
		WatchSummary: "NCR: Signal #2",
		Sample:       true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview: signal-synth:", err)
		os.Exit(1)
	}
	save("signal-synth.png", img)

	// --- Synthetic near quake SAMPLE: M5.6 30 km SW of Nasugbu, Batangas.
	nearTime := time.Date(2026, 10, 25, 18, 13, 0, 0, manila)
	near := phivolcs.Event{
		ID: "synth-nasugbu-m56", CanonicalID: "synth-nasugbu-m56",
		Time: nearTime, Lat: 13.88, Lon: 120.44, DepthKm: 10, Mag: 5.6,
		Place:  "30 km SW of Nasugbu, Batangas",
		Source: "PHIVOLCS",
	}
	near.DistanceKm = phivolcs.DistanceToLasPinas(near.Lat, near.Lon)
	near.BearingDeg = phivolcs.BearingDeg(phivolcs.LasPinasLat, phivolcs.LasPinasLon, near.Lat, near.Lon)
	near.Compass = phivolcs.Compass16(near.BearingDeg)
	img, _, err = card.RenderQuake(f, b, card.QuakeInput{
		Event:     near,
		TimeLabel: nearTime.Format("2 Jan 2006, 3:04 PM") + " PHT",
		Sample:    true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview: quake-near-synth:", err)
		os.Exit(1)
	}
	save("quake-near-synth.png", img)

	fmt.Printf("preview: wrote 6 cards to %s\n", *out)
}
