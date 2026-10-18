# Skills registry

Skills are optional reviewed playbooks used to accelerate a bounded task. They are
never trusted as runtime policy, never executed blindly, and never override repository
instructions, tests, approvals, or security invariants.

## Review requirements

Before a skill is used for implementation, record:

- exact source and owner;
- version, tag, or commit;
- purpose and affected subsystem;
- files/commands it proposes;
- dependencies it introduces;
- network, filesystem, and secret access assumptions;
- conflicts with `AGENTS.md`, `SECURITY.md`, or the threat model;
- review decision and reviewer/date.

A skill that cannot be pinned or reviewed may be used only as non-authoritative
reference material. Generated code receives the same tests, review, SAST, and CI gates
as handwritten code.

## Current registry

| Skill | Source/version | Purpose | Status |
|---|---|---|---|
| None | N/A | P0 catalog/cache work uses repository-native design and Go tests. | Not required |

## Entry template

```text
Skill:
Source:
Version/commit:
Purpose:
Subsystem:
Proposed commands/files:
Dependencies:
Security assumptions:
Review result:
Reviewed by/date:
```
