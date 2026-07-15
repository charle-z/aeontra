FROM node:24-bookworm-slim AS opencode

WORKDIR /opt/opencode
COPY test/opencode-e2e/package.json test/opencode-e2e/package-lock.json ./
RUN npm ci --no-audit --no-fund \
    && test "$(./node_modules/.bin/opencode --version)" = "1.18.1"

FROM golang:1.26.5-bookworm AS test-build

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test -c -tags=opencode_e2e -o /out/mcp-devbox-opencode-e2e ./internal/mcpserver

FROM golang:1.26.5-bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git ripgrep strace \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home --shell /bin/sh edge

WORKDIR /workspace
COPY --from=opencode /opt/opencode /opt/opencode
COPY --from=test-build /out/mcp-devbox-opencode-e2e /usr/local/bin/mcp-devbox-opencode-e2e
COPY . .
RUN mkdir -p /workspace/artifacts \
    && chown -R edge:edge /workspace /home/edge

ENV OPENCODE_E2E=1 \
    OPENCODE_E2E_BIN=/opt/opencode/node_modules/.bin/opencode \
    HOME=/home/edge \
    GOCACHE=/tmp/go-build \
    GOPATH=/tmp/go

USER edge:edge

ENTRYPOINT ["/usr/local/bin/mcp-devbox-opencode-e2e"]
CMD ["-test.run=^TestOpenCodeExternalModelVerticalSlice$", "-test.count=1", "-test.v"]
