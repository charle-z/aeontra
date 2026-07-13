# ADR 0002: embed the authenticated P8 console in the Go HTTP application

- Status: Accepted
- Date: 2026-07-13
- Scope: P8 authenticated console only
- Supersedes: ADR 0001 decision item 6 and its `Console security` implementation choice

## Context

ADR 0001 planned an Astro SSR + TypeScript operator console with a server-side BFF.
That was a reasonable early direction before P6/P7 hardened the Go application and
established a safe public runtime identity contract.

The current product constraints are narrower:

- do not create another Coolify application;
- do not change OAuth or introduce another credential;
- keep the console presentation-only rather than a control plane;
- minimize supply-chain, deployment, and operational complexity;
- preserve the existing 62-tool MCP catalog and authorization paths;
- complete the console as an independently releasable milestone before Asset Broker.

A separate SSR/BFF application would add a Node dependency graph, package manager,
build pipeline, server-side token plumbing, deployment identity, health checks, CSP
surface, and cross-application session/authentication boundary. None of that is needed
to display the existing public `RuntimeInfo` values and static product explanations.

## Decision

P8 is embedded in the existing Go HTTP application. This creates no new listener or Coolify application.

1. HTML, CSS, and JavaScript are versioned embedded assets with no npm, CDN, remote
   font, analytics, service worker, or third-party runtime.
2. The existing HTTP listener registers `/console` routes; no new listener or Coolify
   application is created.
3. Existing static bearer and OAuth access-token validation are reused without
   changing protocol behavior.
4. Browser login creates a cryptographically random, digest-only, in-memory session
   with a bounded count and lifetime.
5. The only dynamic payload is an exact fixed schema derived from the existing public
   `RuntimeInfo` contract.
6. The console has no MCP execution, approval, repository, audit, observability-history,
   deployment, configuration, or generic proxy endpoint.
7. A later unauthenticated public showcase remains a separate optional product. It may
   use a frontend framework if concrete submission/demo requirements justify it, but it
   must not become a proxy to the private control plane.

## Security consequences

Benefits:

- one deployment identity, listener, authentication boundary, and CSP origin;
- no frontend dependency tree or second server-side secret store;
- no cross-origin requests or BFF token exchange;
- simpler rollback: deploy the previous Go commit;
- authenticated smoke testing can compare the same commit/tool count/catalog hash as
  `/version` and MCP runtime identity.

Required controls remain:

- constant-time static-token validation;
- opaque digest-only session ids, expiry, cap, revocation, and restart invalidation;
- HttpOnly/SameSite/secure cookie posture;
- strict CSP and browser security headers;
- exact status JSON keys and contextual rendering through static HTML/text nodes;
- no untrusted raw HTML or JavaScript-readable credential/session value;
- adversarial tests for unauthenticated status/assets, token/query/cookie leakage,
  method confusion, oversized bodies, and external browser capabilities.

## Trade-offs

- The console cannot be released independently from the Go daemon.
- A Go rebuild is required for visual changes.
- The embedded UI intentionally avoids framework conveniences and remains modest.
- Sessions are not shared across replicas and disappear on restart. This is accepted
  because the current application is single-instance and the console is read-only.

These trade-offs are preferable to a second control-plane-adjacent application for the
current scope.

## Compatibility

This ADR does not change:

- MCP tools, schemas, annotations, approvals, or catalog identity;
- OAuth endpoints, grants, token format, clients, or persistence;
- audit or structured-observability data contracts;
- Coolify application identity or public domain;
- future Asset Broker, universal profile, or Edge Agent designs.

## Rollback

Deploy the prior known-good commit. The embedded console has no database, volume,
external asset, migration, or persistent session state to remove.
