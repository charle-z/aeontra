# Handoff — HTB lab autonomy

Branch: `htb-lab-authorized-credentials`
Base: `origin/main` at `f501064597b750533010ad706249f9447c07d6f2`.
Implementation HEAD: `b48c45ba3ee7c85c27b93f9ba0798df814ee4b83`.

## Historical deployment snapshots preserved

- P8.1 is closed and deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, tagged `p8.1`.
- The historical P8.1 snapshot had 67 tools and Edge state `not_paired`; those values are evidence for that closure, not the current production state.
- P9 Brain is deployed at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.
- Current production remains on the later P12/P12-hardening foundation with 85 tools.

## Trigger

A real authorized HTB runtime completed recon and found a locally verifiable authentication chain, but the bridge rejected the next target-authentication operation. The result proved that broad provider-side blocking prevented legitimate lab progress even though the target and workspace were already locally authorized.

## Commits

- `58f3c3b6868d5d066fa47aa1a1b2dd8098ad1ba3` — Add target-locked HTB lab autonomy.
  - adds idempotent `mcp-edge lab init`;
  - adds a private Edge-owned Unix-socket broker for target authentication;
  - accepts no caller-selected target and always uses the immutable workspace registration;
  - consumes one local artifact handle without placing its value in argv, environment, model turns, logs, or the control plane;
  - supports local-only output capture with path, size, and SHA-256 returned to the model;
  - uses descriptor-relative O_NOFOLLOW file access and atomic writes;
  - redacts sensitive HTB checkpoint fields before later model turns while preserving the local checkpoint;
  - updates the HTB profile for non-root TCP nmap and brokered access.
- `b48c45ba3ee7c85c27b93f9ba0798df814ee4b83` — Document streamlined HTB lab workflow.

## Validation

- focused Edge, CLI, broker, file-jail, checkpoint and documentation tests passed;
- all Go packages passed in two serial chunks;
- atomic coverage and the coverage gate passed;
- `go vet ./...` passed;
- `go build ./...` passed;
- `git diff --check` passed;
- local Race was unavailable because the private runner has CGO disabled;
- local Staticcheck invocation was denied by the runner command allowlist;
- exact-head CI Race and Staticcheck remain mandatory.

## Boundary

- operator and host authentication material remains globally protected;
- brokered access is limited to the registered workspace target and registered VPN route;
- no password flag, target flag, raw audit output, or VPS transport exists;
- sensitive output may remain only under workspace `loot/`, `reports/`, or `tmp/`;
- host-shared networking remains explicit and is not presented as universal packet filtering.

## Remaining actions

1. commit this handoff;
2. publish the branch and open a PR;
3. require all exact-head CI checks green;
4. merge and deploy only after green CI;
5. update the reviewed Edge binary on Parrot and restart the packaged service;
6. continue the existing authorized lab workspace without repeating recon;
7. design a separate signed remote bootstrap protocol for literal VPN-only operator setup. The current `lab init` reduces setup to one local command but does not remove the pre-runtime workspace-ID dependency.