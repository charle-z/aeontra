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
RUN go build -o /out/mcp-edge ./cmd/mcp-edge \
    && go build -o /out/model-turn-driver ./cmd/model-turn-driver \
    && go test -c -tags=opencode_e2e -o /out/mcp-devbox-opencode-e2e ./internal/mcpserver

FROM golang:1.26.5-bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git ripgrep strace \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home --shell /bin/sh edge

WORKDIR /workspace
COPY --from=opencode /opt/opencode /opt/opencode
COPY --from=test-build /out/mcp-devbox-opencode-e2e /usr/local/bin/mcp-devbox-opencode-e2e
COPY --from=test-build /out/mcp-edge /usr/local/bin/mcp-edge
COPY --from=test-build /out/model-turn-driver /usr/local/bin/model-turn-driver
COPY . .
RUN mkdir -p /workspace/artifacts \
    && chown -R edge:edge /workspace /home/edge

ARG P11_2_GIT_TREE
ARG P11_2_GIT_COMMIT

ENV P11_2_GIT_TREE=$P11_2_GIT_TREE \
    P11_2_GIT_COMMIT=$P11_2_GIT_COMMIT \
    OPENCODE_E2E=1 \
    OPENCODE_E2E_BIN=/opt/opencode/node_modules/.bin/opencode \
    MCP_EDGE_E2E_BIN=/usr/local/bin/mcp-edge \
    MODEL_TURN_DRIVER_E2E_BIN=/usr/local/bin/model-turn-driver \
    OPENCODE_PROVIDER_E2E_PATH=/workspace/integrations/opencode/provider \
    HOME=/home/edge \
    GOCACHE=/tmp/go-build \
    GOPATH=/tmp/go

USER edge:edge

ENTRYPOINT ["/usr/local/bin/mcp-devbox-opencode-e2e"]
CMD ["-test.run=TestOpenCodeExternalModelVerticalSlice|TestRemote|TestZZP112ReleaseCandidateReport", "-test.count=1", "-test.v"]
