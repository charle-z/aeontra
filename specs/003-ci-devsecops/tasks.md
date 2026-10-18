# Tasks — P6 CI/DevSecOps

Status: **complete**.

- [x] **T01 P6 foundation** — P5 deployment recorded, P6 defined, documentation synchronized, and the 62-tool runtime contract preserved.
- [x] **T02 workflow policy** — tested YAML guard rejects dangerous triggers, broad permissions, PR secrets/production actions, mutable refs/tools, missing timeouts, and malformed workflows.
- [x] **T03 core CI** — blocking verify, atomic coverage/package gate, vet, build, CGO race, pinned staticcheck, and pinned govulncheck jobs defined and policy-validated.
- [x] **T04 security workflows** — least-privilege CodeQL, PR dependency review, local Docker build, SBOM generation, and blocking high-severity image scan defined and policy-validated.
- [x] **T05 scheduled fuzz** — weekly/manual matrix covers all seven known fuzz targets with 30-second budgets, two CPUs, no secrets, and blocking failures.
- [x] **T06 observed GitHub run** — Step 90 exposed exact Staticcheck, Govulncheck,
  and five High container findings; Step 91 remediated them. PR runs `29272847130`
  and `29272847139`, plus post-merge runs `29273109759` and `29273109780`, are green.
- [x] **T07 P6 closure** — baseline, documentation synchronization, audit, full gates,
  fast-forward publication, production identity, and unchanged catalog are recorded in
  `docs/baselines/2026-07-13-p6.md`.

## Boundary

P6 automates evidence only. It cannot deploy from pull requests, access production
secrets, mutate repository policy, or change the MCP runtime/catalog contract.
