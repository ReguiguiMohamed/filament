// Conversions between the catalog.v1 wire format and the Go types both sides
// of the service share. They are faithful: a value that crosses comes back
// as it left, except that open maps carry JSON's value model.
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/galaxy-io/filament"
	catalogv1 "github.com/galaxy-io/filament/api/catalog/v1"
	ingestionv1 "github.com/galaxy-io/filament/api/ingestion/v1"
)

// failure turns a verdict field back into an error. Empty is success.
func failure(message string) error {
	if message == "" {
		return nil
	}
	return errors.New(message)
}

// structOf carries an open map as a Struct. Connector maps hold any
// JSON-encodable value (string slices, tagged structs), which structpb only
// accepts once they are in JSON's value model.
func structOf(values map[string]any) (*structpb.Struct, error) {
	if values == nil {
		return nil, nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, err
	}
	return structpb.NewStruct(normalized)
}

func mapOf(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

// valueOf is structOf for a single value.
func valueOf(value any) (*structpb.Value, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, err
	}
	return structpb.NewValue(normalized)
}

func valueFrom(value *structpb.Value) any {
	if value == nil {
		return nil
	}
	return value.AsInterface()
}

func configToProto(cfg filament.Config) (*structpb.Struct, error) {
	if cfg == nil {
		return nil, nil
	}
	return structOf(cfg.Raw())
}

func configFromProto(s *structpb.Struct) filament.Config {
	return filament.NewConfig(mapOf(s))
}

func decodeSource(connector *catalogv1.Connector) (sourceEntry, error) {
	spec, err := sourceSpecFromProto(connector.GetSource())
	if err != nil {
		return sourceEntry{}, fmt.Errorf("catalog: source %q: %w", connector.GetName(), err)
	}
	return sourceEntry{spec: spec, contracts: filament.SourceContracts{PlansStreams: connector.GetPlansStreams(), Streams: connector.GetStreams()}}, nil
}

func decodeSink(connector *catalogv1.Connector) (sinkEntry, error) {
	spec, err := sinkSpecFromProto(connector.GetSink())
	if err != nil {
		return sinkEntry{}, fmt.Errorf("catalog: sink %q: %w", connector.GetName(), err)
	}
	return sinkEntry{spec: spec, contracts: filament.SinkContracts{Streams: connector.GetStreams()}}, nil
}

func kindToProto(kind filament.ConnectorKind) catalogv1.Kind {
	switch kind {
	case filament.ConnectorKindSource:
		return catalogv1.Kind_KIND_SOURCE
	case filament.ConnectorKindSink:
		return catalogv1.Kind_KIND_SINK
	default:
		return catalogv1.Kind_KIND_UNSPECIFIED
	}
}

func kindFromProto(kind catalogv1.Kind) filament.ConnectorKind {
	switch kind {
	case catalogv1.Kind_KIND_SOURCE:
		return filament.ConnectorKindSource
	case catalogv1.Kind_KIND_SINK:
		return filament.ConnectorKindSink
	default:
		return filament.ConnectorKindUnspecified
	}
}

func sourceSpecToProto(spec filament.ConnectorSpec) (*catalogv1.SourceSpec, error) {
	config, err := configSchemaToProto(spec.Config)
	if err != nil {
		return nil, err
	}
	out := &catalogv1.SourceSpec{
		Name: spec.Name, AliasTarget: spec.AliasTarget, DisplayName: spec.DisplayName, Description: spec.Description,
		DarkLogoUrl: spec.DarkLogoURL, LightLogoUrl: spec.LightLogoURL, Version: spec.Version, ApiVersion: spec.APIVersion,
		Maturity: string(spec.Maturity), Config: config,
		Resources: &catalogv1.ResourceCapabilities{Discoverable: spec.Resources.Discoverable, PerResourceCursor: spec.Resources.PerResourceCursor},
	}
	for _, mode := range spec.Modes {
		out.Modes = append(out.Modes, readModeToProto(mode))
	}
	for _, policy := range spec.SourcePolicies {
		out.SourcePolicies = append(out.SourcePolicies, &catalogv1.SourcePolicy{
			Mode: readModeToProto(policy.Mode), EmitsOps: operationsToProto(policy.EmitsOps),
			Ordered: policy.Ordered, Checkpointing: string(policy.Checkpointing),
		})
	}
	if stream := spec.Stream; stream != nil {
		out.Stream = &catalogv1.StreamCapabilities{
			EmitsOps: operationsToProto(stream.EmitsOps), Input: string(stream.Input),
			Ordering: stringsOf(stream.Ordering), Delivery: string(stream.Delivery),
		}
	}
	return out, nil
}

