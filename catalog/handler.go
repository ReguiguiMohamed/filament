package catalog

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/galaxy-io/filament"
	catalogv1 "github.com/galaxy-io/filament/api/catalog/v1"
	"github.com/galaxy-io/filament/api/catalog/v1/catalogv1connect"
	ingestionv1 "github.com/galaxy-io/filament/api/ingestion/v1"
)

// Handler serves a Catalog over ConnectRPC. A worker mounts it so the
// control binaries can reach the drivers it carries.
func Handler(catalog filament.Catalog, opts ...connect.HandlerOption) (string, http.Handler) {
	return catalogv1connect.NewCatalogServiceHandler(handler{catalog: catalog}, opts...)
}

type handler struct {
	catalog filament.Catalog
}

// ConnectError maps a Catalog failure onto the Connect code its sentinel
// implies: unknown connector, missing contract, or refused config. Anything
// else is a live failure.
func ConnectError(err error) *connect.Error {
	switch {
	case errors.Is(err, filament.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, filament.ErrUnsupported):
		return connect.NewError(connect.CodeUnimplemented, err)
	case errors.Is(err, filament.ErrConfigure):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	// A failed remote call keeps its own code, so a down host reads as
	// unavailable rather than internal.
	var remote *connect.Error
	if errors.As(err, &remote) {
		return remote
	}
	return connect.NewError(connect.CodeInternal, err)
}

var errKindRequired = connect.NewError(connect.CodeInvalidArgument, errors.New("connector kind is required"))

func (h handler) Describe(ctx context.Context, req *connect.Request[catalogv1.DescribeRequest]) (*connect.Response[catalogv1.DescribeResponse], error) {
	kind, name := req.Msg.GetKind(), req.Msg.GetName()
	resp := &catalogv1.DescribeResponse{}
	if name != "" {
		var connector *catalogv1.Connector
		var err error
		switch kind {
		case ingestionv1.ConnectorKind_CONNECTOR_KIND_SOURCE:
			connector, err = h.source(ctx, name)
		case ingestionv1.ConnectorKind_CONNECTOR_KIND_SINK:
			connector, err = h.sink(ctx, name)
		default:
			return nil, errKindRequired
		}
		if err != nil {
			return nil, ConnectError(err)
		}
		resp.Connectors = append(resp.Connectors, connector)
		return connect.NewResponse(resp), nil
	}
	if kind != ingestionv1.ConnectorKind_CONNECTOR_KIND_SINK {
		specs, err := h.catalog.SourceSpecs(ctx)
		if err != nil {
			return nil, ConnectError(err)
		}
		// Listed specs are sent as listed: an alias entry keeps its lookup
		// name and target instead of resolving to the concrete spec.
		for _, spec := range specs {
			connector, err := h.sourceConnector(ctx, spec)
			if err != nil {
				return nil, ConnectError(err)
			}
			resp.Connectors = append(resp.Connectors, connector)
		}
	}
	if kind != ingestionv1.ConnectorKind_CONNECTOR_KIND_SOURCE {
		specs, err := h.catalog.SinkSpecs(ctx)
		if err != nil {
			return nil, ConnectError(err)
		}
		for _, spec := range specs {
			connector, err := h.sinkConnector(ctx, spec)
			if err != nil {
				return nil, ConnectError(err)
			}
			resp.Connectors = append(resp.Connectors, connector)
		}
	}
	return connect.NewResponse(resp), nil
}

// source describes one source by lookup name, resolving an alias to its
// concrete spec.
func (h handler) source(ctx context.Context, name string) (*catalogv1.Connector, error) {
	spec, err := h.catalog.SourceSpec(ctx, name)
	if err != nil {
		return nil, err
	}
	return h.sourceConnector(ctx, spec)
}

func (h handler) sink(ctx context.Context, name string) (*catalogv1.Connector, error) {
	spec, err := h.catalog.SinkSpec(ctx, name)
	if err != nil {
		return nil, err
	}
	return h.sinkConnector(ctx, spec)
}

func (h handler) sourceConnector(ctx context.Context, spec filament.ConnectorSpec) (*catalogv1.Connector, error) {
	contracts, err := h.catalog.SourceContracts(ctx, spec.Name)
	if err != nil {
		return nil, err
	}
	wire, err := sourceSpecToProto(spec)
	if err != nil {
		return nil, err
	}
	return &catalogv1.Connector{
		Kind: ingestionv1.ConnectorKind_CONNECTOR_KIND_SOURCE, Name: spec.Name, Spec: &catalogv1.Connector_Source{Source: wire},
		PlansStreams: contracts.PlansStreams, Streams: contracts.Streams,
	}, nil
}

func (h handler) sinkConnector(ctx context.Context, spec filament.SinkSpec) (*catalogv1.Connector, error) {
	contracts, err := h.catalog.SinkContracts(ctx, spec.Name)
	if err != nil {
		return nil, err
	}
	wire, err := sinkSpecToProto(spec)
	if err != nil {
		return nil, err
	}
	return &catalogv1.Connector{Kind: ingestionv1.ConnectorKind_CONNECTOR_KIND_SINK, Name: spec.Name, Spec: &catalogv1.Connector_Sink{Sink: wire}, Streams: contracts.Streams}, nil
}

func (h handler) PlanReplicationStream(ctx context.Context, req *connect.Request[catalogv1.PlanReplicationStreamRequest]) (*connect.Response[catalogv1.PlanReplicationStreamResponse], error) {
	plan, err := h.catalog.PlanReplicationStream(ctx, req.Msg.GetSource(), filament.ReplicationStreamPlanningRequest{
		ReplicationStreamID: req.Msg.GetReplicationStreamId(),
		SourceConnectionID:  req.Msg.GetSourceConnectionId(),
		Config:              configFromProto(req.Msg.GetConfig()),
		Resources:           req.Msg.GetResources(),
	})
	if err != nil {
		return nil, ConnectError(err)
	}
	wire, err := planToProto(plan)
	if err != nil {
		return nil, ConnectError(err)
	}
	return connect.NewResponse(&catalogv1.PlanReplicationStreamResponse{Plan: wire}), nil
}

func (h handler) Validate(ctx context.Context, req *connect.Request[catalogv1.ValidateRequest]) (*connect.Response[catalogv1.ValidateResponse], error) {
	kind := kindFromProto(req.Msg.GetKind())
	if kind == filament.ConnectorKindUnspecified {
		return nil, errKindRequired
	}
	failure, err := Verdict(h.catalog.Validate(ctx, kind, req.Msg.GetName(), configFromProto(req.Msg.GetConfig())))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&catalogv1.ValidateResponse{Failure: failure}), nil
}

