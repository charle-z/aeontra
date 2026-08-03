#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: stage-pinned.sh --output <ABS_FILE>\n' >&2
  exit 2
}

OUTPUT=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) OUTPUT="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$OUTPUT" = /* ]] || usage
[ ! -e "$OUTPUT" ] || { printf 'output already exists\n' >&2; exit 1; }
for command in gh sha256sum tar install mktemp; do
  command -v "$command" >/dev/null 2>&1 || { printf 'required command unavailable\n' >&2; exit 1; }
done

TAG='v2.97.0'
VERSION="${TAG#v}"
ASSET='gh_2.97.0_linux_amd64.tar.gz'
DIGEST='a2c9b8497e1f85b1ad0dfcb78b5a622e098801b8e461e459e88e1ee12f018112'
WORK="$(mktemp -d)"
trap 'rm -rf -- "$WORK"' EXIT

gh release download "$TAG" --repo cli/cli --pattern "$ASSET" --dir "$WORK"
printf '%s  %s\n' "$DIGEST" "$WORK/$ASSET" | sha256sum --check --status
tar -xzf "$WORK/$ASSET" -C "$WORK"
SOURCE="$WORK/gh_${VERSION}_linux_amd64/bin/gh"
[ -x "$SOURCE" ] && [ ! -L "$SOURCE" ] || { printf 'GitHub CLI artifact is invalid\n' >&2; exit 1; }
install -D -m 0755 "$SOURCE" "$OUTPUT"
[[ "$("$OUTPUT" --version)" = 'gh version 2.97.0 '* ]] || { printf 'GitHub CLI version mismatch\n' >&2; exit 1; }
