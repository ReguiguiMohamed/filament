package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/sync/singleflight"

	"github.com/galaxy-io/filament"
	catalogv1 "github.com/galaxy-io/filament/api/catalog/v1"
	"github.com/galaxy-io/filament/api/catalog/v1/catalogv1connect"
)

const (
	// describeTTL bounds how long specs are served without asking the host
	// again. They are fixed for a host build, so this only matters across a
	// host upgrade.
	describeTTL = 30 * time.Second
	// describeRetry is how long a failed refresh keeps serving the last
	// snapshot before the host is asked again, so an outage costs one
	// timeout per interval rather than one per call.
	describeRetry = 5 * time.Second
	// describeTimeout bounds a spec fetch. The fetch is shared by every
	// waiting caller, so it runs detached from any one caller's deadline.
	describeTimeout = 5 * time.Second
	// callTimeout bounds a live call whose caller set no deadline, such as
	// the scheduler compiling on its process context.
	callTimeout = 30 * time.Second
)

// Remote returns a Catalog that forwards to the worker serving it at baseURL, so
// the caller links no driver. Specs and contracts are served from a cached
// listing that refreshes in the background; every other call is one RPC under
// the caller's deadline. A bare
// host:port, as some platforms inject, is taken as plain HTTP.
func Remote(baseURL string, opts ...connect.ClientOption) filament.Catalog {
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	return &remote{client: catalogv1connect.NewCatalogServiceClient(http.DefaultClient, baseURL, opts...)}
}

type remote struct {
	client catalogv1connect.CatalogServiceClient
	// flight shares one in-flight request among concurrent callers.
	flight singleflight.Group

	// mu guards the snapshot pointer and its name maps. It is never held
	// across a network call.
	mu       sync.Mutex
	snapshot *snapshot
}

var _ filament.Catalog = (*remote)(nil)

// wireError is a sentinel that crossed the wire: it matches the sentinel and
// keeps the host's own message.
type wireError struct {
	sentinel error
	message  string
}

func (e wireError) Error() string { return e.message }
func (e wireError) Unwrap() error { return e.sentinel }

// fromConnect is the inverse of ConnectError. Codes without a sentinel keep
// the Connect error, so its code survives back out through ConnectError.
func fromConnect(err error) error {
	var remote *connect.Error
	if !errors.As(err, &remote) {
		return err
	}
	switch remote.Code() {
	case connect.CodeNotFound:
		return wireError{sentinel: filament.ErrNotFound, message: remote.Message()}
	case connect.CodeUnimplemented:
		return wireError{sentinel: filament.ErrUnsupported, message: remote.Message()}
	case connect.CodeFailedPrecondition:
		return wireError{sentinel: filament.ErrConfigure, message: remote.Message()}
	default:
		return remote
	}
}

// snapshot is one Describe of the host: the listing as listed, plus each
// name's concrete spec and contracts. The listing never changes once
// published. The name maps grow as unlisted names are looked up, and are
// guarded by remote.mu.
type snapshot struct {
	at          time.Time
	sources     []filament.ConnectorSpec
	sinks       []filament.SinkSpec
	sourceNames map[string]sourceEntry
	sinkNames   map[string]sinkEntry
	missing     map[string]error
}

type sourceEntry struct {
	spec      filament.ConnectorSpec
	contracts filament.SourceContracts
}

type sinkEntry struct {
	spec      filament.SinkSpec
	contracts filament.SinkContracts
}

