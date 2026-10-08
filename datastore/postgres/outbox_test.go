package postgres_test

import (
	"context"
	"github.com/galaxy-io/filament"
	"github.com/galaxy-io/filament/datastore/internal/outboxtest"
	"github.com/galaxy-io/filament/datastore/postgres"
	"testing"
	"time"
)

func TestOutboxPersistence(t *testing.T) {
	s := newTestStore(t)
	defer func() { _ = s.Close() }()
	const other = "99999999-9999-4999-8999-999999999999"
	if err := s.EnsureTenant(t.Context(), other, "other"); err != nil {
		t.Fatal(err)
	}
	outboxtest.Persistence(t, s, tenantA, other, func() filament.OutboxStore {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		pool, err := postgres.NewPool(t.Context(), testDSN(t))
		if err != nil {
			t.Fatal(err)
		}
		s = postgres.New(pool)
		return s
	}, func() {
		if _, err := s.Pool().Exec(t.Context(), `UPDATE outbox SET claim_expires_at=0,next_attempt_at=0 WHERE published_at=0`); err != nil {
			t.Fatal(err)
		}
	})
}
func TestOutboxConcurrentClaim(t *testing.T) {
	s := newTestStore(t)
	outboxtest.ConcurrentClaim(t, s, s, tenantA)
}

func TestOutboxEnqueueTransactionOrdering(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.Pool().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	// Hold the first enqueue transaction open while another producer tries to
	// enqueue. The second producer must not allocate a publishable sequence yet.
	if _, err := tx.Exec(t.Context(), `SELECT id FROM tenants WHERE id=$1 FOR NO KEY UPDATE`, tenantA); err != nil {
		t.Fatal(err)
	}
	message := filament.OutboxMessage{ID: "second", Destination: "custom", OrderingKey: "group", Kind: "opaque", PayloadVersion: 1, Payload: []byte("value"), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := s.EnqueueOutbox(ctx, tenantA, []filament.OutboxMessage{message}); err == nil {
		t.Fatal("enqueue passed uncommitted predecessor transaction")
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueOutbox(t.Context(), tenantA, []filament.OutboxMessage{message}); err != nil {
		t.Fatal(err)
	}
}
