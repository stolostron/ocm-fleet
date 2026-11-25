package protection

import (
	"maps"
	"slices"
)

func WriteLocalSliceAlias(record Record) {
	items := record.items
	items[0] = 7 // want "modify protection.Record through NewRecord"
	view := items[1:]
	copy(view, []int{8}) // want "modify protection.Record through NewRecord"
	items = slices.Clone(items)
	items[0] = 9
}

func WriteLocalMapAlias(record Record) {
	var keys = record.keys
	keys[0] = 1     // want "modify protection.Record through NewRecord"
	delete(keys, 0) // want "modify protection.Record through NewRecord"
	keys = maps.Clone(keys)
	keys[0] = 2
}

func WriteAliasesInsideBlocks(record Record, enabled bool) {
	items := record.items
	{
		items[0] = 7 // want "modify protection.Record through NewRecord"
	}
	items = record.items
	if enabled {
		items[0] = 8 // want "modify protection.Record through NewRecord"
	}
	if keys := record.keys; enabled {
		delete(keys, 0) // want "modify protection.Record through NewRecord"
	}
}

func RebindAliasesInsideIf(record Record, enabled bool) {
	items := record.items
	if enabled {
		items = []int{1}
		items[0] = 7
	} else {
		items[0] = 8 // want "modify protection.Record through NewRecord"
	}
	items = record.items
	if items = []int{1}; enabled {
		items[0] = 9
	}
}

func ShadowAliasesInsideIf(record Record, enabled bool) {
	items := record.items
	if enabled {
		items := []int{1}
		items[0] = 7
	} else if enabled = !enabled; enabled {
		items[0] = 8 // want "modify protection.Record through NewRecord"
	}
}

func WriteUnchangedAliasesAfterBlocks(record Record, enabled bool) {
	items := record.items
	if enabled {
		items := []int{1}
		items[0] = 7
	}
	items[0] = 8 // want "modify protection.Record through NewRecord"
	keys := record.keys
	{
		_ = len(keys)
	}
	keys[0] = 1 // want "modify protection.Record through NewRecord"
}

func WriteAfterIndependentRebind(record Record, enabled bool) {
	items := record.items
	if enabled {
		items = []int{1}
	} else {
		items = []int{2}
	}
	items[0] = 7
}

func WriteAfterScopedAliasEscapes(record Record, enabled bool) {
	items := record.items
	if enabled {
		detach := func() { items = []int{1} }
		detach()
	} else {
		items = []int{2}
	}
	items[0] = 7
}

func SwapLocalAliases(record Record) {
	protected, independent := record.items, make([]int, 1)
	protected, independent = independent, protected
	protected[0] = 1
	independent[0] = 2 // want "modify protection.Record through NewRecord"
}

func ReturnLocalAlias(record Record) []int {
	items := record.items
	return items // want "do not expose mutable contents of protection.Record"
}

func ReturnBoxedAliases(record Record) (any, any) {
	return any(record.items), any(any(record.keys)) // want "do not expose mutable contents of protection.Record" "do not expose mutable contents of protection.Record"
}

func ReturnIndependentBoxedStorage(record Record) (any, any) {
	return any(record.items[:0:0]), any(maps.Clone(record.keys))
}

func ReturnNamedSliceAlias(record Record) (items []int) {
	items = record.items
	return // want "do not expose mutable contents of protection.Record"
}

func ReturnNamedMapAlias(record Record) (keys map[int]int) {
	keys = record.keys
	return // want "do not expose mutable contents of protection.Record"
}

func ReturnReboundAlias(record Record) (items []int, keys map[int]int) {
	items, keys = record.items, record.keys
	items, keys = slices.Clone(items), maps.Clone(keys)
	return
}

func ReturnInaccessibleAlias(record Record) []int {
	items := record.items[:0:0]
	return items
}

func ReadLocalAliases(record Record, independent []int) {
	items := record.items
	_ = items[0]
	copy(independent, items)
	keys := record.keys
	_ = keys[0]
}

func TrackAliasAfterClosure(record Record) {
	_ = func() {}
	items := record.items
	items[0] = 7 // want "modify protection.Record through NewRecord"
}

func ForgetAliasesThatAClosureCanRebind(record Record) {
	items := record.items
	detach := func() { items = []int{1} }
	detach()
	items[0] = 7
}

func TrackAliasAfterAddressTaking(record Record) {
	items := record.items
	unrelated := []int{1}
	_ = &unrelated
	items[0] = 7 // want "modify protection.Record through NewRecord"
	view := record.items
	view[0] = 7 // want "modify protection.Record through NewRecord"
}

func CheckEachVariableInitializer(record Record) {
	var (
		items = record.items
		_     = copy(items, []int{7}) // want "modify protection.Record through NewRecord"
	)
}

type itemView []int
type keyView map[int]int

func ModifyThroughStorageConversions(record Record) {
	itemView(record.items)[0] = 7   // want "modify protection.Record through NewRecord"
	delete(keyView(record.keys), 0) // want "modify protection.Record through NewRecord"
	alias := itemView(record.items)
	alias[0] = 7 // want "modify protection.Record through NewRecord"
}

func ReturnConvertedSlice(record Record) []int {
	return itemView(record.items) // want "do not expose mutable contents of protection.Record"
}

func ReturnConvertedMap(record Record) map[int]int {
	return keyView(record.keys) // want "do not expose mutable contents of protection.Record"
}

func ReturnInaccessibleConvertedSlice(record Record) []int {
	return itemView(record.items[:0:0])
}

func ReturnConstrainedSliceView[S ~[]int](record Record) S {
	var items S = record.items
	return items[:] // want "do not expose mutable contents of protection.Record"
}

func ReturnInaccessibleConstrainedSliceView[S ~[]int](record Record) S {
	var items S = record.items
	return items[:0:0]
}

func ConvertConstrainedSliceToArray[S ~[]int](record Record) (*[1]int, [1]int, *[0]int) {
	var items S = record.items
	return (*[1]int)(items), [1]int(items), (*[0]int)(items) // want "do not expose mutable representation of protection.Record"
}
