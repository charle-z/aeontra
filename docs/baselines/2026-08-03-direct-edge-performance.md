# Direct Edge performance baseline — 2026-08-03

Status: **candidate implementation validated locally; production rollout and signed Edge acceptance remain pending.**

This baseline measures the direct path used by GPT Web. It does not start OpenCode,
consume model turns, or estimate model latency:

```text
GPT Web -> MCP Front Door -> backend MCP -> durable operation queue/lease
        -> parrot-trusted-linux -> registered project operation -> response
```

The machine-readable evidence is:

- `artifacts/benchmarks/direct-edge-performance-2026-08-03-baseline.json`;
- `artifacts/benchmarks/direct-edge-performance-2026-08-03-https-ingress.json`.

Recompute either file with:

```bash
python3 scripts/analyze-direct-edge-benchmark.py EVIDENCE.json
```

## Verified starting identity

- Edge alias: `parrot-trusted-linux`.
- Edge release: `p15.0.24`.
- Edge commit: `2286ea80c497c6e1dd9fed15f5e80988236a64e1`.
- Backend commit: `eaf3e6b79bcad171173eaa23d625e620d1bceb6d`.
- Protocol: `2024-11-05`.
- Tool count: `137`.
- Catalog: `sha256:3a1bab74e895c03e734c43748dbf48e16a909d9697fbfe39bc76fe4a23ca1669`.
- Project checkout: attached `main`, exact local/remote HEAD, ahead `0`, behind `0`.
- Managed health: active service, one process, held lock and managed coherence.

The pre-change public diagnostic did not expose `NRestarts`; the candidate adds a
bounded systemd `NRestarts` value with an explicit `known` bit. Until the signed Edge
candidate is installed, the exact p15.0.24 counter must not be invented.

## Baseline method

Twenty fresh `project_exec` operations ran the same near-zero command and recorded an
Edge-local begin/end timestamp. The backend journal supplied operation creation and
terminal timestamps. The old contract did not persist lease, running or finalizing
timestamps, so `pickup_preflight_us` combines queue wait, lease delivery, project
resolution and workcell preparation. This limitation is explicit rather than assigning
that time to a guessed stage.

| Metric | p50 | p90 | p95 | maximum |
|---|---:|---:|---:|---:|
| combined pickup + preflight | 1,882.543 ms | 2,382.340 ms | 2,443.391 ms | 2,579.490 ms |
| real command execution | 0.006 ms | 0.007 ms | 0.007 ms | 0.008 ms |
| completion and return | 215.616 ms | 237.916 ms | 239.404 ms | 253.957 ms |
| total durable operation | 2,095.310 ms | 2,603.361 ms | 2,656.449 ms | 2,743.135 ms |

All twenty samples succeeded, for an error rate of `0.0`.

The first project execution after session preflight completed in `2,728.420 ms`. It is
a cold-reference sample, not a forced service-restart claim.

## HTTPS ingress probe

A separate twenty-sample probe called public `/version` through Front Door from the
Edge without credentials. This covers DNS, TCP, TLS, Front Door, backend handling and
response transfer. It is not relabeled as the private network path from GPT Web.

| HTTPS metric | p50 | p90 | p95 | maximum |
|---|---:|---:|---:|---:|
| DNS | 23.789 ms | 43.027 ms | 84.904 ms | 350.500 ms |
| TCP connect | 127.450 ms | 147.269 ms | 190.415 ms | 448.809 ms |
| TLS complete | 249.888 ms | 270.855 ms | 312.385 ms | 572.939 ms |
| time to first byte | 357.651 ms | 381.136 ms | 424.051 ms | 681.936 ms |
| total | 357.795 ms | 381.368 ms | 424.207 ms | 682.075 ms |

All twenty requests returned HTTP 200. The server cannot measure one-way GPT Web
network transit; the controlled MCP samples begin when the backend persists the durable
operation. Keeping those scopes separate prevents fake precision.

## Bottleneck evidence

The command itself is not the bottleneck. Its p95 is approximately seven microseconds.
Two fixed control-plane behaviors dominate the observed total:

