# Historical CodeQL findings closure

Date: 2026-07-16  
Branch: `security-findings-closure`  
Base: production merge `b9ee5ea9fd18a72d9687784eeb5cbfd8603427b5`

## Scope

This change set addresses only the historical CodeQL findings associated with:

- validation-runner repository path selection;
- console authentication cookies;
- the GitHub token secret detector.

It does not change P11.2, Edge, OpenCode, Bubblewrap, Coolify, production, Parrot,
the frontend, session persistence, telemetry, Brain, the MCP protocol, or the tool
catalog.

## Evidence reconstruction

The available GitHub credential can read check runs and annotations but receives HTTP
403 from the code-scanning alerts REST endpoint because it lacks the required
`security_events` read permission. No alert number, dismissal, or dashboard state is
invented in this report.

The two cookie findings were recovered exactly from historical GitHub CodeQL check
`86951967306` on commit `5f4ffb7d86857759342fc9883149c2dbe1a0030f`.
The path and regular-expression findings were reconstructed from their CodeQL rule
IDs, the production source locations, Git history, and the rule-specific data flow.
The pull-request CodeQL check on the exact final SHA is the authoritative verification
that the changed code no longer emits these results.

## Original findings

### 1. Validation registry lookup path

- Rule ID: `go/path-injection`
- Rule: Uncontrolled data used in path expression
- Production location: `cmd/validation-runner/main.go:138`
- First containing commit reconstructed from Git history:
  `5c3d58482a256b619e8c7e5b4d70d7743987f53e`
- Prior flow:
  `request.repo` → `repoPath(name)` → `filepath.Join(configuredRoot, name)` →
  repository inspection.
- Risk: a remote field participated directly in a host filesystem path expression.
  Existing slash, absolute-path, direct-child, symlink and manifest checks reduced
  exploitability but did not establish a server-owned registry boundary that CodeQL
  could prove.
- Correction: the runner discovers an immutable startup snapshot from its configured
  local root. The request now contains only `repo_id`; it can select an existing map
  entry but cannot contribute bytes to a filesystem path.
- Tests: valid and unknown IDs, empty ID, traversal, slash, backslash, absolute path,
  NUL, special components, symlink discovery, nested/outside repositories, prefix
  collisions, unsafe permissions and duplicate registration.
- Final handling: corrected by code.
- Residual risk: the configured local root remains a trusted administrator input.

### 2. Docker bind-mount source path

- Rule ID: `go/path-injection`
- Rule: Uncontrolled data used in path expression
- Production location: `cmd/validation-runner/main.go:177`
- First containing commit reconstructed from Git history:
  `8d41d8cd310d5a94afb28673840eba622ddc16a7`
- Prior flow:
  `request.repo` → resolved repository path → `filepath.Rel` →
  `filepath.Join(configuredHostRoot, rel)` → Docker bind-mount source.
- Risk: the request indirectly influenced the host-side source string used by Docker.
- Correction: each registry entry stores a precomputed server-owned canonical path,
  host mount source, stable filesystem identity, fixed mode and discovery time. The
  container destination is the constant `/workspace`. Unknown JSON fields are
  rejected, so a caller cannot choose host or container paths.
- TOCTOU mitigation: immediately before Docker execution, the runner reopens the
  configured root, repository and `package.json` using descriptors on Linux with
  `O_NOFOLLOW`; it compares device/inode/type identities and permission bits against
  the startup snapshot. Root, repository, manifest, symlink, move and replacement
  changes fail closed.
- Tests: repository moved/replaced, root replaced, manifest replaced, post-discovery
  symlink substitution, group/world writable directories, fixed destination, remote
  container-path injection and response path redaction.
- Final handling: corrected by code.
- Residual risk: Docker requires a path string after revalidation, leaving a minimal
  cross-process interval. A caller cannot choose that string; exploitation would
  require a separate local actor already able to replace trusted host filesystem
  entries. Production uses Linux descriptor validation.

### 3. GitHub token search expression

- Rule ID: `go/regex/missing-regexp-anchor`
- Rule: Missing regular expression anchor
- Production location: `internal/policy/scan.go:28`
- First containing commit reconstructed from Git history:
  `46bef1bcb26f501eba8c9df6873f6789e572eeb4`
- Expression: `\bgh[pousr]_[0-9A-Za-z]{36,}\b`
- Use: search and redact a secret embedded anywhere inside returned text. It is not a
  validator that accepts or rejects a complete input value.
- Analysis: adding `^` and `$` would be a security regression because tokens inside
  JSON, logs, command output or source lines would no longer be detected.
