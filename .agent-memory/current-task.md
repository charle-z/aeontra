# Current task

P8.1 Console 2.0 is deployed and tagged. PR #10 final head
`e96bbc81a2c524c3c7ee9b3eb4bd3945b61198e7` merged as
`d343264bffdc0ae1bc045a9d723e913be977090c`; deployment
`ody7vjcabb3r24b25ym34of9` finished healthy in the existing Coolify application.
The annotated tag `p8.1` points exactly to the merge commit.

Production catalog, Brain and console smokes passed with 67 tools and catalog hash
`sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`.
Query-string credentials return 401, bearer recovery remains header-only, the console
cookie is strict and opaque, durable tasks and SSE are operational, and Edge reports
`not_paired` without claiming an implementation.

P11 Step 2 closed the measured `workspace_checkpoint` read-only primitive. Its exact
18-field JSON schema is bounded to 4096 bytes and performs only jailed fixed-argv Git
reads plus a 240-rune redacted current-task summary. The reproducible fixture measured
2 to 1 MCP calls, 2052 to 406 response bytes, 260 to 0 repeated bytes and approximately
18.6 ms to 16.7 ms.

Step 3 persists exact content-free hourly/daily metrics in embedded SQLite, prunes at
startup and opportunistically, caps the DB at a 128 MiB page target, and rotates four
16 MiB observability plus four 32 MiB audit segments. The previous audit is never
deleted automatically.

Step 4 persists only redacted large tool output under `/state/results`, replaces it
with compact seven-field metadata, and exposes bounded `result_read`, `result_find`,
and `result_stage` reads. Successes expire after 24 hours, failures after 7 days; the
logical content quota is 256 MiB and reads cap at 16 KiB. Current catalog: 71 tools,
hash `sha256:7dfa9bb83c935c7df875740102dafa5572852e5e8cb6c064c89c1e3acb5e30ac`.

Steps 5-7 now implement the outbound-only Edge identity, signed leased-task protocol,
and separately installed Bubblewrap WSL development workcell. The implementation
head is `3a441e6`; pairing, merge, deployment and WSL installation have not occurred.

Current task: close the P11 release candidate on `codex/p11-edge-core`, publish only
that branch, open a PR against `main`, and require every gate on the exact final SHA.
All required local closure gates passed on 2026-07-15, including the no-cache
production image build. Remote PR gates remain pending.
Do not merge, deploy, tag, create a pairing code, install WSL, add a terminal, or
expand workcell authority.
