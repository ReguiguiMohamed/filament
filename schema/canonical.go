package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/galaxy-io/filament/rowmodel"
)

// CanonicalFormat is a persisted encoding version. Changing the wire shape,
// normalization, or field semantics requires a new version and legacy decoding.
const CanonicalFormat uint16 = 1

// Explicit wire fields isolate format 1 from future rowmodel additions and JSON
// tags. Field/key order and native spelling are significant; nil/empty lists are
// equivalent, matching Schema.Equal. There are no maps or omitted fields.
type wireSchema struct {
	Format            uint16      `json:"format"`
	Resource          string      `json:"resource"`
	Destination       string      `json:"destination"`
	Engine            string      `json:"engine"`
	Envelope          bool        `json:"envelope"`
	ProjectedEnvelope bool        `json:"projected_envelope"`
	Fields            []wireField `json:"fields"`
	Keys              []string    `json:"keys"`
}
type wireField struct {
	Name      string               `json:"name"`
	Logical   rowmodel.LogicalType `json:"logical"`
	Native    string               `json:"native"`
	Nullable  bool                 `json:"nullable"`
	Precision int                  `json:"precision"`
	Scale     int                  `json:"scale"`
}

// Validate checks structural identity, not engine-specific type validity or
// reserved-field permission. Empty/unknown logical types remain representable.
func Validate(s rowmodel.Schema) error {
	for _, value := range []string{s.Resource, s.DestinationResource, s.Engine} {
		if !utf8.ValidString(value) {
			return fmt.Errorf("schema contains invalid UTF-8")
		}
	}
	names := make(map[string]bool, len(s.Fields))
	for _, f := range s.Fields {
		if f.Name == "" || names[f.Name] {
			return fmt.Errorf("schema has empty or duplicate field %q", f.Name)
		}
		if !utf8.ValidString(f.Name) || !utf8.ValidString(string(f.Logical)) || !utf8.ValidString(f.Native) {
			return fmt.Errorf("field %q contains invalid UTF-8", f.Name)
		}
		names[f.Name] = true
	}
	keys := make(map[string]bool, len(s.PrimaryKey))
	for _, key := range s.PrimaryKey {
		if !names[key] || keys[key] {
			return fmt.Errorf("schema has missing or duplicate key field %q", key)
		}
		keys[key] = true
	}
	return nil
}

func Encode(s rowmodel.Schema) ([]byte, error) {
	if err := Validate(s); err != nil {
		return nil, err
	}
	d := s.Data()
	w := wireSchema{Format: CanonicalFormat, Resource: d.Resource, Destination: d.DestinationResource,
		Engine: d.Engine, Envelope: d.Envelope, ProjectedEnvelope: d.ProjectedEnvelope,
		Fields: make([]wireField, 0, len(d.Fields)), Keys: append([]string{}, d.PrimaryKey...)}
	for _, f := range d.Fields {
		w.Fields = append(w.Fields, wireField{f.Name, f.Logical, f.Native, f.Nullable, f.Precision, f.Scale})
	}
	return json.Marshal(w)
}

// Decode accepts only canonical bytes emitted by a supported format. Rejecting
// noncanonical encodings also rejects duplicate/unknown fields, omitted values,
// trailing data, and null lists rather than silently changing hashed evidence.
func Decode(data []byte) (rowmodel.Schema, error) {
	var w wireSchema
	if err := json.Unmarshal(data, &w); err != nil {
		return rowmodel.Schema{}, fmt.Errorf("decode schema: %w", err)
	}
	if w.Format != CanonicalFormat {
		return rowmodel.Schema{}, fmt.Errorf("unsupported schema format %d", w.Format)
	}
	d := rowmodel.SchemaData{Resource: w.Resource, DestinationResource: w.Destination, Engine: w.Engine,
		Envelope: w.Envelope, ProjectedEnvelope: w.ProjectedEnvelope, PrimaryKey: slices.Clone(w.Keys)}
	for _, f := range w.Fields {
		d.Fields = append(d.Fields, rowmodel.Field{Name: f.Name, Logical: f.Logical, Native: f.Native, Nullable: f.Nullable, Precision: f.Precision, Scale: f.Scale})
	}
	s, err := rowmodel.SchemaFromData(d)
	if err != nil {
		return rowmodel.Schema{}, err
	}
	canonical, err := Encode(s)
	if err != nil {
		return rowmodel.Schema{}, err
	}
	if !bytes.Equal(data, canonical) {
		return rowmodel.Schema{}, fmt.Errorf("noncanonical schema encoding")
	}
	return s, nil
}

func Fingerprint(s rowmodel.Schema) (rowmodel.SchemaFingerprint, error) {
	data, err := Encode(s)
	if err != nil {
		return rowmodel.SchemaFingerprint{}, err
	}
	return rowmodel.SchemaFingerprint{Format: CanonicalFormat, SHA256: sha256.Sum256(data)}, nil
}

func NewVersion(id, previous rowmodel.SchemaVersionID, s rowmodel.Schema) (rowmodel.SchemaVersion, error) {
	if id == "" || id == previous || s.Resource == "" {
		return rowmodel.SchemaVersion{}, fmt.Errorf("schema version requires an ID, resource, and distinct predecessor")
	}
	fingerprint, err := Fingerprint(s)
	if err != nil {
		return rowmodel.SchemaVersion{}, err
	}
	return rowmodel.NewSchemaVersion(id, previous, s, fingerprint), nil
}
