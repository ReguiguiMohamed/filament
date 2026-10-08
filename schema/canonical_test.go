package schema_test

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/galaxy-io/filament/rowmodel"
	"github.com/galaxy-io/filament/schema"
)

func model() rowmodel.Schema {
	return rowmodel.Schema{Resource: "users", Engine: "postgres", Fields: []rowmodel.Field{
		{Name: "id", Logical: rowmodel.LogicalInt64, Native: "bigint"},
		{Name: "name", Logical: rowmodel.LogicalString, Native: "text", Nullable: true},
	}, PrimaryKey: []string{"id"}}
}

func TestCanonicalGolden(t *testing.T) {
	const golden = `{"format":1,"resource":"users","destination":"","engine":"postgres","envelope":false,"projected_envelope":false,"fields":[{"name":"id","logical":"int64","native":"bigint","nullable":false,"precision":0,"scale":0},{"name":"name","logical":"string","native":"text","nullable":true,"precision":0,"scale":0}],"keys":["id"]}`
	data, err := schema.Encode(model())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != golden {
		t.Fatalf("canonical format changed:\n%s", data)
	}
	fp, err := schema.Fingerprint(model())
	if err != nil {
		t.Fatal(err)
	}
	if fp.Format != 1 || fp.SHA256 != sha256.Sum256([]byte(golden)) {
		t.Fatalf("unexpected fingerprint: %+v", fp)
	}
	restored, err := schema.Decode(data)
	if err != nil || !restored.Equal(model()) {
		t.Fatalf("round trip: %v, %v", restored, err)
	}
}

func TestCanonicalSemanticInputs(t *testing.T) {
	original := model()
	baseline, _ := schema.Fingerprint(original)
	mutations := map[string]func(*rowmodel.Schema){
		"resource":    func(s *rowmodel.Schema) { s.Resource = "other" },
		"destination": func(s *rowmodel.Schema) { s.DestinationResource = "output" },
		"engine":      func(s *rowmodel.Schema) { s.Engine = "mysql" },
		"name":        func(s *rowmodel.Schema) { s.Fields[1].Name = "label" },
		"order":       func(s *rowmodel.Schema) { s.Fields[0], s.Fields[1] = s.Fields[1], s.Fields[0] },
		"logical":     func(s *rowmodel.Schema) { s.Fields[1].Logical = rowmodel.LogicalJSON },
		"native":      func(s *rowmodel.Schema) { s.Fields[0].Native = "int8" },
		"nullable":    func(s *rowmodel.Schema) { s.Fields[0].Nullable = true },
		"precision":   func(s *rowmodel.Schema) { s.Fields[1].Precision = 20 },
		"scale":       func(s *rowmodel.Schema) { s.Fields[1].Scale = -2 },
		"key":         func(s *rowmodel.Schema) { s.PrimaryKey = []string{"name", "id"} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := original.Clone()
			mutate(&changed)
			fp, err := schema.Fingerprint(changed)
			if err != nil {
				t.Fatal(err)
			}
			if fp == baseline {
				t.Fatal("semantic change did not affect fingerprint")
			}
			encoded, _ := schema.Encode(changed)
			decoded, err := schema.Decode(encoded)
			if err != nil || !decoded.Equal(changed) {
				t.Fatalf("round trip failed: %v", err)
			}
		})
	}
	first := original.Clone()
	first.PrimaryKey = []string{"id", "name"}
	second := first.Clone()
	second.PrimaryKey = []string{"name", "id"}
	a, _ := schema.Fingerprint(first)
	b, _ := schema.Fingerprint(second)
	if a == b {
		t.Fatal("ordered keys must differ")
	}
}

