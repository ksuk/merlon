# Legacy seed samples

`seed.sql` predates the current database schema (it uses columns like `name`
and `occupation_risk` instead of the actual `external_id`, `attributes JSONB`,
and `country_code` columns from `migrations/`). It is kept here for reference
only and is not loaded by any script or compose topology. It is expected to
be replaced by generated demo seed data (see `api/cmd/merlon-demogen`).
