# Development Edge Git authority

Status: implemented on `codex/p15-dev-edge-git`; release and Parrot installation
remain pending until the exact-head gates, merge, signed release, and update finish.

This flow lets an active ChatGPT web task develop in a private repository through
the authenticated local Edge. It separates two uses of GitHub authority:

| Authority | Location | Purpose |
|---|---|---|
| Public MCP GitHub API | VPS/Coolify `GITHUB_TOKEN` | Repository/PR metadata, exact-head checks and workflows, PR creation/merge, and default-branch operations. |
| Local Git transport | Edge private `github.json` | Clone an owner-bound private repository and publish one reviewed branch from a registered `dev` workcell. |

The same fine-grained PAT may be entered in both places, but it is stored separately
because the VPS and PC are separate trust domains. Neither copy is returned to the
model, written into a workspace, placed in Git argv, or mounted into Bubblewrap.

## Required GitHub permission

Restrict a fine-grained PAT to the intended owner and repositories. For the complete
development flow it needs repository Contents read/write and Metadata read. Give it
Actions read and Pull requests read/write when ChatGPT must inspect workflows/checks
and manage the existing PR. Do not add administration or organization-wide access
unless a later explicit operation requires it.

Configure the public copy as private Coolify variables and redeploy the existing MCP:

```text
GITHUB_TOKEN=<same fine-grained PAT>
GITHUB_OWNER=charle-z
GITHUB_OWNER_TYPE=user
```

Never paste the token into ChatGPT, a prompt, a repository file, or a command-line
argument.

## Configure the local Edge copy

Run this as the same non-root user that owns the Edge service. The command reads only
stdin and returns `{configured, owner}` without returning the token:

```bash
read -rsp 'GitHub token: ' EDGE_GITHUB_TOKEN; printf '\n'
printf '%s\n' "$EDGE_GITHUB_TOKEN" | mcp-edge github configure \
  --state "$HOME/.local/state/mcp-edge" --owner charle-z
unset EDGE_GITHUB_TOKEN
mcp-edge github status --state "$HOME/.local/state/mcp-edge"
```

The active file is `$HOME/.local/state/mcp-edge/github.json`, mode `0600`, under a
private state root. Do not copy it into a workspace. Restart the Edge service after
configuration so the next runtime observes the authority:

```bash
sudo systemctl restart "mcp-devbox-opencode-edge@$(id -un).service"
systemctl is-active "mcp-devbox-opencode-edge@$(id -un).service"
```

## Runtime behavior

A configured development runtime receives three private provider tools. They are not
part of the public MCP catalog:

- `workspace_dev_git_clone` accepts only workspace ID, simple repository/directory,
  and one validated branch. Edge constructs the exact owner-bound HTTPS URL.
- `workspace_dev_publish_preview` requires a clean repository, exact current branch,
  owner-bound fetch and push URLs, and a remote branch that is absent or an ancestor.
  It returns a five-minute plan bound to local HEAD and remote HEAD.
- `workspace_dev_publish` consumes that plan once, revalidates all bound state, pushes
  only that branch without force/tags/caller refspecs, and verifies the remote SHA.

Git runs outside the model sandbox in the Edge broker. A fixed askpass helper receives
the PAT only in the child environment. System/global credential helpers, repository
hooks, fsmonitor commands, and the file protocol are disabled for those operations;
output is bounded and token-redacted. Code editing, tests, dependency installation,
rootless containers, commits, and checkpoint updates still happen inside the normal
Linux workcell.

Workflow logs and PR/check state remain public-MCP GitHub API work. The local broker
does not grow a general GitHub API, arbitrary URL, arbitrary command, or free-push
surface.

## Prompt for ChatGPT web

After updating the connector and configuring both token copies, this is sufficient:

```text
Use MCP Devbox to continue the registered development workspace
ws_7c4686f5d9244bbad30ae705d4b660c5. Use a new random idempotency_key.
Clone charle-z/ekoparty-trip-agent through the configured local Edge on branch
mvp-flight-first-telegram-agent, verify HEAD
55068c1a18642812d3cdd14b7492a4815fe94852, and read CODEX_HANDOFF.md first.
Inspect the exact-head GitHub Actions and PR #1 through the GitHub tools, fix only
that repository, test and commit small changes, then preview and publish the same
branch through the Edge. Do not create another PR, force-push, or request the token.
```

Each new continuation call needs a fresh caller-generated idempotency key. Runtime IDs
are terminal execution records and are never resumed after completion/failure; the
idempotency key prevents duplicate creation for one deliberate request, not reuse of
an old runtime for a later chat.

## Recovery

- `github status` reports only whether authority exists and the fixed owner.
- `not configured` means the development runtime starts normally but receives no
  private clone/publish tools.
- An unsafe mode, symlink, malformed credential file, broker failure, changed remote,
  dirty tree, behind/diverged branch, expired plan, or replay fails closed.
- Rotate the PAT in both Coolify and Edge independently. Re-running `github configure`
  atomically replaces only the Edge credential file.

## Direct registered-checkout synchronization

The public direct-Edge path reuses the same private credential authority without
starting OpenCode. `project_git_status`, `project_git_fetch`,
`project_git_fast_forward_preview`, and `project_git_fast_forward` operate only on the
checkout already bound to a human project alias and Edge target. The credential stays
in the askpass child environment, while the public result contains only bounded Git
identity and relationship metadata. The write plan is durable across Edge restart but
expires after five minutes solely as a transaction guard; it is single-use and does
not impose a workspace TTL.
