# Plan — P15 zero-touch local autopilot

1. Add signed versioned Edge bundles and reject mismatched components before runtime.
2. Add a reproducible Debian package, atomic updater, previous-release rollback,
   restricted repair, P12–P14 migration and one-action onboarding.
3. Add idempotent remote lab preparation and retargeting while preserving workspace IDs,
   checkpoints, evidence and incremented authorization state.
4. Add durable local autopilot jobs built from bounded worker cycles, atomic redacted
   state, local model providers, progress circuit breakers and restart recovery.
5. Remove HTB execution tools from the exterior catalog and offer them only through the
   private local worker broker.
6. Add safe public control tools for bundle status/update/rollback, onboarding, repair,
   lab preparation/retarget, and autopilot lifecycle.
7. Verify installer, updater, migration, rollback, Edge, provider, TypeScript, Bubblewrap,
   rootless runtime, durable recovery, coverage, race, vet, static analysis, vulnerability,
   build, formatting and workflow gates.
8. Publish a separate PR, require exact-head checks, merge with a merge commit, wait for
   automatic deployment, then use only structured updater/lab/autopilot operations for
   the real Parrot migration and safe smoke.
