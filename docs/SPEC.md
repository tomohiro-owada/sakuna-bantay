# Sakuna Bantay — MVP spec (Part 2)

Unofficial, non-profit weather & disaster watch for Las Piñas / South Metro Manila, built ONLY
from official/open data. Sibling of Presyo Bantay (/workspace/presyo-bantay — read-only
reference: Go in go/, Facebook cards rendered in Go in go/internal/fbchart, trilingual text
Taglish first, then English, then Japanese). NEVER modify /workspace/presyo-bantay.

Hard rules: Go only (no Python), static site only, no network posting of any kind (Facebook
posting is NOT implemented — dry-run files only), no secrets in code, polite HTTP
(User-Agent "SakunaBantay/0.1 (+https://abalol.xyz/sakuna-bantay/; non-profit)", timeouts
20s, at most 1 request per host per second, on-disk cache in cache/).

Module: github.com/tomohiro-owada/sakuna-bantay/go in go/ (already exists, holds tools/mkbrand).
Brand palette constants already exist in go/tools/mkbrand — move them into a shared package
go/internal/brand (Navy #0E2A3B, Teal #0F6B6B, TealLite #2A9D8F, Amber #F2A516,
Cream #F7F2E6) and reuse. Brand mark PNG: brand/favicon/logo-mark-144.png (embed a copy).
Fonts in assets/fonts/ (Noto Sans Regular/Medium/SemiBold/Bold .ttf, Noto Sans JP
Regular/Bold .otf (CFF), Montserrat for the wordmark). Embed the needed fonts via a package
go/internal/fonts (copy files into it; keep OFL texts). Japanese glyphs → Noto Sans JP
fallback per rune.

Constants: Las Piñas = 14.44 N, 121.01 E. Time zone Asia/Manila (PHT, UTC+8); all displayed
times in PHT, e.g. "25 Oct 2026, 6:13 PM PHT".

## Packages (go/internal/...)

### fetch (shared HTTP)
Client with UA, timeout, per-host 1 req/s limiter, disk cache keyed by URL storing body +
ETag + Last-Modified + fetched-at; conditional GET (If-None-Match / If-Modified-Since); on
304 use cached body. Min refetch interval per source configurable (e.g. PHIVOLCS 5 min).
Fixture mode: `-fixtures DIR` reads from files instead of network (for tests/offline).

### openmeteo
GET https://api.open-meteo.com/v1/forecast?latitude=14.44&longitude=121.01&timezone=Asia/Manila
&daily=temperature_2m_max,temperature_2m_min,apparent_temperature_max,uv_index_max,precipitation_sum,precipitation_probability_max
&hourly=precipitation_probability,temperature_2m,apparent_temperature,uv_index&forecast_days=2
and https://air-quality-api.open-meteo.com/v1/air-quality?latitude=14.44&longitude=121.01
&timezone=Asia/Manila&current=us_aqi,pm2_5&hourly=us_aqi,pm2_5&forecast_days=1
Return a Today struct: date, max temp, min temp, apparent max (heat index proxy), UV max,
precip sum, hourly rain probability for 06:00–23:00 of the target day, US AQI + PM2.5
(current, plus daily max of hourly). AQI labelled "model estimate" everywhere. UV category
(Low/Moderate/High/Very High/Extreme) and US AQI category (Good, Moderate, Unhealthy for
Sensitive Groups, Unhealthy, Very Unhealthy, Hazardous) helpers. Fixtures:
fixtures/openmeteo-forecast.json, fixtures/openmeteo-aq.json.

### pagasa (CAP)
Atom feed https://publicalert.pagasa.dost.gov.ph/feeds/ (fixture fixtures/pagasa-feed.xml):
entries have id, title, updated, link[type=application/cap+xml] href. Path segment tells
the product: /output/wcp/ = Tropical Cyclone Warning (one CAP per wind signal level;
title "... Signal #N"), /output/acp/ = Tropical Cyclone Alert, /output/gfa/ = General Flood
Advisory; also handle any other (rainfall warnings / thunderstorm etc.) generically.
Parse CAP 1.2 XML (fixtures pagasa-wcp-signal1.cap, pagasa-wcp-signal2.cap, pagasa-gfa.cap,
pagasa-acp.cap): identifier, sent, msgType, references, info{event, headline, severity,
urgency, certainty, expires, description, instruction, web, parameters}, areas{areaDesc,
polygon (keep but optional), geocodes (valueName SAME → PSGC code)}.
Fetch the linked .cap only for entries updated within the last 72 h (cache them forever by
URL — CAPs are immutable).
Active = status Actual, msgType != Cancel, expires > now, not superseded (references).
Tropical cyclone wind signals: signal level from headline/title "Signal #N" (1–5); cyclone
name from headline ("Typhoon Rosal (CHAMPI)"). Build ProvinceSignals: map province name →
max active signal level, plus whether any watch-area is affected.
Watch area (PSGC SAME prefixes — handle both 9-digit and 10-digit PSGC):
 NCR: 9-digit starts "13"; 10-digit starts "13"
 Cavite 9d "0421" / 10d "04021"; Laguna "0434"/"04034"; Batangas "0410"/"04010";
 Rizal "0458"/"04058". Also fall back to areaDesc name match ("Metro Manila", "NCR",
 "Las Piñas", "Cavite", "Laguna", "Batangas", "Rizal").
Rainfall warnings: detect event names containing "Rainfall" with color Yellow/Orange/Red
(from event/headline/parameters) and whether the watch area (esp. NCR) is included.
Tsunami: any CAP whose event contains "Tsunami".

### phivolcs (earthquakes)
https://earthquake.phivolcs.dost.gov.ph/ is a huge (~2.8 MB) HTML table; use conditional GET
(server returns ETag + Last-Modified, verified to answer 304) and minimum 5-min refetch.
Rows: cells [datetime "25 October 2026 - 06:13 PM" (PHT), lat "08.01", lon "122.38",
depth km "036", magnitude "1.6", location "004   km N 37° W of Gutalac (Zamboanga Del
Norte)"], first cell has <a href="2026_Earthquake_Information\October\2026_1025_1013_B1.html">
(backslashes! convert to forward slashes, absolute URL under the site root) — the
href basename is the stable source id. Parse robustly with golang.org/x/net/html (skip header
and month rows). Fixture fixtures/phivolcs-list.html. Event detail pages may list
"Reported Intensities"; implement an optional parser for the detail page that extracts
intensity reports "Intensity III - City of Manila; ..." into (place, roman level) — only
fetched for events that already pass M>=4.0 within 300 km, best effort, failures ignored.
Backup: USGS FDSN https://earthquake.usgs.gov/fdsnws/event/1/query?format=geojson&
minlatitude=4&maxlatitude=22&minlongitude=116&maxlongitude=128&minmagnitude=4&orderby=time&limit=50
(fixture fixtures/usgs-ph.geojson) used when PHIVOLCS fails; mark source "USGS".
Haversine distance to Las Piñas + compass bearing helper.

### volcano (Taal)
https://wovodat.phivolcs.dost.gov.ph/bulletin/list-of-bulletin (fixture
fixtures/wovodat-list.html). Page text contains "Alert Level Status: Taal - 1 Kanlaon - 2
Bulusan - 1 Pinatubo - 0 Mayon - 2" → parse all volcano levels. Latest Taal bulletin link:
href "/bulletin/activity-tvo?bid=16933" (+ "&lang=en"), and its title text "Taal Volcano
Summary of 24Hr Observation 25 October 2026 12:00 AM". Return TaalStatus{Level int,
BulletinURL, BulletinTitle, BulletinID}. Level descriptions (PHIVOLCS Taal scheme):
0 Normal, 1 Low-level unrest, 2 Increasing unrest, 3 Magmatic unrest, 4 Hazardous eruption
imminent, 5 Hazardous eruption in progress.

