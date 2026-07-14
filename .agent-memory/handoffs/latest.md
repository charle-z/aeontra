# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p9-brain`
Base: Step 4 `c11af4a97f9e11cb9a4385e4ee2a56bf663c8938` / P8 tag `2e3429c9d6342e8e091cadf65293c5c85b1b3259`

## Current phase

P9 Brain Step 5 is implemented locally and awaiting final gates/commit. Public Brain
tools, runtime configuration, persistent volume, operations, and deployment remain
absent. Production and the current console remain unchanged at P8.

## Step 5 behavior

- every Service owns one non-nil disabled-safe Brain capability;
- one uniform not-configured error across search/read/write/index/context;
- independently validated store root never added to repository policy roots;
- safe typed search/read+backlinks/write/status+reindex/context methods;
- audit spans omit query, body, provenance, private root and secret canaries;
- shared redaction reapplied to all returned textual fields;
- 16-note/4 KiB curated-first context without bodies or expired working notes;
- capability and runtime close release SQLite before log sinks;
- AST boundary preserves Service as configuration-only facade;
- coverage: Brain 81.7%, tools 73.9%, app 68.0%.

## Owner decision preserved

Do not change the deployed console during P9. The creative BIOS-inspired redesign,
live durable task/device state, and OAuth-only migration belong to a separate branch
after P9 closure.

## Next safe step

Commit/publish Step 5. Step 6 starts with failing catalog contract tests for exactly
five appended Brain tools while proving the prior 62 definitions remain unchanged.
Do not wire production env/mounts until Step 7. The invariant is no resident service.
