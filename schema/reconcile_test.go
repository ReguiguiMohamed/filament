package schema_test

import (
	"errors"
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