- Correction: the expression is compiled once as the explicitly named
  `githubTokenSearchPattern`; a narrow in-source
  `codeql[go/regex/missing-regexp-anchor]` suppression documents the intentional
  search semantics immediately before the expression.
- Tests: isolated token, middle of a line, JSON, logs, invalid prefix, insufficient
  length, invalid characters, split token, multiple tokens and a large non-matching
  input. A benchmark records linear RE2 behavior on the large clean input.
- Final handling: justified as a false positive with a local rule-specific
  suppression. It is not globally disabled and is not classified as `won't fix`.
- Residual risk: provider token formats may evolve and require future detector updates.

### 4. Console session cookie creation

- Rule ID: `go/cookie-secure-not-set`
- Rule: Cookie `Secure` attribute is not set to true
- Historical check: `86951967306`
- Detected commit: `5f4ffb7d86857759342fc9883149c2dbe1a0030f`
- Historical range: `internal/console/handler.go:323-332`
- Production range before correction: `internal/console/handler.go:407-417`
- Prior flow: cookie security was computed dynamically from configuration, request TLS
  state and localhost/loopback host inference.
- Risk: CodeQL could not prove that every production path set `Secure=true`.
- Correction: the production cookie constructor now always sets:
  `Secure=true`, `HttpOnly=true`, `SameSite=Strict`, `Path=/`, a positive `MaxAge`,
  and a coherent `Expires` value. No environment variable or production option can
  disable `Secure`.
- Tests: successful bearer login, direct Bearer recovery, OAuth callback, no renewal
  on ordinary authenticated requests, expiry, and absence of credentials in URL/body
  responses.
- Final handling: corrected by code.
- Residual risk: browsers will not send the cookie over plain HTTP, intentionally;
  local browser testing must use TLS when cookie round trips are required.

### 5. Console session cookie deletion

- Rule ID: `go/cookie-secure-not-set`
- Rule: Cookie `Secure` attribute is not set to true
- Historical check: `86951967306`
- Detected commit: `5f4ffb7d86857759342fc9883149c2dbe1a0030f`
- Historical range: `internal/console/handler.go:337-346`
- Production range before correction: `internal/console/handler.go:421-431`
- Prior flow: deletion repeated the same dynamic `Secure` inference and used the
  narrower `/console` path.
- Risk: a deletion cookie with attributes different from the creation cookie may fail
  to remove the browser cookie consistently.
- Correction: logout/revocation emits the deletion cookie with the same
  `Secure=true`, `HttpOnly=true`, `SameSite=Strict`, `Path=/` policy, an expired
  timestamp and `MaxAge=-1`.
- Tests: logout, revocation, expired sessions, invalid OAuth state/replay, and exact
  deletion attributes.
- Final handling: corrected by code.
- Residual risk: session persistence across redeploy remains intentionally outside this
  change set.

## Additional invariants

- The validation response and errors do not expose canonical or host paths.
- The remote request cannot provide a mount source, container destination, command,
  profile implementation or Docker option.
- `?key=` remains unauthorized.
- Bearer recovery and OAuth continue to create opaque digest-backed sessions.
- No frontend files or tool definitions changed.
- Tool count and catalog hash must remain exactly:
  - `78` tools;
  - `sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed`.

## Verification policy

The final branch is eligible only after formatting, serial tests, atomic coverage,
coverage thresholds, vet, build, CGO race, Staticcheck, Govulncheck, Actionlint,
validation-runner adversarial tests, cookie tests, detector tests, no-cache container
builds, CodeQL, Dependency Review and container vulnerability evidence are green.

Because this task does not merge the pull request, default-branch alert records may
remain open until the corrected tree reaches `main`. The PR CodeQL result must show no
new instance for the two path findings or two cookie findings, and the regex result
must be suppressed only by the documented local false-positive annotation.


## Operational closure of the historical regex alert

On 2026-07-17, PR #17 used a branch-scoped one-time GitHub Actions workflow to
query only open CodeQL records matching all of these properties:

- rule `go/regex/missing-regexp-anchor`;
- path `internal/policy/scan.go`;
- default-branch ref `refs/heads/main`.

The workflow dismissed the exact matching record set as `false positive` with
the bounded comment `Intentional embedded-token detector; covered by dedicated tests.`
It then read every affected record back and verified the rule, path, dismissed state,
reason and comment. A final open-alert query confirmed that no exact matching record
remained open. The successful discovery, update and verification jobs ran under
GitHub Actions run `29602402419` on commit
`e5dc4e6054e8dc3bbae8c5fdbce62f75d9fac5bb`.

The one-time workflow was removed before merge so production does not retain an
alert-mutating automation. The detector expression and its tests were not weakened or
changed. This operational result supersedes the earlier pre-closure note that the
historical dashboard record could remain open.
