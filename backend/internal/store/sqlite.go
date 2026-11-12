package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"time"

	"eduforge/backend/migrations"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("record not found")
	ErrConflict = errors.New("state or version conflict")
	ErrLimit    = errors.New("daily generation limit reached")
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	dsn := url.URL{Scheme: "file", Path: absolute}
	query := url.Values{"_pragma": {"busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)"}}
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("connect SQLite: %w", err), db.Close())
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`)
	if err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	files, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	for _, name := range files {
		if err := s.applyMigration(ctx, name); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, name string) error {
	sqlBytes, err := migrations.Files.ReadFile(name)
	if err != nil {
		return err
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var applied bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name = ?)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			return nil
		}
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(name) VALUES (?)`, name)
		return err
	})
}

func (s *Store) RecoverInterrupted(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE document_requests
		SET preview_busy = 0, state = CASE WHEN state = 'generating' THEN 'confirmed' ELSE state END
		WHERE preview_busy = 1 OR state = 'generating'`)
	if err != nil {
		return fmt.Errorf("recover interrupted generation: %w", err)
	}
	return nil
}

func (s *Store) transaction(ctx context.Context, work func(*sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("roll back SQLite transaction: %w", rollbackErr))
		}
	}()
	if err := work(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite transaction: %w", err)
	}
	return nil
}

func requireUpdate(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}

func reserveGeneration(ctx context.Context, tx *sql.Tx, user string, limit int) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO generation_usage(user_id, day, attempts) VALUES (?, ?, 1)
		ON CONFLICT(user_id, day) DO UPDATE SET attempts = attempts + 1 WHERE attempts < ?`,
		user, time.Now().UTC().Format(time.DateOnly), limit)
	if err := requireUpdate(result, err); errors.Is(err, ErrConflict) {
		return ErrLimit
	} else {
		return err
	}
}
