# syntax=docker/dockerfile:1.7

FROM node:22-alpine3.22@sha256:cd7807368cf24826297cbad5dca1a44972ccfd770647db52a8c7589eb4599ac8 AS console-build

# The production VPS has two vCPUs. Keep image assembly to one logical CPU by
# default so the live control plane, Coolify and Traefik retain scheduler time.
# External build hosts can override these args when they have spare capacity.
ARG BUILD_GOMAXPROCS=1
ARG BUILD_UV_THREADPOOL_SIZE=1

WORKDIR /src
RUN corepack enable && corepack prepare pnpm@10.13.1 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/console/package.json web/console/package.json
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY web/console web/console
# CI is the test gate. A production image build only assembles the already gated
# console, avoiding a second CPU-heavy test/typecheck pass on the deployment VPS.
RUN GOMAXPROCS=${BUILD_GOMAXPROCS} \
	UV_THREADPOOL_SIZE=${BUILD_UV_THREADPOOL_SIZE} \
	pnpm console:build

FROM golang:1.26.6-alpine3.24@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build

# GIT_SHA is the commit being built. Coolify (or any CI) should pass it with
# --build-arg GIT_SHA=$(git rev-parse HEAD). It is baked into the binary via -ldflags so
# the running instance can report exactly which commit is live (GET /healthz shows it).
# Because ARG changes bust the build cache from this point on, every new commit also
# forces a genuine rebuild instead of reusing a stale cached image layer.
ARG GIT_SHA=unknown
ARG BUILD_TIME=unknown
ARG BUILD_GOMAXPROCS=1
ARG BUILD_GO_PARALLELISM=1

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
COPY profiles ./profiles
COPY docs/showcase ./docs/showcase
COPY --from=console-build /src/internal/console/assets ./internal/console/assets

RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
	--mount=type=cache,target=/root/.cache/go-build,sharing=locked \
	CGO_ENABLED=0 GOMAXPROCS=${BUILD_GOMAXPROCS} \
	go build -p=${BUILD_GO_PARALLELISM} -trimpath \
	-ldflags="-s -w -X github.com/charle-z/mcp-devbox/internal/buildinfo.Commit=${GIT_SHA} -X github.com/charle-z/mcp-devbox/internal/buildinfo.BuiltAt=${BUILD_TIME}" \
	-o /out/mcp-devbox ./cmd/mcp-devbox

# Docker Official Images consume the Node.js project's musl builds from this
# distribution endpoint. Pin both supported architectures by their published
# SHA-256 so the final runtime never inherits an outdated Node image or Alpine's
# dynamically linked sqlite dependency graph.
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS node-runtime
ARG TARGETARCH=amd64
RUN apk add --no-cache ca-certificates libstdc++ xz \
	&& case "$TARGETARCH" in \
		amd64) node_arch=x64; node_sha=2d18b5731055f7efa6c899004909b00ee110e38d3775745f60ec9ccf1f9982e7 ;; \
		arm64) node_arch=arm64; node_sha=86e3f4d05d92c6a4e51b0ce8bab6c22d602d4b8a372743fed302403de5376d4c ;; \
		*) echo "unsupported Node runtime architecture: $TARGETARCH" >&2; exit 1 ;; \
	esac \
	&& node_archive=/tmp/node-v22.23.2-linux-${node_arch}-musl.tar.xz \
	&& busybox wget -qO "$node_archive" \
		https://unofficial-builds.nodejs.org/download/release/v22.23.2/node-v22.23.2-linux-${node_arch}-musl.tar.xz \
	&& printf '%s  %s\n' "$node_sha" "$node_archive" | busybox sha256sum -c - \
	&& mkdir -p /node \
	&& tar -xJf "$node_archive" --strip-components=1 -C /node \
	&& test "$(/node/bin/node --version)" = v22.23.2

# Runtime keeps the full Go 1.26 toolchain plus Node/npm so the global builder can
# run common Go and web project checks in the VPS container. (Bigger image, but this
# is a dev-agent box.)
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

# OCI metadata (good practice; helps registries/scanners identify the image).
# Tags remain readable while the digest fixes the exact multi-platform image index.
LABEL org.opencontainers.image.title="Aeontra" \
	org.opencontainers.image.description="Scoped, auditable MCP operations for software development" \
	org.opencontainers.image.source="https://github.com/charle-z/aeontra"

COPY --from=build /usr/local/go /usr/local/go
COPY --from=node-runtime /node/bin/node /usr/local/bin/node

