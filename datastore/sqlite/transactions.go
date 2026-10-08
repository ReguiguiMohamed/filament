package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"github.com/galaxy-io/filament"
	"github.com/galaxy-io/filament/datastore/sqlite/sqlcgen"
)

func (s *Store) recordsTransaction(ctx context.Context, readOnly bool, fn func(*sqlcgen.Queries) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	begin := "BEGIN IMMEDIATE"
	if readOnly {
		begin = "BEGIN"
	}
	if _, err := conn.ExecContext(ctx, begin); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()
	if err := fn(sqlcgen.New(conn)); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}

func recordError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return filament.ErrNotFound
	}
	return err
}
