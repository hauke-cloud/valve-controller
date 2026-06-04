package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Reading is a single sensor metric row.
type Reading struct {
	Time       time.Time
	DeviceName string
	Metric     string
	Value      float64
	Unit       string
	Quality    int16
}

// Store provides read/write access to the device_readings hypertable.
// A nil *Store is safe — all methods are no-ops.
type Store struct {
	pool *pgxpool.Pool
}

// New opens a pgxpool connection and runs migrations.
// Returns (nil, nil) when dsn is empty so callers can skip DB entirely.
func New(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		return nil, nil
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

const insertReading = `
    INSERT INTO device_readings (time, device_name, metric, value, unit, quality)
    VALUES ($1, $2, $3, $4, $5, $6)`

func (s *Store) InsertReading(ctx context.Context, r Reading) error {
	if s == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, insertReading,
		r.Time, r.DeviceName, r.Metric, r.Value, r.Unit, r.Quality)
	return err
}

const listReadings = `
    SELECT time, metric, value, unit
    FROM device_readings
    WHERE device_name = $1
      AND time >= $2
      AND time <  $3
    ORDER BY time DESC
    LIMIT $4 OFFSET $5`

type ReadingRow struct {
	Time   time.Time
	Metric string
	Value  float64
	Unit   string
}

func (s *Store) QueryReadings(ctx context.Context, deviceName string, from, to time.Time, limit, offset int) ([]ReadingRow, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, listReadings, deviceName, from, to, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ReadingRow
	for rows.Next() {
		var r ReadingRow
		if err := rows.Scan(&r.Time, &r.Metric, &r.Value, &r.Unit); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.pool.Ping(ctx)
}
