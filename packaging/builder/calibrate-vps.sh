#!/bin/bash
set -Eeuo pipefail
umask 077

readonly SERVICE=mcp-devbox-buildkit.service
readonly BUILDER_USER=mcp-build
readonly BUILDER_ROOT=/usr/local/lib/mcp-devbox-builder
readonly SOCKET=/run/mcp-devbox-buildkit/buildkit/buildkitd.sock
readonly SOURCE_URL=https://github.com/charle-z/mcp-devbox.git
readonly HEALTH_URL=https://mcp-devbox-charlez.duckdns.org/healthz
readonly EVIDENCE_ROOT=/var/lib/mcp-devbox-builder-calibration
readonly WORK_ROOT=/var/lib/mcp-devbox-builder-calibration-work
readonly CACHE_ROOT=/var/cache/mcp-devbox-builder-calibration
readonly LOCK_PATH=/run/lock/mcp-devbox-builder-calibration.lock
readonly DEFAULT_QUOTA=65
readonly BUILD_TIMEOUT=30m
readonly LOG_LIMIT_BYTES=33554432
readonly -a QUOTAS=(50 65 80)
readonly -a PREREQUISITE_PACKAGES=(rootlesskit uidmap slirp4netns fuse-overlayfs)
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
readonly SCRIPT_DIR
readonly REVIEWER="$SCRIPT_DIR/review-vps-calibration.sh"

RUN_ROOT=
RUN_WORK=
RUN_CACHE=
EVIDENCE=
SOURCE=
OUTPUT=
CONTROL_GROUP=
BUILD_PID=
ARCHIVE=
FAILED=0

fail() {
  printf 'mcp-devbox-builder calibration: %s\n' "$1" >&2
  exit 1
}

safe_root_directory() {
  local path=$1 mode=$2
  [[ ! -L "$path" ]] || fail "unsafe fixed directory: $path"
  install -d -o root -g root -m "$mode" "$path"
  [[ -d "$path" && ! -L "$path" ]] || fail "fixed directory unavailable: $path"
  [[ "$(stat -c '%u:%g:%a' "$path")" == "0:0:${mode#0}" ]] || fail "fixed directory metadata changed: $path"
}

