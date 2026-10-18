# P8.1 production closure

Date: 2026-07-14

This is the post-deployment closure record for P8.1 Console 2.0. The historical
release-candidate evidence remains unchanged in `2026-07-14-p8_1.md`.

## Release identity

- Pull request: `https://github.com/charle-z/mcp-devbox/pull/10`
- Final PR head: `e96bbc81a2c524c3c7ee9b3eb4bd3945b61198e7`
- Merge on `main`: `d343264bffdc0ae1bc045a9d723e913be977090c`
- Existing Coolify application: `jqf7qz5ensoqtvl1tb197gcv`
- Deployment: `ody7vjcabb3r24b25ym34of9` — finished, application `running:healthy`
- Annotated tag: `p8.1`
- Tag object: `daf728f2093a2da12089e102446e496410ee5aa0`
- The annotated tag resolves exactly to the merge commit above.

## Required gates

The exact final PR head passed Verify, Race detector, Staticcheck, Govulncheck,
CodeQL for Go and JavaScript/TypeScript, Dependency Review, Docker build, SPDX SBOM
and the unchanged zero High/Critical Grype gate. Dependency Review was correctly
skipped only on the post-merge event because it had already passed on the exact PR
diff. No gate or threshold was weakened.

## Production evidence

The catalog, Brain and console smokes passed with:

```text
commit=d343264bffdc0ae1bc045a9d723e913be977090c
tool_count=67
catalog_hash=sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed
brain_ready=true
brain_schema_version=1
surface=presentation-only
```

The P8.1-specific smoke also verified:

- query-string credentials return HTTP 401;
- header `Authorization: Bearer` remains recovery-only;
- the opaque cookie is `Secure`, `HttpOnly` and `SameSite=Strict`;
- `/console/data` returns schema version 1 and real allowlisted data;
- `/console/tasks` is available and `/console/events` emits a valid first SSE snapshot;
- Brain exposes only opaque graph identifiers;
- Edge reports `not_paired` and does not claim an Edge implementation;
- production OAuth begins with state and PKCE S256; an invalid callback returns 401.

`/state/tasks` remained operational after container replacement. Direct mount listing
was not forced after the connector security filter rejected it; this record therefore
does not claim additional mount evidence beyond the observed durable behavior.
