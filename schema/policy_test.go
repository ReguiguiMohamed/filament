package schema_test

import (
	"testing"

	"github.com/galaxy-io/filament/rowmodel"
	"github.com/galaxy-io/filament/schema"
)

func TestPolicyConservativeDefaults(t *testing.T) {
	cases := []struct {
		name   string
		change func(*rowmodel.Schema)
		policy schema.Policy
		want   schema.Permission
	}{
		{"unchanged", func(s *rowmodel.Schema) {}, schema.Policy{}, schema.Allowed},
		{"strict addition", addNullable, schema.Policy{}, schema.Denied},
		{"compatible opt in required", addNullable, schema.Policy{Mode: schema.Compatible}, schema.Denied},
		{"nullable addition", addNullable, schema.Policy{Mode: schema.Compatible, AllowNullableAdd: true}, schema.Allowed},
		{"unknown addition", func(s *rowmodel.Schema) { addNullable(s); s.Fields[2].Logical = rowmodel.LogicalUnknown }, schema.Policy{Mode: schema.Compatible, AllowNullableAdd: true}, schema.Unproven},
		{"required addition", func(s *rowmodel.Schema) { addNullable(s); s.Fields[2].Nullable = false }, schema.Policy{Mode: schema.Compatible, AllowNullableAdd: true}, schema.Denied},
		{"drop", func(s *rowmodel.Schema) { s.Fields = s.Fields[:1] }, schema.Policy{Mode: schema.Compatible}, schema.Denied},
		{"key", func(s *rowmodel.Schema) { s.PrimaryKey = nil }, schema.Policy{Mode: schema.Compatible}, schema.Denied},
		{"tighten", func(s *rowmodel.Schema) { s.Fields[1].Nullable = false }, schema.Policy{Mode: schema.Compatible, AllowRelaxNullability: true}, schema.Denied},
		{"relax", func(s *rowmodel.Schema) { s.Fields[0].Nullable = true }, schema.Policy{Mode: schema.Compatible, AllowRelaxNullability: true}, schema.Allowed},
		{"order", func(s *rowmodel.Schema) { s.Fields[0], s.Fields[1] = s.Fields[1], s.Fields[0] }, schema.Policy{Mode: schema.Compatible}, schema.Allowed},
		{"unknown policy", func(s *rowmodel.Schema) {}, schema.Policy{Mode: "permissive"}, schema.Denied},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			before, after := model(), model()
			tt.change(&after)
			diff, err := schema.Compare(before, after)
			if err != nil {
				t.Fatal(err)
			}
			got := schema.EvaluatePolicy(diff, tt.policy, nil)
			if got.Permission != tt.want {
				t.Fatalf("got %+v, want %s", got, tt.want)
			}
		})
	}
}
func addNullable(s *rowmodel.Schema) {
	s.Fields = append(s.Fields, rowmodel.Field{Name: "nickname", Logical: rowmodel.LogicalString, Nullable: true})
}

func TestTypePolicyRequiresExactConnectorProof(t *testing.T) {
	before, after := model(), model()
	after.Fields[0].Logical = rowmodel.LogicalDecimal
	after.Fields[0].Native = "numeric(20,0)"
	after.Fields[0].Precision = 20
	diff, err := schema.Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	p := schema.Policy{Mode: schema.Compatible, AllowTypeWidening: true}
	proof := schema.TypeProof{Before: before.Fields[0], After: after.Fields[0], Relation: schema.TypeWidening}
	if got := schema.EvaluatePolicy(diff, p, nil); got.Permission != schema.Unproven {
		t.Fatal(got)
	}
	if got := schema.EvaluatePolicy(diff, p, []schema.TypeProof{proof}); got.Permission != schema.Allowed {
		t.Fatal(got)
	}
	stale := proof
	stale.After.Native = "other"
	if got := schema.EvaluatePolicy(diff, p, []schema.TypeProof{stale}); got.Permission != schema.Unproven {
		t.Fatal(got)
	}
	incompatible := proof
	incompatible.Relation = schema.TypeIncompatible
	if got := schema.EvaluatePolicy(diff, p, []schema.TypeProof{incompatible}); got.Permission != schema.Denied {
		t.Fatal(got)
	}
	if got := schema.EvaluatePolicy(diff, p, []schema.TypeProof{proof, incompatible}); got.Permission != schema.Unproven {
		t.Fatal(got)
	}
	// Denial wins over an unrelated unproven type change in either order.
	for _, changes := range [][]schema.Change{{{Kind: schema.DropField}, diff.Changes[0]}, {diff.Changes[0], {Kind: schema.DropField}}} {
		if got := schema.EvaluatePolicy(schema.Diff{Changes: changes}, p, nil); got.Permission != schema.Denied {
			t.Fatal(got)
		}
	}
}

func TestCompatibilityObligationsAreIndependent(t *testing.T) {
	if got := (schema.Assessment{}).Status(); got != schema.Unknown {
		t.Fatal(got)
	}
	compatible := schema.CompatibilityResult{Status: schema.ProvenCompatible}
	a := schema.Assessment{Decode: compatible, Project: compatible, Write: compatible, Resume: compatible}
	if got := a.Status(); got != schema.ProvenCompatible {
		t.Fatal(got)
	}
	a.Decode.Status = schema.TransitionRequired
	if got := a.Status(); got != schema.TransitionRequired {
		t.Fatal(got)
	}
	a.Resume.Status = ""
	if got := a.Status(); got != schema.Unknown {
		t.Fatal(got)
	}
	a.Write.Status = schema.Blocked
	if got := a.Status(); got != schema.Blocked {
		t.Fatal(got)
	}
	a.Write.Status = "future_status"
	if got := a.Status(); got != schema.Unknown {
		t.Fatal(got)
	}
}
