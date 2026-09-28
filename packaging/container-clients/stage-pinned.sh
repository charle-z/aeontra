#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ] || [ "$1" != '--output' ] || [[ "$2" != /* ]] || [ -e "$2" ]; then
  printf 'usage: stage-pinned.sh --output <NEW_ABSOLUTE_DIR>\n' >&2
  exit 2
fi
OUTPUT="$2"
for command in curl sha256sum tar install mktemp; do
  command -v "$command" >/dev/null 2>&1 || { printf 'missing build command: %s\n' "$command" >&2; exit 1; }
done

DOCKER_VERSION='29.8.1'
DOCKER_ARCHIVE_SHA256='d8db66739d2e28d4933786d73e918d9be643a67fbd835db1bf740d650a259e70'
BUILDX_VERSION='0.37.1'
BUILDX_SHA256='9447199cdb435f25880548343c128a4b6650e8891ee598905d8d29d39a8e359b'
WORK="$(mktemp -d)"
trap 'rm -rf -- "$WORK"' EXIT

curl --fail --location --silent --show-error --proto '=https' --proto-redir '=https' \
  --output "$WORK/docker.tgz" \
  "https://download.docker.com/linux/static/stable/x86_64/docker-${DOCKER_VERSION}.tgz"
printf '%s  %s\n' "$DOCKER_ARCHIVE_SHA256" "$WORK/docker.tgz" | sha256sum --check --status
tar -xzf "$WORK/docker.tgz" -C "$WORK" docker/docker
[ -x "$WORK/docker/docker" ] && [ ! -L "$WORK/docker/docker" ] || { printf 'Docker CLI artifact is invalid\n' >&2; exit 1; }
[[ "$("$WORK/docker/docker" --version)" == *"${DOCKER_VERSION}"* ]] || { printf 'Docker CLI version mismatch\n' >&2; exit 1; }

curl --fail --location --silent --show-error --proto '=https' --proto-redir '=https' \
  --output "$WORK/docker-buildx" \
  "https://github.com/docker/buildx/releases/download/v${BUILDX_VERSION}/buildx-v${BUILDX_VERSION}.linux-amd64"
printf '%s  %s\n' "$BUILDX_SHA256" "$WORK/docker-buildx" | sha256sum --check --status
chmod 0755 "$WORK/docker-buildx"
[[ "$("$WORK/docker-buildx" version)" == *"v${BUILDX_VERSION}"* ]] || { printf 'Buildx version mismatch\n' >&2; exit 1; }

install -d -m 0755 "$OUTPUT/bin" "$OUTPUT/config/cli-plugins"
install -m 0755 "$WORK/docker/docker" "$OUTPUT/bin/docker"
install -m 0755 "$WORK/docker-buildx" "$OUTPUT/config/cli-plugins/docker-buildx"
printf 'staged pinned Docker CLI %s and Buildx %s\n' "$DOCKER_VERSION" "$BUILDX_VERSION"
