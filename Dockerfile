# syntax=docker/dockerfile:1.7

FROM node:22-alpine3.22 AS console-build

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

FROM golang:1.26.5-alpine3.24 AS build

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
COPY --from=console-build /src/internal/console/assets ./internal/console/assets

RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
	--mount=type=cache,target=/root/.cache/go-build,sharing=locked \
	CGO_ENABLED=0 GOMAXPROCS=${BUILD_GOMAXPROCS} \
	go build -p=${BUILD_GO_PARALLELISM} -trimpath \
	-ldflags="-s -w -X github.com/charle-z/mcp-devbox/internal/buildinfo.Commit=${GIT_SHA} -X github.com/charle-z/mcp-devbox/internal/buildinfo.BuiltAt=${BUILD_TIME}" \
	-o /out/mcp-devbox ./cmd/mcp-devbox

# Runtime keeps the full Go 1.26 toolchain plus Node/npm so the global builder can
# run common Go and web project checks in the VPS container. (Bigger image, but this
# is a dev-agent box.)
FROM golang:1.26.5-alpine3.24

# OCI metadata (good practice; helps registries/scanners identify the image).
# For fully reproducible prod builds, pin the base by digest (golang:1.26-alpine@sha256:...).
LABEL org.opencontainers.image.title="mcp-devbox" \
	org.opencontainers.image.description="Secure-by-default local MCP server for AI coding agents" \
	org.opencontainers.image.source="https://github.com/charle-z/mcp-devbox"

RUN apk add --no-cache ca-certificates git nodejs npm \
	&& npm install --global npm@12.0.1 --ignore-scripts \
	&& npm cache clean --force \
	&& apk del npm \
	&& (corepack enable 2>/dev/null || true) \
	&& addgroup -S mcpdevbox \
	&& adduser -S -D -H -u 10001 -G mcpdevbox mcpdevbox \
	&& mkdir -p /repos /brain /state/tasks /state/results /state/edge /state/telemetry /state/model-turns /state/logs /state/console /state/brain \
	&& chmod 0700 /state/tasks /state/results /state/edge /state/telemetry /state/model-turns /state/logs /state/console /state/brain \
	&& chown -R mcpdevbox:mcpdevbox /repos /brain /state \
	# Defense in depth: strip setuid/setgid bits so no binary can be used to
	# escalate privileges (the app runs non-root and needs no setuid tools).
	&& find / -xdev -perm /6000 -type f -exec chmod a-s {} + 2>/dev/null || true

# Writable Go caches for the non-root user (go test/build need these), plus a
# default git identity so git_commit works without a home dir (override in Coolify).
ENV MCP_DEVBOX_TASK_ROOT=/state/tasks \
	MCP_DEVBOX_STATE_ROOT=/state \
	MCP_DEVBOX_OBSERVABILITY=file \
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
	CMD busybox wget -qO- http://127.0.0.1:8765/healthz >/dev/null || exit 1

# Coolify/Docker use SIGTERM for rolling replacement. The Go server catches it,
# stops accepting new traffic, and drains in-flight requests before exit.
STOPSIGNAL SIGTERM

ENTRYPOINT ["/bin/sh", "-c"]
CMD ["exec /usr/local/bin/mcp-devbox serve --root \"${MCP_DEVBOX_ROOT:-/repos/workspace}\" --mode \"${MCP_DEVBOX_MODE:-read-only}\" --http 0.0.0.0:8765"]
