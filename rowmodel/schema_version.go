package rowmodel

import "fmt"

// SchemaData is the explicit portable representation of Schema, including its
// private semantic flags. It is a serialization value, not an immutable snapshot.
// Consumers must version the enclosing encoding before persisting it.
type SchemaData struct {
	Resource            string   `json:"resource"`
	DestinationResource string   `json:"destination_resource"`
	Engine              string   `json:"engine"`
	Envelope            bool     `json:"envelope"`
	ProjectedEnvelope   bool     `json:"projected_envelope"`
	Fields              []Field  `json:"fields"`
	PrimaryKey          []string `json:"primary_key"`
}

// Data returns independently owned serialization data. Unlike default JSON
// serialization of Schema, it preserves the envelope provenance flags.
func (s Schema) Data() SchemaData {
	s = s.Clone()
	return SchemaData{s.Resource, s.DestinationResource, s.Engine, s.envelope, s.projectedEnvelope, s.Fields, s.PrimaryKey}
}

// SchemaFromData restores serialization data without sharing its slices. This
// does not authorize reserved fields or validate a source's ingestion contract:
// destination audit/history schemas also need to round-trip through this codec.
func SchemaFromData(d SchemaData) (Schema, error) {
	if d.Envelope && d.ProjectedEnvelope {
		return Schema{}, fmt.Errorf("schema cannot have both envelope modes")
	}
	return (Schema{Resource: d.Resource, DestinationResource: d.DestinationResource,
		Engine: d.Engine, envelope: d.Envelope, projectedEnvelope: d.ProjectedEnvelope,
		Fields: d.Fields, PrimaryKey: d.PrimaryKey}).Clone(), nil
}

type SchemaVersionID string

// SchemaFingerprint identifies a schema's canonical representation, not a
// source position, migration authorization, or committed write.
type SchemaFingerprint struct {
	Format uint16
	SHA256 [32]byte
}

// SchemaVersion owns an immutable schema snapshot. Use schema.NewVersion to
// construct a version with a validated model and its canonical fingerprint.
type SchemaVersion struct {
	id          SchemaVersionID
	previous    SchemaVersionID
	fingerprint SchemaFingerprint
	model       Schema
}

// NewSchemaVersion snapshots a model and caller-supplied fingerprint. It does
// not compute or authenticate the fingerprint; schema.NewVersion does that.
func NewSchemaVersion(id, previous SchemaVersionID, model Schema, fingerprint SchemaFingerprint) SchemaVersion {
	return SchemaVersion{id, previous, fingerprint, model.Clone()}
}

func (v SchemaVersion) ID() SchemaVersionID            { return v.id }
func (v SchemaVersion) Previous() SchemaVersionID      { return v.previous }
func (v SchemaVersion) Fingerprint() SchemaFingerprint { return v.fingerprint }
func (v SchemaVersion) Model() Schema                  { return v.model.Clone() }

// SchemaBinding separates source decoding from the projected write contract.
type SchemaBinding struct {
	Resource           string
	SourceVersion      SchemaVersionID
	DestinationVersion SchemaVersionID
	ProjectionVersion  string
}
