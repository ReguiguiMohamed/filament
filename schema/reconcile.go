package schema

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/galaxy-io/filament/rowmodel"
)

var (
	ErrRevision    = errors.New("schema revision conflict")
	ErrIdempotency = errors.New("schema request conflicts with historical request")
	ErrTransition  = errors.New("invalid schema operation transition")
	ErrPending     = errors.New("schema operation already pending")
)

type Phase string

const (
	Planned          Phase = "planned"
	Applying         Phase = "applying"
	Verified         Phase = "verified"
	Activated        Phase = "activated"
	OperationBlocked Phase = "blocked"
)

// State is an immutable route-scoped state-machine snapshot. There is no I/O,
// ownership token, checkpoint, or process-global state here. The future store
// adapter must validate ownership and run each reducer atomically with persistence,
// including exact retries. Retain operation receipts for the retry/replay horizon.
// Zero State is invalid; NewState validates the initial versions and bindings.
type State struct {
	revision   int64
	bindings   []rowmodel.SchemaBinding
	versions   map[rowmodel.SchemaVersionID]rowmodel.SchemaVersion
	pending    OperationID
	operations map[OperationID]Operation
}

func NewState(bindings []rowmodel.SchemaBinding, versions []rowmodel.SchemaVersion) (State, error) {
	s := State{revision: 1, bindings: slices.Clone(bindings), versions: map[rowmodel.SchemaVersionID]rowmodel.SchemaVersion{}, operations: map[OperationID]Operation{}}
	if err := s.addVersions(versions); err != nil {
		return State{}, err
	}
	if err := s.validateBindings(bindings); err != nil {
		return State{}, err
	}
	return s, nil
}
func (s State) Revision() int64                            { return s.revision }
func (s State) Bindings() []rowmodel.SchemaBinding         { return slices.Clone(s.bindings) }
func (s State) Pending() OperationID                       { return s.pending }
func (s State) Operation(id OperationID) (Operation, bool) { op, ok := s.operations[id]; return op, ok }
func (s State) Version(id rowmodel.SchemaVersionID) (rowmodel.SchemaVersion, bool) {
	v, ok := s.versions[id]
	return v, ok
}
func (s State) clone() State {
	s.bindings = slices.Clone(s.bindings)
	s.versions = maps.Clone(s.versions)
	s.operations = maps.Clone(s.operations)
	return s
}
func (s State) addVersions(versions []rowmodel.SchemaVersion) error {
	for _, v := range versions {
		checked, err := NewVersion(v.ID(), v.Previous(), v.Model())
		if err != nil {
			return err
		}
		if checked.Fingerprint() != v.Fingerprint() {
			return fmt.Errorf("schema version %q has incorrect fingerprint", v.ID())
		}
		if old, ok := s.versions[v.ID()]; ok && (old.Previous() != v.Previous() || old.Fingerprint() != v.Fingerprint() || !old.Model().Equal(v.Model())) {
			return fmt.Errorf("%w: schema version %q changed", ErrIdempotency, v.ID())
		}
		s.versions[v.ID()] = v
	}
	return nil
}
func (s State) validateBindings(bindings []rowmodel.SchemaBinding) error {
	seen := map[string]bool{}
	for _, b := range bindings {
		source, sourceOK := s.versions[b.SourceVersion]
		_, destOK := s.versions[b.DestinationVersion]
		if b.Resource == "" || seen[b.Resource] || !sourceOK || !destOK || source.Model().Resource != b.Resource {
			return fmt.Errorf("invalid schema binding for %q", b.Resource)
		}
		seen[b.Resource] = true
	}
	return nil
}

// Operation exposes immutable history through copy-returning accessors. begin and
// advances are exact request receipts, not permission to replay external effects.
type Operation struct {
	id            OperationID
	revision      int64
	phase         Phase
	before        []rowmodel.SchemaBinding
	begin         BeginRequest
	applied       []string
	verification  *Verification
	blockedReason string
	replacedBy    OperationID
	advances      map[int64]AdvanceRequest
	activation    *ActivateRequest
}

func (o Operation) ID() OperationID                  { return o.id }
func (o Operation) Revision() int64                  { return o.revision }
func (o Operation) Phase() Phase                     { return o.phase }
func (o Operation) Before() []rowmodel.SchemaBinding { return slices.Clone(o.before) }
func (o Operation) After() []rowmodel.SchemaBinding  { return slices.Clone(o.begin.After) }
func (o Operation) Observation() Observation         { return o.begin.Observation.Clone() }
func (o Operation) Plan() *Plan {
	if o.begin.Plan == nil {
		return nil
	}
	p := o.begin.Plan.clone()
	return &p
}
func (o Operation) Applied() []string { return slices.Clone(o.applied) }
func (o Operation) Verification() *Verification {
	if o.verification == nil {
		return nil
	}
	v := o.verification.clone()
	return &v
}
func (o Operation) BlockedReason() string   { return o.blockedReason }
func (o Operation) Supersedes() OperationID { return o.begin.Supersedes }
func (o Operation) ReplacedBy() OperationID { return o.replacedBy }

