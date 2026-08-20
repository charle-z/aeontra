FROM node:24-bookworm-slim@sha256:3638d9a6fe4030bd716be989438248074489337ba3275657f93595428be4fc03 AS opencode

WORKDIR /opt/opencode
COPY test/opencode-e2e/package.json test/opencode-e2e/package-lock.json ./
RUN npm ci --no-audit --no-fund \
    && test "$(./node_modules/.bin/opencode --version)" = "1.18.1" \
    && test -x ./node_modules/opencode-linux-x64/bin/opencode \
    && install -m 0755 ./node_modules/opencode-linux-x64/bin/opencode /opt/opencode/opencode-1.18.1 \
    && test "$(/opt/opencode/opencode-1.18.1 --version)" = "1.18.1"

FROM golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS test-build

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=opencode /usr/local/bin/node /tmp/opencode-e2e-node
COPY --from=opencode /opt/opencode/opencode-1.18.1 /tmp/opencode-e2e-opencode
RUN BUNDLE_PUBLIC_KEY="$(go run ./cmd/mcp-bundle-keygen --output /tmp/opencode-e2e-release.key)" \
    && install -m 0755 /bin/true /tmp/opencode-e2e-gh \
    && ./packaging/parrot/stage-edge-bundle.sh --output /out/release --release p15.0.0 --manifest-version 4 --commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa --catalog sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb --private-key /tmp/opencode-e2e-release.key --public-key "$BUNDLE_PUBLIC_KEY" --node-bin /tmp/opencode-e2e-node --gh-bin /tmp/opencode-e2e-gh --opencode-bin /tmp/opencode-e2e-opencode --opencode-lock /workspace/test/opencode-e2e/package-lock.json --codex-bin /bin/true --codex-pin /workspace/integrations/codex/pin.json \
    && rm /out/release/manifest.json /out/release/manifest.sig \
    && E2E_LDFLAGS="-s -w -X github.com/charle-z/mcp-devbox/internal/buildinfo.Commit=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa -X github.com/charle-z/mcp-devbox/internal/buildinfo.EdgeBundleRelease=p15.0.0 -X github.com/charle-z/mcp-devbox/internal/buildinfo.EdgeBundleCatalogHash=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb -X github.com/charle-z/mcp-devbox/internal/buildinfo.EdgeBundlePublicKey=$BUNDLE_PUBLIC_KEY" \
    && go build -trimpath -buildvcs=false -tags=opencode_e2e -ldflags "$E2E_LDFLAGS" -o /out/release/bin/mcp-edge ./cmd/mcp-edge \
    && go run ./cmd/mcp-bundle-manifest --root /out/release --release p15.0.0 --commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa --protocol mcp-devbox.edge-bundle.v1 --catalog sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb --architecture amd64 --private-key /tmp/opencode-e2e-release.key \
    && cp /out/release/bin/mcp-edge /out/mcp-edge \
    && cp /out/release/libexec/model-turn-driver /out/model-turn-driver \
    && rm /tmp/opencode-e2e-release.key \
    && go test -c -tags=opencode_e2e -o /out/mcp-devbox-opencode-e2e ./internal/mcpserver

FROM golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git ripgrep strace \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home --shell /bin/sh edge

WORKDIR /workspace
COPY --from=opencode /opt/opencode /opt/opencode
COPY --from=test-build /out/mcp-devbox-opencode-e2e /usr/local/bin/mcp-devbox-opencode-e2e
COPY --from=test-build /out/mcp-edge /usr/local/bin/mcp-edge
COPY --from=test-build /out/model-turn-driver /usr/local/bin/model-turn-driver
COPY --from=test-build /out/release /opt/mcp-devbox/current
COPY test/opencode-e2e/entrypoint.sh /usr/local/bin/opencode-e2e-entrypoint
COPY . .
RUN mkdir -p /workspace/artifacts \
    && chmod 0755 /usr/local/bin/opencode-e2e-entrypoint \
    && chown -R edge:edge /workspace /home/edge

ARG P11_2_GIT_TREE
ARG P11_2_GIT_COMMIT

ENV P11_2_GIT_TREE=$P11_2_GIT_TREE \
    P11_2_GIT_COMMIT=$P11_2_GIT_COMMIT \
    OPENCODE_E2E=1 \
    MCP_DEVBOX_RELAY_CONTAINER_E2E=1 \
    OPENCODE_E2E_BIN=/opt/opencode/opencode-1.18.1 \
    MCP_EDGE_E2E_BIN=/usr/local/bin/mcp-edge \
    MODEL_TURN_DRIVER_E2E_BIN=/usr/local/bin/model-turn-driver \
    OPENCODE_PROVIDER_E2E_PATH=/opt/mcp-devbox/current/opencode-provider \
    OPENCODE_E2E_INTEGRITY=/opt/mcp-devbox/current/opencode/package-lock.json \
    HOME=/home/edge \
    GOCACHE=/tmp/go-build \
    GOPATH=/tmp/go

USER edge:edge

ENTRYPOINT ["/usr/local/bin/opencode-e2e-entrypoint"]
CMD ["-test.run=TestOpenCodeExternalModelVerticalSlice|TestRemoteOpenCodeDistributedRelay", "-test.count=1", "-test.v"]
