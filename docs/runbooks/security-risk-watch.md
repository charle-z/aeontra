# Daily sandbox security risk watch

Status: deployed workcell inventory verified; GitHub watch execution requires
separate acceptance.

`.github/workflows/security-risk-watch.yml` runs daily at 11:41 UTC and supports
manual dispatch from `main`, without caller-selected images or other inputs. It has
one 15-minute job, read-only repository and GHCR package permissions, no image rebuild,
no image execution, no deployment credentials, and no resident monitoring agent.

## Coverage and identity

`security/monitored-images.json` identifies one verified `deployed-workcell` with
the corrected Go, Python, glibc and bundled npm dependency recipe. The protected publication, independent
registry hash checks and private runner rollout are recorded in
[the dated rollout evidence](../baselines/2026-10-09-workcell-rollout.md).
Earlier rollouts remain in their dated evidence; the inventory tracks this deployed image.
A passing run means only that the inventoried immutable image was scanned; it
makes no all-production or seven-image coverage claim. The monitor does not
discover production state or automatically update this inventory.

The workflow validates the index-to-amd64-manifest mapping, manifest-to-OCI-config
digest, pulled image ID, revision label and Linux/amd64 platform. It verifies the
workcell Dockerfile SHA-256 in the recorded immutable source revision. A newer
recipe on `main` does not describe an older deployed image. Only then does it add the local
`mcp-sandbox-workcell:ci` tag used by the
existing corrected-zlib VEX. An old or otherwise mismatched image cannot receive that
tag through this workflow. The current gate has no accepted-risk input.

The monitor reads the GHCR image by digest and does not rebuild it. This has a
larger transfer cost than scanning a retained exact-image SBOM: an ephemeral runner downloads
the full workcell layers each day. Registry SBOM extraction can replace this only after
its immutable subject, package-location coverage and gate/VEX binding are verified.
The initial monitor uses the existing official scanner action and Docker CLI instead
of adding a custom registry client or helper; the public MCP catalog has no canonical
tool for generating a CI workflow or extracting an exact registry image inventory.

CI-built SBOMs do not prove the contents of a Coolify-built backend. Existing source
security CI retains its local SBOM and Grype reports for 7 days, and the published
sandbox release workflow retains registry attestations for only runner and workcell.
To add a Coolify component, first capture an SBOM from its actual deployed image and
bind it to the observed container/deployment, image digest or image ID, source revision
and SBOM checksum. Do not substitute a same-source CI rebuild.

## Database, policy and evidence

The pinned Anchore scan action uses Grype v0.110.0. Database caching is followed by an
explicit update check; update-check failure is blocking. Database age and checksum
validation remain enabled, and a database older than 24 hours is rejected. The raw
Grype JSON and complete database provenance object are retained with the verified image
identity and registry manifests for 7 days, including on threshold failure.

`cmd/grype-gate` enforces High/Critical findings without an accepted-risk input.
The historical bundled-npm approval remains in its dated policy and evidence; the
updated image does not inherit it. The corrected-zlib VEX remains unchanged.
`fail-build: false` permits the scanner to retain its JSON before the tested gate runs;
it does not bypass the gate. Scanner errors, missing input/report/DB provenance and
identity mismatches fail the job. There is no `continue-on-error`.

Registry authentication uses only the job's ephemeral `GITHUB_TOKEN` in trusted login
steps and a private runner-temporary Docker configuration. Credentials are removed
before analysis and on every terminal path; the workcell is never launched and receives
no token. A private package must grant the repository read access. A login succeeding
does not prove that the exact private digest can be read.

The deployed recipe updates Go to 1.26.9, glibc to 2.44-r8, Python to
3.14.8_git20261008-r1 and bundled http-cache-semantics to 4.3.0. Its inventory
was updated only after protected publication, independent immutable identity
verification and healthy runner activation. Older images remain documented in
their historical evidence and do not receive approval for this recipe.

## Operator acceptance and maintenance

1. Merge the reviewed source and run one manual `main` dispatch. Check the actual pull,
   identity checks, database provenance, gate conclusion and retained raw report.
2. Verify GitHub's daily schedule is enabled; schedule delivery may be delayed. A daily
   cron is a requested cadence, not an exact delivery-time guarantee.
3. Review failures through Actions. A finding or scanner/database outage does not
   trigger a rebuild, dependency mutation, risk extension, deployment or automatic fix.
4. After a verified rollout, update the inventory's coverage to `deployed-workcell` and
   `deployment_verified` to `true` with the exact observed digests and dated deployment
   evidence; update the candidate-only inventory test with that evidence. Inventory
   changes require review. The monitor does not inspect live production state, so a
   stale inventory still scans its recorded image and must not be described as current
   deployment discovery.
5. Remediate any new blocking finding. A risk exception would require explicit owner
   approval and a reviewed policy change; this watch currently has none.

Source tests and a green GitHub watch run are separate from production deployment and
real-host sandbox acceptance. See [the security model](../security.md),
[configuration](../configuration.md) and [private runner runbook](private-sandbox-runner.md).
