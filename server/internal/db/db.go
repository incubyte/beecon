// Package db constructs the bun.DB Beecon's persistence port adapters share,
// and runs the boot-time migrator. Postgres and SQLite are the only two
// dialects; both are pure Go (no cgo), so the same binary runs on Windows dev
// machines and production Linux hosts alike.
package db

import (
	"database/sql"
	"fmt"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	_ "modernc.org/sqlite"

	"beecon/internal/config"
)

// New opens a bun.DB for the configured driver. Callers must Close() it on
// shutdown.
func New(driver config.DatabaseDriver, dsn string) (*bun.DB, error) {
	switch driver {
	case config.DriverPostgres:
		sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
		return bun.NewDB(sqldb, pgdialect.New()), nil
	case config.DriverSQLite:
		sqldb, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("open sqlite database: %w", err)
		}
		return bun.NewDB(sqldb, sqlitedialect.New()), nil
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
}
