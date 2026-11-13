# CPA Core 0.2.0 compatibility

CPA-Helper 0.3.23.127 follows the Core `v1-lts-0.2.0` V8 contract in these areas:

- Credential lists include sign-in, renewal, recovery and SIWC lifecycle information. Token refresh, minimal inference verification, targeted reauthorization and SIWC binding use native Core operations. Inference verification requires explicit confirmation and may incur usage. OAuth completion no longer starts legacy inspection automatically.
- Core-managed recovery credentials skip legacy token probing without changing remote disabled state or erasing cached quota measurements. Native API authentication and JSON business errors are not masked by legacy retries. Targeted authorization never falls back to an untargeted login.
- New usage events retain canonical accounting, actual service tiers, execution/session IDs and timing evidence separately from legacy token columns. Complete, conserved accounting version 2 drives token billing; unknown/incomplete accounting is marked unpriced. Actual tier evidence takes precedence over requested FAST tier. Stored historical charges are not recalculated.
- Upstream forms support Interactions and Meta, and preserve per-field connection-pool inheritance, including explicit false/zero and null inheritance. The UI does not guess model-discovery endpoints for unsupported families.
- The V8-only advanced configuration tab edits KV persistence, cooldown persistence, client adaptation, reasoning policies, session behavior, compression, concurrency and upstream policy objects. Untouched YAML and unknown fields remain intact; saving uses revision conflict protection. Legacy configuration paths remain available for older Core deployments.

## Schema and deployment

Schema `202611130001` adds `usage_records.cpa_metadata_json` with an empty-object default in SQLite and PostgreSQL. No historical record, cost, balance or quota is backfilled or repriced. Normal startup applies the incremental migration; do not start a second collector during deployment.

The atomic SQLite-to-PostgreSQL importer has a separate current-schema snapshot in `backend/migrations/postgres-import/`. It records the full applied PostgreSQL migration chain so the imported database can restart normally. Historical goose migration files are unchanged.

This schema increment means an old binary cannot be used as an image-only rollback: its schema-version guard will reject the newer database. Retain a pre-upgrade database backup. Prefer a forward fix; any restoration or schema downgrade requires a separate maintenance plan and must account for new usage collected after deployment. This release does not automatically downgrade or discard metadata.

## Validation boundaries

Regression tests cover native API targeting and admin permissions, actual-tier/canonical-output billing, stored-cost immutability, connection-pool round trips, V8 visual configuration, credential UI confirmation and request detail rendering. SQLite and isolated PostgreSQL parity/import tests are both required. Mock OAuth tests do not replace an end-to-end sign-in with the credential owner; no production credential is reauthorized just to validate the UI.