// BeginRequest persists versions and a plan before external effects. A blocked
// observation may omit the plan/After bindings until enough evidence exists.
// Supersedes explicitly replaces only the currently blocked operation. Existing
// successful effects survive replacement and must be reinspected in the new plan.
type BeginRequest struct {
	Operation              OperationID
	ExpectedSchemaRevision int64
	Observation            Observation
	After                  []rowmodel.SchemaBinding
	Versions               []rowmodel.SchemaVersion
	Plan                   *Plan
	BlockedReason          string
	Supersedes             OperationID
}

func (r BeginRequest) clone() BeginRequest {
	r.Observation = r.Observation.Clone()
	r.After = slices.Clone(r.After)
	r.Versions = slices.Clone(r.Versions)
	if r.Plan != nil {
		p := r.Plan.clone()
		r.Plan = &p
	}
	return r
}

func Begin(s State, r BeginRequest) (State, error) {
	if s.revision < 1 || r.Operation == "" {
		return s, fmt.Errorf("initialized state and operation ID required")
	}
	if old, ok := s.operations[r.Operation]; ok {
		if !reflect.DeepEqual(old.begin, r) {
			return s, ErrIdempotency
		}
		return s, nil // historical success; never overwrite a newer active schema
	}
	if r.ExpectedSchemaRevision != s.revision {
		return s, ErrRevision
	}
	if s.pending != "" {
		old := s.operations[s.pending]
		if r.Supersedes != s.pending || old.phase != OperationBlocked {
			return s, ErrPending
		}
	} else if r.Supersedes != "" {
		return s, fmt.Errorf("replacement must name the pending blocked operation")
	}
	if r.Observation.Resource == "" {
		return s, fmt.Errorf("observation resource required")
	}
	switch r.Observation.Evidence {
	case CatalogEvidence, DeclaredEvidence, InferredEvidence, OrderedEvidence, PayloadEvidence:
	default:
		return s, fmt.Errorf("unknown schema observation evidence")
	}
	if r.Observation.Reference != nil && r.Observation.Reference.Format == "" {
		return s, fmt.Errorf("schema observation reference requires a format")
	}
	if model := r.Observation.Model; model != nil {
		if model.Resource != r.Observation.Resource {
			return s, fmt.Errorf("observation model resource does not match evidence")
		}
		if err := Validate(*model); err != nil {
			return s, err
		}
	}
	next := s.clone()
	if err := next.addVersions(r.Versions); err != nil {
		return s, err
	}
	if err := next.validateBindings(r.After); err != nil {
		return s, err
	}
	if r.Plan != nil {
		if err := r.Plan.validate(); err != nil {
			return s, err
		}
		if err := next.validatePlanBindings(r); err != nil {
			return s, err
		}
	}
	phase := Planned
	if r.BlockedReason != "" {
		phase = OperationBlocked
	} else {
		if r.Plan == nil {
			return s, fmt.Errorf("unblocked operation requires a plan")
		}
		if err := r.Plan.admissible(); err != nil {
			return s, err
		}
	}
	if s.pending != "" {
		old := next.operations[s.pending]
		old.replacedBy = r.Operation
		old.revision++
		old.verification = nil
		next.operations[old.id] = old
	}
	next.operations[r.Operation] = Operation{id: r.Operation, revision: 1, phase: phase, before: slices.Clone(s.bindings), begin: r.clone(), blockedReason: r.BlockedReason, advances: map[int64]AdvanceRequest{}}
	next.pending = r.Operation
	return next, nil
}

func (s State) validatePlanBindings(r BeginRequest) error {
	p := r.Plan
	if p.Resource != r.Observation.Resource {
		return fmt.Errorf("plan and observation resources differ")
	}
	old := map[string]rowmodel.SchemaBinding{}
	for _, b := range s.bindings {
		old[b.Resource] = b
	}
	found := false
	for _, b := range r.After {
		previous, existed := old[b.Resource]
		if b.Resource == p.Resource {
			found = true
			if b.DestinationVersion != p.Desired {
				return fmt.Errorf("plan does not match desired binding")
			}
			desired := s.versions[b.DestinationVersion].Model()
			target := desired.DestinationResource
			if target == "" {
				target = desired.Resource
			}
			if target != p.Destination {
				return fmt.Errorf("plan does not match destination resource")
			}
			if !existed || b.SourceVersion != previous.SourceVersion {
				if r.Observation.Model == nil || !r.Observation.Complete || !r.Observation.Authoritative || r.Observation.Evidence == InferredEvidence || !r.Observation.Model.Equal(s.versions[b.SourceVersion].Model()) {
					return fmt.Errorf("new source binding requires complete authoritative matching evidence")
				}
			}
		} else if !existed || previous != b {
			return fmt.Errorf("plan changes an unrelated resource binding")
		}
		delete(old, b.Resource)
	}
	if !found || len(old) > 0 {
		return fmt.Errorf("plan must preserve route membership and bind its resource")
	}
	return nil
}

type Action string

const (
	StartApplying  Action = "start_applying"
	RecordApplied  Action = "record_applied"
	RecordVerified Action = "record_verified"
	BlockOperation Action = "block"
)

