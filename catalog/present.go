package catalog

import (
	"github.com/galaxy-io/filament"
	ingestionv1 "github.com/galaxy-io/filament/api/ingestion/v1"
)

// presentResources is how the API shows discovered resources. The catalog
// answers in this shape because the API only ever forwards it.
func presentResources(resources []filament.Resource) []*ingestionv1.Resource {
	out := make([]*ingestionv1.Resource, 0, len(resources))
	for _, resource := range resources {
		out = append(out, &ingestionv1.Resource{
			Name:         resource.Name,
			IsSelectable: resource.Selectable,
			PrimaryKey:   resource.PrimaryKey,
			Selector:     resource.Selector,
			DisplayName:  resource.DisplayName,
			Metadata:     resource.Metadata,
		})
	}
	return out
}

// presentColumns is presentResources for a resource's columns.
func presentColumns(columns []filament.CursorColumn) []*ingestionv1.ResourceColumn {
	out := make([]*ingestionv1.ResourceColumn, 0, len(columns))
	for _, column := range columns {
		out = append(out, &ingestionv1.ResourceColumn{
			Name: column.Name, LogicalType: string(column.Logical), NativeType: column.Native,
			IsNullable: column.Nullable, IsPrimaryKey: column.PrimaryKey,
			IsCursorEligible: column.Eligible, IsCursorRecommended: column.Recommended,
			RecommendationRank: int32(column.Rank), Warning: column.Warning, //nolint:gosec // tiny rank
			IsConfigurable: column.Configurable, SupportsLookback: column.SupportsLookback,
		})
	}
	return out
}