### rules (posting rules — PURE logic, no I/O except kill-switch check helper; thorough tests)
Inputs: Now (time), a list of candidate Events {Kind (Daily, Signal, Rain, Quake, Taal,
Tsunami), SourceID, Priority flags, Payload}, and History (posted records with time,
kind, source id, dedupe key), plus previous State (last seen NCR-area max signal, last Taal
level, last rain color for NCR).
Rules:
- Daily: one post per PHT calendar day at/after 06:00 PHT (window 06:00–11:59; if missed by
  noon, skip). Dedupe key "daily:YYYY-MM-DD".
- Signal: post when the max wind signal over the watch area (NCR/Cavite/Laguna/Rizal/
  Batangas) CHANGES (raised, lowered, or lifted to 0) vs State. Dedupe key includes CAP id.
- Rain: post when NCR has an ORANGE or RED rainfall warning (new CAP id, or color change).
- Quake: post if (M>=5.0 and distance to Las Piñas <=150 km) OR (M>=6.5 anywhere within
  PH bounds lat 4–22, lon 116–128) OR (any reported intensity >= III in NCR, Cavite,
  Laguna, Rizal or Batangas). Dedupe by source id; also treat PHIVOLCS + USGS events
  within 60 s and 50 km as duplicates; PHIVOLCS revisions of the same event (same
  timestamp/location, new href suffix like _B1F / _B2) count as the same event.
