# TimescaleDB Rules

## Driver

Use `github.com/jackc/pgx/v5` with `pgxpool`. Never use `database/sql` directly.

```go
pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
```

Connection string env var: `DATABASE_URL` (format: `postgres://user:pass@host:5432/dbname?sslmode=require`).

## Schema Design

All sensor readings go into a single hypertable partitioned by `time`:

```sql
CREATE TABLE device_readings (
    time        TIMESTAMPTZ     NOT NULL,
    device_name TEXT            NOT NULL,  -- matches CR metadata.name
    metric      TEXT            NOT NULL,  -- e.g. "temperature", "battery", "valve_state"
    value       DOUBLE PRECISION NOT NULL,
    unit        TEXT,                      -- "celsius", "percent", null for dimensionless
    quality     SMALLINT        DEFAULT 100 -- link quality 0-255
);

SELECT create_hypertable('device_readings', 'time');

CREATE INDEX ON device_readings (device_name, time DESC);
CREATE INDEX ON device_readings (metric, time DESC);
```

- One row per metric per MQTT message — wide tables with nullable columns are forbidden.
- `device_name` is a plain text FK to the Kubernetes CR name, not a foreign key constraint (devices can be deleted from K8s without orphaning historical data).
- Add a continuous aggregate for hourly rollups if any metric is written more often than once per minute.

## Migrations

- Files in `migrations/`, named `001_init.sql`, `002_add_index.sql`, etc.
- Use `golang-migrate` (`github.com/golang-migrate/migrate/v4`) run at startup before the server accepts traffic.
- Each migration file must have a corresponding `*.down.sql` reversal.
- Never alter a column type in production — add a new column and migrate data in a separate step.

## Query Patterns

Always use `$1, $2, …` positional parameters — never string-interpolate values into SQL.

Insert a reading:

```go
const insertReading = `
    INSERT INTO device_readings (time, device_name, metric, value, unit, quality)
    VALUES ($1, $2, $3, $4, $5, $6)
`
_, err = pool.Exec(ctx, insertReading,
    reading.Time, reading.DeviceName, reading.Metric,
    reading.Value, reading.Unit, reading.Quality,
)
```

Query recent readings (paginated):

```go
const listReadings = `
    SELECT time, metric, value, unit
    FROM device_readings
    WHERE device_name = $1
      AND time >= $2
      AND time <  $3
    ORDER BY time DESC
    LIMIT $4 OFFSET $5
`
rows, err := pool.Query(ctx, listReadings, name, from, to, limit, offset)
```

- Always pass a context with a deadline to `Exec`/`Query` — use the request context.
- Use `pgx.CollectRows(rows, pgx.RowToStructByName[T])` to scan into structs (pgx v5).
- Check `pgconn.PgError.Code == "23505"` (unique violation) before returning `ErrAlreadyExists`.

## Retention

Configure a TimescaleDB data retention policy rather than a DELETE job:

```sql
SELECT add_retention_policy('device_readings', INTERVAL '90 days');
```

Retention period is configurable via env `TIMESCALE_RETENTION_DAYS` (default 90). Apply the policy during migration `002_retention.sql`.
