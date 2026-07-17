# Security findings closure plan

Date: 2026-07-16
Branch: security-findings-closure
Base: b9ee5ea9fd18a72d9687784eeb5cbfd8603427b5

1. [done] Reconstruct main and create the dedicated branch.
2. [done] Recover historical CodeQL evidence; record the code-scanning API HTTP 403 limitation without guessing alert numbers.
3. [done] Bind validation-runner mounts to an immutable server-owned registry and stable filesystem identities.
4. [done] Enforce one production cookie policy with Secure, HttpOnly, SameSite Strict and Path slash for creation and deletion.
5. [done] Prove the GitHub-token regex is a text search, add adversarial/performance tests and a rule-specific in-source CodeQL suppression.
6. [done] Create the dated security report and preserve the 78-tool catalog identity.
7. [done] Run formatting, full serial tests, coverage thresholds, vet, build, Staticcheck, Govulncheck, Actionlint and focused security tests.
8. [blocked locally] Race requires gcc and no-cache Docker builds require Docker; both are unavailable on this VPS and remain mandatory remote gates.
9. [active] Commit the closure tree, publish security-findings-closure and open a PR against current main.
10. [pending] Require Verify, Race detector, Staticcheck, Govulncheck, CodeQL, Dependency review and Container SBOM/vulnerabilities green on the exact final SHA, then inspect original alert development status as far as GitHub permissions allow.

No merge, deployment, tag, production, Coolify, Parrot, P11.2, frontend, protocol, catalog, Brain, Build Workcell or Goal Runtime changes are permitted.
