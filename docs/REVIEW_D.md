# Review of a simulated VPS cycle (`sakuna run`, live data, KILL_SWITCH present, no fb.env)

1. First-run baseline: with empty state, the run proposed a Taal post (level 1 vs unknown).
   When the previous state for a source is UNKNOWN (never recorded), record the current
   value as baseline silently — no Taal/Signal/Rain "change" post. Only later real changes
   post. Same for signal level and NCR rain colour. Tests.
2. Kill switch semantics: kill switch must block LIVE posting only. In dry-run mode
   (which is forced when KILL_SWITCH exists) the pipeline should still simulate fully:
   decide → render → write outbox (PNG + caption + meta) and record history-dryrun/state-dryrun,
   with the summary showing `fb=dry-run (kill switch)`. So the deployment starts disabled
   but observable. Keep: live mode never possible while KILL_SWITCH exists (Mode()). Update
   rules/run tests accordingly (rules.Decide's kill-switch input becomes "liveBlocked" or
   remove it from Decide and enforce in Mode — but make sure there is still a test that
   with KILL_SWITCH present and -live + valid fb.env the HTTP transport is never called).
3. Transient CAP fetch failure (one .cap returned EOF; a retry seconds later was fine) made
   the whole PAGASA source error. Add one retry (2 s later) per CAP fetch in fetch/pagasa. If
   a CAP still fails: if its feed entry is older than 24 h, skip it with a warning; otherwise
   PAGASA is "partial" — events from successfully parsed CAPs may still be emitted when they
   RAISE a level or are new alerts, but no lowering/lifting state change is applied in that
   pass. Tests.
4. In the run summary, list the planned posts in dry-run (kind + dedupe key) so logs show what
   would have been posted.
Run gofmt, go vet ./..., go test ./... until green. No network posting, no ssh, no push.