// shared runs fn once for every concurrent caller of key and waits for it
// under ctx. The call itself is detached, so one caller giving up does not
// fail the others, and no lock is held across the network.
func (r *remote) shared(ctx context.Context, key string, fn func(context.Context) (any, error)) (any, error) {
	result := r.flight.DoChan(key, func() (any, error) {
		callCtx, cancel := context.WithTimeout(context.Background(), describeTimeout)
		defer cancel()
		return fn(callCtx)
	})
	select {
	case res := <-result:
		return res.Val, res.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// described returns the current snapshot. A fresh one is returned as is. A
// stale one is returned too, with a refresh started behind it, so lookups
// never wait on the worker once a first snapshot exists. Only the very first
// lookup waits, and concurrent first lookups share one request.
func (r *remote) described(ctx context.Context) (*snapshot, error) {
	r.mu.Lock()
	snap := r.snapshot
	fresh := snap != nil && time.Since(snap.at) < describeTTL
	r.mu.Unlock()
	if fresh {
		return snap, nil
	}
	if snap != nil {
		r.flight.DoChan("describe", func() (any, error) {
			callCtx, cancel := context.WithTimeout(context.Background(), describeTimeout)
			defer cancel()
			return r.refresh(callCtx)
		})
		return snap, nil
	}
	fetched, err := r.shared(ctx, "describe", r.refresh)
	if err != nil {
		return nil, err
	}
	return fetched.(*snapshot), nil
}

// refresh fetches the listing and publishes it. A failed refresh keeps the
// last snapshot and backs off before the worker is asked again: the specs
// are still true of the worker that comes back.
func (r *remote) refresh(ctx context.Context) (any, error) {
	resp, err := r.client.Describe(ctx, connect.NewRequest(&catalogv1.DescribeRequest{}))
	if err != nil {
		r.mu.Lock()
		if r.snapshot != nil {
			r.snapshot.at = time.Now().Add(describeRetry - describeTTL)
		}
		r.mu.Unlock()
		return nil, fromConnect(err)
	}
	next := &snapshot{at: time.Now(), sourceNames: map[string]sourceEntry{}, sinkNames: map[string]sinkEntry{}, missing: map[string]error{}}
	listed := map[string]sourceEntry{}
	for _, connector := range resp.Msg.GetConnectors() {
		switch connector.GetKind() {
		case catalogv1.Kind_KIND_SOURCE:
			entry, err := decodeSource(connector)
			if err != nil {
				return nil, err
			}
			next.sources = append(next.sources, entry.spec)
			listed[connector.GetName()] = entry
		case catalogv1.Kind_KIND_SINK:
			entry, err := decodeSink(connector)
			if err != nil {
				return nil, err
			}
			next.sinks = append(next.sinks, entry.spec)
			next.sinkNames[connector.GetName()] = entry
		}
	}
	// A lookup by name answers with the concrete spec, so an alias entry
	// points at its target's.
	for name, entry := range listed {
		if target, ok := listed[entry.spec.AliasTarget]; ok {
			entry = target
		}
		next.sourceNames[name] = entry
	}
	r.mu.Lock()
	r.snapshot = next
	r.mu.Unlock()
	return next, nil
}

// lookup answers one name from the snapshot. A name the listing lacks is
// asked for individually, outside the lock, and remembered either way for
// the snapshot's life.
func lookup[E any](ctx context.Context, r *remote, kind catalogv1.Kind, name string, names func(*snapshot) map[string]E, decode func(*catalogv1.Connector) (E, error)) (E, error) {
	var zero E
	snap, err := r.described(ctx)
	if err != nil {
		return zero, err
	}
	key := kind.String() + "\x00" + name
	r.mu.Lock()
	entry, ok := names(snap)[name]
	missing, missed := snap.missing[key]
	r.mu.Unlock()
	if ok {
		return entry, nil
	}
	if missed {
		return zero, missing
	}
	fetched, err := r.shared(ctx, key, func(ctx context.Context) (any, error) {
		resp, err := r.client.Describe(ctx, connect.NewRequest(&catalogv1.DescribeRequest{Kind: kind, Name: name}))
		if err != nil {
			return nil, fromConnect(err)
		}
		if len(resp.Msg.GetConnectors()) != 1 {
			return nil, fmt.Errorf("catalog: describe %q returned %d connectors", name, len(resp.Msg.GetConnectors()))
		}
		return decode(resp.Msg.GetConnectors()[0])
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	if errors.Is(err, filament.ErrNotFound) {
		snap.missing[key] = err
	}
	if err != nil {
		return zero, err
	}
	entry = fetched.(E)
	names(snap)[name] = entry
	return entry, nil
}

func (r *remote) source(ctx context.Context, name string) (sourceEntry, error) {
	return lookup(ctx, r, catalogv1.Kind_KIND_SOURCE, name, func(s *snapshot) map[string]sourceEntry { return s.sourceNames }, decodeSource)
}

func (r *remote) sink(ctx context.Context, name string) (sinkEntry, error) {
	return lookup(ctx, r, catalogv1.Kind_KIND_SINK, name, func(s *snapshot) map[string]sinkEntry { return s.sinkNames }, decodeSink)
}

func (r *remote) SourceSpecs(ctx context.Context) ([]filament.ConnectorSpec, error) {
	snap, err := r.described(ctx)
	if err != nil {
		return nil, err
	}
	return snap.sources, nil
}

func (r *remote) SinkSpecs(ctx context.Context) ([]filament.SinkSpec, error) {
	snap, err := r.described(ctx)
	if err != nil {
		return nil, err
	}
	return snap.sinks, nil
}

func (r *remote) SourceSpec(ctx context.Context, name string) (filament.ConnectorSpec, error) {
	entry, err := r.source(ctx, name)
	return entry.spec, err
}

func (r *remote) SinkSpec(ctx context.Context, name string) (filament.SinkSpec, error) {
	entry, err := r.sink(ctx, name)
	return entry.spec, err
}

func (r *remote) SourceContracts(ctx context.Context, name string) (filament.SourceContracts, error) {
	entry, err := r.source(ctx, name)
	return entry.contracts, err
}

func (r *remote) SinkContracts(ctx context.Context, name string) (filament.SinkContracts, error) {
	entry, err := r.sink(ctx, name)
	return entry.contracts, err
}

// bounded adds callTimeout to a context that carries no deadline of its own.
func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, callTimeout)
}

func (r *remote) PlanReplicationStream(ctx context.Context, source string, req filament.ReplicationStreamPlanningRequest) (filament.ReplicationStreamPlan, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	var plan filament.ReplicationStreamPlan
	raw, err := encodeConfig(req.Config)
	if err != nil {
		return plan, err
	}
	resp, err := r.client.PlanReplicationStream(ctx, connect.NewRequest(&catalogv1.PlanReplicationStreamRequest{
		Source: source, ReplicationStreamId: req.ReplicationStreamID, SourceConnectionId: req.SourceConnectionID,
		ConfigJson: raw, Resources: req.Resources,
	}))
	if err != nil {
		return plan, fromConnect(err)
	}
	err = json.Unmarshal(resp.Msg.GetPlanJson(), &plan)
	return plan, err
}

func (r *remote) Validate(ctx context.Context, kind filament.ConnectorKind, name string, cfg filament.Config) error {
	ctx, cancel := bounded(ctx)
	defer cancel()
	raw, err := encodeConfig(cfg)
	if err != nil {
		return err
	}
	resp, err := r.client.Validate(ctx, connect.NewRequest(&catalogv1.ValidateRequest{Kind: kindToProto(kind), Name: name, ConfigJson: raw}))
	if err != nil {
		return fromConnect(err)
	}
	return failure(resp.Msg.GetFailure())
}

func (r *remote) TestConnection(ctx context.Context, kind filament.ConnectorKind, name string, cfg filament.Config) error {
	ctx, cancel := bounded(ctx)
	defer cancel()
	raw, err := encodeConfig(cfg)
	if err != nil {
		return err
	}
	resp, err := r.client.TestConnection(ctx, connect.NewRequest(&catalogv1.TestConnectionRequest{Kind: kindToProto(kind), Name: name, ConfigJson: raw}))
	if err != nil {
		return fromConnect(err)
	}
	return failure(resp.Msg.GetFailure())
}

func (r *remote) Discover(ctx context.Context, source string, cfg filament.Config, opts filament.DiscoverOpts) (filament.DiscoverResult, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	var result filament.DiscoverResult
	raw, err := encodeConfig(cfg)
	if err != nil {
		return result, err
	}
	resp, err := r.client.Discover(ctx, connect.NewRequest(&catalogv1.DiscoverRequest{Source: source, ConfigJson: raw, Refresh: opts.Refresh}))
	if err != nil {
		return result, fromConnect(err)
	}
	err = json.Unmarshal(resp.Msg.GetResultJson(), &result)
	return result, err
}

func (r *remote) Inspect(ctx context.Context, source string, cfg filament.Config, resources []string) ([]filament.ResourceInspection, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	raw, err := encodeConfig(cfg)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Inspect(ctx, connect.NewRequest(&catalogv1.InspectRequest{Source: source, ConfigJson: raw, Resources: resources}))
	if err != nil {
		return nil, fromConnect(err)
	}
	out := make([]filament.ResourceInspection, 0, len(resp.Msg.GetResources()))
	for _, resource := range resp.Msg.GetResources() {
		inspection := filament.ResourceInspection{
			Name: resource.GetName(), PrimaryKey: resource.GetPrimaryKey(), Ranked: resource.GetRanked(),
			ManagedIncremental: resource.GetManagedIncremental(),
			PrimaryKeyErr:      failure(resource.GetPrimaryKeyError()),
		}
		switch {
		case resource.GetColumnsUnsupported():
			inspection.ColumnsErr = wireError{sentinel: filament.ErrUnsupported, message: resource.GetColumnsError()}
		case resource.GetColumnsError() != "":
			inspection.ColumnsErr = errors.New(resource.GetColumnsError())
		}
		if err := json.Unmarshal(resource.GetColumnsJson(), &inspection.Columns); err != nil {
			return nil, err
		}
		out = append(out, inspection)
	}
	return out, nil
}
