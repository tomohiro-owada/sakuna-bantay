#!/usr/bin/env bash
# deploy/remote/site-publish.sh — publishes the local static site to the
# orphan `site` branch (single force-pushed commit with site/** +
# .github/workflows/pages.yml). Invoked by watch.sh after each `sakuna run`
# pass (or by hand); its own throttle decides whether to push.
#
# Disabled unless "$CONF/site-publish.enabled" exists (exits 0 with a log
# line). Layout: freshly rendered site "$SAKUNA_ROOT/site", workflow source
# "$SAKUNA_ROOT/bin/pages.yml" (shipped by ops/deploy.sh from
# .github/workflows/pages.yml). Each run builds the orphan commit in a
# scratch work tree "$SAKUNA_ROOT/state/publish-tmp" (deleted & recreated
# every run): `git init`, copy site/ + workflow, commit as
# "Sakuna Bantay bot <noreply@example.invalid>", then
# `git push --force "$SAKUNA_SITE_REMOTE" HEAD:refs/heads/site`.
# GitHub GC drops the replaced objects, so the repo does not grow.
#
# Change detection: content hash = sha256 of site/*.html with volatile
# "Generated" lines stripped (sed /data-volatile/d — the generator marks
# them with a data-volatile attribute) plus the sorted referenced PNG names.
# Push only if that hash differs from "$STATE/site-last-hash". Throttle:
# alert flag ("$STATE/site-alert-dirty") → at most one push per 10 min;
# otherwise hourly (>= 55 min since last push). The flag is deleted only
# after a successful push. Last push epoch lives in
# "$STATE/site-last-push".
set -euo pipefail

SAKUNA_ROOT="${SAKUNA_ROOT:-$HOME/sakuna}"
CONF="${SAKUNA_CONF:-$HOME/.config/sakuna}"
SAKUNA_SITE_REMOTE="${SAKUNA_SITE_REMOTE:-git@github.com:tomohiro-owada/sakuna-bantay.git}"

SITE_SRC="$SAKUNA_ROOT/site"
WORKFLOW_SRC="$SAKUNA_ROOT/bin/pages.yml"
STATE_DIR="$SAKUNA_ROOT/state"
TMP_DIR="$STATE_DIR/publish-tmp"
LOG_DIR="$SAKUNA_ROOT/logs"
LOG="$LOG_DIR/site-publish.log"
LOCK_FILE="$STATE_DIR/site-publish.lock"
LAST_PUSH_FILE="$STATE_DIR/site-last-push"
HASH_FILE="$STATE_DIR/site-last-hash"
DIRTY_FLAG="$STATE_DIR/site-alert-dirty"
MAX_BYTES=5242880 # 5 MB

# Safety: everything we write lives under SAKUNA_ROOT.
for p in "$SITE_SRC" "$STATE_DIR" "$TMP_DIR" "$LOG_DIR" "$LOG" "$LOCK_FILE"; do
  case "$p" in
    "$SAKUNA_ROOT"/*) ;;
    *)
      echo "site-publish.sh: refusing to run: path outside SAKUNA_ROOT: $p" >&2
      exit 1
      ;;
  esac
done

mkdir -p "$STATE_DIR" "$LOG_DIR"

# Own flock (non-blocking).
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "$(date -u +%FT%TZ) site-publish: already running (lock held)"
  exit 0
fi

# Size-based rotation, keep 3. Runs before the redirect.
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

if [ ! -f "$CONF/site-publish.enabled" ]; then
  echo "$(date -u +%FT%TZ) site-publish: disabled (no site-publish.enabled)"
  exit 0
fi

NOW=$(date +%s)
LAST_PUSH=0
if [ -f "$LAST_PUSH_FILE" ]; then
  LAST_PUSH=$(cat "$LAST_PUSH_FILE" 2>/dev/null || echo 0)
  case "$LAST_PUSH" in
    ''|*[!0-9]*) LAST_PUSH=0 ;;
  esac
fi
AGE=$((NOW - LAST_PUSH))
if [ "$AGE" -lt 0 ]; then
  AGE=0
fi

DIRTY=0
if [ -f "$DIRTY_FLAG" ]; then
  DIRTY=1
fi

if [ ! -d "$SITE_SRC" ]; then
  echo "$(date -u +%FT%TZ) site-publish: nothing to publish (no $SITE_SRC)"
  exit 0
fi

if [ ! -f "$WORKFLOW_SRC" ]; then
  echo "$(date -u +%FT%TZ) site-publish: missing workflow source $WORKFLOW_SRC (deploy ships .github/workflows/pages.yml there)" >&2
  exit 1
fi

# Content hash: stripped HTML + referenced PNG names. Volatile timestamps
# live on lines carrying data-volatile (generator guarantee).
CONTENT_HASH=$({
  for f in "$SITE_SRC"/*.html; do
    [ -f "$f" ] || continue
    sed '/data-volatile/d' "$f"
    echo "--- $(basename "$f")"
  done
  echo "--- png-refs ---"
  grep -ho 'src="[^"]*\.png"' "$SITE_SRC"/*.html 2>/dev/null | sort -u || true
} | sha256sum | cut -d' ' -f1)

STORED_HASH=""
if [ -f "$HASH_FILE" ]; then
  STORED_HASH=$(cat "$HASH_FILE" 2>/dev/null | tr -d '[:space:]' || echo "")
fi

if [ -n "$STORED_HASH" ] && [ "$CONTENT_HASH" = "$STORED_HASH" ]; then
  echo "$(date -u +%FT%TZ) site-publish: site unchanged (content $CONTENT_HASH), nothing to publish"
  exit 0
fi

if [ "$DIRTY" -eq 1 ]; then
  # Alert fast path: at most one push per 10 min.
  if [ "$AGE" -lt 600 ]; then
    echo "$(date -u +%FT%TZ) site-publish: throttled (alert-dirty, last push ${AGE}s ago, content $CONTENT_HASH)"
    exit 0
  fi
else
  # Hourly cadence.
  if [ "$AGE" -lt 3300 ]; then
    echo "$(date -u +%FT%TZ) site-publish: throttled (last push ${AGE}s ago, content $CONTENT_HASH)"
    exit 0
  fi
fi

# Fresh scratch work tree.
rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR/site" "$TMP_DIR/.github/workflows"
if command -v rsync >/dev/null 2>&1; then
  rsync -a --delete "$SITE_SRC/" "$TMP_DIR/site/"
else
  cp -r "$SITE_SRC/." "$TMP_DIR/site/"
fi
cp "$WORKFLOW_SRC" "$TMP_DIR/.github/workflows/pages.yml"

cd "$TMP_DIR"
if git init -q -b site 2>/dev/null; then
  :
else
  git init -q
  git checkout -q -b site
fi
git add site .github/workflows/pages.yml

PHT_STAMP="$(TZ=Asia/Manila date +"%Y-%m-%d %H:%M PHT")"
git -c user.name="Sakuna Bantay bot" -c user.email="noreply@example.invalid" \
  commit -q -m "site: $PHT_STAMP"

git push --force "$SAKUNA_SITE_REMOTE" HEAD:refs/heads/site

date +%s >"$LAST_PUSH_FILE"
printf '%s\n' "$CONTENT_HASH" >"$HASH_FILE"
rm -f "$DIRTY_FLAG"
echo "$(date -u +%FT%TZ) site-publish: pushed ($PHT_STAMP, content $CONTENT_HASH)"
