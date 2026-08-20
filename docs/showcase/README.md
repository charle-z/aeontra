# Frozen historical showcase evidence

`pixelgrama-evidence.json` is the frozen historical Pixelgrama snapshot used by the
Aeontra presentation. It was captured while the referenced source evidence was public;
the repository is now private, so the landing must not promise current GitHub access.

The file stays under `docs/showcase` because the snapshot itself is public, versioned
documentation, while `evidence.go` embeds those exact bytes into the Go binary. The
public server exposes the embedded resource at:

```text
/showcase/pixelgrama-evidence.json
```

This design adds no runtime GitHub or Pixelgrama dependency. An invalid, incomplete, missing, or unrecognized manifest fails Go tests and prevents the landing handler from starting with partial evidence.

Schema version 1 validates:

- the exact Pixelgrama repository, branch, production URL, wall route, and version endpoint;
- closed JSON fields, HTTPS URLs, lowercase 40-character Git SHAs, successful public checks, CubePath, and Coolify;
- a separate historical execution section and current production observation;
- absence of obvious credential material, private filesystem paths, and private device, runtime, or workspace identifiers;
- an honest authority status when the exact historical policy mode is not publicly verifiable;
- direct operations separately from consequential operations whose public result is visible but whose one-time plan artifacts remain private.

The manifest is static historical evidence, not a live availability check. The landing
must not query GitHub on page load, and the public resource grants no repository or
control-plane authority.