type AdvanceRequest struct {
	Operation                 OperationID
	ExpectedOperationRevision int64
	Action                    Action
	CompletedSteps            []string // cumulative prefix in plan order
	Verification              *Verification
	BlockedReason             string
}

func (r AdvanceRequest) clone() AdvanceRequest {
	r.CompletedSteps = slices.Clone(r.CompletedSteps)
	if r.Verification != nil {
		v := r.Verification.clone()
		r.Verification = &v
	}
	return r
}

// Advance validates a pure transition. Before StartApplying, the adapter must
// resolve data commits, prove writer quiescence, and enforce destination exclusion.
// A repeated request never tells a connector to repeat an external effect blindly.
func Advance(s State, r AdvanceRequest) (State, error) {
	op, ok := s.operations[r.Operation]
	if !ok {
		return s, fmt.Errorf("unknown schema operation")
	}
	if previous, ok := op.advances[r.ExpectedOperationRevision]; ok {
		if !reflect.DeepEqual(previous, r) {
			return s, ErrIdempotency
		}
		return s, nil
	}
	if op.revision != r.ExpectedOperationRevision {
		return s, ErrRevision
	}
	if s.pending != op.id || op.replacedBy != "" || op.phase == Activated || op.phase == OperationBlocked {
		return s, ErrTransition
	}
	// Reject ignored payloads so a request receipt has exactly one interpretation.
	if r.Action != RecordApplied && len(r.CompletedSteps) > 0 || r.Action != RecordVerified && r.Verification != nil || r.Action != BlockOperation && r.BlockedReason != "" {
		return s, fmt.Errorf("unexpected transition payload")
	}
	switch r.Action {
	case StartApplying:
		if op.phase != Planned {
			return s, ErrTransition
		}
		op.phase = Applying
	case RecordApplied:
		if op.phase != Applying {
			return s, ErrTransition
		}
		steps := op.begin.Plan.Steps
		if len(r.CompletedSteps) <= len(op.applied) || len(r.CompletedSteps) > len(steps) {
			return s, fmt.Errorf("step completion must extend the known prefix")
		}
		for i, id := range r.CompletedSteps {
			if id != steps[i].ID {
				return s, fmt.Errorf("step completion is not a plan prefix")
			}
		}
		op.applied = slices.Clone(r.CompletedSteps)
	case RecordVerified:
		if op.phase != Applying || len(op.applied) != len(op.begin.Plan.Steps) {
			return s, ErrTransition
		}
		if err := validateVerification(op, r.Verification); err != nil {
			return s, err
		}
		v := r.Verification.clone()
		op.verification = &v
		op.phase = Verified
	case BlockOperation:
		if r.BlockedReason == "" {
			return s, fmt.Errorf("blocked reason required")
		}
		op.blockedReason = r.BlockedReason
		op.verification = nil
		op.phase = OperationBlocked
	default:
		return s, ErrTransition
	}
	op.advances = maps.Clone(op.advances)
	op.advances[r.ExpectedOperationRevision] = r.clone()
	op.revision++
	next := s.clone()
	next.operations[op.id] = op
	return next, nil
}

func validateVerification(op Operation, v *Verification) error {
	if v == nil {
		return fmt.Errorf("verification required")
	}
	fp, err := op.begin.Plan.Fingerprint()
	if err != nil {
		return err
	}
	if v.Operation != op.id || v.Plan != fp || v.Desired != op.begin.Plan.Desired {
		return fmt.Errorf("verification does not match operation, plan, and desired schema")
	}
	if !v.Actual.Exists || v.Assessment.Status() != ProvenCompatible || v.EvidenceFormat == "" || len(v.Evidence) == 0 {
		return fmt.Errorf("verification requires actual destination evidence and all compatible obligations")
	}
	return v.Actual.validate()
}

type ActivateRequest struct {
	Operation                 OperationID
	ExpectedOperationRevision int64
	ExpectedSchemaRevision    int64
}

// Activate atomically returns new bindings and archived operation state. The
// caller must persist both under current ownership in one transaction. It never
// changes source progress. Even historical retries require fresh ownership checks
// by the adapter; historical success cannot authorize source acknowledgement.
func Activate(s State, r ActivateRequest) (State, error) {
	op, ok := s.operations[r.Operation]
	if !ok {
		return s, fmt.Errorf("unknown schema operation")
	}
	if op.activation != nil {
		if *op.activation != r {
			return s, ErrIdempotency
		}
		return s, nil
	}
	if op.revision != r.ExpectedOperationRevision || s.revision != r.ExpectedSchemaRevision {
		return s, ErrRevision
	}
	if s.pending != op.id || op.phase != Verified {
		return s, ErrTransition
	}
	if err := validateVerification(op, op.verification); err != nil {
		return s, err
	}
	next := s.clone()
	next.revision++
	next.bindings = slices.Clone(op.begin.After)
	next.pending = ""
	op.phase = Activated
	op.revision++
	op.activation = &r
	next.operations[op.id] = op
	return next, nil
}
