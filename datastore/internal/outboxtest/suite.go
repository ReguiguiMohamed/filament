// Package outboxtest checks publication storage without any schema dependency.
package outboxtest

import (
	"errors"
	"testing"
	"time"

	"github.com/galaxy-io/filament"
)

func Persistence(t *testing.T, s filament.OutboxStore, tenant, other filament.TenantID, reopen func() filament.OutboxStore, expire func()) {
	t.Helper()
	ctx := t.Context()
	first := filament.OutboxMessage{ID: "message-1", Destination: "custom-webhook", OrderingKey: "subscription-1", Kind: "custom.input_processed", PayloadVersion: 7, Payload: []byte{0xff, 0x00}, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	second := first
	second.ID = "message-2"
	unrelated := first
	unrelated.ID = "other-dispatcher"
	unrelated.Destination = "another-destination"
	if err := s.EnqueueOutbox(ctx, tenant, []filament.OutboxMessage{first, second, unrelated}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueOutbox(ctx, tenant, []filament.OutboxMessage{first}); err != nil {
		t.Fatal("exact enqueue retry", err)
	}
	conflict := first
	conflict.Payload = []byte("different")
	tentative := first
	tentative.ID = "must-rollback"
	if err := s.EnqueueOutbox(ctx, tenant, []filament.OutboxMessage{tentative, conflict}); !errors.Is(err, filament.ErrVersionConflict) {
		t.Fatal("conflicting retry accepted", err)
	}
	original, err := s.ClaimOutbox(ctx, first.Destination, time.Minute)
	if err != nil || original == nil || original.Message.ID != first.ID {
		t.Fatal("first claim", original, err)
	}
	if d, err := s.ClaimOutbox(ctx, first.Destination, time.Minute); err != nil || d != nil {
		t.Fatal("ordering violated", d, err)
	}
	d, err := s.ClaimOutbox(ctx, unrelated.Destination, time.Minute)
	if err != nil || d == nil || d.Message.ID != unrelated.ID {
		t.Fatal("destination isolation", d, err)
	}
	if err := s.CompleteOutbox(ctx, d); err != nil {
		t.Fatal(err)
	}
	wrong := *original
	wrong.Tenant = other
	if err := s.CompleteOutbox(ctx, &wrong); !errors.Is(err, filament.ErrFenced) {
		t.Fatal("cross tenant completion", err)
	}
	// Tenant namespaces are independent, including duplicate message IDs/keys.
	if err := s.EnqueueOutbox(ctx, other, []filament.OutboxMessage{first}); err != nil {
		t.Fatal(err)
	}
	d, err = s.ClaimOutbox(ctx, first.Destination, time.Minute)
	if err != nil || d == nil || d.Tenant != other {
		t.Fatal("tenant head-of-line blocking", d, err)
	}
	if err := s.CompleteOutbox(ctx, d); err != nil {
		t.Fatal(err)
	}
	s = reopen()
	expire()
	if err := s.CompleteOutbox(ctx, original); !errors.Is(err, filament.ErrFenced) {
		t.Fatal("expired completion", err)
	}
	recovered, err := s.ClaimOutbox(ctx, first.Destination, time.Minute)
	if err != nil || recovered == nil {
		t.Fatal(err)
	}
	if recovered.Message.ID != original.Message.ID || string(recovered.Message.Payload) != string(original.Message.Payload) || recovered.ClaimToken == original.ClaimToken || recovered.AttemptCount != 2 {
		t.Fatal("recovery changed message")
	}
	if err := s.CompleteOutbox(ctx, original); !errors.Is(err, filament.ErrFenced) {
		t.Fatal("stale completion", err)
	}
	if err := s.RetryOutbox(ctx, original, 0, "delivery_failed"); !errors.Is(err, filament.ErrFenced) {
		t.Fatal("stale retry", err)
	}
	if err := s.RetryOutbox(ctx, recovered, time.Hour, "delivery_failed"); err != nil {
		t.Fatal(err)
	}
	if d, err := s.ClaimOutbox(ctx, first.Destination, time.Minute); err != nil || d != nil {
		t.Fatal("backoff bypassed", d, err)
	}
	expire()
	for _, id := range []string{first.ID, second.ID} {
		d, err := s.ClaimOutbox(ctx, first.Destination, time.Minute)
		if err != nil || d == nil || d.Message.ID != id {
			t.Fatal("ordered recovery", d, err)
		}
		if err := s.CompleteOutbox(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.EnqueueOutbox(ctx, tenant, []filament.OutboxMessage{first}); err != nil {
		t.Fatal("published retry", err)
	}
	if d, err := s.ClaimOutbox(ctx, first.Destination, time.Minute); err != nil || d != nil {
		t.Fatal("duplicate or rollback leak", d, err)
	}
	// An empty ordering key allows concurrent independent messages.
	first.ID = "unordered-1"
	first.OrderingKey = ""
	second = first
	second.ID = "unordered-2"
	if err := s.EnqueueOutbox(ctx, tenant, []filament.OutboxMessage{first, second}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		d, err := s.ClaimOutbox(ctx, first.Destination, time.Minute)
		if err != nil || d == nil {
			t.Fatal("unordered claim blocked", err)
		}
	}
}

func ConcurrentClaim(t *testing.T, first, second filament.OutboxStore, tenant filament.TenantID) {
	t.Helper()
	m := filament.OutboxMessage{ID: "concurrent", Destination: "custom", Kind: "opaque", PayloadVersion: 1, Payload: []byte("payload"), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := first.EnqueueOutbox(t.Context(), tenant, []filament.OutboxMessage{m}); err != nil {
		t.Fatal(err)
	}
	type result struct {
		delivery *filament.OutboxDelivery
		err      error
	}
	results := make(chan result, 2)
	for _, s := range []filament.OutboxStore{first, second} {
		go func() { d, err := s.ClaimOutbox(t.Context(), "custom", time.Minute); results <- result{d, err} }()
	}
	count := 0
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.delivery != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatal("claim duplicated", count)
	}
}
