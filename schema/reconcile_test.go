package schema_test

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/galaxy-io/filament/rowmodel"
	"github.com/galaxy-io/filament/schema"
)

func compatibleAssessment() schema.Assessment {
	c := schema.CompatibilityResult{Status: schema.ProvenCompatible}
	return schema.Assessment{Decode: c, Project: c, Write: c, Resume: c}
}
func initialOperation(t *testing.T) (schema.State, schema.BeginRequest) {
	t.Helper()
	old := model()
	desired := old.Clone()
	addNullable(&desired)
	v1, err := schema.NewVersion("v1", "", old)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := schema.NewVersion("v2", "v1", desired)
	if err != nil {
		t.Fatal(err)
	}
	before := []rowmodel.SchemaBinding{{Resource: "users", SourceVersion: "v1", DestinationVersion: "v1", ProjectionVersion: "p1"}}
	s, err := schema.NewState(before, []rowmodel.SchemaVersion{v1})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := schema.Compare(old, desired)
	if err != nil {
		t.Fatal(err)
	}
	assessment := compatibleAssessment()
	assessment.Write.Status = schema.TransitionRequired
	request := schema.BeginRequest{
		Operation: "op1", ExpectedSchemaRevision: s.Revision(),
		Observation: schema.Observation{Resource: "users", Model: &desired, Evidence: schema.CatalogEvidence, Complete: true, Authoritative: true},
		After:       []rowmodel.SchemaBinding{{Resource: "users", SourceVersion: "v2", DestinationVersion: "v2", ProjectionVersion: "p1"}},
		Versions:    []rowmodel.SchemaVersion{v2},
		Plan: &schema.Plan{Resource: "users", Destination: "users", Connector: "fake", PlanVersion: "1", Desired: "v2",
			Precondition: schema.DestinationState{Exists: true, Fingerprint: v1.Fingerprint(), DetailsFormat: "fake/v1", Details: []byte("generation=1")},
			Transition:   schema.TableDDL, Steps: []schema.Step{{ID: "nickname", Kind: schema.AddField, Summary: "add nullable nickname", Payload: []byte("nickname")}},
			Permission: schema.EvaluatePolicy(diff, schema.Policy{Mode: schema.Compatible, AllowNullableAdd: true}, nil), Assessment: assessment, RequiresExclusiveOwner: true, RequiresWriterRestart: true},
	}
	return s, request
}
func operation(t *testing.T, s schema.State, id schema.OperationID) schema.Operation {
	t.Helper()
	op, ok := s.Operation(id)
	if !ok {
		t.Fatalf("missing operation %q", id)
	}
	return op
}
func beginOperation(t *testing.T, s schema.State, r schema.BeginRequest) schema.State {
	t.Helper()
	next, err := schema.Begin(s, r)
	if err != nil {
		t.Fatal(err)
	}
	return next
}
func advanceOperation(t *testing.T, s schema.State, id schema.OperationID, action schema.Action) schema.State {
	t.Helper()
	op := operation(t, s, id)
	r := schema.AdvanceRequest{Operation: id, ExpectedOperationRevision: op.Revision(), Action: action}
	switch action {
	case schema.RecordApplied:
		for _, step := range op.Plan().Steps {
			r.CompletedSteps = append(r.CompletedSteps, step.ID)
		}
	case schema.RecordVerified:
		r.Verification = verification(t, op)
	case schema.BlockOperation:
		r.BlockedReason = "target precondition changed"
	}
	next, err := schema.Advance(s, r)
	if err != nil {
		t.Fatal(err)
	}
	return next
}
func verification(t *testing.T, op schema.Operation) *schema.Verification {
	t.Helper()
	fp, err := op.Plan().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	// A compatible actual schema may have extra columns; the fingerprint need
	// not equal the desired version's fingerprint.
	extra := model()
	addNullable(&extra)
	extra.Fields = append(extra.Fields, rowmodel.Field{Name: "extra", Logical: rowmodel.LogicalString, Nullable: true})
	actual, err := schema.Fingerprint(extra)
	if err != nil {
		t.Fatal(err)
	}
	return &schema.Verification{Operation: op.ID(), Plan: fp, Desired: op.Plan().Desired, Actual: schema.DestinationState{Exists: true, Fingerprint: actual, DetailsFormat: "fake/v1", Details: []byte("generation=2")}, Assessment: compatibleAssessment(), EvidenceFormat: "fake-inspection/v1", Evidence: []byte("verified under exclusive target ownership")}
}

