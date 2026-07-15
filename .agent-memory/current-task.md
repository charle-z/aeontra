# Current task

P8.1 Console 2.0 remains deployed and tagged from merge `d343264bffdc0ae1bc045a9d723e913be977090c`. P11 production is deployed on top of that historical base; this branch changes neither production closure.

P11.1 OpenCode external-model bridge is complete locally on branch `p11-1-opencode-model-bridge`, based on deployed P11 production commit `62d433e91373c0882e33be0fd88de4bf7d4f0503`.

Implementation commits:

- `80736ef` — safe MCP client capability probe;
- `2b151f3` — external model-turn transport abstraction;
- `840873e` — durable SQLite rendezvous;
- `2b5341f` — bounded MCP model-runtime controls;
- `91e1f18` — OpenCode 1.18.1 research and exact pinning;
- `fe0c2eb` — Unix-socket external model driver and LanguageModelV3 provider;
- `0887a88` — real OpenCode vertical slice, restart/resume, long-poll optimization and benchmark evidence.

Verified E2E:

- GitHub run `29430972855`, job `87405554810`, temporary head `42db163f20c65fdf8fe4358dfed5788c0f3111c8`: success;
- OpenCode 1.18.1 loaded `file://integrations/opencode/provider`;
- four external model turns;
- real `read`, `grep`, `edit` and `bash` tool executions;
- repository edit and `go test ./...` completed successfully;
- restart preserved exact turn ID, sequence, request digest and request reference;
- container had no default route and no non-loopback AF_INET/AF_INET6 connections;
- no API keys, Codex, fallback provider or local model.

The model-turn controller now uses bounded long polling instead of 20 ms polling. Benchmark A remained 4 MCP calls / 1,039 bytes. Benchmark B fell from 1,100 calls to 9 calls, with 4 model turns, 4 OpenCode tool executions and zero retries. Restart/resume also used 9 calls and exactly one expected retry. The report is `artifacts/opencode-e2e-report.json`, SHA-256 `822b3adf9eafb84ce74f0fd957c142a4079fc76e42594f6d17c4633cde6eca3b`.

Candidate catalog: 77 tools, hash `sha256:3f4e1812bd72a0508eba108d97dfd353ea9abc4c883cded262abd768f1f94518`.

`sampling_supported` is still intentionally undetermined because production has not deployed the capability probe. Pull rendezvous remains the only active transport; no fallback model exists.

Next: commit this release-candidate memory, publish only `p11-1-opencode-model-bridge`, open a PR against `main`, and require all gates on the exact final SHA. Do not merge, deploy, tag, pair Edge, install OpenCode on Parrot, or modify Coolify.
