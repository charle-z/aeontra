# P16 admission and fairness

This document describes the private Step 6 scheduler policy. It does not add public MCP tools or execution authority.

## Resource vectors

Every profile and request uses five integer dimensions:

- `cpu_millis`;
- `memory_mib`;
- `io_weight`;
- `pids`;
- `slots`.

Unknown dimensions, fractional values, negatives, NaN, infinity and overflow fail closed. A pool/profile is administrator-owned. Remote callers cannot increase its budget or per-job maximum.

Admission is conjunctive: a job starts only when every dimension fits both the profile maximum and the remaining pool budget. No score can override these limits.

## Fairness

Eligible jobs are grouped by workspace. Deficit Round Robin adds a fixed quantum per workspace and charges a bounded estimated cost. The cursor advances after each selection so one workspace cannot monopolize a pool.

Jobs older than the configured aging limit are selected by oldest creation time and job ID before ordinary DRR. Equal candidates are ordered deterministically by workspace, creation time and job ID.

## Estimates and shadow scoring

EWMA estimates are isolated by pool, device and profile. They require a configured minimum sample count. Samples are clamped to one quarter through four times the prior estimate before the EWMA update, preventing one outlier from dominating history.

Shadow scoring is observational only. It cannot change target, authorization, requested resources, pool budgets or profile maxima.

## Backpressure and cleanup

Enqueue bounds remain enforced by the Step 5 store. `CleanupTerminal` removes only old terminal jobs, uses a caller-supplied bounded limit, and never removes queued, blocked or leased work. A terminal job referenced as a dependency is retained.

## Metrics and controls

Queue metrics expose only aggregate queued count, workspace count, active/capacity slots, bounded estimated wait and oldest age. They contain no job IDs, workspace names, paths or payload details. Wait estimation is informational and never overrides admission.

Estimate history can be disabled without deleting evidence, re-enabled, reset per key or reset globally. These controls affect estimates and shadow scoring only; they never alter authorization, target, pool budgets or profile maxima.

Step 6 remains a private scheduler-policy component. It adds no public MCP tools or execution authority.
