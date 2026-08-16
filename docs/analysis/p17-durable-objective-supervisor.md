# P17 durable objective supervisor

Status: **Planned. Not implemented.**

P16 owns durable task groups, exact-base worktrees, leases, fences and independent model
runtimes. P17 is the optional coordination layer above those primitives. Its purpose is
to drive a bounded objective to evidence-backed acceptance without confusing an ended
model runtime with completed work. It must reuse the existing workqueue, model-turn,
Edge, Git, GitHub, CI, deployment and Brain contracts rather than introduce a parallel
execution plane.

## Invariants and non-goals

- The supervisor grants no filesystem, network, model, GitHub or deployment authority.
  Every effect still uses the existing scoped tool and preview/execute contracts.
- A worker runtime ending is only a lifecycle fact. It never implies semantic success.
- One writer owns one exact-base worktree at a time. Stale fences and cross-worktree
  writes fail closed.
- Source content, prompts, credentials and model reasoning are not copied into scheduler
  state or metrics.
- GPT Web remains the primary reasoning provider, but P17 cannot treat subscription
  access as an API, bypass product limits or promise unlimited/free inference.
- P17 will not click ChatGPT by screen coordinates, scrape its UI, enter credentials or
  automate 2FA. Until an official handoff interface exists, continuity uses an exact
  checkpoint and a generated continuation prompt for an operator-opened chat.
- Built-in Codex multiagent remains subordinate to MCP Devbox ownership and is not a
  second scheduler. P17 does not enable multiple writers in one checkout.
- Automatic conflict guessing, gate weakening, force-push and merge without exact-head
  green evidence are out of scope.

## State model

An objective owns task identities and a task-specific acceptance contract. Its public
state is one of:

```text
planned -> running -> acceptance_pending -> accepted
                    \-> reconciliation_required
running/planned ----> cancelled
any nonterminal ----> failed
```

Worker views preserve three independent facts:

1. lifecycle state from the private workqueue;
2. model runtime state from the durable model-turn store;
3. acceptance state derived from the explicit objective contract and verified evidence.

Only the objective evaluator may move `acceptance_pending` to `accepted`. Evidence that
is absent, stale, contradictory or outside its freshness/identity bounds produces
`reconciliation_required`, not success and not an automatic replay.

## Planned capabilities

### 1. Durable objective supervision

Persist an objective definition, bounded dependency graph, task identities, acceptance
contract version and current checkpoint. The supervisor leases ready tasks, observes
terminal/runtime changes, schedules evaluation and resumes after process restart. It
must recover existing operations by their durable identities and never create a second
effect merely because an acknowledgement was lost.

Acceptance cases:

- restart between task creation and worker binding reuses the same task/worktree;
- restart between worker completion and evaluation resumes evaluation only;
- cancellation stops new scheduling and preserves already produced evidence;
- an expired or stale supervisor lease cannot complete or integrate work.

### 2. Efficient model-turn attention

Replace serial fixed polling across workers with one bounded fair attention loop over
pending model turns. Prefer event/wakeup primitives already owned by the model-turn
store; if polling remains necessary, use bounded backoff, a global wait budget and fair
round-robin selection. A noisy worker cannot starve another.

Acceptance cases:

- multiple pending turns are served without duplicate responses;
- no pending turn causes near-zero idle CPU and bounded queries;
- a disconnected provider does not block unrelated workers;
- late responses for expired sequences fail closed;
- restart resumes from the last consumed sequence without replay.

### 3. Task-specific semantic verification

Define versioned evaluator types rather than a universal model-judged boolean. Initial
evaluators may combine exact Git evidence, declared test commands, required/forbidden
paths, clean-tree state and exact output schemas. Evaluators receive bounded evidence,
not arbitrary host access. A model review may advise but cannot be the sole blocking
proof for deterministic claims.

Acceptance cases:

- runtime completed with no commit remains `acceptance_pending` or becomes failed under
  an explicit commit-required contract;
- clean exact-base commit with the required path and green test can be accepted;
- unexpected paths, dirty tree, stale base or missing test evidence fail closed;
- evaluator crash/redeploy resumes without rerunning a consequential test or write;
- contracts unknown to the deployed evaluator are rejected.

### 4. Review, CI and integration