func TestReconciliationLifecycleAndSnapshotIsolation(t *testing.T) {
	initial, request := initialOperation(t)
	planned := beginOperation(t, initial, request)
	if initial.Pending() != "" || len(initial.Bindings()) != 1 || initial.Bindings()[0].SourceVersion != "v1" {
		t.Fatal("begin mutated initial state")
	}
	request.Plan.Steps[0].Payload[0] = 'X'
	request.Plan.Precondition.Details[0] = 'X'
	request.Observation.Model.Fields[0].Name = "changed"
	request.After[0].SourceVersion = "changed"
	op := operation(t, planned, "op1")
	if string(op.Plan().Steps[0].Payload) != "nickname" || string(op.Plan().Precondition.Details) != "generation=1" || op.Observation().Model.Fields[0].Name != "id" || op.After()[0].SourceVersion != "v2" {
		t.Fatal("begin retained mutable request aliases")
	}
	copy := op.Plan()
	copy.Steps[0].Payload[0] = 'Y'
	op.After()[0].Resource = "changed"
	planned.Bindings()[0].Resource = "changed"
	if string(op.Plan().Steps[0].Payload) != "nickname" || op.After()[0].Resource != "users" || planned.Bindings()[0].Resource != "users" {
		t.Fatal("accessors leaked mutable state")
	}
	applying := advanceOperation(t, planned, "op1", schema.StartApplying)
	completed := advanceOperation(t, applying, "op1", schema.RecordApplied)
	verified := advanceOperation(t, completed, "op1", schema.RecordVerified)
	v := operation(t, verified, "op1").Verification()
	v.Evidence[0] = 'X'
	v.Actual.Details[0] = 'X'
	if operation(t, verified, "op1").Verification().Evidence[0] == 'X' || operation(t, verified, "op1").Verification().Actual.Details[0] == 'X' {
		t.Fatal("verification accessor aliases stored data")
	}
	activate := schema.ActivateRequest{Operation: "op1", ExpectedOperationRevision: operation(t, verified, "op1").Revision(), ExpectedSchemaRevision: verified.Revision()}
	active, err := schema.Activate(verified, activate)
	if err != nil {
		t.Fatal(err)
	}
	if active.Pending() != "" || active.Revision() != 2 || active.Bindings()[0].DestinationVersion != "v2" || operation(t, active, "op1").Phase() != schema.Activated {
		t.Fatal("activation did not atomically replace state")
	}
	for _, s := range []schema.State{planned, applying, completed, verified} {
		if s.Revision() != 1 || s.Bindings()[0].DestinationVersion != "v1" {
			t.Fatal("schema activated before verification or old snapshot mutated")
		}
	}
	if operation(t, planned, "op1").Phase() != schema.Planned {
		t.Fatal("advance mutated old operation")
	}
}

func TestReconciliationRejectsInvalidTransitions(t *testing.T) {
	initial, r := initialOperation(t)
	planned := beginOperation(t, initial, r)
	cases := []struct {
		name    string
		request schema.AdvanceRequest
	}{
		{"skip applying", schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: 1, Action: schema.RecordVerified, Verification: verification(t, operation(t, planned, "op1"))}},
		{"stale revision", schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: 0, Action: schema.StartApplying}},
		{"unknown action", schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: 1, Action: "activated"}},
		{"ignored payload", schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: 1, Action: schema.StartApplying, CompletedSteps: []string{"nickname"}}},
		{"empty blocked reason", schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: 1, Action: schema.BlockOperation}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := schema.Advance(planned, tt.request); err == nil {
				t.Fatal("invalid transition accepted")
			}
		})
	}
	if _, err := schema.Activate(planned, schema.ActivateRequest{Operation: "op1", ExpectedOperationRevision: 1, ExpectedSchemaRevision: 1}); !errors.Is(err, schema.ErrTransition) {
		t.Fatal(err)
	}
	applying := advanceOperation(t, planned, "op1", schema.StartApplying)
	for _, ids := range [][]string{nil, {"unknown"}, {"nickname", "nickname"}} {
		if _, err := schema.Advance(applying, schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: 2, Action: schema.RecordApplied, CompletedSteps: ids}); err == nil {
			t.Fatal("invalid step prefix accepted")
		}
	}
	if _, err := schema.Advance(applying, schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: 2, Action: schema.RecordVerified, Verification: verification(t, operation(t, applying, "op1"))}); err == nil {
		t.Fatal("verified before all steps completed")
	}
}

