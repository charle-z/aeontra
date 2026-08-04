#!/bin/sh
set -eu
umask 077

MAX_CACHE_BYTES=4294967296
MAX_ARTIFACT_BYTES=4294967296
MAX_LOG_BYTES=33554432
NO_CACHE_PERCENT=135
CACHED_PERCENT=125

fail() {
  echo "mcp-devbox-builder calibration review: $1" >&2
  exit 2
}

if command -v awk >/dev/null 2>&1; then
  AWK_BIN=awk
elif command -v mawk >/dev/null 2>&1; then
  AWK_BIN=mawk
else
  fail "awk implementation is unavailable"
fi

[ "$#" -eq 1 ] || fail "exactly one evidence directory is required"
evidence=$1
[ -d "$evidence" ] && [ ! -L "$evidence" ] || fail "evidence directory is unsafe"

summary=$evidence/summary.tsv
commit_file=$evidence/commit
selection=$evidence/selection.tsv
selected_file=$evidence/selected-quota-percent
policy_file=$evidence/selection-policy

for path in "$summary" "$commit_file"; do
  [ -f "$path" ] && [ ! -L "$path" ] || fail "required evidence file is missing or unsafe"
done
for path in "$selection" "$selected_file" "$policy_file"; do
  [ ! -L "$path" ] || fail "selection output path is unsafe"
  [ ! -e "$path" ] || [ -f "$path" ] || fail "selection output path is not a regular file"
done

commit=$(cat "$commit_file")
[ "${#commit}" -eq 40 ] || fail "evidence commit is invalid"
case "$commit" in
  *[!a-f0-9]*) fail "evidence commit is invalid" ;;
esac

cache_reused() {
  quota=$1
  log=$evidence/q${quota}-cached/build.log
  [ -f "$log" ] && [ ! -L "$log" ] || fail "cached build log is missing or unsafe for quota $quota"
  size=$(stat -c '%s' "$log")
  case "$size" in
    ''|*[!0-9]*) fail "cached build log size is invalid for quota $quota" ;;
  esac
  [ "$size" -gt 0 ] && [ "$size" -le "$MAX_LOG_BYTES" ] || fail "cached build log is empty or oversized for quota $quota"
  if grep -Eiq '(^|[[:space:]])CACHED([[:space:]]|$)' "$log"; then
    printf 'yes\n'
  else
    printf 'no\n'
  fi
}

cache_50=$(cache_reused 50)
cache_65=$(cache_reused 65)
cache_80=$(cache_reused 80)

temporary=$(mktemp "$evidence/.selection.XXXXXX")
trap 'rm -f -- "$temporary"' EXIT HUP INT TERM

status=0
"$AWK_BIN" -F '\t' \
  -v expected_commit="$commit" \
  -v max_cache="$MAX_CACHE_BYTES" \
  -v max_artifact="$MAX_ARTIFACT_BYTES" \
  -v cache_50="$cache_50" \
  -v cache_65="$cache_65" \
  -v cache_80="$cache_80" \
  -v no_cache_percent="$NO_CACHE_PERCENT" \
  -v cached_percent="$CACHED_PERCENT" '
