#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: stage-pinned.sh --output-bin <ABS_FILE> --output-pin <ABS_FILE>\n' >&2
  exit 2
}

OUTPUT_BIN=''; OUTPUT_PIN=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-bin) OUTPUT_BIN="${2:-}"; shift 2 ;;
    --output-pin) OUTPUT_PIN="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$OUTPUT_BIN" = /* && "$OUTPUT_PIN" = /* ]] || usage
[ ! -e "$OUTPUT_BIN" ] && [ ! -e "$OUTPUT_PIN" ] || { printf 'output already exists\n' >&2; exit 1; }
for command in curl sha256sum tar install mktemp; do
  command -v "$command" >/dev/null 2>&1 || { printf 'required command unavailable\n' >&2; exit 1; }
done

TAG='rust-v0.147.0'
ASSET='codex-x86_64-unknown-linux-musl.tar.gz'
ARCHIVE_SHA256='0246e2e773834e07f0fb5249ed6ebad12e4591e608f8c7bb97dd6a9690544c36'
BINARY_SHA256='cb0a15567e9a60a5820d54b0f6ae86d504dc3805c1eab21a47f70e3eb7b73a40'
WORK="$(mktemp -d)"
trap 'rm -rf -- "$WORK"' EXIT

curl --fail --location --proto '=https' --proto-redir '=https' \
  --output "$WORK/$ASSET" \
  "https://github.com/openai/codex/releases/download/$TAG/$ASSET"
printf '%s  %s\n' "$ARCHIVE_SHA256" "$WORK/$ASSET" | sha256sum --check --status
tar -xzf "$WORK/$ASSET" -C "$WORK"
SOURCE="$WORK/codex-x86_64-unknown-linux-musl"
[ -x "$SOURCE" ] && [ ! -L "$SOURCE" ] || { printf 'Codex artifact is invalid\n' >&2; exit 1; }
printf '%s  %s\n' "$BINARY_SHA256" "$SOURCE" | sha256sum --check --status
[[ "$("$SOURCE" --version)" = 'codex-cli 0.147.0' ]] || { printf 'Codex version mismatch\n' >&2; exit 1; }
install -D -m 0755 "$SOURCE" "$OUTPUT_BIN"
install -D -m 0644 integrations/codex/pin.json "$OUTPUT_PIN"
