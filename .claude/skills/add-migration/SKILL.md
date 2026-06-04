---
name: add-migration
description: Create a new numbered TimescaleDB migration pair (up + down SQL files) and verify it applies cleanly against a local database. Use when schema changes are needed — new tables, indexes, continuous aggregates, retention policies, or column additions.
---

# add-migration

Do NOT touch any existing migration file. Migrations are immutable once committed.

---

## Step 1 — Determine the next sequence number

```bash
ls migrations/ | sort | tail -1
```

Increment the number by 1. If the latest is `003_retention.sql`, the new files are `004_<description>.sql` and `004_<description>.down.sql`.

Name the description in snake_case, describing what this migration does (e.g. `add_hourly_rollup`, `add_device_index`, `valve_state_column`).

---

## Step 2 — Write the up migration

File: `migrations/<NNN>_<description>.sql`

Rules:
- Use `IF NOT EXISTS` on CREATE TABLE / CREATE INDEX to make the migration re-runnable.
- Use `IF EXISTS` on DROP statements in the down migration.
- Do NOT use `ALTER COLUMN … TYPE` — add a new column and migrate data instead.
- For new hypertables: create the regular table first, then call `create_hypertable`.
- For continuous aggregates: use `CREATE MATERIALIZED VIEW … WITH (timescaledb.continuous)`.
- For retention policies: `SELECT add_retention_policy('table', INTERVAL '...')`.
- End every statement with `;`.
- Add a `-- Migration: <NNN> <description>` header comment.

Example structure for a new metric table:
```sql
-- Migration: 004 add_hourly_rollup

CREATE MATERIALIZED VIEW IF NOT EXISTS device_readings_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    device_name,
    metric,
    AVG(value)   AS avg_value,
    MIN(value)   AS min_value,
    MAX(value)   AS max_value,
    COUNT(*)     AS sample_count
FROM device_readings
GROUP BY bucket, device_name, metric
WITH NO DATA;

SELECT add_continuous_aggregate_policy('device_readings_hourly',
    start_offset => INTERVAL '3 hours',
    end_offset   => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour'
);
```

---

## Step 3 — Write the down migration

File: `migrations/<NNN>_<description>.down.sql`

The down migration must exactly reverse the up migration.

```sql
-- Migration: 004 add_hourly_rollup (down)

SELECT remove_continuous_aggregate_policy('device_readings_hourly');
DROP MATERIALIZED VIEW IF EXISTS device_readings_hourly;
```

---

## Step 4 — Apply and verify

If a local TimescaleDB instance is available (`DATABASE_URL` set):

```bash
# Apply up migration
migrate -database "$DATABASE_URL" -path migrations up 1

# Verify the schema change
psql "$DATABASE_URL" -c "\d+ device_readings_hourly"   # or relevant check

# Test rollback
migrate -database "$DATABASE_URL" -path migrations down 1

# Verify rollback succeeded
psql "$DATABASE_URL" -c "\d device_readings_hourly" 2>&1 | grep "did not exist"

# Re-apply (leave DB in migrated state)
migrate -database "$DATABASE_URL" -path migrations up 1
```

If no local DB is available, state this explicitly — do NOT claim the migration is verified.

---

## Step 5 — Update Go query code if needed

If the migration adds a column or table that queries will use:
- Add or update query functions in `internal/db/`.
- Add metric constants in `internal/db/metrics.go` for any new metric names.
- Run `go build ./... && go test ./internal/db/... -race`.

---

## Verification checklist

- [ ] File names follow `NNN_description.sql` / `NNN_description.down.sql` pattern
- [ ] `IF NOT EXISTS` / `IF EXISTS` guards present
- [ ] Down migration fully reverses the up migration
- [ ] Migration applied and rolled back successfully against a real DB (or flagged as untested)
- [ ] No existing migration file was modified
- [ ] `go build ./...` exits 0