BEGIN {
  OFS = "\t"
  expected_header = "commit\tquota_percent\tmode\tduration_ms\texit_status\tcpu_usage_usec\tcpu_throttled_usec\tnr_throttled\tmemory_peak_bytes\tmemory_high_events\toom_kills\tcpu_pressure_total\tio_pressure_total\tpids_peak\thealth_samples\thealth_failures\thttp_502\thealth_max_seconds\thealth_avg_seconds\tcache_bytes\tartifact_bytes\tartifact_sha256"
  quotas[1] = 50
  quotas[2] = 65
  quotas[3] = 80
  cache_reuse[50] = cache_50
  cache_reuse[65] = cache_65
  cache_reuse[80] = cache_80
}
function invalid(message) {
  if (error == "") error = message
}
function unsigned_integer(value) {
  return value ~ /^[0-9]+$/
}
function signed_integer(value) {
  return value ~ /^-?[0-9]+$/
}
function decimal_number(value) {
  return value ~ /^[0-9]+([.][0-9]+)?$/
}
NR == 1 {
  if ($0 != expected_header) invalid("summary header is invalid")
  next
}
{
  if (NF != 22) {
    invalid("summary row width is invalid")
    next
  }
  if ($1 != expected_commit) invalid("summary commit differs from evidence commit")
  if ($2 != "50" && $2 != "65" && $2 != "80") invalid("summary quota is invalid")
  if ($3 != "no-cache" && $3 != "cached") invalid("summary mode is invalid")
  key = $2 SUBSEP $3
  if (seen[key]++) invalid("summary contains a duplicate quota/mode row")

  for (column = 4; column <= 17; column++) {
    if (column == 14) {
      if (!signed_integer($column)) invalid("summary PID metric is invalid")
    } else if (!unsigned_integer($column)) {
      invalid("summary integer metric is invalid")
    }
  }
  if (!decimal_number($18) || !decimal_number($19)) invalid("summary health duration is invalid")
  if (!unsigned_integer($20) || !unsigned_integer($21)) invalid("summary size metric is invalid")
  if (length($22) != 71 || $22 !~ /^sha256:[a-f0-9]+$/) invalid("summary artifact identity is invalid")

  duration[$2, $3] = $4 + 0
  hard[$2, $3] = ($4 > 0 && $4 <= 1800000 && $5 == 0 && $9 > 0 && $11 == 0 && $14 > 0 && $15 > 0 && $16 == 0 && $17 == 0 && $20 > 0 && $20 <= max_cache && $21 > 0 && $21 <= max_artifact)
  rows++
}
END {
  if (NR == 0) invalid("summary is empty")
  if (rows != 6) invalid("summary must contain exactly six measurements")
  for (i = 1; i <= 3; i++) {
    quota = quotas[i]
    if (!seen[quota SUBSEP "no-cache"] || !seen[quota SUBSEP "cached"]) invalid("summary matrix is incomplete")
  }
  if (error != "") {
    print "mcp-devbox-builder calibration review: " error > "/dev/stderr"
    exit 2
  }

  fastest_no_cache = 0
  fastest_cached = 0
  for (i = 1; i <= 3; i++) {
    quota = quotas[i]
    metric_hard[quota] = hard[quota, "no-cache"] && hard[quota, "cached"]
    hard_quota[quota] = metric_hard[quota] && cache_reuse[quota] == "yes"
    if (hard_quota[quota]) {
      if (fastest_no_cache == 0 || duration[quota, "no-cache"] < fastest_no_cache) fastest_no_cache = duration[quota, "no-cache"]
      if (fastest_cached == 0 || duration[quota, "cached"] < fastest_cached) fastest_cached = duration[quota, "cached"]
    }
  }

  print "commit", "quota_percent", "hard_eligible", "duration_eligible", "no_cache_ms", "cached_ms", "reason"
  selected = 0
  for (i = 1; i <= 3; i++) {
    quota = quotas[i]
    duration_ok = hard_quota[quota] && duration[quota, "no-cache"] * 100 <= fastest_no_cache * no_cache_percent && duration[quota, "cached"] * 100 <= fastest_cached * cached_percent
    reason = "eligible"
    if (!metric_hard[quota]) reason = "hard-failure"
    else if (cache_reuse[quota] != "yes") reason = "cache-not-reused"
    else if (!duration_ok) reason = "duration-regression"
    print expected_commit, quota, (hard_quota[quota] ? "yes" : "no"), (duration_ok ? "yes" : "no"), duration[quota, "no-cache"], duration[quota, "cached"], reason
    if (selected == 0 && duration_ok) selected = quota
  }
  if (selected == 0) exit 3
}
' "$summary" > "$temporary" || status=$?

if [ "$status" -ne 0 ] && [ "$status" -ne 3 ]; then
  exit 2
fi

chmod 0600 "$temporary"
mv -f -- "$temporary" "$selection"
selected=$("$AWK_BIN" -F '\t' 'NR > 1 && $4 == "yes" {print $2; exit}' "$selection")
if [ "$status" -eq 0 ]; then
  case "$selected" in
    50|65|80) : ;;
    *) fail "review completed without a valid selected quota" ;;
  esac
else
  selected=none
fi
printf '%s\n' "$selected" > "$selected_file"
printf 'lowest hard-eligible quota with no-cache duration <= %s%% and cached duration <= %s%% of the fastest hard-eligible run\n' "$NO_CACHE_PERCENT" "$CACHED_PERCENT" > "$policy_file"
chmod 0600 "$selection" "$selected_file" "$policy_file"
trap - EXIT HUP INT TERM

if [ "$status" -eq 3 ]; then
  echo "mcp-devbox-builder calibration review: no quota satisfies the reviewed policy" >&2
  exit 3
fi

printf 'mcp-devbox-builder selected quota: %s%%\n' "$selected"
