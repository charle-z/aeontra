# Authorized security engagement design

This document defines how MCP Devbox may support legitimate security research
without turning a model, provider, repository, or public console into a source of
network authority.

Program-specific rules, assets, credentials, evidence, and reports are private
operator data. They do not belong in this public repository.

## Authority model

An engagement is administrator-owned state stored outside agent-writable repository
roots, under a location such as `/state/engagements/<id>/engagement.yaml`.

Engagements are blocked by default. Network execution requires all of the following:

- an exact current asset match;
- a live program-policy review timestamp;
- an activation and expiration set by a human administrator;
- a compatible installed execution profile;
- destination, protocol, port, rate, concurrency, and time limits;
- a single-use MCP action plan and any required human approval;
- independent revalidation by the selected private edge.

A program name, repository file, model response, credential, wildcard inferred by a
model, or previously observed hostname never grants authority.

## Data separation

| Data | Storage | Agent visibility |
|---|---|---|
| Generic engagement schema and documentation | Public MCP Devbox repository | Readable |
| Program rules and exact asset scope | Private operator state | Bounded summaries only |
| Credentials, cookies, tokens, and browser sessions | Local secret store/browser | Never returned |
| Hypotheses and sanitized notes | Private engagement workspace | Engagement-scoped |
| Raw evidence | Encrypted/private evidence store | Minimum necessary, redacted |
| Final report draft | Private engagement workspace | Human-reviewed |
| Public disclosure | External publication | Only after formal approval |

The public MCP Devbox Console may demonstrate a synthetic or recorded sanitized
engagement. It must not expose live targets, private program membership, rules,
credentials, evidence, or report content.

## Initial safe workflow

The first security release should support planning and evidence discipline before
it supports active testing:

```text
engagement_list
-> engagement_status
-> engagement_scope_check
-> security_task_preview
-> security_task_execute
-> evidence_note_preview
-> evidence_note_write
-> report_draft
```

`engagement_scope_check` is read-only. It returns only whether a normalized target
and requested task family are authorized, plus a bounded reason. It does not return
the complete private scope.

`security_task_execute` accepts a plan id, not a shell command or arbitrary URL. The
selected profile owns fixed argv and network behavior. A Parrot/PC edge performs the
same scope check again before any packet leaves the device.

## MVP task families

Start with narrow, low-volume families:

1. Local/source artifact analysis with no network.
2. Manual hypothesis and reproduction planning.
3. Bounded HTTP metadata collection for one exact authorized asset.
4. Two-program-account authorization testing with explicit human interaction.
5. Evidence redaction, deduplication, timeline, screenshot indexing, and report
   drafting.

Mass scanning, brute force, DoS/load testing, spam, social engineering, malware,
credential attacks, persistence, and destructive actions are not general MCP Devbox
profiles. Any exceptional testing category requires explicit program authorization
and a separately reviewed design.

## Credential handling

Credentials must never be copied into prompts, repositories, MCP plans, audit logs,
notes, or model memory. Authentication should happen through an operator-controlled
browser session or a local edge credential broker that returns only opaque session
handles. Handles are device-bound, short-lived, engagement-bound, and revocable.

Cross-account authorization testing should use only accounts owned by the researcher
or issued by the active program. Real personal/customer/employee accounts are not
valid test targets.

## Stop conditions

The edge stops immediately when:

- DNS, redirects, or a request would leave exact scope;
- authorization expired or policy/scope changed;
- unexpected personal, customer, employee, or third-party data appears;
- service errors or latency suggest instability;
- the profile exceeds its request/resource budget;
- the task requires a prohibited technique;
- the expected safe test state cannot be restored.

## Implementation sequence

1. Define and test the engagement schema and administrator-only loader.
2. Implement exact scope normalization/matching with deny-by-default adversarial
   tests, including redirects and DNS changes.
3. Add engagement-aware plan claims and audit fields.
4. Implement the outbound-only edge identity/revocation protocol.
5. Add a no-network local-analysis profile.
6. Add one exact-target bounded HTTP profile with enforced egress and stop controls.
7. Add the opaque local session broker for program-issued test users.
8. Add sanitized evidence/report tooling.
9. Run the full workflow in a deliberately owned local lab before any third-party
   program engagement.

The implementation is not complete until egress enforcement exists at the network
boundary. Prompt instructions and argument validation alone are insufficient.

