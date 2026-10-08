package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/galaxy-io/filament"
	"github.com/galaxy-io/filament/datastore/internal/outboxtest"
)

func TestOutboxPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	for _, tenant := range []filament.TenantID{"first", "second"} {
		if err := s.EnsureTenant(t.Context(), tenant, string(tenant)); err != nil {
			t.Fatal(err)
		}
	}
	outboxtest.Persistence(t, s, "first", "second", func() filament.OutboxStore {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}, func() {
		if _, err := s.db.Exec(`UPDATE outbox SET claim_expires_at=0,next_attempt_at=0 WHERE published_at=0`); err != nil {
			t.Fatal(err)
		}
	})
}
func TestOutboxConcurrentClaim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := s.EnsureTenant(t.Context(), "first", "first"); err != nil {
		t.Fatal(err)
	}
	outboxtest.ConcurrentClaim(t, s, other, "first")
}
