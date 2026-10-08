package schema_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/galaxy-io/filament/rowmodel"
	"github.com/galaxy-io/filament/schema"
)

func TestObservationRequiresCompleteAuthoritativeEvidence(t *testing.T) {
	accepted := model()
	sample := model()
	sample.Fields = sample.Fields[:1]
	for _, evidence := range []schema.EvidenceKind{schema.CatalogEvidence, schema.DeclaredEvidence, schema.InferredEvidence, schema.OrderedEvidence, schema.PayloadEvidence} {
		for _, complete := range []bool{false, true} {
			for _, authoritative := range []bool{false, true} {
				observation := schema.Observation{Resource: "users", Model: &sample, Evidence: evidence, Complete: complete, Authoritative: authoritative}
				result, err := schema.Observe(accepted, observation, schema.Projection{})
				if err != nil {
					t.Fatal(err)
				}
				resolved := complete && authoritative && evidence != schema.InferredEvidence
				if (result.Diff != nil) != resolved {
					t.Fatalf("invalid evidence resolution: %+v -> %+v", observation, result)
				}
				if resolved && (len(result.Diff.Changes) != 1 || result.Diff.Changes[0].Kind != schema.DropField) {
					t.Fatalf("expected authoritative drop: %+v", result)
				}
			}
		}
	}
	unresolved, err := schema.Observe(accepted, schema.Observation{Resource: "users", Evidence: schema.PayloadEvidence}, schema.Projection{})
	if err != nil || unresolved.Diff != nil || unresolved.Reason == "" {
		t.Fatalf("unresolved mismatch: %+v %v", unresolved, err)
	}
}

func TestObservationExplicitProjection(t *testing.T) {
	accepted, next := model(), model()
	next.Fields = append(next.Fields, rowmodel.Field{Name: "nickname", Logical: rowmodel.LogicalString, Nullable: true})
	o := schema.Observation{Resource: "users", Model: &next, Evidence: schema.DeclaredEvidence, Complete: true, Authoritative: true}
	full, err := schema.Observe(accepted, o, schema.Projection{})
	if err != nil || len(full.Diff.Changes) != 1 {
		t.Fatalf("full observation: %+v %v", full, err)
	}
	projected, err := schema.Observe(accepted, o, schema.Projection{Version: "p1", Fields: []string{"id", "name"}})
	if err != nil || len(projected.Diff.Changes) != 0 {
		t.Fatalf("intentional exclusion became drift: %+v %v", projected, err)
	}
	if len(next.Fields) != 3 || len(accepted.Fields) != 2 {
		t.Fatal("projection mutated inputs")
	}
	if _, err := schema.Observe(accepted, o, schema.Projection{Fields: []string{"id"}}); err == nil {
		t.Fatal("unversioned projection accepted")
	}
	if _, err := schema.Observe(accepted, o, schema.Projection{Version: "p1", Fields: []string{"id", "id"}}); err == nil {
		t.Fatal("duplicate projection accepted")
	}
}

func TestMismatchErrorOwnershipAndWrapping(t *testing.T) {
	cause := errors.New("decoder mismatch")
	model := model()
	observation := schema.Observation{Resource: "users", Model: &model, Evidence: schema.PayloadEvidence, Reference: &schema.Reference{Format: "object/v1", Data: []byte("first")}}
	mismatch := schema.NewMismatchError(observation, cause)
	model.Fields[0].Name = "mutated"
	observation.Reference.Data[0] = 'x'
	wrapped := fmt.Errorf("extract: %w", mismatch)
	var found *schema.MismatchError
	if !errors.As(wrapped, &found) || !errors.Is(wrapped, cause) {
		t.Fatal("error chain lost")
	}
	restored := found.Observation()
	if restored.Model.Fields[0].Name != "id" || string(restored.Reference.Data) != "first" {
		t.Fatal("mismatch evidence aliases input")
	}
	restored.Model.Fields[0].Name = "again"
	restored.Reference.Data[0] = 'z'
	if found.Observation().Model.Fields[0].Name != "id" || string(found.Observation().Reference.Data) != "first" {
		t.Fatal("mismatch getter aliases evidence")
	}
	if found.Error() != `schema mismatch for "users": decoder mismatch` {
		t.Fatal(found.Error())
	}
}

func TestProjectionNeverInventsCompoundKeyUniqueness(t *testing.T) {
	before, after := model(), model()
	before.PrimaryKey = []string{"id", "name"}
	after.PrimaryKey = []string{"id"}
	observation := schema.Observation{Resource: "users", Model: &after, Evidence: schema.CatalogEvidence, Complete: true, Authoritative: true}
	result, err := schema.Observe(before, observation, schema.Projection{Version: "p1", Fields: []string{"id"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diff.OldKey) != 0 || len(result.Diff.NewKey) != 1 || len(result.Diff.Changes) != 1 || result.Diff.Changes[0].Kind != schema.ChangeKey {
		t.Fatalf("projection invented equivalent unique keys: %+v", result.Diff)
	}
}
