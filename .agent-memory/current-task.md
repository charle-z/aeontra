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

Current step: close P11 Step 2, the measured `workspace_checkpoint` read-only
primitive. Its exact 18-field JSON schema is bounded to 4096 bytes and performs only
jailed fixed-argv Git reads plus a 240-rune redacted current-task summary. The
reproducible fixture measured 2 to 1 MCP calls, 2052 to 406 response bytes, 260 to 0
repeated bytes and approximately 18.6 ms to 16.7 ms.

Step 3 persists exact content-free hourly/daily metrics in embedded SQLite, prunes at
startup and opportunistically, caps the DB at a 128 MiB page target, and rotates four
16 MiB observability plus four 32 MiB audit segments. The previous audit is never
deleted automatically.

Next implementation step: bounded redacted result store under `/state/results`.
Do not add a free terminal, pair a device, install WSL automatically, or expand
workcell authority.