- Taal: post when the alert level differs from State.
- Tsunami: any active tsunami CAP/advisory.
- Caps: max 6 posts per rolling 24 h; posts >= 30 min apart. Bypass of the 30-min spacing
  (but still counted in the 24-h cap) for: Tsunami, Rain RED, Signal >= 3 in the watch area.
  When cap would be exceeded, higher priority wins (Tsunami > Signal>=3 > Rain RED >
  Quake > Signal/Rain other > Taal > Daily); deferred events are reported as Deferred with
  reason, not dropped silently.
- Dedupe: never post the same dedupe key twice.
- Kill switch: if file `KILL_SWITCH` exists in the state dir (path configurable), Decide
  returns no posts and reason "kill switch".
Output: Decision{Post []Event, Deferred []{Event, Reason}, Skipped []{Event, Reason}}.
Table-driven tests covering every rule, boundaries (exactly 150 km, M4.99/5.0, 6.5, 29/30 min,
6th/7th post, 05:59/06:00 PHT, kill switch, dedupe, bypass still counts toward cap).

### geo (offline basemap)
A generator `go/tools/mkgeo` reads assets/geo/geoBoundaries-PHL-ADM2_simplified.geojson
(87 provinces/NCR districts, property shapeName; CC BY 3.0 IGO) and
assets/geo/ne_50m_land.geojson (Natural Earth, public domain; context land e.g. Taiwan,
Borneo), clips to lon 114–130 / lat 3–23, simplifies (Douglas–Peucker ~0.005°), quantizes,
and writes a compact embedded file go/internal/geo/basemap.bin (or .json.gz) —
committed, < 1.5 MB. Runtime package geo: provinces with names + polygons, land polygons,
equirectangular projection with cos(lat) correction for a bbox → pixel rect, name matching
helper (normalize case/accents/“Province of”, NCR district names map to "Metro Manila").

### card (1080x1080 PNG renderers; package go/internal/card)
Style: calm, readable, brand palette, cream background (#F7F2E6) with navy text, teal
accents, amber/red only for alerts. Header band: brand mark (small) + "Sakuna Bantay" +
card title + date/time PHT. Footer: attribution line(s) small, e.g.
"Data: DOST-PAGASA (CC BY 4.0), PHIVOLCS, Open-Meteo (CC BY 4.0), geoBoundaries (CC BY 3.0
IGO), Natural Earth" — include only the relevant sources per card — plus
"Unofficial · Not an official warning · abalol.xyz/sakuna-bantay". All text must fit (measure
and shrink/ellipsize; add a test that no text overflows the canvas). If the card is built
from synthetic data, draw a big diagonal or corner "SAMPLE" badge (red/amber) clearly.
1. Daily Las Piñas card: big max temp, "Feels like" apparent max, min temp; hourly rain
   chance bars 06:00–23:00 (labels every 3 h, % on top for >=30%); chips: UV max + category,
   US AQI + category "(model est.)", PM2.5, Taal alert level; active alerts strip at the
   bottom (e.g. "Signal #1: Batanes, Cagayan" / "No active PAGASA warnings for NCR area" /
   rainfall warnings / latest notable quake).
