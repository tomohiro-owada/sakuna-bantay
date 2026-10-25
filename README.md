# Sakuna Bantay

Unofficial, non-profit weather & disaster watch for Las Piñas / South Metro
Manila, built ONLY from official/open data.

> **Unofficial · Non-profit · Open data.**
> Sakuna Bantay is **not an official source**. For warnings, always follow
> DOST-PAGASA, PHIVOLCS, NDRRMC and your LGU.

Sibling of [Presyo Bantay](https://github.com/tomohiro-owada/presyo-bantay) (read-only reference).
Go only, static site only. **Posting defaults to dry-run**: `sakuna run`
writes outbox files unless `-live` is given with a usable `fb.env` and no
kill switch (see `docs/DEPLOY.md`).

Constants: Las Piñas = 14.44 N, 121.01 E. All displayed times are PHT
(Asia/Manila, UTC+8), e.g. "25 Oct 2026, 6:13 PM PHT".

## Data sources & licenses

| Source | Use | License |
|---|---|---|
| DOST-PAGASA CAP alerts (`publicalert.pagasa.dost.gov.ph`) | Tropical cyclone wind signals, rainfall warnings, flood advisories, tsunami | CC BY 4.0 |
| PHIVOLCS earthquake list + detail pages | Earthquakes (distance/bearing from Las Piñas, intensities) | PHIVOLCS (official info, link back) |
| WOVODAT / PHIVOLCS bulletin list | Taal (and other volcano) alert levels + latest bulletin | PHIVOLCS |
| Open-Meteo forecast + air quality | Daily max/feels-like/min, UV, rain chance, AQI/PM2.5 | CC BY 4.0 (AQI labelled "model estimate" everywhere) |
| USGS FDSN GeoJSON | Earthquake backup when PHIVOLCS fails | Public domain |
| geoBoundaries PHL-ADM2 | Offline province basemap | CC BY 3.0 IGO |
| Natural Earth `ne_50m_land` | Context land | Public domain |
| Noto / Montserrat fonts | Card + site text | SIL OFL |

HTTP is polite: `User-Agent: SakunaBantay/0.1 (+https://abalol.xyz/sakuna-bantay/; non-profit)`,
20 s timeouts, at most 1 request per host per second, on-disk cache in
`cache/` with ETag / Last-Modified conditional GET.

## How to run

```sh
# from repo root
make brand   # render brand/ (SVG, profile, cover, favicons)
make test    # gofmt + go vet + go test ./...

# live run (once per source; the reviewer runs this)
make samples # fetch + cards + decide + site → samples/, out/site/
make site    # rebuild only the site from out/snapshot.json + samples/

# fixture / offline run (no network)
cd go && go run ./cmd/sakuna all \
  -fixtures ../fixtures -cache ../cache \
  -out /tmp/sb-fixture -brand ../brand \
  -now 2026-10-25T18:00:00+08:00
```

CLI subcommands (`cd go && go run ./cmd/sakuna <cmd> -h`):

```sh
go run ./cmd/sakuna fetch \
  [-fixtures DIR] [-cache ../cache] [-out ../out/snapshot.json] [-now RFC3339]

go run ./cmd/sakuna cards \
  [-snapshot ../out/snapshot.json] -out ../samples

go run ./cmd/sakuna decide \
  [-snapshot ../out/snapshot.json] [-state DIR] [-now RFC3339]

go run ./cmd/sakuna site \
  [-snapshot ../out/snapshot.json] [-cards ../samples] \
  [-out ../out/site] [-base /sakuna-bantay/] [-brand ../brand]

go run ./cmd/sakuna all \
  [-fixtures DIR] [-cache ../cache] [-out ../out] \
  [-snapshot FILE] [-samples-out DIR] [-site-out DIR] \
  [-state DIR] [-now RFC3339] [-base /sakuna-bantay/] [-brand ../brand]

go run ./cmd/sakuna run \
  [-home DIR] [-state DIR] [-cache DIR] [-out DIR] \
  [-fb-env FILE] [-kill-switch FILE] [-live] [-dry-run] \
  [-fixtures DIR] [-now RFC3339] [-site-out DIR] \
  [-base /sakuna-bantay/] [-brand ../brand]
```

- `fetch` writes `snapshot.json` (Today, PAGASA active alerts + province
  signals + watch-area summary + rainfall + tsunami, PHIVOLCS/USGS quakes
  with notable flags, Taal status, fetch times, per-source errors — a
  failing source never aborts the run).
- `cards` renders from the snapshot: `daily-YYYYMMDD.png`,
  `quake-<id>.png`, `signal-*.png`, `taal-*.png`, each with a matching
  `.txt` trilingual post text and `manifest.json` (live vs `SAMPLE`,
  source ids). Card types with no live event get synthetic `SAMPLE` cards
  (marked on the image and in the manifest); with no notable quake the most
  relevant real recent event is still rendered as LIVE with the line
  `latest notable: none; showing most relevant recent event` plus a
  synthetic near-quake `SAMPLE`. The daily alerts strip lists only ACTIVE
  (non-expired) items.
- `decide` runs the posting rules against `-state` (`state.json` +
  `history.json` + optional `KILL_SWITCH`), prints Decision JSON, dry-run
  only, never posts.
- `run` performs one cron pass (never loops): flock-guarded fetch with
  per-source min-refetch (quakes 2.5 min, PAGASA feed 10 min, Open-Meteo /
  WOVODAT 15 min, CAPs immutable), decide with the mode's history/state,
  render + post each event (every post includes an image; tsunami & rain
  use a generic alert card), then writes `out/latest/` and rebuilds the
  site (at most every 15 min). Default is dry-run (outbox files in
  `out/outbox/`, separate `history-dryrun.json` / `state-dryrun.json`);
  only `-live` with a usable `fb.env` (0600, self-owned) and no kill
  switch posts to Facebook (Graph API v21.0). See `docs/DEPLOY.md`.
- `site` copies `brand/sakuna-bantay-logo.svg`, `brand/favicon/*` and the
  card PNGs, then writes `index.html`, `alerts.html`, `quakes.html`,
  `taal.html`, `sources.html` + `style.css`. All nav links are relative so
  the site also works opened locally from disk; `-base` only sets absolute
  references (default `/sakuna-bantay/`).

## Deploy

Runs from cron on a small Linux server as an unprivileged user; see
`docs/DEPLOY.md` for the generic layout (`$HOME/sakuna`,
`$HOME/.config/sakuna`, opt-in files, kill switch).

`main` holds code only (`/site/` is gitignored). The public site is the
orphan `site` branch (single force-pushed commit with `site/**` +
`.github/workflows/pages.yml`); `make site` rebuilds into `out/site`, and
the public build ships only referenced cards under stable names
(`today.png`, `taal.png`) — never `*-synth*.png` SAMPLEs.

```sh
./deploy/test-crontab-merge.sh  # local merge test (no ssh)
```

## Posting rules (summary)

Pure logic in `go/internal/rules` (see `docs/SPEC.md` §rules for the full
table-driven spec):

- **Daily:** one post per PHT calendar day, 06:00–11:59 PHT. Dedupe
  `daily:YYYY-MM-DD`.
- **Signal:** post when the max wind signal over the watch area
  (NCR/Cavite/Laguna/Rizal/Batangas) *changes* (raised, lowered, lifted).
- **Rain:** post on new ORANGE/RED rainfall warning for NCR (new CAP id or
  color change).
- **Quake:** post if (M≥5.0 within 150 km of Las Piñas) OR (M≥6.5 in PH
  lat 4–22, lon 116–128) OR (intensity ≥ III in NCR/Cavite/Laguna/Rizal/
  Batangas). PHIVOLCS+USGS within 60 s / 50 km and PHIVOLCS `_B1F`/`_B2`
  revisions dedupe.
- **Taal:** post when the alert level changes.
- **Tsunami:** any active tsunami CAP posts.
- **Caps:** max 6 posts / rolling 24 h, ≥30 min apart. Bypass (still
  counted) for Tsunami, Rain RED, Signal ≥3 in the watch area. Higher
  priority wins; the rest are Deferred with reasons, never silently dropped.
- **Dedupe:** never post the same dedupe key twice.
- **Kill switch:** if `<state>/KILL_SWITCH` exists, post nothing.

## Dry-run & posting

`decide` prints JSON and exits. Card `.txt` files are the trilingual
dry-run post texts (Taglish → `———` → English → `———` → Japanese, then
`Source: …`, `https://abalol.xyz/sakuna-bantay/`,
`Unofficial · not an official warning — …`). `sakuna run` without `-live`
writes the same image+text pairs to `out/outbox/` and never touches the
Graph API (the HTTP transport is only constructed for live posts).
