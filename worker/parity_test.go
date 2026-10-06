package worker_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/galaxy-io/filament"
	"github.com/galaxy-io/filament/internal/convert"
	"github.com/galaxy-io/filament/registry"
	"github.com/galaxy-io/filament/worker"

	// The root module's connectors, so the parity check covers real specs,
	// aliases and every capability shape they declare.
	_ "github.com/galaxy-io/filament/connectors/clickhouse"
	_ "github.com/galaxy-io/filament/connectors/http"
	_ "github.com/galaxy-io/filament/connectors/hubspot"
	_ "github.com/galaxy-io/filament/connectors/kafka"
	_ "github.com/galaxy-io/filament/connectors/meilisearch"
	_ "github.com/galaxy-io/filament/connectors/mysql"
	_ "github.com/galaxy-io/filament/connectors/nats"
	_ "github.com/galaxy-io/filament/connectors/postgres"
	_ "github.com/galaxy-io/filament/connectors/sample"
	_ "github.com/galaxy-io/filament/connectors/stdout"
)

// TestRemoteMatchesLocal serves the local worker over HTTP and checks that
// the remote client reports every spec the local one does. Specs are compared
// in their wire form, which normalizes what the wire cannot carry: Struct
// defaults come back as JSON numbers and secret-typed fields are marked
// secret.
func TestRemoteMatchesLocal(t *testing.T) {
	local := worker.Local(registry.DefaultSources, registry.DefaultSinks)
	mux := http.NewServeMux()
	mux.Handle(worker.Handler(local))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	remote := worker.Remote(srv.URL)
	ctx := context.Background()

	want, err := local.Describe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := remote.Describe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != len(want.Sources) || len(got.Sinks) != len(want.Sinks) {
		t.Fatalf("remote lists %d sources and %d sinks, local %d and %d", len(got.Sources), len(got.Sinks), len(want.Sources), len(want.Sinks))
	}
	for i, spec := range want.Sources {
		if !proto.Equal(convert.SourceToProto(got.Sources[i]), convert.SourceToProto(spec)) {
			t.Errorf("source %q differs over the wire", spec.Name)
		}
		// Lookups by name resolve aliases to the concrete spec on both sides.
		localSpec, err := local.SourceSpec(ctx, spec.Name)
		if err != nil {
			t.Fatal(err)
		}
		remoteSpec, err := remote.SourceSpec(ctx, spec.Name)
		if err != nil {
			t.Fatalf("remote source %q: %v", spec.Name, err)
		}
		if !proto.Equal(convert.SourceToProto(remoteSpec), convert.SourceToProto(localSpec)) {
			t.Errorf("source lookup %q differs over the wire", spec.Name)
		}
	}
	for i, spec := range want.Sinks {
		if !proto.Equal(convert.SinkToProto(got.Sinks[i]), convert.SinkToProto(spec)) {
			t.Errorf("sink %q differs over the wire", spec.Name)
		}
	}

	// Sentinels survive the wire.
	if _, err := remote.SourceSpec(ctx, "no-such-source"); !errors.Is(err, filament.ErrNotFound) {
		t.Fatalf("unknown source: got %v, want ErrNotFound", err)
	}
	if err := remote.Validate(ctx, filament.ConnectorRef{Kind: filament.ConnectorKindSink, Name: "no-such-sink"}, filament.NewConfig(nil)); !errors.Is(err, filament.ErrNotFound) {
		t.Fatalf("unknown sink: got %v, want ErrNotFound", err)
	}
	if err := remote.TestConnection(ctx, filament.ConnectorRef{Kind: filament.ConnectorKindSource, Name: "sample"}, filament.NewConfig(nil)); !errors.Is(err, filament.ErrUnsupported) {
		t.Fatalf("source without a probe: got %v, want ErrUnsupported", err)
	}
	if _, err := remote.PlanReplicationStream(ctx, "sample", filament.ReplicationStreamPlanningRequest{Config: filament.NewConfig(nil)}); !errors.Is(err, filament.ErrUnsupported) {
		t.Fatalf("source without a planner: got %v, want ErrUnsupported", err)
	}
}
