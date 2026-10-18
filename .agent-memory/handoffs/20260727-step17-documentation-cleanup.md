# Handoff — Step 17 documentation cleanup

Branch `step17-documentation-cleanup` is based on the merged Step 16 foundation at `fe19479c5022a0836426c9553c403b9833f2a772`.

## Completed locally

- Current documentation no longer embeds moving releases, commits, catalog hashes or tool counts.
- README presents the product, live identity sources and CubePath hosting without event-specific promotion.
- AGENTS, context capsule, connection guide, Coolify guide, historical design/features and Parrot installation documents have clear canonical roles.
- Historical proof remains in baselines, specs, ADRs, roadmap, runbooks and Git rather than being duplicated into current operational sources.
- No runtime or authority-bearing implementation changed.

## Validation

- documentation suite passed;
- all Go packages passed across bounded runs after the monolithic executor reached its time limit without a test failure;
- Vet passed;
- build passed;
- diff check passed;
- changed-document relative links resolve;
- complete diff was reviewed and an accidentally removed static constitution-source check was restored.

## Publication gate

Do not merge until the exact Step 17 commit has every required GitHub check green. After merge, synchronize `main`, allow the existing Coolify automation to deploy, and verify the exact live commit. Do not trigger a redundant manual or no-cache deployment.