func TestCanonicalEnvelopeAndAuditRoundTrips(t *testing.T) {
	for _, projected := range []bool{false, true} {
		var s rowmodel.Schema
		var err error
		if projected {
			s, err = rowmodel.WithEventMetadataFields(model())
		} else {
			s, err = rowmodel.WithEnvelopeFields(model())
		}
		if err != nil {
			t.Fatal(err)
		}
		// Default Schema JSON loses provenance despite retaining the same fields.
		plain := rowmodel.Schema{Resource: s.Resource, Engine: s.Engine, Fields: s.Fields, PrimaryKey: s.PrimaryKey}
		markedFP, _ := schema.Fingerprint(s)
		plainFP, _ := schema.Fingerprint(plain)
		if markedFP == plainFP {
			t.Fatal("semantic marker was omitted")
		}
		for _, audit := range []bool{false, true} {
			candidate := s
			if audit {
				candidate, err = rowmodel.WithAuditFields(rowmodel.AsCDCAppendHistory(s), true)
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := schema.Encode(candidate)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := schema.Decode(data)
			if err != nil || !restored.Equal(candidate) {
				t.Fatalf("semantic flags lost: %v", err)
			}
		}
	}
}

func TestCanonicalRejectsAmbiguousEncoding(t *testing.T) {
	data, _ := schema.Encode(model())
	for name, input := range map[string][]byte{
		"unknown format":    bytes.Replace(data, []byte(`"format":1`), []byte(`"format":2`), 1),
		"duplicate":         bytes.Replace(data, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1),
		"unknown field":     bytes.Replace(data, []byte(`"format":1`), []byte(`"format":1,"extra":true`), 1),
		"missing":           bytes.Replace(data, []byte(`"envelope":false,`), nil, 1),
		"trailing":          append(append([]byte{}, data...), []byte(`{}`)...),
		"whitespace":        append([]byte(" "), data...),
		"conflicting flags": bytes.Replace(bytes.Replace(data, []byte(`"envelope":false`), []byte(`"envelope":true`), 1), []byte(`"projected_envelope":false`), []byte(`"projected_envelope":true`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := schema.Decode(input); err == nil {
				t.Fatal("accepted ambiguous encoding")
			}
		})
	}
	nilModel := rowmodel.Schema{}
	emptyModel := rowmodel.Schema{Fields: []rowmodel.Field{}, PrimaryKey: []string{}}
	a, _ := schema.Encode(nilModel)
	b, _ := schema.Encode(emptyModel)
	if !bytes.Equal(a, b) {
		t.Fatal("nil and empty lists differ")
	}
	if _, err := schema.Decode([]byte(strings.Replace(string(a), `"fields":[]`, `"fields":null`, 1))); err == nil {
		t.Fatal("accepted noncanonical null list")
	}
}

func TestSchemaValidation(t *testing.T) {
	for name, change := range map[string]func(*rowmodel.Schema){
		"duplicate":     func(s *rowmodel.Schema) { s.Fields[1].Name = "id" },
		"empty":         func(s *rowmodel.Schema) { s.Fields[1].Name = "" },
		"missing key":   func(s *rowmodel.Schema) { s.PrimaryKey = []string{"missing"} },
		"duplicate key": func(s *rowmodel.Schema) { s.PrimaryKey = []string{"id", "id"} },
		"invalid UTF8":  func(s *rowmodel.Schema) { s.Fields[1].Native = string([]byte{0xff}) },
	} {
		t.Run(name, func(t *testing.T) {
			s := model()
			change(&s)
			if _, err := schema.Encode(s); err == nil {
				t.Fatal("invalid schema accepted")
			}
		})
	}
}

func TestVersionOwnsSnapshot(t *testing.T) {
	input := model()
	expected := input.Clone()
	version, err := schema.NewVersion("s2", "s1", input)
	if err != nil {
		t.Fatal(err)
	}
	input.Fields[0].Native = "changed"
	input.PrimaryKey[0] = "changed"
	output := version.Model()
	output.Fields[0].Name = "changed"
	output.PrimaryKey[0] = "changed"
	if !version.Model().Equal(expected) {
		t.Fatal("version aliases input or returned model")
	}
	if version.ID() != "s2" || version.Previous() != "s1" {
		t.Fatal("version identity changed")
	}
	fp, _ := schema.Fingerprint(expected)
	if version.Fingerprint() != fp {
		t.Fatal("version fingerprint differs")
	}
	for _, ids := range [][2]rowmodel.SchemaVersionID{{"", ""}, {"s1", "s1"}} {
		if _, err := schema.NewVersion(ids[0], ids[1], expected); err == nil {
			t.Fatal("invalid version accepted")
		}
	}
}

func FuzzCanonicalRoundTrip(f *testing.F) {
	golden, _ := schema.Encode(model())
	f.Add(golden)
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := schema.Decode(data)
		if err != nil {
			return
		}
		encoded, err := schema.Encode(s)
		if err != nil || !bytes.Equal(data, encoded) {
			t.Fatalf("accepted bytes do not round trip: %v", err)
		}
	})
}