func TestVerificationBindsExactPlanAndAllObligations(t *testing.T) {
	initial, r := initialOperation(t)
	s := beginOperation(t, initial, r)
	s = advanceOperation(t, s, "op1", schema.StartApplying)
	s = advanceOperation(t, s, "op1", schema.RecordApplied)
	for name, mutate := range map[string]func(*schema.Verification){
		"operation":           func(v *schema.Verification) { v.Operation = "other" },
		"plan":                func(v *schema.Verification) { v.Plan.SHA256[0] ^= 1 },
		"desired":             func(v *schema.Verification) { v.Desired = "other" },
		"absent target":       func(v *schema.Verification) { v.Actual.Exists = false },
		"unknown fingerprint": func(v *schema.Verification) { v.Actual.Fingerprint.Format = 99 },
		"missing evidence":    func(v *schema.Verification) { v.Evidence = nil },
		"missing decode":      func(v *schema.Verification) { v.Assessment.Decode.Status = "" },
		"missing projection":  func(v *schema.Verification) { v.Assessment.Project.Status = schema.Unknown },
		"write transition":    func(v *schema.Verification) { v.Assessment.Write.Status = schema.TransitionRequired },
		"blocked resume":      func(v *schema.Verification) { v.Assessment.Resume.Status = schema.Blocked },
	} {
		t.Run(name, func(t *testing.T) {
			v := verification(t, operation(t, s, "op1"))
			mutate(v)
			if _, err := schema.Advance(s, schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: 3, Action: schema.RecordVerified, Verification: v}); err == nil {
				t.Fatal("invalid verification accepted")
			}
		})
	}
}

