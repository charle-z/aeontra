# Direct Edge general browser harness baseline — 2026-08-03

This dated baseline records the Hito 2 candidate developed and validated on
`parrot-trusted-linux` from accepted base
`954ee978dd791360780fa5d4d07fd4ffb7f3d6c9`. It is historical evidence, not live
deployment state.

## Candidate public identity

- Protocol: `2024-11-05`.
- Tool count: `151`.
- Catalog: `sha256:d02bc196de5829ec8ea529a4f8cd1b684e94af72305600b74f46413bd2a13f13`.
- General harness tools:
  - `project_browser_harness_start`;
  - `project_browser_harness_status`;
  - `project_browser_harness_list`;
  - `project_browser_harness_stop`;
  - `project_browser_harness_cleanup`;
  - `project_browser_harness_artifact_list`;
  - `project_browser_harness_artifact_read`.
- Convenience Chromium tools retained:
  - `project_browser_create`, `project_browser_status`, `project_browser_list`;
  - `project_browser_run`, `project_browser_artifact_read`;
  - `project_browser_close`, `project_browser_cleanup`.

## Programming model

The general harness accepts arbitrary argv inside the persistent rootless project toolbox.
It is not an action DSL and it does not require a separate owner mode. A project may use
Playwright, Puppeteer, Selenium, WebDriver, browser CLIs or any custom automation in any
installed language/framework. JavaScript and normal browser APIs are available through
that code. Browser engines, drivers, libraries and utilities are installed in the
persistent toolbox rootfs by the existing installation/package-manager surfaces.

Every run receives stable managed locations for its run tree, arbitrary artifacts,
downloads and a named persistent profile. Workspace files are available for uploads.
Profiles retain cookies, local/session storage and authentication across browser and Edge
process restarts. Chromium is not mandatory for the harness; alternate engines are
installable when project tooling supports them.

The existing seven Chromium tools are convenience only. They use the workcell's general
HTTP/HTTPS network and allow managed downloads, but arbitrary code, uploads, traces,
videos, alternate browsers and custom workflows use the harness.

## Network and security boundary

The toolbox/workcell is the boundary. Browser code may use ordinary HTTP/HTTPS Internet,
private development endpoints and localhost services in the toolbox. MCP Devbox does not
add a browser-specific domain, port, action, JavaScript, upload, download or engine
allowlist.

The harness does not add Windows mounts, the general host home, rootful Docker sockets,
Edge identity/state or a public debugging endpoint. It reuses only the authorities already
assigned to the authorized toolbox, including its validated user-owned rootless engine.
Caller argv is positional to a fixed supervisor. Public results exclude argv, environment,
PID, container identity, cookies, profile contents and host paths.

Managed directories reject traversal, symlinks, foreign ownership and root escape. Logs
are separate, bounded and redacted. Artifact/download enumeration is relative and bounded;
reads use exact base64 chunks. `.mcp-devbox/` is repository-ignored.

## Durability and limits

Toolbox CPU, memory and process limits apply to the harness. Each run also selects a
wall-clock timeout and a combined run/profile storage ceiling. The private supervisor
records state and Linux PID/start ticks, monitors timeout/storage, stops only its owned
process tree and persists terminal metadata. Reopening the Edge manager revalidates the
same live process rather than replaying argv. Lost process identity becomes
`indeterminate`. Cleanup is explicit and terminal-only; shared profiles remain while any
retained run uses them.

## Real Edge evidence

A persistent toolbox was created on the Edge with 4 CPU, 4096 MiB and 2048-process limits.
Inside that rootfs the acceptance installed Python, a virtual environment, Playwright
`1.61.0`, browser dependencies and Playwright Chromium. The same mechanism reported the
install plan for Firefox, demonstrating that alternate engines are not blocked.

An arbitrary Python Playwright script then passed all of these real checks:

- public navigation to `https://example.com`;
- project localhost application on `127.0.0.1:18765`;
- arbitrary `page.evaluate` JavaScript;
- workspace file upload and server-side confirmation;
- browser download saved into the managed download tree;
- persistent login cookie across Chromium close/reopen;
- screenshots, PDF, Playwright trace and WebM video;
- no `/mnt/c` and no rootful `/var/run/docker.sock`;
- durable long-running toolbox process start and idempotent stop.

Produced evidence included `localhost.png`, `localhost.pdf`, `public.png`, `trace.zip`, a
WebM video, `downloads/sample.txt` and `resumed.png`. Cookie/profile content remained local
and was not read through MCP.

## Automated acceptance

Unit/integration tests cover arbitrary argv, no action/selector schema, request/result
validation, persistent manager reopen, idempotency, indeterminate state, configurable
limits, incremental logs, total artifact counts, arbitrary artifact reads, traversal and
symlink rejection, shared-profile cleanup, safe result mapping, convenience general
network and real Chromium behavior.

GitHub Actions keeps the deterministic rootless Podman/PostgreSQL lifecycle and a real
Chromium convenience-process smoke. It does not run the complete toolbox harness on the
hosted runner: the job executes below a root-owned `/system.slice` unit whose
`cgroup.procs` is not writable by the job user. A separate user-manager subtree, when
present, does not transfer that authority to the job cgroup. The workflow records that
exact posture as `not-reproducible`, bound to the checked-out commit and tree; a different
posture or failure remains blocking.

The authoritative runtime acceptance was completed on
`c27053c56b6214e52862ead675b874670f322295`: the exact
`TestProjectBrowserHarnessRealPlaywrightE2E` passed on `parrot-trusted-linux` in 295.87
seconds. That evidence may be carried forward only while the candidate changes no Edge,
server, toolbox or browser-harness runtime code. The candidate diff from that accepted
commit is restricted to this workflow, documentation and its workflow-policy contract,
and a fresh real-browser smoke on the Edge reconfirmed Internet, localhost, JavaScript,
upload/download, persistent authentication, screenshots, PDF, trace and video. Any
runtime change invalidates this evidence and requires a new exact E2E before publication
or merge.

## Rollout requirement

This candidate is the first productive catalog change after the stable Front Door work.
Acceptance requires previous+candidate catalog overlap, candidate backend deployment,
OAuth discovery and authenticated MCP verification through the public Front Door, one
signed Edge release/update, real harness invocation from GPT Web, retirement of the
previous catalog and evidence of no HTTP 503 during the transition.
