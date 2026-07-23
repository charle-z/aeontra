# Current task

Historical deployed baseline preserved for documentation guards: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`.

P16 Step 3 first cut is implemented locally but not deployed. It adds a private versioned `projects.db`, human alias and owner/repository canonicalization, durable target/workspace bindings, profile enforcement, real read-only Git checkout revalidation, stable blocker codes, and `mcp-edge project status|resolve --alias <name> [--target <alias>]`. Normal output omits paths and device/workspace/runtime/job identifiers. Registration remains internal; discovery, ambiguity handling, safe association, clone and public MCP tools remain unfinished.

Verification: all Go packages pass when run in bounded groups; `go vet ./...`, Staticcheck v0.7.0, component builds, documentation guards and `git diff --check` pass. The single `go test ./...` process was killed by the workcell limit after passing its reported packages, so coverage was completed by explicit package groups. Local `-race` cannot run because CGO is disabled; exact-head CI remains required.

Next: commit this first Step 3 cut, publish the same branch without force push, inspect PR #48 exact-head checks, then continue test-first with unbound checkout discovery, ambiguity classification and lossless association preview. Do not alter the real Parrot Edge or production.
