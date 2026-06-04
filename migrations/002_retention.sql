-- Retention period is controlled via TIMESCALE_RETENTION_DAYS env var (default 90).
-- This migration sets a default 90-day policy; operators can alter it via the CRD or SQL.
SELECT add_retention_policy('device_readings', INTERVAL '90 days', if_not_exists => TRUE);
