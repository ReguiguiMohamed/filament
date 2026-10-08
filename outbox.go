package filament

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"
)

// OutboxMessage is committed output awaiting delivery. Destination selects an
// application-owned dispatcher, never an arbitrary URL. Payload is opaque here.
// ID is a stable producer idempotency key within a tenant. Re-enqueue requires
// identical contents, including CreatedAt. OrderingKey is optional FIFO grouping
// within one tenant/destination. Retain delivered records for the retry horizon.
type OutboxMessage struct {
	ID             string
	Destination    string
	OrderingKey    string
	Kind           string
	PayloadVersion int64
	Payload        []byte
	CreatedAt      time.Time
}

func (m OutboxMessage) Validate() error {
	for _, s := range []string{m.ID, m.Destination, m.Kind} {
		if s == "" || len(s) > 256 || !utf8.ValidString(s) {
			return fmt.Errorf("invalid outbox identity")
		}
	}
	if len(m.OrderingKey) > 1024 || !utf8.ValidString(m.OrderingKey) || m.PayloadVersion < 1 || len(m.Payload) == 0 || len(m.Payload) > 1024*1024 || m.CreatedAt.IsZero() || !m.CreatedAt.Equal(m.CreatedAt.Truncate(time.Millisecond)) {
		return fmt.Errorf("invalid outbox payload or timestamp")
	}
	return nil
}

type OutboxDelivery struct {
	Tenant       TenantID
	Message      OutboxMessage
	ClaimToken   string
	AttemptCount int64
}

// OutboxStore is trusted publication infrastructure, not an RPC surface.
// ClaimOutbox spans tenants but only claims the requested dispatcher destination.
// Complete/Retry are tenant-scoped and fenced by an unexpired claim token.
// EnqueueOutbox is an independent transaction. Domain adapters must use their
// transaction-local enqueue helper to commit domain writes and messages together.
type OutboxStore interface {
	EnqueueOutbox(context.Context, TenantID, []OutboxMessage) error
	ClaimOutbox(context.Context, string, time.Duration) (*OutboxDelivery, error)
	CompleteOutbox(context.Context, *OutboxDelivery) error
	RetryOutbox(context.Context, *OutboxDelivery, time.Duration, string) error
}

// OutboxDispatcher owns payload decoding and transport behavior. Delivery may be
// repeated after ambiguous acknowledgement. Use Message.ID for idempotency.
type OutboxDispatcher interface {
	Deliver(context.Context, OutboxDelivery) error
}
