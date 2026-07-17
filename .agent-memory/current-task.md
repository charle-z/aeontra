# Current task

Date: 2026-07-16
Branch: security-findings-closure
Base: origin/main at b9ee5ea9fd18a72d9687784eeb5cbfd8603427b5

P8.1 is deployed at d343264bffdc0ae1bc045a9d723e913be977090c. P9 Brain is deployed and P11.2 is the deployed successor. This task does not change those milestones, production, Parrot, Edge, OpenCode, Bubblewrap, frontend, protocol, catalog, Brain or telemetry.

Objective: close the historical CodeQL findings for validation-runner paths, console cookies and the GitHub-token detector on branch security-findings-closure without merge or deployment.

Completed commits:

- 0e5b6768fbdfdfbdc447cbec2435a59f745b7cbf — Step 1: bind validation mounts to server-owned registry.
- 414ef78f09fe061e93f144b92cf21e3fa4460aa0 — Step 2: enforce secure production cookies.
- bbc4316ec79f545d18993792609be22e9e76c978 — Step 3: document secret-scanner regex semantics.

Current work: pin manifest filesystem identity, update console smoke expectations, create the dated security report, execute all gates, publish the branch and open a PR against main.

The available GitHub token receives HTTP 403 from the code-scanning alerts REST endpoint. Historical check annotations recovered the two cookie findings exactly. Path and regex findings are reconstructed from their CodeQL rule IDs, production locations and Git history. The PR CodeQL check is the authoritative exact-SHA validation.