func sourceSpecFromProto(spec *catalogv1.SourceSpec) (filament.ConnectorSpec, error) {
	if spec == nil {
		return filament.ConnectorSpec{}, errors.New("missing source spec")
	}
	config, err := configSchemaFromProto(spec.GetConfig())
	if err != nil {
		return filament.ConnectorSpec{}, err
	}
	out := filament.ConnectorSpec{
		Name: spec.GetName(), AliasTarget: spec.GetAliasTarget(), DisplayName: spec.GetDisplayName(), Description: spec.GetDescription(),
		DarkLogoURL: spec.GetDarkLogoUrl(), LightLogoURL: spec.GetLightLogoUrl(), Version: spec.GetVersion(), APIVersion: spec.GetApiVersion(),
		Maturity: filament.ConnectorMaturity(spec.GetMaturity()), Config: config,
		Resources: filament.ResourceCapabilities{Discoverable: spec.GetResources().GetDiscoverable(), PerResourceCursor: spec.GetResources().GetPerResourceCursor()},
	}
	for _, mode := range spec.GetModes() {
		m, err := readModeFromProto(mode)
		if err != nil {
			return filament.ConnectorSpec{}, err
		}
		out.Modes = append(out.Modes, m)
	}
	for _, policy := range spec.GetSourcePolicies() {
		mode, err := readModeFromProto(policy.GetMode())
		if err != nil {
			return filament.ConnectorSpec{}, err
		}
		ops, err := operationsFromProto(policy.GetEmitsOps())
		if err != nil {
			return filament.ConnectorSpec{}, err
		}
		out.SourcePolicies = append(out.SourcePolicies, filament.SourcePolicy{
			Mode: mode, EmitsOps: ops, Ordered: policy.GetOrdered(), Checkpointing: filament.CheckpointPolicy(policy.GetCheckpointing()),
		})
	}
	if stream := spec.GetStream(); stream != nil {
		ops, err := operationsFromProto(stream.GetEmitsOps())
		if err != nil {
			return filament.ConnectorSpec{}, err
		}
		out.Stream = &filament.StreamCapabilities{
			EmitsOps: ops, Input: filament.InputSemantics(stream.GetInput()),
			Ordering: typedStrings[filament.Ordering](stream.GetOrdering()), Delivery: filament.DeliveryGuarantee(stream.GetDelivery()),
		}
	}
	return out, nil
}

func sinkSpecToProto(spec filament.SinkSpec) (*catalogv1.SinkSpec, error) {
	config, err := configSchemaToProto(spec.Config)
	if err != nil {
		return nil, err
	}
	caps := spec.Capabilities
	out := &catalogv1.SinkSpec{
		Name: spec.Name, DisplayName: spec.DisplayName, Description: spec.Description,
		DarkLogoUrl: spec.DarkLogoURL, LightLogoUrl: spec.LightLogoURL, Version: spec.Version,
		Maturity: string(spec.Maturity), Config: config, SchemaField: spec.SchemaField,
		Capabilities: &catalogv1.SinkCapabilities{
			Transactional: caps.Transactional, Schematized: caps.Schematized, EncodedIntegrity: caps.EncodedIntegrity,
			WritePolicies:      writePoliciesToProto(caps.WritePolicies),
			PreferredBatchRows: int64(caps.PreferredBatchRows), PreferredBatchBytes: caps.PreferredBatchBytes,
			PreferredFlushInterval: durationpb.New(caps.PreferredFlushInterval),
		},
	}
	if stream := caps.Stream; stream != nil {
		out.Capabilities.Stream = &catalogv1.StreamingSinkCapabilities{
			WritePolicies: writePoliciesToProto(stream.WritePolicies),
			OwnerFencing:  stream.OwnerFencing, IsolatedEpochs: stream.IsolatedEpochs,
		}
		if bound := stream.InFlightBound; bound != nil {
			out.Capabilities.Stream.InFlightBound = durationpb.New(*bound)
		}
	}
	return out, nil
}

