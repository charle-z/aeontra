#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: generate-edge-notices.sh --go-licenses <ABS_FILE> --output <ABS_DIR> --release <vMAJOR.MINOR.PATCH>\n' >&2
  exit 2
}

GO_LICENSES=''
OUTPUT=''
RELEASE=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --go-licenses) GO_LICENSES="${2:-}"; shift 2 ;;
    --output) OUTPUT="${2:-}"; shift 2 ;;
    --release) RELEASE="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$GO_LICENSES" = /* && "$OUTPUT" = /* ]] || usage
[[ "$RELEASE" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || usage
[ -x "$GO_LICENSES" ] && [ ! -L "$GO_LICENSES" ] || usage
[ -d "$OUTPUT" ] && [ ! -L "$OUTPUT" ] || usage

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
TEMPLATE="$ROOT/packaging/licenses/third-party-notices.tmpl"
GH_LICENSE="$ROOT/packaging/licenses/github-cli-v2.97.0.LICENSE"
CODEX_NOTICE="$ROOT/packaging/licenses/openai-codex-v0.147.0.NOTICE"
PROJECT_LICENSE="$ROOT/LICENSE"

printf '%s  %s\n' \
  '6da4adc42392c8485e40b4251c7e332fc3352df1947c9ffade71dd60b14a7a4f' "$GH_LICENSE" \
  '9d71575ecfd9a843fc1677b0efb08053c6ba9fd686a0de1a6f5382fd3c220915' "$CODEX_NOTICE" |
  sha256sum --check --status

WORK="$(mktemp -d)"
trap 'rm -rf -- "$WORK"' EXIT

generate_go_notices() {
  local platform="$1"
  local output="$2"
  shift 2
  GOOS="$platform" GOARCH=amd64 "$GO_LICENSES" report "$@" \
    --ignore github.com/charle-z/mcp-devbox \
    --template "$TEMPLATE" >"$output"
  grep -q '^Component: ' "$output"
  if grep -Eq '^License: (Unknown|UNKNOWN|NOASSERTION)$' "$output"; then
    printf 'unknown Go dependency license in %s notice\n' "$platform" >&2
    exit 1
  fi
}

LINUX_GO="$WORK/linux-go.txt"
WINDOWS_GO="$WORK/windows-go.txt"
generate_go_notices linux "$LINUX_GO" \
  ./cmd/mcp-edge ./cmd/mcp-autopilot-worker ./cmd/mcp-bundle-updater
generate_go_notices windows "$WINDOWS_GO" \
  ./cmd/mcp-edge ./cmd/mcp-bundle-updater

LINUX_OUTPUT="$OUTPUT/mcp-devbox-edge_${RELEASE}_amd64.third-party-notices.txt"
WINDOWS_OUTPUT="$OUTPUT/mcp-devbox-edge_${RELEASE}_windows_amd64.third-party-notices.txt"
[ ! -e "$LINUX_OUTPUT" ] && [ ! -e "$WINDOWS_OUTPUT" ] || {
  printf 'notice output already exists\n' >&2
  exit 1
}

{
  printf 'Aeontra Edge third-party notices\n'
  printf 'Target: linux-amd64\n'
  printf 'Release: %s\n\n' "$RELEASE"
  printf 'Go dependencies included in Aeontra Edge binaries\n\n'
  cat "$LINUX_GO"
  printf '\n================================================================================\n'
  printf 'Bundled component: GitHub CLI 2.97.0\n'
  printf 'Source commit: 55dbb4dc6b7edb10b48e3d7fc5bccd32318d1b55\n'
  printf 'License: MIT\n\n'
  cat "$GH_LICENSE"
  printf '\n================================================================================\n'
  printf 'Bundled component: OpenAI Codex CLI 0.147.0\n'
  printf 'Source commit: be6e8eac029b183056b7e4402879f15d2c85f61b\n'
  printf 'License: Apache-2.0\n\n'
  cat "$PROJECT_LICENSE"
  printf '\nUpstream NOTICE:\n\n'
  cat "$CODEX_NOTICE"
} >"$LINUX_OUTPUT"

{
  printf 'Aeontra Edge third-party notices\n'
  printf 'Target: windows-amd64\n'
  printf 'Release: %s\n\n' "$RELEASE"
  printf 'Go dependencies included in Aeontra Edge binaries\n\n'
  cat "$WINDOWS_GO"
} >"$WINDOWS_OUTPUT"

chmod 0644 "$LINUX_OUTPUT" "$WINDOWS_OUTPUT"
printf 'generated Edge third-party notices for %s\n' "$RELEASE"
