#!/bin/sh
set -eu
umask 077

ROOT_PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
PATH=$ROOT_PATH
export PATH
SOURCE_URL=https://github.com/charle-z/aeontra.git
WORK_ROOT=/var/lib/mcp-devbox-builder-bootstrap
LOCK_PATH=/run/lock/mcp-devbox-builder-bootstrap.lock
UNIT=mcp-devbox-builder-bootstrap.service
BUILDER_UNIT=mcp-devbox-buildkit.service
INSTALL_ROOT=/usr/local/lib/mcp-devbox-builder
CONFIG_PATH=/etc/mcp-devbox-builder/buildkitd.toml
UNIT_PATH=/etc/systemd/system/mcp-devbox-buildkit.service
APPARMOR_PROFILE_PATH=/etc/apparmor.d/mcp-devbox-buildkit-runc
APPARMOR_ENABLED=/sys/module/apparmor/parameters/enabled
APPARMOR_PARSER=/usr/sbin/apparmor_parser
STAGING=/var/lib/mcp-devbox-builder-staging
WORK=
REPO=
COMMIT=
SELF=
WAS_INSTALLED=0
INSTALLED_BY_RUN=0

fail() {
  echo "mcp-devbox-builder bootstrap: $1" >&2
  exit 1
}

safe_remove_work() {
  [ -n "$WORK" ] || return 0
  case "$WORK" in
    "$WORK_ROOT"/run.*) rm -rf --one-file-system -- "$WORK" ;;
    *) fail "refused unsafe bootstrap cleanup path" ;;
  esac
}

run_fixed() {
  env -i HOME=/root PATH="$ROOT_PATH" LANG=C LC_ALL=C "$@"
}

git_fixed() {
  env -i HOME=/root PATH="$ROOT_PATH" LANG=C LC_ALL=C \
    GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0 \
    git "$@"
}

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  if [ "$status" -ne 0 ] && [ "$INSTALLED_BY_RUN" -eq 1 ] && [ -n "$REPO" ]; then
    remover=$REPO/packaging/builder/remove.sh
    if [ -f "$remover" ] && [ ! -L "$remover" ] && [ -x "$remover" ]; then
      run_fixed "$remover" >/dev/null 2>&1 || true
    fi
  fi
  safe_remove_work
  exit "$status"
}

validate_invocation() {
  [ "$(id -u)" -eq 0 ] || fail "root is required"
  [ "$#" -eq 1 ] || fail "exactly one commit is required"
  COMMIT=$1
  [ "${#COMMIT}" -eq 40 ] || fail "commit must be one lowercase 40-character SHA"
  printf '%s\n' "$COMMIT" | grep -Eq '^[a-f0-9]{40}$' || fail "commit must be one lowercase 40-character SHA"
  for tool in awk cat chmod cmp env flock git grep id install mktemp readlink rm stat systemctl systemd-run; do
    command -v "$tool" >/dev/null 2>&1 || fail "required host tool is missing: $tool"
  done
  [ ! -L "$0" ] || fail "bootstrap entrypoint must not be a symlink"
  SELF=$(readlink -f -- "$0")
  [ -f "$SELF" ] && [ ! -L "$SELF" ] && [ -x "$SELF" ] || fail "bootstrap entrypoint is unsafe"
  set -- $(stat -c '%u %a' "$SELF")
  [ "$1" -eq 0 ] && [ $((0$2 & 0022)) -eq 0 ] || fail "bootstrap entrypoint must be root-owned and not writable by group or other"
}

inside_durable_unit() {
  [ -r /proc/self/cgroup ] || return 1
  cgroup=$(awk -F: '$1 == "0" {print $3}' /proc/self/cgroup)
  case "$cgroup" in
    *"/$UNIT") return 0 ;;
    *) return 1 ;;
  esac
}

enter_durable_unit() {
  if ! inside_durable_unit; then
    exec env -i PATH="$ROOT_PATH" LANG=C LC_ALL=C systemd-run \
      --unit="$UNIT" \
      --wait \
      --collect \
      --property=Type=exec \
      --property=RuntimeMaxSec=4h \
      --property=TimeoutStopSec=45s \
      --property=Nice=10 \
      --property=IOSchedulingClass=best-effort \
      --property=IOSchedulingPriority=7 \
      "$SELF" "$COMMIT"
  fi
}

