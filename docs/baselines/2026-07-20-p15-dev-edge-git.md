# P15 development Edge Git candidate — 2026-07-20

Status: local candidate; not yet merged, released, or installed on Parrot.

Branch `codex/p15-dev-edge-git` is based on deployed p15.0.4 commit
`3a91fb703ca8543869098ba0aa8c80f69e8233a1`. Step 21 commits explicit
caller-generated continuation idempotency. Step 22 adds private, owner-bound Git
transport for development workcells while leaving the 98-tool public catalog at
`sha256:8a9a637f2817e9e2824ac9756c5cf8f5146fee3b6ee5515ea2f72903ed922e12`.

Candidate behavior:

- the VPS/Coolify PAT remains the authority for GitHub workflow/check/PR APIs;
- a separately entered local copy is stored only in private Edge state;
- configured `dev` runtimes receive closed clone, publish-preview and publish actions;
- clone constructs only the configured-owner HTTPS URL;
- publication consumes a five-minute single-use plan after clean-tree, branch, HEAD,
  remote SHA, fetch URL and push URL revalidation;
- force, tags, arbitrary URL/refspec/command and credential schema fields do not exist;
- Git helpers, hooks, fsmonitor commands and file protocol are neutralized;
- the PAT is absent from workspace mounts, model requests, argv, responses and logs;
- manifest version 2 signs `dev-actions.js`; exact version-1 verification is retained
  so p15.0.4 remains a valid rollback target.

Local evidence on Parrot WSL2 with Go 1.26.5:

- `go test ./... -count=1` — pass;
- `go vet ./...` — pass;
- `go build ./...` — pass;
- `node --test integrations/opencode/provider/provider.test.mjs` — 19 pass;
- packaging script syntax and `git diff --check` — pass.

Pending before deployed status: commit, push, exact-head PR checks, merge commit,
automatic VPS deployment verification, signed release, structured Parrot update,
private local PAT entry, connector refresh, and one real private repository smoke.
