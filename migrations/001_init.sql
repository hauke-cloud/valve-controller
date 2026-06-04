CREATE TABLE IF NOT EXISTS device_readings (
    time        TIMESTAMPTZ      NOT NULL,
    device_name TEXT             NOT NULL,
    metric      TEXT             NOT NULL,
    value       DOUBLE PRECISION NOT NULL,
    unit        TEXT,
    quality     SMALLINT         DEFAULT 100
);

SELECT create_hypertable('device_readings', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS device_readings_device_time ON device_readings (device_name, time DESC);
CREATE INDEX IF NOT EXISTS device_readings_metric_time ON device_readings (metric, time DESC);
