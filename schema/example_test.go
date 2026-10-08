package schema_test

import (
	"fmt"

	"github.com/galaxy-io/filament/rowmodel"
	"github.com/galaxy-io/filament/schema"
)

func ExampleEvaluatePolicy() {
	before := rowmodel.Schema{Resource: "users", Fields: []rowmodel.Field{{Name: "id", Logical: rowmodel.LogicalInt64}}, PrimaryKey: []string{"id"}}
	after := before.Clone()
	after.Fields = append(after.Fields, rowmodel.Field{Name: "nickname", Logical: rowmodel.LogicalString, Nullable: true})
	diff, err := schema.Compare(before, after)
	if err != nil {
		panic(err)
	}
	decision := schema.EvaluatePolicy(diff, schema.Policy{Mode: schema.Compatible, AllowNullableAdd: true}, nil)
	fmt.Println(decision.Permission)
	// The allowed category is not proof that a decoder, sink, or retained cursor
	// supports the transition. No connector effects or lifecycle calls occur here.
	fmt.Println((schema.Assessment{}).Status())
	// Output:
	// allowed
	// unknown
}
