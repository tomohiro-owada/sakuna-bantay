#!/usr/bin/env bash
# deploy/remote/watch.sh — cron wrapper (installed as
# "$SAKUNA_ROOT/bin/watch.sh"). One `sakuna run` pass per invocation; cron
# schedules it every few minutes. All written paths live under SAKUNA_ROOT.
# Logs append to logs/watch.log with size-based rotation (5 MB, keep
# watch.log.1..3).
#
# Posting stays OFF unless "$CONF/live.enabled" exists — a second explicit
# opt-in file in addition to fb.env and no KILL_SWITCH. (The binary itself
# defaults to dry-run without -live.)
#
# After the `sakuna run` pass, if "$CONF/site-publish.enabled" exists, run
# site-publish.sh (its own throttle decides), so a single cron line drives
# everything.
set -euo pipefail

SAKUNA_ROOT="${SAKUNA_ROOT:-$HOME/sakuna}"
CONF="${SAKUNA_CONF:-$HOME/.config/sakuna}"

STATE_DIR="$SAKUNA_ROOT/state"
CACHE_DIR="$SAKUNA_ROOT/cache"
OUT_DIR="$SAKUNA_ROOT/out"
SITE_OUT="$SAKUNA_ROOT/site"
LOG_DIR="$SAKUNA_ROOT/logs"
LOG="$LOG_DIR/watch.log"
LOCK_FILE="$STATE_DIR/watch.lock"
MAX_BYTES=5242880 # 5 MB

# Safety: every path we WRITE must live under SAKUNA_ROOT. Abort otherwise
# so a bad edit can never scribble outside the app directory.
for p in "$STATE_DIR" "$CACHE_DIR" "$OUT_DIR" "$SITE_OUT" "$LOG_DIR" "$LOG" "$LOCK_FILE"; do
  case "$p" in
    "$SAKUNA_ROOT"/*) ;;
    *)
      echo "watch.sh: refusing to run: path outside SAKUNA_ROOT: $p" >&2
      exit 1
      ;;
  esac
done

mkdir -p "$STATE_DIR" "$CACHE_DIR" "$OUT_DIR" "$LOG_DIR"

# Own flock (non-blocking) in addition to the binary's state/run.lock.
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "$(date -u +%FT%TZ) watch: already running (watch.lock held)" >>"$LOG"
  exit 0
fi

# Size-based rotation, keep 3 (watch.log.1..3). Runs before the redirect.
if [ -f "$LOG" ]; then
  size=$(wc -c <"$LOG")
  if [ "$size" -ge "$MAX_BYTES" ]; then
    rm -f "$LOG.3"
    mv -f "$LOG.2" "$LOG.3" 2>/dev/null || true
    mv -f "$LOG.1" "$LOG.2" 2>/dev/null || true
    mv -f "$LOG" "$LOG.1"
  fi
fi

exec >>"$LOG" 2>&1

LIVE_FLAG=()
if [ -f "$CONF/live.enabled" ]; then
  LIVE_FLAG=(-live)
fi

# Do NOT exec: capture the exit code so we can run the site publisher next.
set +e
"$SAKUNA_ROOT/bin/sakuna" run \
  -home "$SAKUNA_ROOT" \
  -fb-env "$CONF/fb.env" \
  -kill-switch "$CONF/KILL_SWITCH" \
  -cache "$CACHE_DIR" \
  -state "$STATE_DIR" \
  -out "$OUT_DIR" \
  -site-out "$SITE_OUT" \
  "${LIVE_FLAG[@]}"
RUN_CODE=$?
set -e

# Site publish (throttled inside the script). Single cron line drives all.
if [ -f "$CONF/site-publish.enabled" ]; then
  PUBLISHER="$SAKUNA_ROOT/bin/site-publish.sh"
  if [ -x "$PUBLISHER" ]; then
    set +e
    "$PUBLISHER"
    PUB_CODE=$?
    set -e
    if [ "$PUB_CODE" -ne 0 ]; then
      echo "watch: site-publish.sh exited $PUB_CODE" >&2
    fi
  else
    echo "watch: site-publish enabled but $PUBLISHER not executable" >&2
  fi
fi

exit "$RUN_CODE"
