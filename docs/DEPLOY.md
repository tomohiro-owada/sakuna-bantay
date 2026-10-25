# Sakuna Bantay — deploy guide (generic)

Runs from cron on a small Linux server as an unprivileged user.

`main` holds code only (`/site/` is gitignored). The published site lives on
the orphan `site` branch: always a single force-pushed commit containing
`site/**` (static build) + `.github/workflows/pages.yml`. GitHub GC drops
the replaced objects, so card PNG re-renders don't grow the repo.
`make site` rebuilds into `out/site` for verification.

- App files live under `$HOME/sakuna` (`bin/sakuna`, `bin/watch.sh`,
  `bin/site-publish.sh`, `bin/pages.yml`, `state/`, `cache/`, `out/`,
  `logs/`, `site/`).
- Config lives under `$HOME/.config/sakuna` (`fb.env` mode 0600,
  `KILL_SWITCH`, `live.enabled`, `site-publish.enabled`).
- Overrides (mainly for testing):
  `SAKUNA_ROOT` (default `$HOME/sakuna`),
  `SAKUNA_CONF` (default `$HOME/.config/sakuna`),
  `SAKUNA_SITE_REMOTE` (default `git@github.com:tomohiro-owada/sakuna-bantay.git`).

Posting needs all opt-ins: a usable `fb.env`, a `live.enabled` file, and no
kill switch. If `KILL_SWITCH` exists, nothing posts (dry-run only). The
`sakuna run` binary defaults to dry-run (outbox files); only `-live` with a
usable `fb.env` and no kill switch posts. Removing `live.enabled` returns to
dry-run without touching keys.

Local merge test (no ssh):

```sh
./deploy/test-crontab-merge.sh
```

Example cron entry (every 3 minutes; the wrapper throttles site rebuilds and
publishes):

```sh
*/3 * * * * $HOME/sakuna/bin/watch.sh
```

## Site publish (GitHub Pages, orphan `site` branch)

`.github/workflows/pages.yml` is kept on `main` too, identical: on push to
`site` (+ `workflow_dispatch`) it checks out the `site` branch
(`actions/checkout@v4` with `ref: site`), `configure-pages@v5`,
`upload-pages-artifact@v3` with `path: site`, `deploy-pages@v4`.

`site-publish.sh` (disabled unless `site-publish.enabled` exists) builds the
orphan commit in a scratch tree `$SAKUNA_ROOT/state/publish-tmp` (deleted &
recreated each run): copies `$SAKUNA_ROOT/site` → `site/`, copies
`$SAKUNA_ROOT/bin/pages.yml` (shipped by `ops/deploy.sh` from
`.github/workflows/pages.yml`) → `.github/workflows/pages.yml`, commits as
`Sakuna Bantay bot <noreply@example.invalid>` (`site: YYYY-MM-DD HH:MM PHT`),
then `git push --force "$SAKUNA_SITE_REMOTE" HEAD:refs/heads/site`.

Change detection is a content hash of `site/*.html` with volatile
`Generated` lines stripped (`sed /data-volatile/d` — the generator marks
them with a `data-volatile` attribute) plus the referenced PNG names. Push
only when that hash differs from `state/site-last-hash`: hourly cadence
(≥55 min since `state/site-last-push`), or within ~10 min when the alert
flag `state/site-alert-dirty` exists (deleted after a successful push).
The public site copies only referenced cards under stable names (`today.png`,
`taal.png`, `quake-latest.png` if shown); `*-synth*.png` SAMPLEs never ship.