prepare_private_checkout() {
  [ ! -L "$WORK_ROOT" ] || fail "bootstrap work root is unsafe"
  install -d -o root -g root -m 0700 "$WORK_ROOT"
  set -- $(stat -c '%u %g %a' "$WORK_ROOT")
  [ "$1:$2:$3" = "0:0:700" ] || fail "bootstrap work root metadata changed"
  exec 9>"$LOCK_PATH"
  flock -n 9 || fail "another builder bootstrap is already running"

  WORK=$(mktemp -d "$WORK_ROOT/run.XXXXXX")
  chmod 0700 "$WORK"
  REPO=$WORK/repository
  install -d -o root -g root -m 0700 "$REPO"

  git_fixed -C "$REPO" init --quiet
  git_fixed -C "$REPO" remote add origin "$SOURCE_URL"
  git_fixed -C "$REPO" -c protocol.file.allow=never fetch --quiet --depth=1 origin "$COMMIT"
  git_fixed -C "$REPO" checkout --quiet --detach FETCH_HEAD
  [ "$(git_fixed -C "$REPO" rev-parse HEAD)" = "$COMMIT" ] || fail "fetched commit did not match approval"
  [ -z "$(git_fixed -C "$REPO" status --porcelain=v1 --untracked-files=all)" ] || fail "fetched checkout is not clean"

  for name in install-prerequisites.sh stage-official-v0.31.2.sh install-preverified.sh calibrate-vps.sh review-vps-calibration.sh remove.sh; do
    path=$REPO/packaging/builder/$name
    [ -f "$path" ] && [ ! -L "$path" ] && [ -x "$path" ] || fail "reviewed builder script is missing or unsafe"
    set -- $(stat -c '%u %a' "$path")
    [ "$1" -eq 0 ] && [ $((0$2 & 0022)) -eq 0 ] || fail "reviewed builder script metadata changed"
  done
  path=$REPO/packaging/builder/mcp-devbox-buildkit-runc.apparmor
  [ -f "$path" ] && [ ! -L "$path" ] || fail "reviewed AppArmor profile is missing or unsafe"
  set -- $(stat -c '%u %a' "$path")
  [ "$1" -eq 0 ] && [ $((0$2 & 0022)) -eq 0 ] || fail "reviewed AppArmor profile metadata changed"
}

verify_existing_candidate() {
  for name in buildkitd buildctl buildkit-runc; do
    installed=$INSTALL_ROOT/$name
    staged=$STAGING/$name
    [ -f "$installed" ] && [ ! -L "$installed" ] || fail "existing builder candidate differs from the reviewed staging"
    [ -f "$staged" ] && [ ! -L "$staged" ] || fail "reviewed builder staging is incomplete"
    cmp -s "$installed" "$staged" || fail "existing builder candidate differs from the reviewed staging"
  done
  [ -f "$CONFIG_PATH" ] && [ ! -L "$CONFIG_PATH" ] || fail "existing builder candidate differs from the reviewed configuration"
  [ -f "$UNIT_PATH" ] && [ ! -L "$UNIT_PATH" ] || fail "existing builder candidate differs from the reviewed unit"
  [ -f "$APPARMOR_PROFILE_PATH" ] && [ ! -L "$APPARMOR_PROFILE_PATH" ] || fail "existing builder candidate differs from the reviewed AppArmor profile"
  cmp -s "$CONFIG_PATH" "$REPO/packaging/builder/buildkitd.toml" || fail "existing builder candidate differs from the reviewed configuration"
  cmp -s "$UNIT_PATH" "$REPO/packaging/builder/mcp-devbox-buildkit.service" || fail "existing builder candidate differs from the reviewed unit"
  cmp -s "$APPARMOR_PROFILE_PATH" "$REPO/packaging/builder/mcp-devbox-buildkit-runc.apparmor" || fail "existing builder candidate differs from the reviewed AppArmor profile"
  if [ -r "$APPARMOR_ENABLED" ] && [ "$(cat "$APPARMOR_ENABLED")" = Y ]; then
    [ -x "$APPARMOR_PARSER" ] && [ ! -L "$APPARMOR_PARSER" ] || fail "AppArmor parser is missing or unsafe"
    "$APPARMOR_PARSER" -r "$APPARMOR_PROFILE_PATH"
  fi
  systemctl is-active --quiet "$BUILDER_UNIT" || fail "existing reviewed builder candidate is not active"
}

reject_partial_installation() {
  for path in "$UNIT_PATH" "$CONFIG_PATH" "$APPARMOR_PROFILE_PATH" "$INSTALL_ROOT/buildkitd" "$INSTALL_ROOT/buildctl" "$INSTALL_ROOT/buildkit-runc"; do
    [ ! -e "$path" ] && [ ! -L "$path" ] || fail "partial or unmanaged builder installation exists"
  done
}

run_install_and_calibration() {
  run_fixed "$REPO/packaging/builder/stage-official-v0.31.2.sh"
  if systemctl cat "$BUILDER_UNIT" >/dev/null 2>&1; then
    WAS_INSTALLED=1
    verify_existing_candidate
  else
    reject_partial_installation
    run_fixed "$REPO/packaging/builder/install-preverified.sh"
    INSTALLED_BY_RUN=1
  fi
  run_fixed "$REPO/packaging/builder/calibrate-vps.sh" "$COMMIT"
  systemctl is-active --quiet "$BUILDER_UNIT" || fail "builder service is not active after calibration"
  echo "mcp-devbox-builder bootstrap completed; evidence is under /var/lib/mcp-devbox-builder-calibration"
}

main() {
  validate_invocation "$@"
  enter_durable_unit
  trap cleanup EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
  prepare_private_checkout
  run_fixed "$REPO/packaging/builder/install-prerequisites.sh"
  run_install_and_calibration
  trap - EXIT HUP INT TERM
  safe_remove_work
}

main "$@"
