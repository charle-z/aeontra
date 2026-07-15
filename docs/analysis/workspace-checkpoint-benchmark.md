# Workspace checkpoint benchmark

Date: 2026-07-14

`BenchmarkWorkspaceStateReconstruction` is a reproducible local benchmark in
`internal/tools/workspace_checkpoint_benchmark_test.go`. It creates one Git repository
with 80 source files and compares the prior state reconstruction
(`build_context_pack` plus `repo_status`) with one `workspace_checkpoint` call.

Command:

```text
go test ./internal/tools -run '^$' -bench BenchmarkWorkspaceStateReconstruction -benchtime=5x -count=1
```

Measured on Linux/amd64 in the official `golang:1.26.5` container:

| Flow | MCP calls/op | Response bytes/op | Repeated bytes/op | Duration/op |
|---|---:|---:|---:|---:|
| Previous reconstruction | 2 | 2052 | 260 | 18.63 ms |
| `workspace_checkpoint` | 1 | 406 | 0 | 16.66 ms |

For this fixture the measured reduction is 50% in MCP calls, about 80.2% in response
bytes, 100% in repeated bytes and about 10.6% in tool duration. These values are not
universal performance claims; CI/host/filesystem conditions and repository size can
change them. Re-run the command when the implementation or fixture changes.

The benchmark measures server-visible tool duration. It does not label inter-call
gaps as model latency and does not estimate OpenAI billing or model tokens.
