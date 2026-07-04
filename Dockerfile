FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mcp-devbox ./cmd/mcp-devbox

# Runtime keeps the full Go 1.26 toolchain plus Node/npm so the global builder can
# run common Go and web project checks in the VPS container. (Bigger image, but this
# is a dev-agent box.)
FROM golang:1.26-alpine

# OCI metadata (good practice; helps registries/scanners identify the image).
# For fully reproducible prod builds, pin the base by digest (golang:1.26-alpine@sha256:...).
LABEL org.opencontainers.image.title="mcp-devbox" \
	org.opencontainers.image.description="Secure-by-default local MCP server for AI coding agents" \
	org.opencontainers.image.source="https://github.com/charle-z/mcp-devbox" \
	org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache ca-certificates git nodejs npm wget \
	&& (corepack enable 2>/dev/null || true) \
	&& addgroup -S mcpdevbox \
	&& adduser -S -D -H -u 10001 -G mcpdevbox mcpdevbox \
	&& mkdir -p /repos \
	&& chown -R mcpdevbox:mcpdevbox /repos \
	# Defense in depth: strip setuid/setgid bits so no binary can be used to
	# escalate privileges (the app runs non-root and needs no setuid tools).
	&& find / -xdev -perm /6000 -type f -exec chmod a-s {} + 2>/dev/null || true

# Writable Go caches for the non-root user (go test/build need these), plus a
# default git identity so git_commit works without a home dir (override in Coolify).
ENV GOCACHE=/tmp/go-build \
	GOPATH=/tmp/go \
	GIT_AUTHOR_NAME=mcp-devbox \
	GIT_AUTHOR_EMAIL=mcp-devbox@localhost \
	GIT_COMMITTER_NAME=mcp-devbox \
	GIT_COMMITTER_EMAIL=mcp-devbox@localhost

COPY --from=build /out/mcp-devbox /usr/local/bin/mcp-devbox

USER 10001:10001
WORKDIR /repos
VOLUME ["/repos"]
EXPOSE 8765

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
	CMD wget -qO- http://127.0.0.1:8765/healthz >/dev/null || exit 1

ENTRYPOINT ["/bin/sh", "-c"]
CMD ["exec /usr/local/bin/mcp-devbox serve --root \"${MCP_DEVBOX_ROOT:-/repos/workspace}\" --mode \"${MCP_DEVBOX_MODE:-read-only}\" --http 0.0.0.0:8765"]