1. after an empty operation lease response, the Edge waits two seconds before polling
   again;
2. the backend terminal waiter checks status every 200 milliseconds.

Those intervals explain the broad pickup distribution and the approximately 200 ms
completion tail. Both measured objectives already pass, so this change does not alter
polling, timeouts, CPU, memory or service limits merely to make a chart look faster.

## Measured portability corrections

The first full Edge suite exposed three host-specific defects in the validation path:

- the direct sandbox mounted a private `/tmp` but overrode `TMPDIR` with a long
  workspace path, which exceeded the Unix socket limit and left temporary state in the
  checkout; the candidate now uses the existing private tmpfs;
- several permission tests relied on the process umask to materialize insecure modes;
  they now apply explicit `chmod` before asserting fail-closed behavior;
- the minimal Edge profile provides `mawk` without an `awk` alias; the closed calibration
  reviewer now selects only `awk` or `mawk` and fails if both are absent.

These corrections add no host package, timeout, resource, model or authority. The final
complete suite passed with the candidate temporary-directory contract.

## Candidate observability

The candidate persists only non-sensitive timestamps in the private operation journal:

- queued creation;
- current lease pickup;
- signed `running` progress;
- signed `finalizing` progress;
- terminal completion.

`edge_operation_status` derives bounded microsecond durations named `queue_us`,
`pickup_us`, `edge_work_us`, `completion_us` and `total_us`. Absolute internal lease
and phase timestamps are not returned. Requeued attempts clear stale current-attempt
timing.

For `project_exec`, the signed Edge also reports three local monotonic durations:

- `preflight_us`: project resolution plus workcell/Bubblewrap preparation;
- `execution_us`: the actual child process interval;
- `result_us`: local capture, redaction and bounding.

Together with `completion_us`, these distinguish local result processing from transfer
and terminal persistence without subtracting clocks from different machines.

## Representative surface evidence

The run also exercised:

- project status and five clean snapshots;
- five small reads and five bounded searches;
- five Git status operations;
- a temporary rootless toolbox with five stable status reads and exclusive cleanup;
- three overlapping durable processes;
- bounded large output using offsets `0 -> 24576 -> 25600 -> 26624`, with no repeated
  page;
- repeated stop and exclusive cleanup for every benchmark process.

The host-idle probe observed eight CPUs, load averages `0.00/0.36/0.64`, and 100 percent
idle over its bounded one-second sample. This is a point observation, not a capacity
claim.

## Initial acceptance result

- hot pickup target p50 at most 3 seconds: **passes using the stricter combined
  pickup+preflight value**;
- pickup p95 at most 10 seconds: **passes using the stricter combined value**;
- simple operation end-to-end p95 at most 15 seconds: **passes**;
- duplicate operations or paginated results: **not observed**;
- destructive load: **not generated**;
- deployment overlap: **none**.

## Candidate verification

The final Edge source tree passed this exact matrix:

- `env TMPDIR=/tmp go test ./... -count=1` — green in 38.930 seconds;
- `env TMPDIR=/tmp go vet ./...` — green;
- `env TMPDIR=/tmp go build ./...` — green;
- `env TMPDIR=/tmp go test ./docs -count=1` — green;
- `git diff --check` — green;
- catalog identity verification — unchanged 137 tools and the existing catalog hash;
- shell syntax for the calibration reviewer — green;
- both benchmark analyzers — green with zero recorded errors.

The explicit `TMPDIR` wrapper reproduces the candidate bundle while the installed Edge
remains on p15.0.24. After installation the direct workcell exports that value itself.

## Remaining gate

This document is not final acceptance yet. The exact candidate must pass the full suite,
exact-head CI, merge and catalog-aware backend rollout. Because the Edge binary changes,
a signed Edge release is also required. Release publication must remain pending until
the backend GitHub authority proves Actions Write through a safe
`source_workflow_dispatch_preview`; no local workflow-dispatch fallback is accepted.
After installation, the same twenty-sample command and representative matrix must be
repeated using the new authoritative stage fields.