func sinkSpecFromProto(spec *catalogv1.SinkSpec) (filament.SinkSpec, error) {
	if spec == nil {
		return filament.SinkSpec{}, errors.New("missing sink spec")
	}
	config, err := configSchemaFromProto(spec.GetConfig())
	if err != nil {
		return filament.SinkSpec{}, err
	}
	caps := spec.GetCapabilities()
	policies, err := writePoliciesFromProto(caps.GetWritePolicies())
	if err != nil {
		return filament.SinkSpec{}, err
	}
	out := filament.SinkSpec{
		Name: spec.GetName(), DisplayName: spec.GetDisplayName(), Description: spec.GetDescription(),
		DarkLogoURL: spec.GetDarkLogoUrl(), LightLogoURL: spec.GetLightLogoUrl(), Version: spec.GetVersion(),
		Maturity: filament.ConnectorMaturity(spec.GetMaturity()), Config: config, SchemaField: spec.GetSchemaField(),
		Capabilities: filament.SinkCapabilities{
			Transactional: caps.GetTransactional(), Schematized: caps.GetSchematized(), EncodedIntegrity: caps.GetEncodedIntegrity(),
			WritePolicies:      policies,
			PreferredBatchRows: int(caps.GetPreferredBatchRows()), PreferredBatchBytes: caps.GetPreferredBatchBytes(),
			PreferredFlushInterval: caps.GetPreferredFlushInterval().AsDuration(),
		},
	}
	if stream := caps.GetStream(); stream != nil {
		policies, err := writePoliciesFromProto(stream.GetWritePolicies())
		if err != nil {
			return filament.SinkSpec{}, err
		}
		out.Capabilities.Stream = &filament.StreamingSinkCapabilities{
			WritePolicies: policies, OwnerFencing: stream.GetOwnerFencing(), IsolatedEpochs: stream.GetIsolatedEpochs(),
		}
		if bound := stream.GetInFlightBound(); bound != nil {
			d := bound.AsDuration()
			out.Capabilities.Stream.InFlightBound = &d
		}
	}
	return out, nil
}

func writePoliciesToProto(policies []filament.WritePolicyCapability) []*catalogv1.WritePolicyCapability {
	out := make([]*catalogv1.WritePolicyCapability, 0, len(policies))
	for _, policy := range policies {
		out = append(out, &catalogv1.WritePolicyCapability{
			Mode: string(policy.Mode), RequiresPk: policy.RequiresPK, RequiresOrder: policy.RequiresOrder,
			AcceptsOps: operationsToProto(policy.AcceptsOps),
			Atomicity:  string(policy.Atomicity), Durability: string(policy.Durability),
		})
	}
	return out
}

func writePoliciesFromProto(policies []*catalogv1.WritePolicyCapability) ([]filament.WritePolicyCapability, error) {
	var out []filament.WritePolicyCapability
	for _, policy := range policies {
		ops, err := operationsFromProto(policy.GetAcceptsOps())
		if err != nil {
			return nil, err
		}
		out = append(out, filament.WritePolicyCapability{
			Mode: filament.WriteMode(policy.GetMode()), RequiresPK: policy.GetRequiresPk(), RequiresOrder: policy.GetRequiresOrder(),
			AcceptsOps: ops,
			Atomicity:  filament.WriteAtomicity(policy.GetAtomicity()), Durability: filament.WriteDurability(policy.GetDurability()),
		})
	}
	return out, nil
}

// configSchemaToProto carries a schema as declared. It differs from the
// API's presentation, which marks every secret-typed field secret.
func configSchemaToProto(schema filament.ConfigSchema) (*ingestionv1.ConfigSchema, error) {
	fields, err := configFieldsToProto(schema.Fields)
	if err != nil {
		return nil, err
	}
	return &ingestionv1.ConfigSchema{Fields: fields}, nil
}