func TestExactRetriesAndHistoricalActivation(t *testing.T) {
	initial, r := initialOperation(t)
	s := beginOperation(t, initial, r)
	retry, err := schema.Begin(s, r)
	if err != nil || !reflect.DeepEqual(s, retry) {
		t.Fatalf("begin retry: %v", err)
	}
	changed := r
	changed.Observation = changed.Observation.Clone()
	changed.Observation.Complete = false
	if _, err := schema.Begin(s, changed); !errors.Is(err, schema.ErrIdempotency) {
		t.Fatal(err)
	}
	start := schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: 1, Action: schema.StartApplying}
	s, err = schema.Advance(s, start)
	if err != nil {
		t.Fatal(err)
	}
	retry, err = schema.Advance(s, start)
	if err != nil || !reflect.DeepEqual(s, retry) {
		t.Fatalf("advance retry: %v", err)
	}
	conflict := start
	conflict.Action = schema.BlockOperation
	conflict.BlockedReason = "changed"
	if _, err := schema.Advance(s, conflict); !errors.Is(err, schema.ErrIdempotency) {
		t.Fatal(err)
	}
	s = advanceOperation(t, s, "op1", schema.RecordApplied)
	s = advanceOperation(t, s, "op1", schema.RecordVerified)
	activate := schema.ActivateRequest{Operation: "op1", ExpectedOperationRevision: 4, ExpectedSchemaRevision: 1}
	s, err = schema.Activate(s, activate)
	if err != nil {
		t.Fatal(err)
	}
	// A later operation activates another revision. Old successful requests must
	// return current state without restoring old bindings or incrementing revision.
	r2 := r
	r2.Operation = "op2"
	r2.ExpectedSchemaRevision = 2
	r2.Plan = operation(t, s, "op1").Plan()
	r2.Plan.Transition = schema.NoTransition
	r2.Plan.Steps = nil
	r2.Plan.Assessment = compatibleAssessment()
	s = beginOperation(t, s, r2)
	s = advanceOperation(t, s, "op2", schema.StartApplying)
	s = advanceOperation(t, s, "op2", schema.RecordVerified)
	s, err = schema.Activate(s, schema.ActivateRequest{Operation: "op2", ExpectedOperationRevision: 3, ExpectedSchemaRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, retryFn := range []func(schema.State) (schema.State, error){func(s schema.State) (schema.State, error) { return schema.Begin(s, r) }, func(s schema.State) (schema.State, error) { return schema.Advance(s, start) }, func(s schema.State) (schema.State, error) { return schema.Activate(s, activate) }} {
		current, err := retryFn(s)
		if err != nil || !reflect.DeepEqual(s, current) {
			t.Fatalf("historical retry altered state: %v", err)
		}
	}
	activate.ExpectedSchemaRevision = 3
	if _, err := schema.Activate(s, activate); !errors.Is(err, schema.ErrIdempotency) {
		t.Fatal(err)
	}
}

func TestBlockedReplacementInvalidatesVerification(t *testing.T) {
	initial, r := initialOperation(t)
	s := beginOperation(t, initial, r)
	s = advanceOperation(t, s, "op1", schema.StartApplying)
	s = advanceOperation(t, s, "op1", schema.RecordApplied)
	s = advanceOperation(t, s, "op1", schema.RecordVerified)
	oldVerification := operation(t, s, "op1").Verification()
	s = advanceOperation(t, s, "op1", schema.BlockOperation)
	if operation(t, s, "op1").Verification() != nil {
		t.Fatal("blocked operation retained verification")
	}
	replacement := r
	replacement.Operation = "op2"
	replacement.Supersedes = "op1"
	replacement.Plan = operation(t, s, "op1").Plan()
	replacement.Plan.Precondition.Details = []byte("generation=2")
	s = beginOperation(t, s, replacement)
	old, current := operation(t, s, "op1"), operation(t, s, "op2")
	if old.ReplacedBy() != "op2" || current.Supersedes() != "op1" || len(old.Applied()) != 1 || len(current.Applied()) != 0 || current.Verification() != nil {
		t.Fatal("replacement lost history or inherited execution state")
	}
	s = advanceOperation(t, s, "op2", schema.StartApplying)
	s = advanceOperation(t, s, "op2", schema.RecordApplied)
	oldVerification.Operation = "op2"
	if _, err := schema.Advance(s, schema.AdvanceRequest{Operation: "op2", ExpectedOperationRevision: 3, Action: schema.RecordVerified, Verification: oldVerification}); err == nil {
		t.Fatal("replacement accepted old plan verification")
	}
}

func TestUnresolvedObservationPersistsBlocked(t *testing.T) {
	s, _ := initialOperation(t)
	request := schema.BeginRequest{Operation: "unresolved", ExpectedSchemaRevision: 1, Observation: schema.Observation{Resource: "users", Evidence: schema.PayloadEvidence}, BlockedReason: "decoder cannot establish historical layout"}
	s = beginOperation(t, s, request)
	if operation(t, s, "unresolved").Phase() != schema.OperationBlocked || s.Bindings()[0].SourceVersion != "v1" {
		t.Fatal("unresolved observation activated")
	}
	if _, err := schema.Advance(s, schema.AdvanceRequest{Operation: "unresolved", ExpectedOperationRevision: 1, Action: schema.StartApplying}); !errors.Is(err, schema.ErrTransition) {
		t.Fatal(err)
	}
}

func TestBeginChecksBindingsVersionsAndPlanAdmission(t *testing.T) {
	for name, mutate := range map[string]func(*schema.BeginRequest){
		"revision":              func(r *schema.BeginRequest) { r.ExpectedSchemaRevision = 0 },
		"missing version":       func(r *schema.BeginRequest) { r.Versions = nil },
		"desired binding":       func(r *schema.BeginRequest) { r.Plan.Desired = "v1" },
		"wrong destination":     func(r *schema.BeginRequest) { r.Plan.Destination = "elsewhere" },
		"unknown policy":        func(r *schema.BeginRequest) { r.Plan.Permission.Permission = schema.Unproven },
		"unknown assessment":    func(r *schema.BeginRequest) { r.Plan.Assessment.Resume.Status = schema.Unknown },
		"backfill":              func(r *schema.BeginRequest) { r.Plan.RequiresBackfill = true },
		"duplicate step":        func(r *schema.BeginRequest) { r.Plan.Steps = append(r.Plan.Steps, r.Plan.Steps[0]) },
		"unresolved new schema": func(r *schema.BeginRequest) { r.Observation.Model = nil },
		"sampled new schema":    func(r *schema.BeginRequest) { r.Observation.Evidence = schema.InferredEvidence },
		"wrong version fingerprint": func(r *schema.BeginRequest) {
			v := r.Versions[0]
			r.Versions[0] = rowmodel.NewSchemaVersion(v.ID(), v.Previous(), v.Model(), rowmodel.SchemaFingerprint{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, r := initialOperation(t)
			mutate(&r)
			if _, err := schema.Begin(s, r); err == nil {
				t.Fatal("invalid begin accepted")
			}
			if s.Pending() != "" || s.Revision() != 1 {
				t.Fatal("failed begin mutated state")
			}
		})
	}
	s, r := initialOperation(t)
	s = beginOperation(t, s, r)
	r.Operation = "op2"
	if _, err := schema.Begin(s, r); !errors.Is(err, schema.ErrPending) {
		t.Fatal(err)
	}
	r.Supersedes = "op1"
	if _, err := schema.Begin(s, r); !errors.Is(err, schema.ErrPending) {
		t.Fatal("replaced nonblocked operation", err)
	}
}

// fakeStore models atomic ownership + persistence. It is deliberately test-only:
// retaining a State across coordinator restarts is not a production datastore.
type fakeStore struct {
	mu    sync.Mutex
	owner int
	state schema.State
}

var errStaleOwner = errors.New("stale owner")

func (f *fakeStore) update(owner int, fn func(schema.State) (schema.State, error)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if owner != f.owner {
		return errStaleOwner
	}
	next, err := fn(f.state)
	if err != nil {
		return err
	}
	f.state = next
	return nil
}
func (f *fakeStore) takeover() int { f.mu.Lock(); defer f.mu.Unlock(); f.owner++; return f.owner }

// fakeDestination models inspection-driven convergence across a crash after an
// effect but before its completion receipt. Same-named incompatible fields fail.
type fakeDestination struct {
	fields  map[string][]byte
	effects int
}

func (d *fakeDestination) apply(step schema.Step) error {
	if existing, ok := d.fields[step.ID]; ok {
		if !reflect.DeepEqual(existing, step.Payload) {
			return fmt.Errorf("precondition conflict for %s", step.ID)
		}
		return nil
	}
	d.fields[step.ID] = append([]byte{}, step.Payload...)
	d.effects++
	return nil
}

func TestRecoveryAfterEffectAndLostActivationResponse(t *testing.T) {
	initial, r := initialOperation(t)
	r.Plan.Steps = append(r.Plan.Steps, schema.Step{ID: "second", Kind: schema.AddField, Summary: "second fake effect", Payload: []byte("second")})
	store := &fakeStore{owner: 1, state: initial}
	destination := &fakeDestination{fields: map[string][]byte{}}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(store.update(1, func(s schema.State) (schema.State, error) { return schema.Begin(s, r) }))
	start := schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: 1, Action: schema.StartApplying}
	must(store.update(1, func(s schema.State) (schema.State, error) { return schema.Advance(s, start) }))
	plan := operation(t, store.state, "op1").Plan()
	must(destination.apply(plan.Steps[0]))
	// Crash: effect exists but completion is not persisted. A replacement worker
	// first proves predecessor quiescence/exclusion (simulated here), then takes over.
	owner := store.takeover()
	if err := store.update(1, func(s schema.State) (schema.State, error) { return schema.Advance(s, start) }); !errors.Is(err, errStaleOwner) {
		t.Fatal("stale exact retry passed ownership check", err)
	}
	for i, step := range operation(t, store.state, "op1").Plan().Steps {
		must(destination.apply(step))
		op := operation(t, store.state, "op1")
		ids := op.Applied()
		ids = append(ids, step.ID)
		receipt := schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: op.Revision(), Action: schema.RecordApplied, CompletedSteps: ids}
		must(store.update(owner, func(s schema.State) (schema.State, error) { return schema.Advance(s, receipt) }))
		if i == 0 {
			must(store.update(owner, func(s schema.State) (schema.State, error) { return schema.Advance(s, receipt) }))
		} // lost receipt response
	}
	if destination.effects != 2 {
		t.Fatalf("recovery duplicated effects: %d", destination.effects)
	}
	op := operation(t, store.state, "op1")
	verified := schema.AdvanceRequest{Operation: "op1", ExpectedOperationRevision: op.Revision(), Action: schema.RecordVerified, Verification: verification(t, op)}
	must(store.update(owner, func(s schema.State) (schema.State, error) { return schema.Advance(s, verified) }))
	activate := schema.ActivateRequest{Operation: "op1", ExpectedOperationRevision: operation(t, store.state, "op1").Revision(), ExpectedSchemaRevision: store.state.Revision()}
	must(store.update(owner, func(s schema.State) (schema.State, error) { return schema.Activate(s, activate) }))
	owner = store.takeover() // activation committed, response lost
	must(store.update(owner, func(s schema.State) (schema.State, error) { return schema.Activate(s, activate) }))
	if store.state.Revision() != 2 || operation(t, store.state, "op1").Phase() != schema.Activated {
		t.Fatal("activation retry changed history")
	}
	// An existing incompatible object is not evidence that the effect succeeded.
	destination.fields["nickname"] = []byte("wrong type")
	if err := destination.apply(plan.Steps[0]); err == nil {
		t.Fatal("same-named incompatible object accepted")
	}
}