safe_remove_run_tree() {
  local path=$1 root=$2
  [[ -n "$path" ]] || return 0
  case "$path" in
    "$root"/*) rm -rf --one-file-system -- "$path" ;;
    *) fail 'refused unsafe calibration cleanup path' ;;
  esac
}

write_archive() {
  local suffix=$1
  [[ -n "$RUN_ROOT" && -d "$EVIDENCE" ]] || return 0
  ARCHIVE="$EVIDENCE_ROOT/p16-buildkit-calibration-${suffix}-$(date -u +%Y%m%dT%H%M%SZ).tar.gz"
  tar --create --gzip --file "$ARCHIVE" --directory "$RUN_ROOT" evidence
  chmod 0600 "$ARCHIVE"
  chown root:root "$ARCHIVE"
  sha256sum "$ARCHIVE" > "$ARCHIVE.sha256"
  chmod 0600 "$ARCHIVE.sha256"
  chown root:root "$ARCHIVE.sha256"
  printf 'mcp-devbox-builder calibration archive: %s\n' "$ARCHIVE"
}

restore() {
  local status=$?
  trap - EXIT HUP INT TERM
  if [[ -n "${BUILD_PID:-}" ]] && kill -0 "$BUILD_PID" 2>/dev/null; then
    kill -TERM -- "-$BUILD_PID" 2>/dev/null || true
    for _ in {1..20}; do
      kill -0 "$BUILD_PID" 2>/dev/null || break
      sleep 0.25
    done
    kill -KILL -- "-$BUILD_PID" 2>/dev/null || true
  fi
  systemctl set-property --runtime "$SERVICE" "CPUQuota=${DEFAULT_QUOTA}%" >/dev/null 2>&1 || true
  if [[ -n "${EVIDENCE:-}" && -d "$EVIDENCE" ]]; then
    printf '%s\n' "$status" > "$EVIDENCE/calibration-exit-status"
    chmod 0600 "$EVIDENCE/calibration-exit-status" || true
    if [[ -z "${ARCHIVE:-}" ]]; then
      write_archive "failed-${COMMIT:-unknown}" || true
    fi
  fi
  safe_remove_run_tree "${RUN_WORK:-}" "$WORK_ROOT" || true
  safe_remove_run_tree "${RUN_CACHE:-}" "$CACHE_ROOT" || true
  exit "$status"
}

validate_host() {
  [[ "$(id -u)" -eq 0 ]] || fail 'root is required'
  [[ "$#" -eq 1 ]] || fail 'exactly one 40-character commit is required'
  [[ "$1" =~ ^[a-f0-9]{40}$ ]] || fail 'commit is invalid'
  for tool in awk cat chmod chown cp curl date dirname dpkg-query du env find flock git grep id install kill mktemp mv rm runuser sed setsid sha256sum sleep sort stat systemctl tail tar timeout tr uname wc; do
    command -v "$tool" >/dev/null 2>&1 || fail "required host tool is missing: $tool"
  done
  [[ -x "$BUILDER_ROOT/buildctl" && ! -L "$BUILDER_ROOT/buildctl" ]] || fail 'reviewed buildctl is unavailable'
  [[ -x "$BUILDER_ROOT/buildkit-runc" && ! -L "$BUILDER_ROOT/buildkit-runc" ]] || fail 'reviewed buildkit-runc is unavailable'
  [[ -f "$REVIEWER" && ! -L "$REVIEWER" && -x "$REVIEWER" ]] || fail 'reviewed calibration selector is unavailable'
  systemctl is-active --quiet "$SERVICE" || fail 'builder service is not active'
  systemctl is-enabled --quiet "$SERVICE" || fail 'builder service is not enabled'
  [[ -S "$SOCKET" && ! -L "$SOCKET" ]] || fail 'private BuildKit socket is unavailable'
  id "$BUILDER_USER" >/dev/null 2>&1 || fail 'builder identity is unavailable'
  [[ "$(id -u "$BUILDER_USER")" -ne 0 ]] || fail 'builder identity resolved to root'

  CONTROL_GROUP="$(systemctl show "$SERVICE" --property=ControlGroup --value)"
  [[ "$CONTROL_GROUP" == /*/mcp-devbox-buildkit.service ]] || fail 'service cgroup is unexpected'
  [[ "$CONTROL_GROUP" != *'..'* && "$CONTROL_GROUP" != *$'\n'* ]] || fail 'service cgroup is unsafe'
  local cgroup_root="/sys/fs/cgroup$CONTROL_GROUP"
  [[ -d "$cgroup_root" && ! -L "$cgroup_root" ]] || fail 'service cgroup is unavailable'
  for metric in cpu.max cpu.stat memory.current memory.peak memory.events cpu.pressure io.pressure pids.current; do
    [[ -r "$cgroup_root/$metric" && ! -L "$cgroup_root/$metric" ]] || fail "required cgroup metric is unavailable: $metric"
  done

  safe_root_directory "$EVIDENCE_ROOT" 0700
  safe_root_directory "$WORK_ROOT" 0711
  safe_root_directory "$CACHE_ROOT" 0711
  exec 9>"$LOCK_PATH"
  flock -n 9 || fail 'another calibration is already running'
}

