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