RUN apk upgrade --no-cache \
	&& apk add --no-cache ca-certificates git libstdc++ \
	&& npm_archive=/tmp/npm-12.0.1.tgz \
	&& brace_archive=/tmp/brace-expansion-5.0.9.tgz \
	&& ip_archive=/tmp/ip-address-10.3.1.tgz \
	&& tar_archive=/tmp/tar-7.5.21.tgz \
	&& busybox wget -qO "$npm_archive" https://registry.npmjs.org/npm/-/npm-12.0.1.tgz \
	&& busybox wget -qO "$brace_archive" https://registry.npmjs.org/brace-expansion/-/brace-expansion-5.0.9.tgz \
	&& busybox wget -qO "$ip_archive" https://registry.npmjs.org/ip-address/-/ip-address-10.3.1.tgz \
	&& busybox wget -qO "$tar_archive" https://registry.npmjs.org/tar/-/tar-7.5.21.tgz \
	&& printf '%s  %s\n' 5e02bea4c784df1c3bbea9e55c7d2232329e1d1920c254789833ed9e8b0a5f16 "$npm_archive" \
		| busybox sha256sum -c - \
	&& printf '%s  %s\n' 5d06001fddd25cbee90c96db4dc5b7b57711b984c3141e28d10f143deb52dbaf "$brace_archive" \
		| busybox sha256sum -c - \
	&& printf '%s  %s\n' ad1790063beea11a312c801df30d58e147de762f4f77787552376eb7424623e5 "$ip_archive" \
		| busybox sha256sum -c - \
	&& printf '%s  %s\n' bcedf25a21daecd1a18fb5e19ab855b7d79ec8ef1da175e8ba85cfc0ed0069d1 "$tar_archive" \
		| busybox sha256sum -c - \
	&& mkdir -p /tmp/npm-unpack /tmp/brace-unpack /tmp/ip-unpack /tmp/tar-unpack /usr/local/lib/node_modules \
	&& busybox tar -xzf "$npm_archive" -C /tmp/npm-unpack \
	&& busybox tar -xzf "$brace_archive" -C /tmp/brace-unpack \
	&& busybox tar -xzf "$ip_archive" -C /tmp/ip-unpack \
	&& busybox tar -xzf "$tar_archive" -C /tmp/tar-unpack \
	&& rm -rf /tmp/npm-unpack/package/node_modules/brace-expansion \
	&& mkdir -p /tmp/npm-unpack/package/node_modules/brace-expansion \
	&& cp -a /tmp/brace-unpack/package/. /tmp/npm-unpack/package/node_modules/brace-expansion/ \
	&& rm -rf /tmp/npm-unpack/package/node_modules/ip-address \
	&& mkdir -p /tmp/npm-unpack/package/node_modules/ip-address \
	&& cp -a /tmp/ip-unpack/package/. /tmp/npm-unpack/package/node_modules/ip-address/ \
	&& rm -rf /tmp/npm-unpack/package/node_modules/tar \
	&& mkdir -p /tmp/npm-unpack/package/node_modules/tar \
	&& cp -a /tmp/tar-unpack/package/. /tmp/npm-unpack/package/node_modules/tar/ \
	&& mv /tmp/npm-unpack/package /usr/local/lib/node_modules/npm \
	&& ln -sf ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
	&& ln -sf ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
	&& test "$(npm --version)" = 12.0.1 \
	&& test "$(node -p \
		"require('/usr/local/lib/node_modules/npm/node_modules/brace-expansion/package.json').version")" = 5.0.9 \
	&& test "$(node -p \
		"require('/usr/local/lib/node_modules/npm/node_modules/ip-address/package.json').version")" = 10.3.1 \
	&& test "$(node -p \
		"require('/usr/local/lib/node_modules/npm/node_modules/tar/package.json').version")" = 7.5.21 \
	&& test "$(find /usr/local/lib/node_modules/npm -path '*/brace-expansion/package.json' -type f | wc -l)" -eq 1 \
	&& test "$(find /usr/local/lib/node_modules/npm -path '*/ip-address/package.json' -type f | wc -l)" -eq 1 \
	&& test "$(find /usr/local/lib/node_modules/npm -path '*/tar/package.json' -type f | wc -l)" -eq 1 \
	&& test ! -e /usr/lib/node_modules/npm \
	&& rm -rf "$npm_archive" "$brace_archive" "$ip_archive" "$tar_archive" /tmp/npm-unpack /tmp/brace-unpack /tmp/ip-unpack /tmp/tar-unpack \
	&& (corepack enable 2>/dev/null || true) \
	&& addgroup -S -g 10001 mcpdevbox \
	&& adduser -S -D -H -u 10001 -G mcpdevbox mcpdevbox \
	&& mkdir -p /repos /brain /state/tasks /state/results /state/edge /state/telemetry /state/model-turns /state/logs /state/console /state/brain \
	&& chmod 0700 /state/tasks /state/results /state/edge /state/telemetry /state/model-turns /state/logs /state/console /state/brain \
	&& chown -R mcpdevbox:mcpdevbox /repos /brain /state \
	# Defense in depth: strip setuid/setgid bits so no binary can be used to
	# escalate privileges (the app runs non-root and needs no setuid tools).
	&& (find / -xdev -perm /6000 -type f -exec chmod a-s {} + 2>/dev/null || true)

# Writable Go caches for the non-root user (go test/build need these), plus a
# default git identity so git_commit works without a home dir (override in Coolify).
ENV MCP_DEVBOX_TASK_ROOT=/state/tasks \
	MCP_DEVBOX_STATE_ROOT=/state \
	MCP_DEVBOX_OBSERVABILITY=file \
	PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
	GOCACHE=/tmp/go-build \
	GOPATH=/tmp/go \
	GIT_AUTHOR_NAME=mcp-devbox \
	GIT_AUTHOR_EMAIL=mcp-devbox@localhost \
	GIT_COMMITTER_NAME=mcp-devbox \
	GIT_COMMITTER_EMAIL=mcp-devbox@localhost

COPY --from=build /out/mcp-devbox /usr/local/bin/mcp-devbox

USER 10001:10001
WORKDIR /repos
VOLUME ["/repos", "/brain", "/state"]
EXPOSE 8765

HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=12 \
	CMD busybox wget -qO- http://127.0.0.1:8765/readyz >/dev/null || exit 1

# Coolify/Docker use SIGTERM for rolling replacement. The Go server catches it,
# stops accepting new traffic, and drains in-flight requests before exit.
STOPSIGNAL SIGTERM

ENTRYPOINT ["/bin/sh", "-c"]
CMD ["exec /usr/local/bin/mcp-devbox serve --root \"${MCP_DEVBOX_ROOT:-/repos/workspace}\" --mode \"${MCP_DEVBOX_MODE:-read-only}\" --http 0.0.0.0:8765"]
