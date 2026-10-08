package schema

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/galaxy-io/filament/rowmodel"
)

type OperationID string

type TransitionKind string

const (
	TableDDL          TransitionKind = "table_ddl"
	FileRotation      TransitionKind = "file_rotation"
	SchemaPublication TransitionKind = "schema_publication"
	NoTransition      TransitionKind = "none"
)

// DestinationState is inspection evidence, including connector-specific target
// generation/constraints. A column fingerprint alone cannot protect those facts.
// Its interpretation and exclusion strategy remain the adapter's responsibility.
type DestinationState struct {
	Exists        bool
	Fingerprint   rowmodel.SchemaFingerprint
	DetailsFormat string
	Details       []byte
}

func (d DestinationState) clone() DestinationState { d.Details = slices.Clone(d.Details); return d }
func (d DestinationState) validate() error {
	if d.Exists && (d.Fingerprint.Format != CanonicalFormat || d.Fingerprint.SHA256 == [32]byte{}) {
		return fmt.Errorf("existing destination requires a supported schema fingerprint")
	}
	if len(d.Details) > 0 && d.DetailsFormat == "" {
		return fmt.Errorf("destination details require a format")
	}
	return nil
}

type Step struct {
	ID      string
	Kind    ChangeKind
	Summary string
	Payload []byte // interpreted only under Plan.Connector/PlanVersion
}

// Plan is a proposal value. Begin snapshots it; later edits cannot modify the
// stored operation. One operation transitions one resource's physical target;
// route-wide bindings for other resources must remain unchanged.
type Plan struct {
	Resource               string
	Destination            string
	Connector              string
	PlanVersion            string
	Desired                rowmodel.SchemaVersionID
	Precondition           DestinationState
	Transition             TransitionKind
	Steps                  []Step
	Permission             PolicyDecision
	Assessment             Assessment
	RequiresExclusiveOwner bool
	RequiresWriterRestart  bool
	RequiresBackfill       bool
}

func (p Plan) clone() Plan {
	p.Precondition = p.Precondition.clone()
	p.Steps = slices.Clone(p.Steps)
	for i := range p.Steps {
		p.Steps[i].Payload = slices.Clone(p.Steps[i].Payload)
	}
	p.Permission.Reasons = slices.Clone(p.Permission.Reasons)
	return p
}
func (p Plan) validate() error {
	if p.Resource == "" || p.Destination == "" || p.Connector == "" || p.PlanVersion == "" || p.Desired == "" {
		return fmt.Errorf("plan requires resource, destination, connector, version, and desired schema")
	}
	switch p.Transition {
	case TableDDL, FileRotation, SchemaPublication, NoTransition:
	default:
		return fmt.Errorf("unknown destination transition %q", p.Transition)
	}
	if p.Transition == NoTransition && len(p.Steps) > 0 {
		return fmt.Errorf("no-op plan cannot contain steps")
	}
	if p.Transition != NoTransition && len(p.Steps) == 0 {
		return fmt.Errorf("effectful plan requires steps")
	}
	if err := p.Precondition.validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, step := range p.Steps {
		if step.ID == "" || seen[step.ID] || step.Kind == "" || step.Summary == "" {
			return fmt.Errorf("steps require unique IDs, kinds, and reviewable summaries")
		}
		seen[step.ID] = true
	}
	return nil
}
func (p Plan) admissible() error {
	if p.Permission.Permission != Allowed {
		return fmt.Errorf("plan policy is not allowed")
	}
	if status := p.Assessment.Status(); status != ProvenCompatible && status != TransitionRequired {
		return fmt.Errorf("plan has unresolved compatibility obligations")
	}
	if p.RequiresBackfill {
		return fmt.Errorf("automatic backfill is unsupported")
	}
	return nil
}

// PlanFingerprint binds verification to the exact immutable plan, including
// preconditions, desired schema, policy decision, step order/payload, and assessment.
// This format is independent of the canonical schema format. Future wire changes
// require a new version; this is not an ownership or destination fencing token.
type PlanFingerprint struct {
	Format uint16
	SHA256 [32]byte
}

const PlanFormat uint16 = 1

func (p Plan) Fingerprint() (PlanFingerprint, error) {
	if err := p.validate(); err != nil {
		return PlanFingerprint{}, err
	}
	// All fields of the explicitly versioned Plan shape are values/slices; no maps
	// or private schema fields enter this representation.
	data, err := json.Marshal(struct {
		Format uint16
		Plan   Plan
	}{PlanFormat, p})
	if err != nil {
		return PlanFingerprint{}, err
	}
	return PlanFingerprint{PlanFormat, sha256.Sum256(data)}, nil
}

// Verification is an adapter assertion under destination exclusion. Core checks
// identity/completeness, not whether the external inspection actually happened.
// The store must persist it before activation and protect/revalidate the writer
// handoff. Compatible extra destination columns need not match Desired exactly.
type Verification struct {
	Operation      OperationID
	Plan           PlanFingerprint
	Desired        rowmodel.SchemaVersionID
	Actual         DestinationState
	Assessment     Assessment
	EvidenceFormat string
	Evidence       []byte
}

func (v Verification) clone() Verification {
	v.Actual = v.Actual.clone()
	v.Evidence = slices.Clone(v.Evidence)
	return v
}
