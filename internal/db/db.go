// Package db owns the SQLite connection lifecycle and schema migrations.
// It opens the archive database in WAL mode so committed writes survive a
// stop, crash or power loss (NFR 6.3), and applies embedded, versioned
// migrations in order at startup.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver (no cgo): builds on Windows/macOS/Linux
)

// migrationFS embeds the versioned SQL migration files. Each file is named
// NNNN_name.sql where NNNN is a zero-padded, monotonically increasing version.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// dbFileName is the SQLite database file created inside the data directory.
const dbFileName = "axon.db"

// Open opens (or creates) axon.db under dataDir with WAL journaling, foreign
// key enforcement and a busy timeout, then applies any pending migrations.
// The returned *sql.DB is ready for use by the store layer.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir %q: %w", dataDir, err)
	}

	dbPath := filepath.Join(dataDir, dbFileName)

	// PRAGMAs are set via the DSN so every pooled connection inherits them
	// (modernc.org/sqlite uses the _pragma= query syntax).
	// journal_mode=WAL: durable append-only writes, readers don't block writer.
	// foreign_keys=ON: enforce ON DELETE CASCADE from conversations to messages.
	// busy_timeout: wait instead of failing immediately on a locked database.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", dbPath)

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}

	// Verify the connection and PRAGMAs actually took effect.
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", dbPath, err)
	}

	if err := migrate(sqlDB); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	return sqlDB, nil
}

// AppDataDir returns the per-user application data directory for axon,
// creating it if it does not exist. It resolves the OS-native config root via
// os.UserConfigDir: ~/Library/Application Support on macOS, %AppData% on
// Windows, $XDG_CONFIG_HOME (or ~/.config) on Linux — then appends "axon".
func AppDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}

	dir := filepath.Join(base, "axon")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create app data dir %q: %w", dir, err)
	}

	return dir, nil
}

// migration is one embedded, versioned SQL file.
type migration struct {
	version int
	name    string
	sql     string
}

// migrate creates the bookkeeping table and applies every embedded migration
// whose version has not yet been recorded, in ascending version order. It is
// idempotent: already-applied versions are skipped.
func migrate(sqlDB *sql.DB) error {
	if _, err := sqlDB.Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at INTEGER NOT NULL
		)`,
	); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(sqlDB)
	if err != nil {
		return err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := applyMigration(sqlDB, m); err != nil {
			return fmt.Errorf("migration %04d_%s: %w", m.version, m.name, err)
		}
	}

	return nil
}

// appliedVersions returns the set of migration versions already recorded.
func appliedVersions(sqlDB *sql.DB) (map[int]bool, error) {
	rows, err := sqlDB.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query applied versions: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan applied version: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied versions: %w", err)
	}

	return applied, nil
}

// applyMigration runs one migration and records it in the same transaction so
// a partial migration can never be marked as applied.
func applyMigration(sqlDB *sql.DB, m migration) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(m.sql); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, time.Now().UnixMilli(),
	); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

// loadMigrations reads and parses the embedded migration files, sorted by
// ascending version. File names must match NNNN_name.sql.
func loadMigrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	var migrations []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}

		version, name, err := parseMigrationName(e.Name())
		if err != nil {
			return nil, err
		}

		content, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", e.Name(), err)
		}

		migrations = append(migrations, migration{
			version: version,
			name:    name,
			sql:     string(content),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	return migrations, nil
}

// parseMigrationName splits a "NNNN_name.sql" file name into its numeric
// version and descriptive name.
func parseMigrationName(fileName string) (int, string, error) {
	base := strings.TrimSuffix(fileName, ".sql")
	parts := strings.SplitN(base, "_", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("bad migration name %q: want NNNN_name.sql", fileName)
	}

	version, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", fmt.Errorf("bad migration version in %q: %w", fileName, err)
	}

	return version, parts[1], nil
}
