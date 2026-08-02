# Handoff — Hito 4 persistent toolbox services candidate

Hito 4 core merged through PR #128 at
`010c6091358f62b6fada35dbfc33eeaf50c2ae11`, was deployed with 130 tools, and the
stable Front Door retired the prior catalog through deployment
`f5lj9mh5l20zfnh8xjg3jvrg`. OAuth discovery and the unauthenticated MCP challenge are
healthy. Brain note `gpt-web-direct-edge-h4-toolbox-core` records that closure.

Current branch `codex/toolbox-services-repair` is based exactly on that merge. The
candidate adds fail-closed toolbox repair and named background service
start/status/stop. All operations reuse the existing server-owned rootless container,
exact image identity and one verified workspace bind mount. Public responses contain
only toolbox metadata plus opaque service id, closed name, lifecycle state and
timestamps.

Private service identity pairs PID with Linux process start ticks. A fixed supervisor
receives caller argv positionally, never through text interpolation. Status does not
start a stopped toolbox. Stop revalidates identity, sends TERM, waits a bounded interval
and uses KILL only if required. Service argv/environment are not persisted or replayed
after a container or WSL restart; a later explicit start uses a fresh opaque identity.
Repair restarts only a valid stopped/created toolbox and refuses missing, unknown,
unowned or unsafe state.

Candidate identity is 134 tools at
`sha256:504e6f371de9a46a6e255913a019a9990d8977de286fa4f51d90f27fdf06308b`.
Focused, ordinary full-suite, coverage, vet and build gates are green in the correct
non-root Linux/Node 22 environment. Full race was green except one pre-existing
wall-clock detector that exceeded its limit under the concurrent run and then passed
alone under `-race` in 1.681 seconds. The next action is commit, PR, exact-head CI and
dual Front Door transition/retirement deployment. Signed release and real-device
restart acceptance remain blocked only on an exact operator-supplied release version
after installed `p15.0.12`; do not infer it.

PR #129 is open and ready for review. Its initial exact head is
`59b2581a09c5cbf13e2d1cc84b9385e90b17296a`; any documentation follow-up changes that
head and must be verified again before merge.

The documentation follow-up head
`964cc6a3ae3fa0ef4a2524452a9aba18b7197b39` failed only Staticcheck S1016 for a
duplicated struct literal. The typed conversion fix passes the focused toolbox test and
Staticcheck v0.7.0 locally; publish it and require a completely new exact-head run.