func configFieldsToProto(fields []filament.ConfigField) ([]*ingestionv1.ConfigField, error) {
	var out []*ingestionv1.ConfigField
	for _, field := range fields {
		def, err := valueOf(field.Default)
		if err != nil {
			return nil, fmt.Errorf("config field %q default: %w", field.Name, err)
		}
		nested, err := configFieldsToProto(field.Fields)
		if err != nil {
			return nil, err
		}
		converted := &ingestionv1.ConfigField{
			Name: field.Name, Type: fieldTypeToProto(field.Type), Required: field.Required, Default: def,
			Help: field.Help, Scope: fieldScopeToProto(field.Scope), Secret: field.Secret, Fields: nested,
		}
		for _, option := range field.Enum {
			converted.Enum = append(converted.Enum, &ingestionv1.EnumOption{Value: option.Value, Label: option.Label})
		}
		if condition := field.VisibleWhen; condition != nil {
			converted.VisibleWhen = &ingestionv1.FieldCondition{Field: condition.Field, Values: condition.Values}
		}
		out = append(out, converted)
	}
	return out, nil
}

func configSchemaFromProto(schema *ingestionv1.ConfigSchema) (filament.ConfigSchema, error) {
	fields, err := configFieldsFromProto(schema.GetFields())
	return filament.ConfigSchema{Fields: fields}, err
}

func configFieldsFromProto(fields []*ingestionv1.ConfigField) ([]filament.ConfigField, error) {
	var out []filament.ConfigField
	for _, field := range fields {
		fieldType, err := fieldTypeFromProto(field.GetType())
		if err != nil {
			return nil, fmt.Errorf("config field %q: %w", field.GetName(), err)
		}
		nested, err := configFieldsFromProto(field.GetFields())
		if err != nil {
			return nil, err
		}
		converted := filament.ConfigField{
			Name: field.GetName(), Type: fieldType, Required: field.GetRequired(), Default: valueFrom(field.GetDefault()),
			Help: field.GetHelp(), Scope: fieldScopeFromProto(field.GetScope()), Secret: field.GetSecret(), Fields: nested,
		}
		for _, option := range field.GetEnum() {
			converted.Enum = append(converted.Enum, filament.EnumOption{Value: option.GetValue(), Label: option.GetLabel()})
		}
		if condition := field.GetVisibleWhen(); condition != nil {
			converted.VisibleWhen = &filament.FieldCondition{Field: condition.GetField(), Values: condition.GetValues()}
		}
		out = append(out, converted)
	}
	return out, nil
}

func planToProto(plan filament.ReplicationStreamPlan) (*catalogv1.ReplicationStreamPlan, error) {
	consumer, err := structOf(plan.ConsumerConfig)
	if err != nil {
		return nil, fmt.Errorf("consumer config: %w", err)
	}
	continuity, err := structOf(plan.ContinuityConfig)
	if err != nil {
		return nil, fmt.Errorf("continuity config: %w", err)
	}
	return &catalogv1.ReplicationStreamPlan{
		Resources: plan.Resources, ConsumerName: plan.ConsumerName,
		ConsumerConfig: consumer, ContinuityConfig: continuity,
	}, nil
}

func planFromProto(plan *catalogv1.ReplicationStreamPlan) filament.ReplicationStreamPlan {
	return filament.ReplicationStreamPlan{
		Resources: plan.GetResources(), ConsumerName: plan.GetConsumerName(),
		ConsumerConfig: mapOf(plan.GetConsumerConfig()), ContinuityConfig: mapOf(plan.GetContinuityConfig()),
	}
}

// Go enums count from zero; their proto enums reserve zero for unspecified,
// so every value is mapped by name rather than by number.

func readModeToProto(mode filament.ReadMode) catalogv1.ReadMode {
	switch mode {
	case filament.ModeFull:
		return catalogv1.ReadMode_READ_MODE_FULL
	case filament.ModeIncremental:
		return catalogv1.ReadMode_READ_MODE_INCREMENTAL
	case filament.ModeCDC:
		return catalogv1.ReadMode_READ_MODE_CDC
	default:
		return catalogv1.ReadMode_READ_MODE_UNSPECIFIED
	}
}

func readModeFromProto(mode catalogv1.ReadMode) (filament.ReadMode, error) {
	switch mode {
	case catalogv1.ReadMode_READ_MODE_FULL:
		return filament.ModeFull, nil
	case catalogv1.ReadMode_READ_MODE_INCREMENTAL:
		return filament.ModeIncremental, nil
	case catalogv1.ReadMode_READ_MODE_CDC:
		return filament.ModeCDC, nil
	default:
		return 0, fmt.Errorf("unknown read mode %v", mode)
	}
}

