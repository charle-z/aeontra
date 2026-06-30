FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mcp-devbox ./cmd/mcp-devbox

# Runtime keeps the full Go 1.26 toolchain so run_tests ("go test ./...") works in
# the container and matches this/most repos' go.mod. (Bigger image, but this is a
# dev-agent box.) For pure read/patch/commit you only need git, which is included.
FROM golang:1.26-alpine

RUN apk add --no-cache ca-certificates git wget \
	&& addgroup -S mcpdevbox \
	&& adduser -S -D -H -u 10001 -G mcpdevbox mcpdevbox \
	&& mkdir -p /repos \
	&& chown -R mcpdevbox:mcpdevbox /repos

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