func TestAtomicCompetingActivation(t *testing.T) {
	s, r := initialOperation(t)
	s = beginOperation(t, s, r)
	s = advanceOperation(t, s, "op1", schema.StartApplying)
	s = advanceOperation(t, s, "op1", schema.RecordApplied)
	s = advanceOperation(t, s, "op1", schema.RecordVerified)
	store := &fakeStore{owner: 1, state: s}
	r1 := schema.ActivateRequest{Operation: "op1", ExpectedOperationRevision: 4, ExpectedSchemaRevision: 1}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- store.update(1, func(s schema.State) (schema.State, error) { return schema.Activate(s, r1) })
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if store.state.Revision() != 2 {
		t.Fatal("competing exact activation incremented twice")
	}
}

func TestPlanFingerprintCoversExecutionInputs(t *testing.T) {
	_, r := initialOperation(t)
	original, err := r.Plan.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*schema.Plan){
		"connector":     func(p *schema.Plan) { p.Connector = "different" },
		"plan version":  func(p *schema.Plan) { p.PlanVersion = "2" },
		"desired":       func(p *schema.Plan) { p.Desired = "v3" },
		"generation":    func(p *schema.Plan) { p.Precondition.Details = []byte("generation=2") },
		"payload":       func(p *schema.Plan) { p.Steps[0].Payload = []byte("other") },
		"step identity": func(p *schema.Plan) { p.Steps[0].ID = "other" },
		"policy":        func(p *schema.Plan) { p.Permission.Permission = schema.Denied },
		"assessment":    func(p *schema.Plan) { p.Assessment.Resume.Status = schema.Blocked },
		"exclusion":     func(p *schema.Plan) { p.RequiresExclusiveOwner = false },
		"restart":       func(p *schema.Plan) { p.RequiresWriterRestart = false },
	} {
		t.Run(name, func(t *testing.T) {
			_, request := initialOperation(t)
			mutate(request.Plan)
			fp, err := request.Plan.Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			if fp == original {
				t.Fatal("plan input missing from fingerprint")
			}
		})
	}
}

