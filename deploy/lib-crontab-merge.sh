#!/usr/bin/env bash
# deploy/lib-crontab-merge.sh — shared crontab merge logic for Sakuna Bantay.
# Sourced by deploy/deploy.sh and deploy/test-crontab-merge.sh so the tested
# logic is exactly the shipped logic. No ssh, no side effects on source.
#
# Block markers: '# BEGIN sakuna-bantay' / '# END sakuna-bantay'.

# strip_crontab_block <infile> — print <infile> with any existing
# BEGIN/END sakuna-bantay block (inclusive) removed.
strip_crontab_block() {
  awk '
    BEGIN { skip = 0 }
    /^# BEGIN sakuna-bantay/ { skip = 1; next }
    /^# END sakuna-bantay/ { skip = 0; next }
    !skip { print }
  ' "$1"
}

# merge_crontab <old> <block> <new> — build <new> = <old> with any existing
# block removed + <block> appended verbatim. Idempotent: merging twice
# yields the same bytes. Trailing blank lines on the stripped base are
# trimmed so the separator stays exactly one blank line.
merge_crontab() {
  local old="$1" block="$2" new="$3"
  local tmp_stripped
  tmp_stripped=$(mktemp)
  strip_crontab_block "$old" >"$tmp_stripped"
  # Trim trailing blank lines from the stripped base (keeps merge idempotent).
  local tmp_trimmed
  tmp_trimmed=$(mktemp)
  awk '
    { lines[NR] = $0 }
    END {
      last = NR
      while (last > 0 && lines[last] ~ /^[[:space:]]*$/) { last-- }
      for (i = 1; i <= last; i++) { print lines[i] }
    }
  ' "$tmp_stripped" >"$tmp_trimmed"
  if [ -s "$tmp_trimmed" ]; then
    { cat "$tmp_trimmed"; echo; cat "$block"; } >"$new"
  else
    cat "$block" >"$new"
  fi
  rm -f "$tmp_stripped" "$tmp_trimmed"
}

# verify_crontab_merge <old> <new> — every non-sakuna line of <old> must
# appear unchanged and in order in <new>. Implemented as: stripping the
# block from both files must give identical bytes. Returns 0 on success.
verify_crontab_merge() {
  local old="$1" new="$2"
  local so sn
  so=$(mktemp)
  sn=$(mktemp)
  strip_crontab_block "$old" >"$so"
  strip_crontab_block "$new" >"$sn"
  # Compare trimmed (trailing blanks ignored: merge normalizes the separator).
  local so_t sn_t
  so_t=$(mktemp)
  sn_t=$(mktemp)
  awk '
    { lines[NR] = $0 }
    END {
      last = NR
      while (last > 0 && lines[last] ~ /^[[:space:]]*$/) { last-- }
      for (i = 1; i <= last; i++) { print lines[i] }
    }
  ' "$so" >"$so_t"
  awk '
    { lines[NR] = $0 }
    END {
      last = NR
      while (last > 0 && lines[last] ~ /^[[:space:]]*$/) { last-- }
      for (i = 1; i <= last; i++) { print lines[i] }
    }
  ' "$sn" >"$sn_t"
  if ! diff -u "$so_t" "$sn_t" >&2; then
    echo "verify_crontab_merge: non-sakuna lines changed — aborting" >&2
    rm -f "$so" "$sn" "$so_t" "$sn_t"
    return 1
  fi
  rm -f "$so" "$sn" "$so_t" "$sn_t"
  return 0
}

# check_crontab_nonempty <file> — return 0 iff <file> contains at least one
# non-comment non-blank line (a line whose first non-space character is not
# '#'). Guards the deploy merge against installing over an empty fetch
# (e.g. after an ssh failure), which would wipe Presyo Bantay's cron lines.
# Silent on success; caller prints the abort message so tests stay quiet.
check_crontab_nonempty() {
  grep -q '^[[:space:]]*[^[:space:]#]' "$1"
}
