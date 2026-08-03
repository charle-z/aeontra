# Development Edge Git authority

Status: clone, safe publication, registered-checkout synchronization and the first
read-only Hito 5 GitHub API slice are deployed. A real normal login and private import
were verified on `p15.0.13`. PR #134 and signed `p15.0.14` delivered the required
manifest-v2 bridge; the current candidate is the separately reviewed v3 bundle.

This flow lets an active ChatGPT web task develop in a private repository through
the authenticated local Edge. It separates two uses of GitHub authority:

| Authority | Location | Purpose |
|---|---|---|
| Public MCP GitHub API | VPS/Coolify `GITHUB_TOKEN` | Repository/PR metadata, exact-head checks and workflows, PR creation/merge, and default-branch operations. |
| Local Git and GitHub broker | Edge private `github.json` | Clone/publish one owner-bound repository and execute only server-constructed `gh api` reads for its registered `dev` project. |

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

The Debian package installs the official `gh` CLI. The manifest-v3 archive path
cryptographically binds pinned official `gh` 2.97.0; the preceding `p15.0.14` bridge
teaches installed updaters to verify v3. You may keep a complete,
normal GitHub CLI login for your own interactive work and import it into the separate
Edge store:

```bash
gh auth login --hostname github.com --git-protocol https --web
gh auth status --hostname github.com
mcp-edge github import-gh --owner charle-z \
  --state "$HOME/.local/state/mcp-edge"
mcp-edge github status --state "$HOME/.local/state/mcp-edge"
```

`import-gh` invokes only a fixed safe official CLI path, preferring the signed
`/opt/mcp-devbox/current/libexec/gh` once available and retaining `/usr/bin/gh` as the
package-transition fallback. It runs `auth token --hostname github.com`,
ignores ambient GitHub token variables so the stored login is authoritative, copies
the token into the existing owner-only `0600` Edge credential file, clears its capture
buffer, and returns only `{configured, owner}`. It does not delete, replace or otherwise
modify the normal `gh` profile, so both uses remain available.

Alternatively, configure the Edge copy directly from stdin. Run this as the same
non-root user that owns the Edge service:

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

The Hito 5 direct broker also invokes the installed official `gh` binary outside the
workcell. `GH_TOKEN` exists only in that child environment, with a private HOME and
XDG config root. The model can request `project_github_status` only by registered
project alias and Edge target; the Edge constructs fixed `gh api` calls for repository
metadata, one bounded PR probe and one bounded Actions probe. The public response has
closed capability booleans and safe issue codes, not CLI output.

This first slice does not yet create PRs, dispatch workflows, publish releases or
provide arbitrary `gh`, URLs, endpoints, headers, GraphQL, pagination or free shell.
Those Hito 5 writes require their own closed schemas, previews/revalidation where
consequential, durable operation tests and release proof.

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
- `github import-gh` fails with a generic safe error when no complete GitHub CLI login
  exists; it never prints the login token or raw CLI error.
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