2. Earthquake card: basemap (land + province outlines) zoomed to include epicenter and Las
   Piñas (min span ~4°, whole PH when far); amber/red star at epicenter, Las Piñas dot +
   label, dashed distance ring around Las Piñas with radius = distance and the distance
   label ("123 km from Las Piñas"); big M value, depth km, time PHT, location text, source
   (PHIVOLCS or USGS). Note: "Preliminary; check PHIVOLCS for updates".
3. Typhoon / wind signal card: Luzon–Visayas map (lon 116–127, lat 9–21) with provinces
   shaded by signal level (1 light-amber .. 5 dark red; legend), Las Piñas marker, cyclone
   name, issued time PHT, watch-area summary ("NCR: no signal" / "NCR: Signal #2").
4. Taal gauge card: semicircular gauge 0–5 with the needle at the current level, level
   names, big "Alert Level 1 — Low-level unrest", bulletin date, short reminders text from
   PHIVOLCS ("Entry to Taal Volcano Island (PDZ) not allowed").

### posttext (trilingual dry-run text)
For each card a text: Taglish block first, then "———", English, then "———", Japanese; each
block 2–5 short lines; then a source line ("Source: DOST-PAGASA / PHIVOLCS / Open-Meteo …")
and link https://abalol.xyz/sakuna-bantay/ and "Unofficial · not an official warning — follow
PAGASA, PHIVOLCS and your LGU." Tone calm, no panic words, no emoji spam (max 2 emoji).
Golden tests with fixtures.

### site (static site generator; go/internal/site + `sakuna site -out out/site`)
Plain HTML + one CSS file, no JS framework (tiny optional inline JS ok), mobile-first,
brand colors, logo header (brand/sakuna-bantay-logo.svg + favicons from brand/favicon/).
Pages: index (Today card image + summary), alerts.html (active PAGASA CAP items affecting
watch area + all active TC signals), quakes.html (latest 20 events with distance to
Las Piñas, notable highlighted), taal.html, sources.html (Sources & licenses: DOST-PAGASA
CAP CC BY 4.0, PHIVOLCS, Open-Meteo CC BY 4.0, USGS public domain, geoBoundaries CC BY 3.0
IGO, Natural Earth public domain, Noto fonts OFL; prominent disclaimer "Unofficial,
not an official source. For warnings follow DOST-PAGASA, PHIVOLCS, NDRRMC and your LGU.").
Trilingual labels where short (Taglish / EN / JP). Base path /sakuna-bantay/ configurable.

## CLI: go/cmd/sakuna
Subcommands:
- `sakuna fetch [-fixtures DIR] [-cache DIR]` → writes out/snapshot.json (all normalized data).
- `sakuna cards [-snapshot out/snapshot.json] -out samples/` → renders all cards that have
  live data, and synthetic SAMPLE cards (marked) for card types with no live event:
  daily-YYYYMMDD.png, quake-<id>.png (latest notable or latest M>=4 nearest; if none notable,
  still render the most relevant real event and label it in text), signal-*.png, taal-*.png,
  plus matching .txt post texts and a samples/manifest.json listing each file, live vs
  sample, source ids.
- `sakuna decide [-state DIR] [-now RFC3339]` → runs rules on the snapshot, prints decision
  JSON (dry-run; never posts).
- `sakuna site -out out/site`.
- `sakuna all` = fetch + cards + decide + site.
Makefile targets: brand, test, samples, site.
README.md describing all of the above, data sources & licenses, and that it is unofficial.
