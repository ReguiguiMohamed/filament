package schema

import (
	"fmt"
	"slices"

	"github.com/galaxy-io/filament/rowmodel"
)

type EvidenceKind string

const (
	CatalogEvidence  EvidenceKind = "catalog"
	DeclaredEvidence EvidenceKind = "declared"
	InferredEvidence EvidenceKind = "inferred"
	OrderedEvidence  EvidenceKind = "ordered_event"
	PayloadEvidence  EvidenceKind = "payload_mismatch"
)

// Reference is versioned connector-owned evidence, never generic progress.
// Its consumer validates the format, size, and meaning; core does not compare it.
type Reference struct {
	Format string
	Data   []byte
}
type Observation struct {
	Resource      string
	Model         *rowmodel.Schema
	Evidence      EvidenceKind
	Complete      bool
	Authoritative bool // supplied by a validated connector contract, not inferred from provenance
	Reference     *Reference
}

func (o Observation) Clone() Observation {
	if o.Model != nil {
		model := o.Model.Clone()
		o.Model = &model
	}
	if o.Reference != nil {
		ref := *o.Reference
		ref.Data = slices.Clone(ref.Data)
		o.Reference = &ref
	}
	return o
}

// Projection identifies an already-admitted selection, not a request to change
// configuration. Fields are exact names to compare; nil means the whole model.
// This comparison view does not transform records or prove resume compatibility.
type Projection struct {
	Version string
	Fields  []string
}

type ObservationResult struct {
	Diff   *Diff // nil when evidence cannot establish a complete comparison
	Reason string
}

// Observe compares authoritative complete evidence within an explicit projection.
// Samples and unresolved mismatches remain unknown, including additions whose
// type is not established. In particular, missing sample fields never become drops.
func Observe(accepted rowmodel.Schema, o Observation, projection Projection) (ObservationResult, error) {
	if err := Validate(accepted); err != nil {
		return ObservationResult{}, err
	}
	if o.Resource != accepted.Resource {
		return ObservationResult{}, fmt.Errorf("observation resource does not match accepted schema")
	}
	switch o.Evidence {
	case CatalogEvidence, DeclaredEvidence, InferredEvidence, OrderedEvidence, PayloadEvidence:
	default:
		return ObservationResult{}, fmt.Errorf("unknown schema evidence %q", o.Evidence)
	}
	if o.Reference != nil && o.Reference.Format == "" {
		return ObservationResult{}, fmt.Errorf("schema reference requires a format")
	}
	if projection.Fields != nil && projection.Version == "" {
		return ObservationResult{}, fmt.Errorf("explicit projection requires a version")
	}
	if o.Model == nil {
		return ObservationResult{Reason: "observation has no resolved schema"}, nil
	}
	if err := Validate(*o.Model); err != nil {
		return ObservationResult{}, err
	}
	if o.Model.Resource != o.Resource {
		return ObservationResult{}, fmt.Errorf("observation model resource does not match evidence")
	}
	if !o.Complete || !o.Authoritative || o.Evidence == InferredEvidence {
		return ObservationResult{Reason: "observation cannot establish a complete authoritative schema"}, nil
	}
	before, after := accepted.Clone(), o.Model.Clone()
	if projection.Fields != nil {
		selected := make(map[string]bool, len(projection.Fields))
		for _, name := range projection.Fields {
			if name == "" || selected[name] {
				return ObservationResult{}, fmt.Errorf("empty or duplicate projected field %q", name)
			}
			selected[name] = true
		}
		// Preserve source-relative ordering. Decoder rebuilds may still be required
		// even when a destination maps fields by name.
		filter := func(s rowmodel.Schema) rowmodel.Schema {
			s.Fields = slices.DeleteFunc(s.Fields, func(f rowmodel.Field) bool { return !selected[f.Name] })
			// A subset of a compound key does not establish uniqueness. Retain
			// the key only if the entire key is represented in this view.
			for _, key := range s.PrimaryKey {
				if !selected[key] {
					s.PrimaryKey = nil
					break
				}
			}
			return s
		}
		before, after = filter(before), filter(after)
	}
	diff, err := Compare(before, after)
	if err != nil {
		return ObservationResult{}, err
	}
	return ObservationResult{Diff: &diff}, nil
}

// MismatchError snapshots detection evidence. It grants no safe prefix, source
// acknowledgement, or permission to mutate a destination.
type MismatchError struct {
	observation Observation
	cause       error
}

func NewMismatchError(observation Observation, cause error) *MismatchError {
	return &MismatchError{observation.Clone(), cause}
}
func (e *MismatchError) Observation() Observation { return e.observation.Clone() }
func (e *MismatchError) Unwrap() error            { return e.cause }
func (e *MismatchError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("schema mismatch for %q: %v", e.observation.Resource, e.cause)
	}
	return fmt.Sprintf("schema mismatch for %q", e.observation.Resource)
}
