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

	"github.com/galaxy-io/filament"
	catalogv1 "github.com/galaxy-io/filament/api/catalog/v1"
	"github.com/galaxy-io/filament/api/catalog/v1/catalogv1connect"
)

const (
	// describeTTL bounds how long specs are served without asking the host
	// again. They are fixed for a host build, so this only matters across a
	// host upgrade.
	describeTTL = 30 * time.Second
	// describeTimeout bounds a spec fetch, which has no caller deadline.
	describeTimeout = 5 * time.Second
)

// Remote returns a Catalog that forwards to the connector host at baseURL, so
// the caller links no driver. Specs and contracts are fetched once and
// cached; every other call is one RPC under the caller's deadline. A bare
// host:port, as some platforms inject, is taken as plain HTTP.
func Remote(baseURL string, opts ...connect.ClientOption) filament.Catalog {
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	return &remote{client: catalogv1connect.NewCatalogServiceClient(http.DefaultClient, baseURL, opts...)}
}

type remote struct {
	client catalogv1connect.CatalogServiceClient

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
// name's concrete spec and contracts. A name the listing lacks is asked for
// individually and added as it is looked up.
type snapshot struct {
	at          time.Time
	sources     []filament.ConnectorSpec
	sinks       []filament.SinkSpec
	sourceNames map[string]sourceEntry
	sinkNames   map[string]sinkEntry
}

type sourceEntry struct {
	spec      filament.ConnectorSpec
	contracts filament.SourceContracts
}

type sinkEntry struct {
	spec      filament.SinkSpec
	contracts filament.SinkContracts
}

// described returns the current snapshot, refreshing it when stale. A failed
// refresh keeps serving the last snapshot: the specs are still true of the
// host that comes back. Callers hold r.mu.
func (r *remote) described() (*snapshot, error) {
	if r.snapshot != nil && time.Since(r.snapshot.at) < describeTTL {
		return r.snapshot, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), describeTimeout)
	defer cancel()
	resp, err := r.client.Describe(ctx, connect.NewRequest(&catalogv1.DescribeRequest{}))
	if err != nil {
		if r.snapshot != nil {
			return r.snapshot, nil
		}
		return nil, fromConnect(err)
	}
	next := &snapshot{at: time.Now(), sourceNames: map[string]sourceEntry{}, sinkNames: map[string]sinkEntry{}}
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
	r.snapshot = next
	return next, nil
}

// describeOne asks the host for a name the listing does not carry.
func (r *remote) describeOne(kind catalogv1.Kind, name string) (*catalogv1.Connector, error) {
	ctx, cancel := context.WithTimeout(context.Background(), describeTimeout)
	defer cancel()
	resp, err := r.client.Describe(ctx, connect.NewRequest(&catalogv1.DescribeRequest{Kind: kind, Name: name}))
	if err != nil {
		return nil, fromConnect(err)
	}
	if len(resp.Msg.GetConnectors()) != 1 {
		return nil, fmt.Errorf("catalog: describe %q returned %d connectors", name, len(resp.Msg.GetConnectors()))
	}
	return resp.Msg.GetConnectors()[0], nil
}

func (r *remote) source(name string) (sourceEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, err := r.described()
	if err != nil {
		return sourceEntry{}, err
	}
	if entry, ok := snap.sourceNames[name]; ok {
		return entry, nil
	}
	connector, err := r.describeOne(catalogv1.Kind_KIND_SOURCE, name)
	if err != nil {
		return sourceEntry{}, err
	}
	entry, err := decodeSource(connector)
	if err != nil {
		return sourceEntry{}, err
	}
	snap.sourceNames[name] = entry
	return entry, nil
}

func (r *remote) sink(name string) (sinkEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, err := r.described()
	if err != nil {
		return sinkEntry{}, err
	}
	if entry, ok := snap.sinkNames[name]; ok {
		return entry, nil
	}
	connector, err := r.describeOne(catalogv1.Kind_KIND_SINK, name)
	if err != nil {
		return sinkEntry{}, err
	}
	entry, err := decodeSink(connector)
	if err != nil {
		return sinkEntry{}, err
	}
	snap.sinkNames[name] = entry
	return entry, nil
}

func (r *remote) SourceSpecs() ([]filament.ConnectorSpec, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, err := r.described()
	if err != nil {
		return nil, err
	}
	return snap.sources, nil
}

func (r *remote) SinkSpecs() ([]filament.SinkSpec, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, err := r.described()
	if err != nil {
		return nil, err
	}
	return snap.sinks, nil
}

func (r *remote) SourceSpec(name string) (filament.ConnectorSpec, error) {
	entry, err := r.source(name)
	return entry.spec, err
}

func (r *remote) SinkSpec(name string) (filament.SinkSpec, error) {
	entry, err := r.sink(name)
	return entry.spec, err
}

func (r *remote) SourceContracts(name string) (filament.SourceContracts, error) {
	entry, err := r.source(name)
	return entry.contracts, err
}

func (r *remote) SinkContracts(name string) (filament.SinkContracts, error) {
	entry, err := r.sink(name)
	return entry.contracts, err
}

func (r *remote) Replication(ctx context.Context, source string, cfg filament.Config) (filament.ReplicationMode, error) {
	raw, err := encodeConfig(cfg)
	if err != nil {
		return "", err
	}
	resp, err := r.client.Replication(ctx, connect.NewRequest(&catalogv1.ReplicationRequest{
		Queries: []*catalogv1.ReplicationQuery{{Source: source, ConfigJson: raw}},
	}))
	if err != nil {
		return "", fromConnect(err)
	}
	modes := resp.Msg.GetModes()
	if len(modes) != 1 || modes[0] == "" {
		return "", wireError{sentinel: filament.ErrNotFound, message: fmt.Sprintf("not found: source %q", source)}
	}
	return filament.ReplicationMode(modes[0]), nil
}

func (r *remote) PlanReplicationStream(ctx context.Context, source string, req filament.ReplicationStreamPlanningRequest) (filament.ReplicationStreamPlan, error) {
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
			PrimaryKeyErr: failure(resource.GetPrimaryKeyError()),
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
