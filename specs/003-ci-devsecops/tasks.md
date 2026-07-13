# Tasks — P6 CI/DevSecOps

Status: **active**.

- [x] **T01 P6 foundation** — P5 deployment recorded, P6 defined, documentation synchronized, and the 62-tool runtime contract preserved.
- [x] **T02 workflow policy** — tested YAML guard rejects dangerous triggers, broad permissions, PR secrets/production actions, mutable refs/tools, missing timeouts, and malformed workflows.
- [x] **T03 core CI** — blocking verify, atomic coverage/package gate, vet, build, CGO race, pinned staticcheck, and pinned govulncheck jobs defined and policy-validated.
- [x] **T04 security workflows** — least-privilege CodeQL, PR dependency review, local Docker build, SBOM generation, and blocking high-severity image scan defined and policy-validated.
- [ ] **T05 scheduled fuzz** — explicit bounded execution for every P5 fuzz target.
- [ ] **T06 observed GitHub run** — publish branch, inspect all workflow conclusions, and fix reproducible failures.
- [ ] **T07 P6 closure** — baseline, documentation synchronization, audit, full gates, and release/deployment posture.

## Boundary

P6 automates evidence only. It cannot deploy from pull requests, access production
secrets, mutate repository policy, or change the MCP runtime/catalog contract.