prepare_run() {
  local commit=$1 stamp fetched_head
  stamp="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  RUN_ROOT="$EVIDENCE_ROOT/$stamp-$commit"
  RUN_WORK="$WORK_ROOT/$stamp-$commit"
  RUN_CACHE="$CACHE_ROOT/$stamp-$commit"
  EVIDENCE="$RUN_ROOT/evidence"
  SOURCE="$RUN_WORK/source"
  OUTPUT="$RUN_WORK/output"
  for path in "$RUN_ROOT" "$RUN_WORK" "$RUN_CACHE"; do
    [[ ! -e "$path" && ! -L "$path" ]] || fail 'calibration run already exists'
  done
  install -d -o root -g root -m 0700 "$RUN_ROOT" "$EVIDENCE"
  install -d -o "$BUILDER_USER" -g "$BUILDER_USER" -m 0700 "$RUN_WORK" "$RUN_CACHE" "$SOURCE" "$OUTPUT"

  local -a git_env=(env -i HOME=/var/lib/mcp-devbox-buildkit PATH=/usr/bin:/bin LANG=C LC_ALL=C GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0)
  runuser -u "$BUILDER_USER" -- "${git_env[@]}" git -C "$SOURCE" init --quiet
  runuser -u "$BUILDER_USER" -- "${git_env[@]}" git -C "$SOURCE" remote add origin "$SOURCE_URL"
  runuser -u "$BUILDER_USER" -- "${git_env[@]}" git -C "$SOURCE" -c protocol.file.allow=never fetch --quiet --depth=1 origin "$commit"
  runuser -u "$BUILDER_USER" -- "${git_env[@]}" git -C "$SOURCE" checkout --quiet --detach FETCH_HEAD
  fetched_head="$(runuser -u "$BUILDER_USER" -- "${git_env[@]}" git -C "$SOURCE" rev-parse HEAD)"
  [[ "$fetched_head" == "$commit" ]] || fail 'fetched commit did not match approval'
  [[ -f "$SOURCE/Dockerfile" && ! -L "$SOURCE/Dockerfile" ]] || fail 'Dockerfile is missing or unsafe'
  chown -R "$BUILDER_USER:$BUILDER_USER" "$RUN_WORK" "$RUN_CACHE"

  printf 'commit\tquota_percent\tmode\tduration_ms\texit_status\tcpu_usage_usec\tcpu_throttled_usec\tnr_throttled\tmemory_peak_bytes\tmemory_high_events\toom_kills\tcpu_pressure_total\tio_pressure_total\tpids_peak\thealth_samples\thealth_failures\thttp_502\thealth_max_seconds\thealth_avg_seconds\tcache_bytes\tartifact_bytes\tartifact_sha256\n' > "$EVIDENCE/summary.tsv"
  printf '%s\n' "$commit" > "$EVIDENCE/commit"
  printf '%s\n' "$SOURCE_URL" > "$EVIDENCE/source-url"
  printf '%s\n' "$HEALTH_URL" > "$EVIDENCE/health-url"
  printf '%s\n' "$CONTROL_GROUP" > "$EVIDENCE/control-group"
  uname -srmo > "$EVIDENCE/kernel"
  systemctl show "$SERVICE" --property=User --property=Group --property=ControlGroup --property=CPUQuotaPerSecUSec --property=MemoryHigh --property=MemoryMax --property=TasksMax --property=IOWeight > "$EVIDENCE/service-properties"
  printf 'package\tversion\n' > "$EVIDENCE/host-prerequisites.tsv"
  local package version
  for package in "${PREREQUISITE_PACKAGES[@]}"; do
    version="$(dpkg-query -W -f='${Version}' "$package" 2>/dev/null)" || fail "required host package is not installed: $package"
    [[ -n "$version" && "$version" != *$'\n'* && "$version" != *$'\t'* ]] || fail "required host package version is invalid: $package"
    printf '%s\t%s\n' "$package" "$version" >> "$EVIDENCE/host-prerequisites.tsv"
  done
  chmod 0600 "$EVIDENCE"/*
}

metric_value() {
  local file=$1 key=$2
  awk -v key="$key" '$1 == key {print $2; found=1; exit} END {if (!found) exit 1}' "$file"
}

pressure_total() {
  local file=$1
  awk '$1 == "some" {for (i=2; i<=NF; i++) if ($i ~ /^total=/) {split($i,a,"="); print a[2]; found=1; exit}} END {if (!found) exit 1}' "$file"
}

copy_metrics() {
  local destination=$1 root="/sys/fs/cgroup$CONTROL_GROUP"
  install -d -o root -g root -m 0700 "$destination"
  for metric in cpu.stat memory.current memory.peak memory.events cpu.pressure io.pressure pids.current; do
    cp --no-dereference "$root/$metric" "$destination/${metric//./-}"
  done
  if [[ -r "$root/pids.peak" && ! -L "$root/pids.peak" ]]; then
    cp --no-dereference "$root/pids.peak" "$destination/pids-peak"
  else
    printf '%s\n' '-1' > "$destination/pids-peak"
  fi
  chmod 0600 "$destination"/*
}

reset_peaks() {
  local root="/sys/fs/cgroup$CONTROL_GROUP"
  printf '0\n' > "$root/memory.peak" || true
  if [[ -w "$root/pids.peak" ]]; then printf '0\n' > "$root/pids.peak" || true; fi
}

apply_quota() {
  local quota=$1 root="/sys/fs/cgroup$CONTROL_GROUP" max period
  systemctl set-property --runtime "$SERVICE" "CPUQuota=${quota}%"
  read -r max period < "$root/cpu.max"
  [[ "$max" =~ ^[0-9]+$ && "$period" =~ ^[0-9]+$ && "$period" -gt 0 ]] || fail 'CPU quota cgroup state is invalid'
  (( max * 100 == quota * period )) || fail 'CPU quota did not match the requested cgroup value'
}

monitor_health() {
  local output=$1 pid=$2
  : > "$output"
  chmod 0600 "$output"
  while :; do
    local sample code seconds
    sample="$(env -i PATH=/usr/bin:/bin LANG=C LC_ALL=C curl --proto '=https' --tlsv1.2 --silent --show-error --max-redirs 0 --connect-timeout 4 --max-time 8 --output /dev/null --write-out '%{http_code}\t%{time_total}' "$HEALTH_URL" 2>/dev/null || printf '000\t8.000000')"
    code="${sample%%$'\t'*}"
    seconds="${sample#*$'\t'}"
    [[ "$code" =~ ^[0-9]{3}$ ]] || code=000
    [[ "$seconds" =~ ^[0-9]+([.][0-9]+)?$ ]] || seconds=8.000000
    printf '%s\t%s\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$code" "$seconds" >> "$output"
    kill -0 "$pid" 2>/dev/null || break
    sleep 1
  done
}

bound_log() {
  local log=$1 size temporary
  size="$(stat -c '%s' "$log")"
  [[ "$size" =~ ^[0-9]+$ ]] || fail 'build log size is invalid'
  if (( size > LOG_LIMIT_BYTES )); then
    temporary="$log.tail"
    tail -c "$LOG_LIMIT_BYTES" "$log" > "$temporary"
    chmod 0600 "$temporary"
    mv "$temporary" "$log"
  fi
}

run_build() {
  local commit=$1 quota=$2 mode=$3
  local run="$EVIDENCE/q${quota}-${mode}" before="$run/before" after="$run/after"
  local health="$run/health.tsv" log="$run/build.log" cache="$RUN_CACHE/q${quota}"
  local artifact="$OUTPUT/q${quota}-${mode}.oci.tar"
  install -d -o root -g root -m 0700 "$run"
  if [[ "$mode" == no-cache ]]; then
    safe_remove_run_tree "$cache" "$RUN_CACHE"
    install -d -o "$BUILDER_USER" -g "$BUILDER_USER" -m 0700 "$cache"
  fi

  apply_quota "$quota"
  reset_peaks
  copy_metrics "$before"

  local -a args=(--addr "unix://$SOCKET" build --progress plain --frontend dockerfile.v0 --local "context=$SOURCE" --local "dockerfile=$SOURCE" --opt filename=Dockerfile --output "type=oci,dest=$artifact")
  if [[ "$mode" == no-cache ]]; then
    args+=(--no-cache --export-cache "type=local,dest=$cache,mode=max,reset=true")
  else
    args+=(--import-cache "type=local,src=$cache" --export-cache "type=local,dest=$cache,mode=max,reset=true")
  fi

  local started ended status monitor_pid
  started="$(date +%s%N)"
  setsid timeout --signal=TERM --kill-after=30s "$BUILD_TIMEOUT" runuser -u "$BUILDER_USER" -- env -i HOME=/var/lib/mcp-devbox-buildkit XDG_RUNTIME_DIR=/run/mcp-devbox-buildkit PATH=/usr/local/lib/mcp-devbox-builder:/usr/bin:/bin LANG=C LC_ALL=C "$BUILDER_ROOT/buildctl" "${args[@]}" > "$log" 2>&1 &
  BUILD_PID=$!
  monitor_health "$health" "$BUILD_PID" &
  monitor_pid=$!
  if wait "$BUILD_PID"; then status=0; else status=$?; fi
  BUILD_PID=
  wait "$monitor_pid" || true
  ended="$(date +%s%N)"
  copy_metrics "$after"
  chmod 0600 "$log"
  bound_log "$log"

  local duration_ms=$(((ended - started) / 1000000))
  local cpu_before cpu_after throttle_before throttle_after nr_before nr_after memory_peak high_before high_after oom_before oom_after cpu_pressure_before cpu_pressure_after io_pressure_before io_pressure_after pids_peak
  cpu_before="$(metric_value "$before/cpu-stat" usage_usec)"; cpu_after="$(metric_value "$after/cpu-stat" usage_usec)"
  throttle_before="$(metric_value "$before/cpu-stat" throttled_usec)"; throttle_after="$(metric_value "$after/cpu-stat" throttled_usec)"
  nr_before="$(metric_value "$before/cpu-stat" nr_throttled)"; nr_after="$(metric_value "$after/cpu-stat" nr_throttled)"
  memory_peak="$(cat "$after/memory-peak")"
  high_before="$(metric_value "$before/memory-events" high)"; high_after="$(metric_value "$after/memory-events" high)"
  oom_before="$(metric_value "$before/memory-events" oom_kill)"; oom_after="$(metric_value "$after/memory-events" oom_kill)"
  cpu_pressure_before="$(pressure_total "$before/cpu-pressure")"; cpu_pressure_after="$(pressure_total "$after/cpu-pressure")"
  io_pressure_before="$(pressure_total "$before/io-pressure")"; io_pressure_after="$(pressure_total "$after/io-pressure")"
  pids_peak="$(cat "$after/pids-peak")"

  local health_samples health_failures http_502 health_max health_avg
  read -r health_samples health_failures http_502 health_max health_avg < <(awk -F '\t' '{n++; if ($2 != 200) fail++; if ($2 == 502) e502++; sum += $3; if ($3 > max) max=$3} END {if (!n) {print "0 0 0 0 0"} else {printf "%d %d %d %.6f %.6f\n", n, fail, e502, max, sum/n}}' "$health")
  local cache_bytes artifact_bytes artifact_sha
  cache_bytes="$(du -sb "$cache" | awk 'NR == 1 {print $1}')"
  if [[ -f "$artifact" && ! -L "$artifact" ]]; then
    artifact_bytes="$(stat -c '%s' "$artifact")"
    artifact_sha="$(sha256sum "$artifact" | awk '{print "sha256:"$1}')"
  else
    artifact_bytes=0
    artifact_sha=missing
  fi

  printf '%s\t%d\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%d\t%d\t%d\t%s\t%s\t%d\t%d\t%s\n' "$commit" "$quota" "$mode" "$duration_ms" "$status" "$((cpu_after - cpu_before))" "$((throttle_after - throttle_before))" "$((nr_after - nr_before))" "$memory_peak" "$((high_after - high_before))" "$((oom_after - oom_before))" "$((cpu_pressure_after - cpu_pressure_before))" "$((io_pressure_after - io_pressure_before))" "$pids_peak" "$health_samples" "$health_failures" "$http_502" "$health_max" "$health_avg" "$cache_bytes" "$artifact_bytes" "$artifact_sha" >> "$EVIDENCE/summary.tsv"

  rm -f "$artifact"
  if [[ "$status" -ne 0 || "$health_samples" -lt 1 || "$health_failures" -ne 0 || "$http_502" -ne 0 ]]; then
    FAILED=1
    return 1
  fi
}

finalize() {
  local commit=$1 final_status=0
  apply_quota "$DEFAULT_QUOTA"
  if [[ "$FAILED" -eq 0 ]]; then
    if ! "$REVIEWER" "$EVIDENCE"; then
      FAILED=1
    fi
  fi
  if [[ "$FAILED" -ne 0 ]]; then final_status=1; fi
  printf '%s\n' "$final_status" > "$EVIDENCE/calibration-exit-status"
  chmod 0600 "$EVIDENCE/calibration-exit-status"
  write_archive "$commit"
  safe_remove_run_tree "$RUN_WORK" "$WORK_ROOT"
  safe_remove_run_tree "$RUN_CACHE" "$CACHE_ROOT"
  RUN_WORK=
  RUN_CACHE=
  [[ "$FAILED" -eq 0 ]] || fail 'one or more calibration runs failed'
}

main() {
  validate_host "$@"
  COMMIT=$1
  trap restore EXIT HUP INT TERM
  prepare_run "$COMMIT"
  for quota in "${QUOTAS[@]}"; do
    run_build "$COMMIT" "$quota" no-cache || true
    run_build "$COMMIT" "$quota" cached || true
  done
  finalize "$COMMIT"
  trap - EXIT HUP INT TERM
}

main "$@"
