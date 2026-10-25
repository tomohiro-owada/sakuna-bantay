#!/usr/bin/env bash
# deploy/test-crontab-merge.sh — local test for the crontab merge logic.
# Feeds a fictional sample crontab through deploy/lib-crontab-merge.sh and
# asserts: other-app lines unchanged & in order, sakuna block replaced, merge
# is idempotent. No ssh, no network.
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck disable=SC1091
source deploy/lib-crontab-merge.sh

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# Fictional sakuna block (mirrors the ops/crontab-block.txt shape, but with
# example paths so this public test carries no real host details).
BLOCK="$WORK/sakuna-block.txt"
cat >"$BLOCK" <<'EOF'
# BEGIN sakuna-bantay
# Sakuna Bantay cron block (example; managed by the deploy script).
*/3 * * * * /home/example/sakuna/bin/watch.sh
15 3 * * 0 find /home/example/sakuna/logs -name 'watch.log.*' -mtime +30 -delete
# END sakuna-bantay
EOF

pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1" >&2; exit 1; }

# --- 1. Sample other-app crontab (no sakuna block yet) ---
cat >"$WORK/other-old" <<'EOF'
MAILTO=owner@example.com
# Example other-app — do not touch
*/5 * * * * /home/example/other-app/bin/watch.sh
30 2 * * * /home/example/other-app/bin/daily.sh
EOF

merge_crontab "$WORK/other-old" "$BLOCK" "$WORK/new1"
verify_crontab_merge "$WORK/other-old" "$WORK/new1" || fail "verify fresh merge"
# Other-app lines unchanged & in order.
for line in 'MAILTO=owner@example.com' '# Example other-app — do not touch' '*/5 * * * * /home/example/other-app/bin/watch.sh' '30 2 * * * /home/example/other-app/bin/daily.sh'; do
  grep -qxF "$line" "$WORK/new1" || fail "other-app line missing after merge: $line"
done
# Sakuna block present with the exact cron lines.
grep -qxF '# BEGIN sakuna-bantay' "$WORK/new1" || fail "BEGIN marker missing"
grep -qxF '# END sakuna-bantay' "$WORK/new1" || fail "END marker missing"
grep -qxF '*/3 * * * * /home/example/sakuna/bin/watch.sh' "$WORK/new1" || fail "sakuna watch line missing"
grep -qxF "15 3 * * 0 find /home/example/sakuna/logs -name 'watch.log.*' -mtime +30 -delete" "$WORK/new1" || fail "sakuna cleanup line missing"
# No other-app paths clobbered.
grep -q '/home/example/other-app/bin/watch.sh' "$WORK/new1" || fail "other-app watch line lost"
pass "fresh merge keeps other-app lines + appends sakuna block"

# --- 2. Idempotency: merging the result again changes nothing ---
merge_crontab "$WORK/new1" "$BLOCK" "$WORK/new2"
if ! diff -u "$WORK/new1" "$WORK/new2"; then
  fail "merge is not idempotent (second merge differs)"
fi
verify_crontab_merge "$WORK/new1" "$WORK/new2" || fail "verify idempotent merge"
pass "merge is idempotent"

# --- 3. Replacement: stale sakuna block gets replaced, other-app kept ---
cat >"$WORK/stale" <<'EOF'
*/5 * * * * /home/example/other-app/bin/watch.sh
# BEGIN sakuna-bantay
*/3 * * * * /home/example/sakuna/bin/watch.sh-STALE
# END sakuna-bantay
30 2 * * * /home/example/other-app/bin/daily.sh
EOF
merge_crontab "$WORK/stale" "$BLOCK" "$WORK/new3"
verify_crontab_merge "$WORK/stale" "$WORK/new3" || fail "verify replacement merge"
grep -q 'watch.sh-STALE' "$WORK/new3" && fail "stale sakuna line not replaced"
grep -qxF '*/3 * * * * /home/example/sakuna/bin/watch.sh' "$WORK/new3" || fail "fresh sakuna line missing after replace"
grep -qxF '*/5 * * * * /home/example/other-app/bin/watch.sh' "$WORK/new3" || fail "other-app line lost on replace"
grep -qxF '30 2 * * * /home/example/other-app/bin/daily.sh' "$WORK/new3" || fail "other-app daily lost on replace"
# Stripped contents (non-sakuna lines) identical ignoring the separator blank.
if ! diff -u <(strip_crontab_block "$WORK/stale" | sed '/^[[:space:]]*$/d') <(strip_crontab_block "$WORK/new3" | sed '/^[[:space:]]*$/d'); then
  fail "non-sakuna lines changed on replacement"
fi
pass "stale block replaced, other-app lines unchanged"

# --- 4. Negative check: verify detects tampered non-sakuna lines ---
cp "$WORK/new1" "$WORK/tampered"
# Change an other-app line in the merged output.
sed -i 's|/home/example/other-app/bin/watch.sh|/home/example/other-app/bin/EVIL.sh|' "$WORK/tampered"
if verify_crontab_merge "$WORK/other-old" "$WORK/tampered" 2>/dev/null; then
  fail "verify should reject tampered non-sakuna lines"
fi
pass "verify rejects tampered non-sakuna lines"

# --- 5. Empty old crontab yields exactly the block ---
: >"$WORK/empty"
merge_crontab "$WORK/empty" "$BLOCK" "$WORK/new-empty"
if ! diff -u "$BLOCK" "$WORK/new-empty"; then
  fail "empty old crontab should yield exactly the block"
fi
pass "empty crontab yields block only"

# --- 6. check_crontab_nonempty guards against empty fetches ---
check_crontab_nonempty "$WORK/other-old" || fail "nonempty other-app crontab rejected"
: >"$WORK/blank"
check_crontab_nonempty "$WORK/blank" 2>/dev/null && fail "blank file accepted as nonempty"
printf '# comment\n   # indented comment\n\n   \n' >"$WORK/comments-only"
check_crontab_nonempty "$WORK/comments-only" 2>/dev/null && fail "comments-only file accepted as nonempty"
printf '\n# Example\n\n*/5 * * * * /home/example/other-app/bin/watch.sh\n' >"$WORK/one-real"
check_crontab_nonempty "$WORK/one-real" || fail "single real cron line rejected"
# Sakuna block alone counts as nonempty (it has real cron lines).
check_crontab_nonempty "$BLOCK" || fail "sakuna block rejected as empty"
pass "check_crontab_nonempty accepts real lines, rejects blank/comments-only"

echo "ALL CRONTAB MERGE TESTS PASSED"
