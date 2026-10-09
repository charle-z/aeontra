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

FROM golang:1.26.9-alpine3.24@sha256:cdfd4fe2da6b225d8b40c6b7a105736e548e83ff56d5d8f9394446eeb5eb84e0 AS build

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

# Retain the Go toolchain and Node runtime. Repository-code execution and package
# management belong to the private L3 executor or Edge, not the public backend.
FROM cgr.dev/chainguard/wolfi-base:latest@sha256:1d95114038f76513a9ace6fca107d5582b08c65981f81f61cb56bf7fd2ef216d

# OCI metadata (good practice; helps registries/scanners identify the image).
# Tags remain readable while the digest fixes the exact multi-platform image index.
LABEL org.opencontainers.image.title="Aeontra" \
	org.opencontainers.image.description="Scoped, auditable MCP operations for software development" \
	org.opencontainers.image.source="https://github.com/charle-z/aeontra"

COPY --from=build /usr/local/go /usr/local/go

RUN apk upgrade --no-cache \
	&& apk add --no-cache ca-certificates curl git libstdc++ nodejs-22 libssl3=3.6.5-r1 libcrypto3=3.6.5-r1 \
	&& test "$(node --version)" = v22.23.2 \
	&& test ! -e /usr/local/lib/node_modules/npm \
	&& test ! -e /usr/lib/node_modules/npm \
	&& ! command -v npm \
	&& ! command -v npx \
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

HEALTHCHECK --interval=10s --timeout=10s --start-period=20s --retries=12 \
	CMD curl -fsS --max-time 2 http://127.0.0.1:8765/readyz >/dev/null || exit 1

# Coolify/Docker use SIGTERM for rolling replacement. The Go server catches it,
# stops accepting new traffic, and drains in-flight requests before exit.
STOPSIGNAL SIGTERM

ENTRYPOINT ["/bin/sh", "-c"]
CMD ["exec /usr/local/bin/mcp-devbox serve --root \"${MCP_DEVBOX_ROOT:-/repos/workspace}\" --mode \"${MCP_DEVBOX_MODE:-read-only}\" --http 0.0.0.0:8765"]
