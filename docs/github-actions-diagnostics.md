# GitHub Actions failure diagnostics

Status: **implemented on `p16-global-work-scheduler`; validation pending until the
exact-head remote gates and a deployed MCP smoke are green.**

This document is the repository source of truth for reading GitHub Actions failures
through the GitHub authority already configured on the MCP Devbox control plane.
Installing `gh`, adding another token, configuring the Edge, or pasting credentials into
chat is not part of this workflow.

## Existing authority

The tools reuse the VPS/Coolify environment:

```text
GITHUB_TOKEN
GITHUB_OWNER
GITHUB_OWNER_TYPE
```

Required fine-grained repository permissions are:

```text
Actions: Read
Checks: Read
Contents: Read
```

`Actions: Read` covers workflow runs, jobs and job logs. `Checks: Read` covers check-run
annotations. The Edge credential configured by `mcp-edge github configure` is separate
and is not used for these public control-plane reads.

## Public tools

### Failure-focused view

```text
source_pull_request_failure_diagnostics
```

Inputs identify an owner-bound repository and pull request. Optional exact workflow and
job names narrow the result. The implementation resolves the current PR `head_sha`,
keeps only the newest attempt of each workflow, and reads failed jobs from that exact
head.

The result contains:

- workflow and job names;
- run attempt;
- failed step numbers, names and conclusions;
- check-run annotations with repository-relative path and line;
- line-numbered log context around error/failure markers and the log tail;
- an explicit indication when the inspected log exceeded the diagnostic read window.

It does not accept or expose run IDs, job IDs, signed URLs or tokens.

### Full job log by bounded chunks

```text
source_pull_request_job_log
```

The caller supplies an exact human job name and, only when needed to resolve a collision,
an exact workflow name. The tool returns a redacted log byte range with:

```text
offset_bytes
returned_bytes
next_offset
complete
```

Defaults and limits:

```text
default chunk: 256 KiB
maximum chunk: 1 MiB
maximum readable window per job: 16 MiB
```

Repeated calls using `next_offset` can read the complete log within that window. Large
responses may be persisted automatically by MCP Devbox and read through `result_read`;
this does not change the job-log authority or expose a filesystem path.

The full-log tool exists because a short failure summary can omit the line that actually
explains a CI failure. The diagnostic tool remains the fastest first read; the chunked
log is the authoritative fallback.

## Redirect and credential isolation

GitHub's job-log endpoint returns a temporary redirect. MCP Devbox handles it in two
separate requests:

1. Request the owner-bound GitHub API endpoint with `Authorization: Bearer ...`.
2. Refuse automatic redirect following and validate the returned URL.
3. Require HTTPS in production, no userinfo, no fragment, and no direct private,
   loopback, link-local or unspecified IP target.
4. Request the signed URL without `Authorization`.
5. Refuse further redirects and read only the requested bounded range.

The signed URL is never returned, audited, persisted in Brain or written to Git.

## Redaction and output handling

Both tools remove ANSI control sequences, replace NUL bytes and pass all returned text
through the central secret redactor. The log is not written to repository files, Brain or
the audit log. Audit records contain only the repository, PR and safe job name.

The tools reject:

- repositories outside the configured owner;
- invalid PR numbers;
- control characters or oversized workflow/job selectors;
- ambiguous job names without a workflow selector;
- stale workflow attempts or jobs from another commit;
- unsafe or missing redirects;
- log requests beyond the 16 MiB window;
- response pagination or size-limit inconsistencies.

A GitHub 403 from the job-log endpoint reports that `Actions: Read` is required. A 403
from annotations reports that `Checks: Read` is required.

## Normal troubleshooting flow

```text
source_pull_request_status
→ source_pull_request_failure_diagnostics
→ source_pull_request_job_log only when more context is needed
→ fix the exact failure
→ publish a new head
→ re-read exact-head status
```

No manual GitHub CLI setup is needed.

## Verification

Required tests cover:

- exact PR-head and latest-attempt selection;
- failed step and annotation rendering;
- ambiguous job-name rejection;
- bounded consecutive chunks and `next_offset`;
- no Authorization header on the signed download;
- rejection of unsafe redirects;
- explicit missing-permission diagnostics;
- ANSI/NUL cleanup and token redaction;
- catalog schema, annotations and documentation completeness.