func (h handler) TestConnection(ctx context.Context, req *connect.Request[catalogv1.TestConnectionRequest]) (*connect.Response[catalogv1.TestConnectionResponse], error) {
	kind := kindFromProto(req.Msg.GetKind())
	if kind == filament.ConnectorKindUnspecified {
		return nil, errKindRequired
	}
	failure, err := Verdict(h.catalog.TestConnection(ctx, kind, req.Msg.GetName(), configFromProto(req.Msg.GetConfig())))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&catalogv1.TestConnectionResponse{Failure: failure}), nil
}

// Verdict splits a Validate or TestConnection result into what the connector
// said about the config and a call that never got an answer. An unknown
// connector, a host without the route, and a failed remote call are errors;
// anything else is the connector's own verdict, empty when it accepted.
func Verdict(err error) (string, error) {
	var remote *connect.Error
	switch {
	case err == nil:
		return "", nil
	case errors.Is(err, filament.ErrNotFound), errors.Is(err, filament.ErrUnsupported), errors.As(err, &remote):
		return "", ConnectError(err)
	default:
		return err.Error(), nil
	}
}

func (h handler) Discover(ctx context.Context, req *connect.Request[catalogv1.DiscoverRequest]) (*connect.Response[catalogv1.DiscoverResponse], error) {
	resources, err := h.catalog.Discover(ctx, req.Msg.GetSource(), configFromProto(req.Msg.GetConfig()), filament.DiscoverOpts{Refresh: req.Msg.GetRefresh()})
	if err != nil {
		return nil, ConnectError(err)
	}
	return connect.NewResponse(&catalogv1.DiscoverResponse{Resources: resources}), nil
}

func (h handler) Inspect(ctx context.Context, req *connect.Request[catalogv1.InspectRequest]) (*connect.Response[catalogv1.InspectResponse], error) {
	inspections, err := h.catalog.Inspect(ctx, req.Msg.GetSource(), configFromProto(req.Msg.GetConfig()), req.Msg.GetResources())
	if err != nil {
		return nil, ConnectError(err)
	}
	resp := &catalogv1.InspectResponse{Resources: make([]*catalogv1.ResourceInspection, 0, len(inspections))}
	for _, inspection := range inspections {
		out := &catalogv1.ResourceInspection{
			Name: inspection.Name, PrimaryKey: inspection.PrimaryKey, Columns: inspection.Columns,
			Ranked: inspection.Ranked, ManagedIncremental: inspection.ManagedIncremental,
		}
		if err := inspection.PrimaryKeyErr; err != nil {
			out.PrimaryKeyError = err.Error()
		}
		if err := inspection.ColumnsErr; err != nil {
			out.ColumnsError = err.Error()
			out.ColumnsUnsupported = errors.Is(err, filament.ErrUnsupported)
		}
		resp.Resources = append(resp.Resources, out)
	}
	return connect.NewResponse(resp), nil
}
