package schema_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/galaxy-io/filament/rowmodel"
	"github.com/galaxy-io/filament/schema"
)

func roundTripState(t *testing.T, s schema.State) schema.State {
	t.Helper()
	data, err := schema.MarshalRecord(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var stored schema.Snapshot
	if err := schema.UnmarshalRecord(data, &stored); err != nil {
		t.Fatal(err)
	}
	restored, err := schema.Restore(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, restored) {
		t.Fatal("state/request history did not round trip")
	}
	return restored
}
func TestStorageRestoresEveryPhaseAndExactReceipts(t *testing.T) {
	s, r := initialOperation(t)
	s = roundTripState(t, s)
	s = beginOperation(t, s, r)
	s = roundTripState(t, s)
	if _, err := schema.Begin(s, r); err != nil {
		t.Fatal("begin retry changed after restore", err)
	}
	for _, action := range []schema.Action{schema.StartApplying, schema.RecordApplied, schema.RecordVerified} {
		s = advanceOperation(t, s, "op1", action)
		s = roundTripState(t, s)
	}
	s = advanceOperation(t, s, "op1", schema.BlockOperation)
	s = roundTripState(t, s)
	r.Operation = "op2"
	r.Supersedes = "op1"
	s = beginOperation(t, s, r)
	s = roundTripState(t, s)
	for _, action := range []schema.Action{schema.StartApplying, schema.RecordApplied, schema.RecordVerified} {
		s = advanceOperation(t, s, "op2", action)
	}
	request := schema.ActivateRequest{Operation: "op2", ExpectedOperationRevision: 4, ExpectedSchemaRevision: 1}
	s, err := schema.Activate(s, request)
	if err != nil {
		t.Fatal(err)
	}
	s = roundTripState(t, s)
	if _, err := schema.Activate(s, request); err != nil {
		t.Fatal("activation retry lost", err)
	}
}
func TestStoredVersionPreservesPrivateFlagsAndEmptySlices(t *testing.T) {
	for _, empty := range []bool{false, true} {
		m := rowmodel.Schema{Resource: "events"}
		if empty {
			m.Fields = []rowmodel.Field{}
			m.PrimaryKey = []string{}
		}
		m, err := rowmodel.WithEnvelopeFields(m)
		if err != nil {
			t.Fatal(err)
		}
		v, err := schema.NewVersion("v", "", m)
		if err != nil {
			t.Fatal(err)
		}
		record := schema.StoreVersion(v)
		data, err := schema.MarshalRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		var out schema.VersionRecord
		if err := schema.UnmarshalRecord(data, &out); err != nil {
			t.Fatal(err)
		}
		restored, err := out.Restore()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(v, restored) {
			t.Fatal("private flags or nil/empty slices lost")
		}
	}
}
func TestStorageRejectsCorruptHistory(t *testing.T) {
	initial, r := initialOperation(t)
	s := beginOperation(t, initial, r)
	s = advanceOperation(t, s, "op1", schema.StartApplying)
	for name, mutate := range map[string]func(*schema.Snapshot){
		"unknown format":      func(s *schema.Snapshot) { s.Format = 99 },
		"version fingerprint": func(s *schema.Snapshot) { s.Versions[0].Fingerprint.SHA256[0] ^= 1 },
		"missing version":     func(s *schema.Snapshot) { s.Versions = s.Versions[:1] },
		"duplicate version":   func(s *schema.Snapshot) { s.Versions = append(s.Versions, s.Versions[0]) },
		"skip phase":          func(s *schema.Snapshot) { s.Operations[0].Advances[0].Action = schema.RecordVerified },
		"duplicate receipt": func(s *schema.Snapshot) {
			s.Operations[0].Advances = append(s.Operations[0].Advances, s.Operations[0].Advances[0])
		},
		"duplicate operation": func(s *schema.Snapshot) { s.Operations = append(s.Operations, s.Operations[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := s.Snapshot()
			mutate(&snapshot)
			if _, err := schema.Restore(snapshot); err == nil {
				t.Fatal("corrupt history accepted")
			}
		})
	}
	data, _ := schema.MarshalRecord(s.Snapshot())
	for _, bad := range [][]byte{append([]byte(" "), data...), bytes.Replace(data, []byte(`"Format":1`), []byte(`"Format":1,"Format":1`), 1), append(data, []byte("{}")...)} {
		var snapshot schema.Snapshot
		if err := schema.UnmarshalRecord(bad, &snapshot); err == nil {
			t.Fatal("ambiguous serialization accepted")
		}
	}
}
