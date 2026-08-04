# Direct Edge managed browser baseline — 2026-08-03

This dated baseline records the Hito 2 candidate developed and validated on
`parrot-trusted-linux` from accepted base `954ee978dd791360780fa5d4d07fd4ffb7f3d6c9`.
It is historical evidence, not live deployment state.

## Candidate public identity

- Protocol: `2024-11-05`.
- Tool count: `144`.
- Catalog: `sha256:a8da24675ce4f365b9ac9e5809087d225ddba8f86417fbed628d6bcde5ffa005`.
- Added tools: `project_browser_create`, `project_browser_status`,
  `project_browser_list`, `project_browser_run`,
  `project_browser_artifact_read`, `project_browser_close`, and
  `project_browser_cleanup`.

## Closed authority

The caller chooses only a registered project alias, human Edge target, opaque session
or artifact ids, one of two network scopes, bounded viewport/timeout/capture options,
and up to 32 closed steps: navigate, click, type, press, select, or wait. There is no
input for JavaScript, browser executable, flags, extension, download, header, cookie,
proxy, filesystem path, debugging address, CDP endpoint, arbitrary port in public scope,
or arbitrary loopback origin.

Each run executes the fixed `/usr/lib/chromium/chromium` binary through a private
subcommand of the signed Edge and a nested Bubblewrap namespace. The namespace exposes
only read-only system runtime, `/proc`, `/dev`, private `/tmp`, and one exact `0700`
profile mounted at `/browser-profile`. It does not mount the project checkout, Edge
state root, host home, WSL mounts, Docker/rootless sockets, or unrelated repositories.
Downloads are denied through CDP.

An ephemeral proxy bound to `127.0.0.1` resolves and pins every destination. Public
scope accepts only HTTP(S) ports 80/443 and rejects literal, DNS-resolved, or mixed DNS
answers containing private, loopback, link-local, multicast, or unspecified addresses.
Loopback scope is fixed to the initial scheme/host/high port and cannot pivot to another
origin. HTTPS errors may be ignored only in loopback scope.

## Durable state

Owner-only Edge state contains:

- SQLite session, explicit cookie jar, operation receipt, and artifact metadata;
- exact `0700` profiles named by `br_...`;
- exact `0600` JPEG artifacts named by `ba_...`.

Public results expose only opaque ids, safe URLs without credentials/query/fragment,
bounded redacted text, title, state/revision/timestamps, artifact size/hash/media type,
and exact bounded base64 artifact chunks. A completed run receipt replays its saved
result after ACK loss without repeating page effects. An interrupted run becomes
`indeterminate` and fails closed on retry. Close preserves state; cleanup is explicit
and removes only exact closed-session profile and artifact paths after symlink and root
validation.

## Real Edge acceptance

Two opt-in tests ran against the actual Edge Chromium runtime inside Bubblewrap:

1. Runner acceptance: loopback navigation, visible locator wait, keyboard entry,
   click-driven SPA update, body text/title/location capture, JPEG capture, cookie
   extraction, and cookie restoration in a second ephemeral Chromium process.
2. Manager acceptance: create, SQLite persistence, run receipt, private JPEG artifact
   read, manager close/reopen, durable cookie restoration, session close, and explicit
   cleanup.

Both passed on the Edge. Unit tests also cover request/result validation, secret-shaped
input rejection, public-versus-loopback URL policy, launcher flag allowlist, narrow
Bubblewrap mounts, durable result replay, indeterminate failure behavior, and cleanup
escape/symlink rejection. Exact-head CI runs the real acceptance using the Playwright
Chromium distribution copied to the same fixed production path before the test.

## Rollout requirement

This candidate is the first intentional productive catalog change after the stable Front
Door work. Acceptance requires catalog-aware overlap of previous and candidate catalogs,
new backend deployment, OAuth discovery and authenticated MCP verification through the
public Front Door, Edge signed-release update, candidate cutover, real browser calls from
GPT Web, retirement of the previous catalog, and evidence of no HTTP 503 during the
transition.
