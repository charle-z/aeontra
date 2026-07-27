# Current task — Step 16 documentation foundation

Historical closure markers remain evidence, not current operational configuration: P8.1 was deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain followed at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `step16-documentation-foundation`, based on clean synchronized `main` at `63825986e0d86d9f8fb623a76a218dbe00bd1969`.

## Scope

Step 16 replaces the phase-diary README with a product entry point; creates canonical `docs/configuration.md` and `config/mcp-devbox.env.sample`; separates the technical security model in `docs/security.md` from the public reporting policy in `SECURITY.md`; defines canonical documentation ownership; and updates documentation contracts so mutable counts, hashes, commits, releases, and device state remain in `/version`, `system_runtime_info`, `docs/tools.md`, or dated baselines.

No runtime behavior, tool schema, catalog, route, OAuth behavior, CSP, Edge package, service, BuildKit, AppArmor, quota, runner, or deployment configuration was changed.

## Deliberate RED evidence

`go test ./docs -run TestStep16 -count=1` failed before implementation because the old README was a phase diary, `docs/configuration.md` and the safe sample did not exist, and security/documentation ownership was not canonical.

## Local validation

- `go test ./docs -run TestStep16 -count=1`: passed.
- `go test ./docs -count=1`: passed.
- `go test ./... -count=1`: passed.
- `go vet ./...`: passed.
- `go build ./...`: passed.
- `git diff --check`: passed before this memory update and must be rerun before commit.

## Remaining Step 16 actions

1. Review the final diff including this task note.
2. Rerun all required local gates.
3. Commit with the required Step 16 prefix and `mcp-devbox-edge <edge@mcp-devbox.local>` identity.
4. Publish a normal PR, require every exact-head gate green, and merge with a merge commit.
5. Synchronize clean local `main` before beginning Step 17.
