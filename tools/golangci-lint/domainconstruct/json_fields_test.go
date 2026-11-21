package domainconstruct

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"testing"
)

func TestJSONFieldSelectionUsesItsFullBudget(t *testing.T) {
	fields := make([]*types.Var, maxInferenceWork+1)
	for index := range fields {
		fields[index] = types.NewField(token.NoPos, nil, fmt.Sprintf("Field%d", index), types.Typ[types.Int], false)
	}
	var cache jsonFieldCache
	selected := slices.Collect(cache.fields(types.NewStruct(fields[:maxInferenceWork], nil), true))
	if len(selected) != maxInferenceWork {
		t.Errorf("selected %d out of %d fields", len(selected), maxInferenceWork)
	}
	broad := types.NewStruct(fields, nil)
	if selected := slices.Collect(cache.fields(broad, true)); len(selected) != 0 || cache[broad].complete {
		t.Errorf("incomplete field discovery published %d selected fields", len(selected))
	}
}