func operationsToProto(ops []filament.Operation) []catalogv1.Operation {
	var out []catalogv1.Operation
	for _, op := range ops {
		switch op {
		case filament.OpInsert:
			out = append(out, catalogv1.Operation_OPERATION_INSERT)
		case filament.OpUpdate:
			out = append(out, catalogv1.Operation_OPERATION_UPDATE)
		case filament.OpDelete:
			out = append(out, catalogv1.Operation_OPERATION_DELETE)
		default:
			out = append(out, catalogv1.Operation_OPERATION_UNSPECIFIED)
		}
	}
	return out
}

func operationsFromProto(ops []catalogv1.Operation) ([]filament.Operation, error) {
	var out []filament.Operation
	for _, op := range ops {
		switch op {
		case catalogv1.Operation_OPERATION_INSERT:
			out = append(out, filament.OpInsert)
		case catalogv1.Operation_OPERATION_UPDATE:
			out = append(out, filament.OpUpdate)
		case catalogv1.Operation_OPERATION_DELETE:
			out = append(out, filament.OpDelete)
		default:
			return nil, fmt.Errorf("unknown operation %v", op)
		}
	}
	return out, nil
}

func fieldTypeToProto(fieldType filament.FieldType) ingestionv1.FieldType {
	switch fieldType {
	case filament.FieldString:
		return ingestionv1.FieldType_FIELD_TYPE_STRING
	case filament.FieldInt:
		return ingestionv1.FieldType_FIELD_TYPE_INT
	case filament.FieldBool:
		return ingestionv1.FieldType_FIELD_TYPE_BOOL
	case filament.FieldSecret:
		return ingestionv1.FieldType_FIELD_TYPE_SECRET
	case filament.FieldDuration:
		return ingestionv1.FieldType_FIELD_TYPE_DURATION
	case filament.FieldEnum:
		return ingestionv1.FieldType_FIELD_TYPE_ENUM
	case filament.FieldObject:
		return ingestionv1.FieldType_FIELD_TYPE_OBJECT
	case filament.FieldList:
		return ingestionv1.FieldType_FIELD_TYPE_LIST
	default:
		return ingestionv1.FieldType_FIELD_TYPE_UNSPECIFIED
	}
}

func fieldTypeFromProto(fieldType ingestionv1.FieldType) (filament.FieldType, error) {
	switch fieldType {
	case ingestionv1.FieldType_FIELD_TYPE_STRING:
		return filament.FieldString, nil
	case ingestionv1.FieldType_FIELD_TYPE_INT:
		return filament.FieldInt, nil
	case ingestionv1.FieldType_FIELD_TYPE_BOOL:
		return filament.FieldBool, nil
	case ingestionv1.FieldType_FIELD_TYPE_SECRET:
		return filament.FieldSecret, nil
	case ingestionv1.FieldType_FIELD_TYPE_DURATION:
		return filament.FieldDuration, nil
	case ingestionv1.FieldType_FIELD_TYPE_ENUM:
		return filament.FieldEnum, nil
	case ingestionv1.FieldType_FIELD_TYPE_OBJECT:
		return filament.FieldObject, nil
	case ingestionv1.FieldType_FIELD_TYPE_LIST:
		return filament.FieldList, nil
	default:
		return 0, fmt.Errorf("unknown field type %v", fieldType)
	}
}

func fieldScopeToProto(scope filament.FieldScope) ingestionv1.FieldScope {
	switch scope {
	case filament.ScopeConnection:
		return ingestionv1.FieldScope_FIELD_SCOPE_CONNECTION
	case filament.ScopePipeline:
		return ingestionv1.FieldScope_FIELD_SCOPE_PIPELINE
	default:
		return ingestionv1.FieldScope_FIELD_SCOPE_UNSPECIFIED
	}
}

func fieldScopeFromProto(scope ingestionv1.FieldScope) filament.FieldScope {
	switch scope {
	case ingestionv1.FieldScope_FIELD_SCOPE_CONNECTION:
		return filament.ScopeConnection
	case ingestionv1.FieldScope_FIELD_SCOPE_PIPELINE:
		return filament.ScopePipeline
	default:
		return filament.ScopeUnspecified
	}
}

func stringsOf[T ~string](values []T) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

func typedStrings[T ~string](values []string) []T {
	out := make([]T, 0, len(values))
	for _, value := range values {
		out = append(out, T(value))
	}
	return out
}
