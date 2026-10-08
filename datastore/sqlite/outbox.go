package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/galaxy-io/filament"
	"github.com/galaxy-io/filament/datastore/sqlite/sqlcgen"
	"github.com/google/uuid"
	"time"
)

var _ filament.OutboxStore = (*Store)(nil)

func (s *Store) EnqueueOutbox(ctx context.Context, tenant filament.TenantID, messages []filament.OutboxMessage) error {
	return s.recordsTransaction(ctx, false, func(q *sqlcgen.Queries) error { return enqueueOutbox(ctx, q, tenant, messages) })
}

// enqueueOutbox is reused by domain commits inside their existing transaction.
func enqueueOutbox(ctx context.Context, q *sqlcgen.Queries, tenant filament.TenantID, messages []filament.OutboxMessage) error {
	if err := tenant.Valid(); err != nil {
		return err
	}
	if len(messages) > 100 {
		return fmt.Errorf("outbox batch exceeds 100 messages")
	}
	for _, m := range messages {
		if err := m.Validate(); err != nil {
			return err
		}
		_, err := q.InsertOutbox(ctx, sqlcgen.InsertOutboxParams{TenantID: string(tenant), ID: m.ID, Destination: m.Destination, OrderingKey: m.OrderingKey, Kind: m.Kind, PayloadVersion: m.PayloadVersion, Payload: m.Payload, CreatedAt: m.CreatedAt.UnixMilli()})
		if errors.Is(err, sql.ErrNoRows) {
			return filament.ErrVersionConflict
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) ClaimOutbox(ctx context.Context, destination string, ttl time.Duration) (*filament.OutboxDelivery, error) {
	if destination == "" || ttl < time.Millisecond || ttl > 5*time.Minute {
		return nil, fmt.Errorf("destination and claim TTL between 1ms and 5m required")
	}
	r, err := s.q.ClaimOutbox(ctx, sqlcgen.ClaimOutboxParams{Destination: destination, ClaimToken: uuid.NewString(), TtlMs: ttl.Milliseconds()})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &filament.OutboxDelivery{Tenant: filament.TenantID(r.TenantID), Message: filament.OutboxMessage{ID: r.ID, Destination: r.Destination, OrderingKey: r.OrderingKey, Kind: r.Kind, PayloadVersion: r.PayloadVersion, Payload: r.Payload, CreatedAt: time.UnixMilli(r.CreatedAt)}, ClaimToken: r.ClaimToken, AttemptCount: r.AttemptCount}, nil
}
func validDelivery(d *filament.OutboxDelivery) error {
	if d == nil || d.Tenant == "" || d.Message.ID == "" || d.ClaimToken == "" {
		return fmt.Errorf("outbox delivery identity required")
	}
	return nil
}
func (s *Store) CompleteOutbox(ctx context.Context, d *filament.OutboxDelivery) error {
	if err := validDelivery(d); err != nil {
		return err
	}
	n, err := s.q.CompleteOutbox(ctx, sqlcgen.CompleteOutboxParams{TenantID: string(d.Tenant), ID: d.Message.ID, ClaimToken: d.ClaimToken})
	if err != nil {
		return err
	}
	if n != 1 {
		return filament.ErrFenced
	}
	return nil
}
func (s *Store) RetryOutbox(ctx context.Context, d *filament.OutboxDelivery, delay time.Duration, code string) error {
	if err := validDelivery(d); err != nil {
		return err
	}
	if delay < 0 || delay > time.Hour || code == "" || len(code) > 64 {
		return fmt.Errorf("invalid outbox retry")
	}
	for _, c := range code {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return fmt.Errorf("invalid outbox error code")
		}
	}
	n, err := s.q.RetryOutbox(ctx, sqlcgen.RetryOutboxParams{TenantID: string(d.Tenant), ID: d.Message.ID, ClaimToken: d.ClaimToken, DelayMs: delay.Milliseconds(), ErrorCode: code})
	if err != nil {
		return err
	}
	if n != 1 {
		return filament.ErrFenced
	}
	return nil
}