func TestActivationChecksBothRevisions(t *testing.T) {
	s, r := initialOperation(t)
	s = beginOperation(t, s, r)
	s = advanceOperation(t, s, "op1", schema.StartApplying)
	s = advanceOperation(t, s, "op1", schema.RecordApplied)
	s = advanceOperation(t, s, "op1", schema.RecordVerified)
	for _, request := range []schema.ActivateRequest{{Operation: "op1", ExpectedOperationRevision: 3, ExpectedSchemaRevision: 1}, {Operation: "op1", ExpectedOperationRevision: 4, ExpectedSchemaRevision: 0}} {
		if _, err := schema.Activate(s, request); !errors.Is(err, schema.ErrRevision) {
			t.Fatal(err)
		}
	}
}

func TestPlanPreservesUnrelatedBindings(t *testing.T) {
	_, r := initialOperation(t)
	old := model()
	v1, err := schema.NewVersion("v1", "", old)
	if err != nil {
		t.Fatal(err)
	}
	other := model()
	other.Resource = "orders"
	orders, err := schema.NewVersion("orders1", "", other)
	if err != nil {
		t.Fatal(err)
	}
	bindings := []rowmodel.SchemaBinding{{Resource: "users", SourceVersion: "v1", DestinationVersion: "v1"}, {Resource: "orders", SourceVersion: "orders1", DestinationVersion: "orders1"}}
	s, err := schema.NewState(bindings, []rowmodel.SchemaVersion{v1, orders})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.Begin(s, r); err == nil {
		t.Fatal("plan silently removed another resource")
	}
	r.After = append(r.After, bindings[1])
	r.After[1].ProjectionVersion = "changed"
	if _, err := schema.Begin(s, r); err == nil {
		t.Fatal("plan changed another resource's projection")
	}
	r.After[1] = bindings[1]
	if _, err := schema.Begin(s, r); err != nil {
		t.Fatal(err)
	}
}
