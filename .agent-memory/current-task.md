# Current task

P8.1 Console 2.0 is complete / merge-ready from deployed P9 base `4fbe1dda02351c632e67c0f10a5c5b314df745e2` on PR #10 (`https://github.com/charle-z/mcp-devbox/pull/10`). The corrected implementation head `90d3e38018d7cc8cd1df1bd71c1050805626ed4e` passed Verify, Race, Staticcheck, Govulncheck, CodeQL, Dependency Review, Docker build, SPDX SBOM and the unchanged zero High/Critical Grype gate.

The first PR head failed because pnpm caching was configured before pnpm existed, Staticcheck rejected a nil-context literal, and Dependency Review found vulnerable Vite/Vitest versions. Step 6 fixed workflow ordering, used a fail-closed test without suppression, and upgraded to Vite 7.3.5 and Vitest 4.1.0. No gate or threshold was weakened.

The public catalog remains exactly 67 tools with P9 hash `sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`. Query-string credentials return 401, console OAuth uses state/PKCE/single-use codes and a strict opaque cookie, and durable content-free tasks live under `/state/tasks` with authenticated SSE.

Next: commit the release-evidence documentation, publish it, require the same exact-head gates again, merge PR #10 without force, deploy only through existing Coolify application `jqf7qz5ensoqtvl1tb197gcv`, verify production OAuth/cookie/query-401/recovery/task/SSE/data/catalog/Brain behavior, and create annotated tag `p8.1`. Do not start Edge, Parrot, HTB, web-terminal or durable-agent work.
