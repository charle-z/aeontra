FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mcp-devbox ./cmd/mcp-devbox

FROM alpine:3.22

RUN apk add --no-cache ca-certificates git wget \
	&& addgroup -S mcpdevbox \
	&& adduser -S -D -H -u 10001 -G mcpdevbox mcpdevbox \
	&& mkdir -p /repos \
	&& chown -R mcpdevbox:mcpdevbox /repos

COPY --from=build /out/mcp-devbox /usr/local/bin/mcp-devbox

USER 10001:10001
WORKDIR /repos
VOLUME ["/repos"]
EXPOSE 8765

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
	CMD wget -qO- http://127.0.0.1:8765/healthz >/dev/null || exit 1

ENTRYPOINT ["/bin/sh", "-c"]
CMD ["exec /usr/local/bin/mcp-devbox serve --root \"${MCP_DEVBOX_ROOT:-/repos/workspace}\" --mode \"${MCP_DEVBOX_MODE:-read-only}\" --http 0.0.0.0:8765"]
