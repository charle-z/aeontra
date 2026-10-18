# Console Durable Live State — release-candidate baseline

- Date: 2026-07-17.
- Branch: `console-durable-live-state`.
- Initial base: `origin/main` merge `399d7ac58842f83475c581945d0d5065a517875a`; later main integrations are merged normally before publication.
- Status: release candidate; not merged and not deployed.
- Catalog invariant: exactly 85 tools.
- Catalog hash: `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

## Durable state

| State | Private path | Contract |
|---|---|---|
| Operation Journal and Event Log | `/state/tasks/tasks.db` | SQLite/WAL, terminal task retention 30 days, max 10,000 tasks, event retention 30 days, max 20,000 events, database page cap 64 MiB. |
| Console sessions | `/state/console/sessions.db` | Digest-only durable sessions, revocation/expiry/version, no raw cookie or bearer. |
| Brain node identity | `/state/brain/console-node.key` | Atomic no-overwrite creation, directory 0700, file 0600, stable domain-separated HMAC IDs. |
| Brain console metadata | `/brain/.cache/brain.db` derived cache | Transactional `console_metadata`; explicit `console_summary` <=160 bytes; no body-derived summary. |

Legacy task JSON migration is transactional and idempotent. Every newly imported legacy task receives exactly one persisted event. Reopening does not duplicate tasks or events.

## Console data schema v3

`GET /console/data` returns exact allowlisted sections:

- system;
- payload and durable activity windows/lifetime;
- controllers and model runtimes as distinct entities;
- opaque real project selectors;
- opaque active Edge selectors;
- combined DB/WAL/log storage budget;
- safe Brain status and graph metadata;
- aggregate observability;
- security posture.

Project labels are generic and derived only from real configured policy roots. Edge labels are generic and derived only from real active durable devices. No repository path/name, raw device ID/name/key or network detail is returned.

## Task and event APIs

- `/console/tasks`: exact `limit` and opaque `cursor` only.
- `/console/event-log`: exact `limit`, `cursor`, `controller`, `state`, `operation`, `event_type` only.
- `/console/events`: SSE `snapshot`, `event_snapshot`, `journal`, `stream`.

Unknown keys, duplicate values, malformed cursors, invalid enums and conflicting `Last-Event-ID`/`last_event_id` values fail closed. Retained events replay without duplication. A gap resets both durable snapshots.

## Browser behavior

React exposes `connecting`, `live`, `reconnecting` and `offline`, reconnects with the last durable event ID, deduplicates by `event_id`, preserves events arriving before initial JSON, pages older tasks/events with server cursors, uses precise RFC3339 timestamps and cleans up EventSource, timers and aborted fetches.

The Event Log is server-persisted. Browser-only notices are not presented as durable history.

## Authentication firmware

`/console` login and `/oauth/authorize` share `/auth/assets/firmware.css`. Both remain functional without JavaScript and use CSP without `unsafe-inline`. OAuth and recovery bearer are visibly separate. Throttling renders a safe `[ LOCKED ]` state.

## Storage reporting

Combined reporting includes only SQLite database files, WAL/SHM and `.log`/`.jsonl` files. Limit: 256 MiB; `nearing_limit` at 75%; `degraded` at 90%. The scanner rejects symlinks, non-regular files and more than 4,096 entries. No path or filename is returned.

## Migration and rollback

Upgrade keeps the existing `/state` and `/brain` volumes. Memory-only console sessions require one final login; there is no raw session material to migrate. Do not delete `tasks.db`, `sessions.db` or `console-node.key` during rollback. Older binaries ignore additive state they do not understand.

## Required closure gates

Before PR completion:

- full Go suite and coverage;
- race, vet, build;
- Staticcheck and Govulncheck;
- Actionlint;
- frontend check/test/build;
- OAuth and durable session restart E2E;
- legacy migration and journal restart E2E;
- production Docker build;
- CodeQL, SBOM and Dependency Review;
- console smoke against the exact branch commit.

No merge or deployment is authorized by this baseline.