Use normal Git review and owner-bound publication. The supervisor may prepare a review
candidate, run deterministic local gates, publish through the existing single-use plan,
open a PR, wait for exact-head checks and request a merge plan. It must not resolve
source conflicts automatically unless a later explicit conflict policy authorizes a
new isolated worker and requires the same review gates.

Acceptance cases:

- independent non-conflicting commits integrate in a deterministic reviewed order;
- overlapping commits stop at a bounded conflict report;
- a new PR head invalidates previous CI and merge evidence;
- failed/pending checks block merge; cancelled runs are not green;
- lost publication/merge acknowledgement is reconciled by exact branch/PR/SHA identity;
- no force push, hidden refspec or duplicate PR/deployment is possible.

### 5. Compact continuation handoff

Before a provider/session budget is exhausted, write a bounded checkpoint containing
objective/task IDs, current states, exact source identities, pending approvals, last
consumed sequences, evidence references, blockers and the next safe action. Generate a
self-contained continuation prompt that instructs a new chat to read and reconcile the
checkpoint before acting.

The handoff must contain no credential, prompt transcript, chain of thought, absolute
private path or raw source diff. Producing a handoff never cancels active durable work.
An operator opens and authenticates the next chat. If an official ChatGPT transfer API
appears later, it may consume this same contract without changing task semantics.

Acceptance cases:

- handoff near a time/turn budget preserves every nonterminal identity;
- two chats reading the same checkpoint cannot both acquire the same supervisor lease;
- a stale continuation prompt is rejected after objective revision;
- a handoff created during an external write records indeterminate/reconcile-first state
  rather than telling the next chat to repeat it.

### 6. Optional worker model backends

Keep GPT Web as the default external model-turn participant. Add administrator-selected
provider profiles only after their transports are explicit: local models, or API-backed
Sol/Terra/Luna equivalents when actually available to the deployment. Each worker binds
one provider profile at creation; repositories and model output cannot select or enlarge
it. Credentials remain in the provider-owned broker and never enter worker prompts,
worktrees or task status.

Acceptance cases:

- provider outage can be retried or reassigned only under an explicit policy and fresh
  runtime identity;
- reassignment cannot replay an already acknowledged consequential tool call;
- mixed providers preserve the same worktree/fence/acceptance rules;
- cost-bearing API providers require an administrator budget and expose usage metadata
  without secrets;
- a local model cannot obtain broader authority than GPT Web.

### 7. Efficiency and waste metrics

Record content-free timestamps and counters for queue wait, pickup, model wait, tool
execution, evaluation, CI wait, retries, reconciliations, cancellations and handoffs.
Track work discarded by stale heads, failed gates, conflicts and duplicate/late model
responses. Metrics use opaque task/provider classes and bounded reason codes; they do
not contain prompts, source, command output, domains, paths or credentials.

Acceptance cases:

- total duration reconciles with bounded phase durations;
- retry and wasted-work counters are monotonic and restart-safe;
- unknown measurements remain explicitly unknown, never zero;
- cancelled and failed work is distinguished from accepted useful work;
- metrics cannot be used to infer secret values or raw model content.

## Delivery sequence

1. Freeze the P16 semantic-status contract and accept it on the real Edge.
2. Specify the objective/evaluator schema and migration with no new execution authority.
3. Implement supervisor persistence and restart/idempotency tests using deterministic
   non-model workers first.
4. Add fair multi-turn attention and duplicate/late-sequence tests.
5. Add one deterministic Git/test evaluator and prove `acceptance_pending -> accepted`.
6. Add PR/CI integration behind existing exact preview/execute gates.
7. Add compact handoff/checkpoint generation and a two-chat manual acceptance.
8. Add content-free metrics and benchmark against the P16 baseline.
9. Evaluate optional provider profiles one at a time. Browser/UI automation remains a
   separate later decision and is not a P17 acceptance dependency.

## Release gates

P17 is not complete until exact-head CI, restart/replay/fencing tests and one real-device
project demonstrate: at least two independent workers, fair turn handling, deterministic
semantic acceptance, reviewed integration, exact-head CI, one merge/deployment without
duplication, a compact continuation across chats, and auditable metrics. Source merge,
backend deployment, signed Edge release and installed-device acceptance are reported as
separate facts.
