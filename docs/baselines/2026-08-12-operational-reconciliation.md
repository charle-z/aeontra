# Operational reconciliation — 2026-08-12

This dated baseline reconciles repository, CI, production and installed Edge facts
before the Codex harness and multiagent roadmap begins. It is evidence, not a source of
mutable configuration or a substitute for live `/version` and `mcp-edge doctor` checks.

## Source and GitHub

- `origin/main`: `f8d0a38af06527dcf59763c793bee81aca9dd044`.
- The exact main head completed the CI, Catalog Identity, Security Evidence, P15 Edge,
  P16 builder, Trusted Linux Workcell and distributed OpenCode workflows successfully.
- GitHub reported no open pull request and no open issue.
- PR #153 is closed without merge. PR #154 is the clean replacement and merged the
  general Direct Edge browser harness at
  `7d8ead017a2a9ec381432177feb496a819e1365d`.
- Remote branch inventory after fetch contained 193 refs: 169 merged into main and 24
  not merged. These counts justify a later audit; they do not authorize blind deletion.

## Production

Public `/version` returned:

```text
status=ok
commit=f8d0a38af06527dcf59763c793bee81aca9dd044
protocol=2024-11-05
tool_count=166
catalog=sha256:088a0bacfb5a631bb8b4fd45185ec3f997648992ab9810f7a401d0d4d8eeefe5
```

`/front-door/healthz` returned `ok` for Front Door commit
`489a64f40cbbde014986ff130662a485f9513d6c`.

## Installed Parrot Edge

The read-only doctor check returned:

```text
status=ready
bundle=valid
layout=valid
identity=valid
alias=parrot-trusted-linux
service=active
process=single
lock=held
coherence=managed
release=p15.0.34
commit=04c544b776ffca2071cb5b5a9951b8b32f423a36
rootless=podman
journal=empty
```

The release commit is 40 commits after the browser-harness merge, so the installed
release contains PR #154. The host PATH had Node `v24.18.0`; no host-level `codex`
executable was installed. This does not make a claim about private bundle components.

## Deliberate non-actions and limitations

- No restart or Edge update was performed.
- No process, toolbox, browser profile, branch, image, bundle or legacy unit was
  removed.
- The operator reported active mathematical-research work on the VPS and active public
  OSS work through the Edge. A release remains blocked until those owners persist a
  checkpoint and confirm that a coordinated restart is safe.
- SSH authentication to the VPS was unavailable from this Codex session. No claim is
  made about a VPS Codex installation or its local process inventory.
- The current Windows worktree was clean but 62 commits behind main before a new clean
  branch was created. No reset, clean or history rewrite was used.

## Next gates

1. Merge the documentation reconciliation through exact-head CI.
2. Audit active operations and obsolete remote refs without deleting unowned state.
3. Publish and install the next stabilization release only after active-work checkpoint.
4. Run the Codex CLI/App Server compatibility spike in a disposable Edge toolbox.
5. Implement worktree ownership before enabling writing multiagent execution.
