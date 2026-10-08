package postgres

import (
	"context"
	"errors"
	"github.com/galaxy-io/filament"
	"github.com/galaxy-io/filament/datastore/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
)

func (s *Store) recordsTransaction(ctx context.Context, readOnly bool, fn func(*sqlcgen.Queries) error) error {
	opts := pgx.TxOptions{}
	if readOnly {
		opts.IsoLevel = pgx.RepeatableRead
		opts.AccessMode = pgx.ReadOnly
	}
	tx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func recordError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return filament.ErrNotFound
	}
	return err
}
