//go:build optionalbench

package kubernetes

import (
	"runtime"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

type benchmarkPointerPendingDelete struct {
	GVR  schema.GroupVersionResource
	Name *domain.ResourceName
}

type benchmarkPlainNameOptional struct {
	Value   domain.ResourceName
	Present bool
}

type benchmarkPlainSchemaOptional struct {
	Value   domain.ResourceQuerySchema
	Present bool
}

type benchmarkPairPendingDelete struct {
	GVR  schema.GroupVersionResource
	Name benchmarkPlainNameOptional
}

// The raw-pointer controls own fresh copies, matching some's contract.
// Borrowed-pointer controls point into existing fixture storage; they explicitly
// measure sharing an already-addressable value, which has different ownership.
var (
	optionalControlSchemaPair     benchmarkPlainSchemaOptional
	optionalControlName           domain.ResourceName
	optionalControlNamePointer    *domain.ResourceName
	optionalControlNameValues     []domain.ResourceName
	optionalControlNamePointers   []*domain.ResourceName
	optionalControlSchema         domain.ResourceQuerySchema
	optionalControlSchemaPointer  *domain.ResourceQuerySchema
	optionalControlSchemaValues   []domain.ResourceQuerySchema
	optionalControlSchemaPointers []*domain.ResourceQuerySchema
)

func benchmarkSomePlainSchema(value domain.ResourceQuerySchema) benchmarkPlainSchemaOptional {
	return benchmarkPlainSchemaOptional{Value: value, Present: true}
}

//domain:presence
func (value benchmarkPlainSchemaOptional) Get() (domain.ResourceQuerySchema, bool) {
	return value.Value, value.Present
}

func benchmarkOptionalPendingDeleteControls(b *testing.B, names []domain.ResourceName, identities []KubernetesObjectIdentity, keys []string, construct bool, percent int) {
	b.Run("repr=raw-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		entries := make(map[string]benchmarkPointerPendingDelete, len(keys))
		for _, key := range keys {
			entries[key] = benchmarkPointerPendingDelete{GVR: testGVR}
		}
		var sum uint64
		index := 0
		for b.Loop() {
			entry := benchmarkPointerPendingDelete{GVR: testGVR}
			if index%100 < percent {
				name := names[index%len(names)]
				if construct {
					var err error
					name, err = ObjectResourceName(identities[index%len(identities)])
					if err != nil {
						b.Fatal(err)
					}
				}
				entry.Name = &name
			}
			entries[keys[index%len(keys)]] = entry
			if name := entries[keys[(index+257)%len(keys)]].Name; name != nil {
				sum += uint64(len(*name))
			}
			index++
		}
		optionalBenchmarkSum = sum
		runtime.KeepAlive(entries)
	})
	if construct {
		return
	}
	b.Run("repr=borrowed-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		entries := make(map[string]benchmarkPointerPendingDelete, len(keys))
		for _, key := range keys {
			entries[key] = benchmarkPointerPendingDelete{GVR: testGVR}
		}
		var sum uint64
		index := 0
		for b.Loop() {
			entry := benchmarkPointerPendingDelete{GVR: testGVR}
			if index%100 < percent {
				entry.Name = &names[index%len(names)]
			}
			entries[keys[index%len(keys)]] = entry
			if name := entries[keys[(index+257)%len(keys)]].Name; name != nil {
				sum += uint64(len(*name))
			}
			index++
		}
		optionalBenchmarkSum = sum
		runtime.KeepAlive(entries)
	})
}

func benchmarkOptionalLocalControls(b *testing.B, names []domain.ResourceName, schemas []domain.ResourceQuerySchema) {
	b.Run("payload=name/repr=raw-value", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalRawValueName(names[index%len(names)])
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=name/repr=raw-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalRawPointerName(names[index%len(names)])
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=schema/repr=raw-value", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalRawValueSchema(schemas[index%len(schemas)])
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=schema/repr=raw-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalRawPointerSchema(schemas[index%len(schemas)])
			index++
		}
		optionalBenchmarkSum = sum
	})
}

func benchmarkOptionalCacheControls(b *testing.B, names []domain.ResourceName, schemas []domain.ResourceQuerySchema, percent int) {
	if percent != 0 && percent != 100 {
		return
	}
	b.Run("payload=name/repr=raw-value", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			if index%100 < percent {
				optionalControlName = names[index%len(names)]
			} else {
				optionalControlName = ""
			}
			value := optionalControlName
			if value != "" {
				sum += uint64(len(value))
			}
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=name/repr=raw-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			if index%100 < percent {
				value := names[index%len(names)]
				optionalControlNamePointer = &value
			} else {
				optionalControlNamePointer = nil
			}
			if pointer := optionalControlNamePointer; pointer != nil {
				value := *pointer
				sum += uint64(len(value))
			}
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=name/repr=borrowed-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			if index%100 < percent {
				optionalControlNamePointer = &names[index%len(names)]
			} else {
				optionalControlNamePointer = nil
			}
			if pointer := optionalControlNamePointer; pointer != nil {
				value := *pointer
				sum += uint64(len(value))
			}
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=schema/repr=raw-value", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			if index%100 < percent {
				optionalControlSchema = schemas[index%len(schemas)]
			} else {
				optionalControlSchema = domain.ResourceQuerySchema{}
			}
			value := optionalControlSchema
			if value.ResourceType != "" {
				sum += optionalBenchmarkSchemaSum(value)
			}
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=schema/repr=raw-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			if index%100 < percent {
				value := schemas[index%len(schemas)]
				optionalControlSchemaPointer = &value
			} else {
				optionalControlSchemaPointer = nil
			}
			if pointer := optionalControlSchemaPointer; pointer != nil {
				value := *pointer
				sum += optionalBenchmarkSchemaSum(value)
			}
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=schema/repr=borrowed-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			if index%100 < percent {
				optionalControlSchemaPointer = &schemas[index%len(schemas)]
			} else {
				optionalControlSchemaPointer = nil
			}
			if pointer := optionalControlSchemaPointer; pointer != nil {
				value := *pointer
				sum += optionalBenchmarkSchemaSum(value)
			}
			index++
		}
		optionalBenchmarkSum = sum
	})
}

func benchmarkOptionalBatchControls(b *testing.B, names []domain.ResourceName, schemas []domain.ResourceQuerySchema, percent int) {
	if percent != 10 && percent != 100 {
		return
	}
	b.Run("payload=name/repr=raw-value", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		for b.Loop() {
			batch := make([]domain.ResourceName, 1000)
			for i := range batch {
				if i%100 < percent {
					batch[i] = names[i%len(names)]
				}
			}
			optionalControlNameValues = batch
		}
	})
	b.Run("payload=name/repr=raw-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		for b.Loop() {
			batch := make([]*domain.ResourceName, 1000)
			for i := range batch {
				if i%100 < percent {
					value := names[i%len(names)]
					batch[i] = &value
				}
			}
			optionalControlNamePointers = batch
		}
	})
	b.Run("payload=name/repr=borrowed-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		for b.Loop() {
			batch := make([]*domain.ResourceName, 1000)
			for i := range batch {
				if i%100 < percent {
					batch[i] = &names[i%len(names)]
				}
			}
			optionalControlNamePointers = batch
		}
	})
	b.Run("payload=schema/repr=raw-value", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		for b.Loop() {
			batch := make([]domain.ResourceQuerySchema, 1000)
			for i := range batch {
				if i%100 < percent {
					batch[i] = schemas[i%len(schemas)]
				}
			}
			optionalControlSchemaValues = batch
		}
	})
	b.Run("payload=schema/repr=raw-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		for b.Loop() {
			batch := make([]*domain.ResourceQuerySchema, 1000)
			for i := range batch {
				if i%100 < percent {
					value := schemas[i%len(schemas)]
					batch[i] = &value
				}
			}
			optionalControlSchemaPointers = batch
		}
	})
	b.Run("payload=schema/repr=borrowed-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		for b.Loop() {
			batch := make([]*domain.ResourceQuerySchema, 1000)
			for i := range batch {
				if i%100 < percent {
					batch[i] = &schemas[i%len(schemas)]
				}
			}
			optionalControlSchemaPointers = batch
		}
	})
}

func benchmarkOptionalReadControls(b *testing.B, names []domain.ResourceName, schemas []domain.ResourceQuerySchema, entries, percent, stride int) {
	if entries != 100000 || stride != 7919 {
		return
	}
	b.Run("payload=name/repr=raw-value", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		values := make([]domain.ResourceName, entries)
		for i := range values {
			if i%100 < percent {
				values[i] = names[i%len(names)]
			}
		}
		var sum uint64
		index := 0
		for b.Loop() {
			value := values[index%len(values)]
			if value != "" {
				sum += uint64(len(value))
			}
			index += stride
		}
		optionalBenchmarkSum = sum
		runtime.KeepAlive(values)
	})
	b.Run("payload=name/repr=raw-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		values := make([]*domain.ResourceName, entries)
		for i := range values {
			if i%100 < percent {
				value := names[i%len(names)]
				values[i] = &value
			}
		}
		var sum uint64
		index := 0
		for b.Loop() {
			if pointer := values[index%len(values)]; pointer != nil {
				value := *pointer
				sum += uint64(len(value))
			}
			index += stride
		}
		optionalBenchmarkSum = sum
		runtime.KeepAlive(values)
	})
	b.Run("payload=schema/repr=raw-value", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		values := make([]domain.ResourceQuerySchema, entries)
		for i := range values {
			if i%100 < percent {
				values[i] = schemas[i%len(schemas)]
			}
		}
		var sum uint64
		index := 0
		for b.Loop() {
			value := values[index%len(values)]
			if value.ResourceType != "" {
				sum += optionalBenchmarkSchemaSum(value)
			}
			index += stride
		}
		optionalBenchmarkSum = sum
		runtime.KeepAlive(values)
	})
	b.Run("payload=schema/repr=raw-pointer", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		values := make([]*domain.ResourceQuerySchema, entries)
		for i := range values {
			if i%100 < percent {
				value := schemas[i%len(schemas)]
				values[i] = &value
			}
		}
		var sum uint64
		index := 0
		for b.Loop() {
			if pointer := values[index%len(values)]; pointer != nil {
				value := *pointer
				sum += optionalBenchmarkSchemaSum(value)
			}
			index += stride
		}
		optionalBenchmarkSum = sum
		runtime.KeepAlive(values)
	})
}

//go:noinline
func optionalBenchmarkLocalRawValueName(value domain.ResourceName) uint64 {
	return uint64(len(value))
}

//go:noinline
func optionalBenchmarkLocalRawPointerName(value domain.ResourceName) uint64 {
	pointer := &value
	if pointer == nil {
		return 0
	}
	value = *pointer
	return uint64(len(value))
}

//go:noinline
func optionalBenchmarkLocalPointerName(value domain.ResourceName) uint64 {
	wrapped := benchmarkSomePointer(value)
	if value, ok := wrapped.Get(); ok {
		return uint64(len(value))
	}
	return 0
}

//go:noinline
func optionalBenchmarkLocalValueName(value domain.ResourceName) uint64 {
	wrapped := benchmarkSomeValue(value)
	if value, ok := wrapped.Get(); ok {
		return uint64(len(value))
	}
	return 0
}

//go:noinline
func optionalBenchmarkLocalRawValueSchema(value domain.ResourceQuerySchema) uint64 {
	return optionalBenchmarkSchemaSum(value)
}

//go:noinline
func optionalBenchmarkLocalRawPointerSchema(value domain.ResourceQuerySchema) uint64 {
	pointer := &value
	if pointer == nil {
		return 0
	}
	value = *pointer
	return optionalBenchmarkSchemaSum(value)
}

//go:noinline
func optionalBenchmarkLocalPointerSchema(value domain.ResourceQuerySchema) uint64 {
	wrapped := benchmarkSomePointer(value)
	if value, ok := wrapped.Get(); ok {
		return optionalBenchmarkSchemaSum(value)
	}
	return 0
}

//go:noinline
func optionalBenchmarkLocalValueSchema(value domain.ResourceQuerySchema) uint64 {
	wrapped := benchmarkSomeValue(value)
	if value, ok := wrapped.Get(); ok {
		return optionalBenchmarkSchemaSum(value)
	}
	return 0
}

func prepareOptionalControlBenchmark(b *testing.B) {
	b.Helper()
	prepareOptionalBenchmark(b)
	b.Cleanup(func() {
		optionalControlSchemaPair = benchmarkPlainSchemaOptional{}
		optionalControlName = ""
		optionalControlNamePointer = nil
		optionalControlNameValues = nil
		optionalControlNamePointers = nil
		optionalControlSchema = domain.ResourceQuerySchema{}
		optionalControlSchemaPointer = nil
		optionalControlSchemaValues = nil
		optionalControlSchemaPointers = nil
	})
}

func benchmarkOptionalPairMapControl(b *testing.B, names []domain.ResourceName, identities []KubernetesObjectIdentity, keys []string, construct bool, percent int) {
	b.Run("repr=raw-pair", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		entries := make(map[string]benchmarkPairPendingDelete, len(keys))
		for _, key := range keys {
			entries[key] = benchmarkPairPendingDelete{GVR: testGVR}
		}
		var sum uint64
		index := 0
		for b.Loop() {
			entry := benchmarkPairPendingDelete{GVR: testGVR}
			if index%100 < percent {
				name := names[index%len(names)]
				if construct {
					var err error
					name, err = ObjectResourceName(identities[index%len(identities)])
					if err != nil {
						b.Fatal(err)
					}
				}
				entry.Name = benchmarkPlainNameOptional{Value: name, Present: true}
			}
			entries[keys[index%len(keys)]] = entry
			if name := entries[keys[(index+257)%len(keys)]].Name; name.Present {
				sum += uint64(len(name.Value))
			}
			index++
		}
		optionalBenchmarkSum = sum
		runtime.KeepAlive(entries)
	})
}

func benchmarkOptionalPairLocalControls(b *testing.B, names []domain.ResourceName, schemas []domain.ResourceQuerySchema) {
	b.Run("payload=name/repr=raw-pair", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalRawPairName(names[index%len(names)])
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=schema/repr=raw-pair", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalRawPairSchema(schemas[index%len(schemas)])
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=schema/repr=monomorphic-value", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalMonomorphicSchema(schemas[index%len(schemas)])
			index++
		}
		optionalBenchmarkSum = sum
	})
}

func benchmarkOptionalPairCacheControl(b *testing.B, schemas []domain.ResourceQuerySchema, percent int) {
	if percent != 0 && percent != 100 {
		return
	}
	b.Run("payload=schema/repr=raw-pair", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			if index%100 < percent {
				optionalControlSchemaPair = benchmarkPlainSchemaOptional{Value: schemas[index%len(schemas)], Present: true}
			} else {
				optionalControlSchemaPair = benchmarkPlainSchemaOptional{}
			}
			if optionalControlSchemaPair.Present {
				sum += optionalBenchmarkSchemaSum(optionalControlSchemaPair.Value)
			}
			index++
		}
		optionalBenchmarkSum = sum
	})
}

func benchmarkOptionalPairReadControls(b *testing.B, schemas []domain.ResourceQuerySchema, entries, percent, stride int) {
	if entries != 100000 || stride != 7919 {
		return
	}
	b.Run("payload=schema/repr=raw-pair", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		values := make([]benchmarkPlainSchemaOptional, entries)
		for i := range values {
			if i%100 < percent {
				values[i] = benchmarkSomePlainSchema(schemas[i%len(schemas)])
			}
		}
		var sum uint64
		index := 0
		for b.Loop() {
			if value := values[index%len(values)]; value.Present {
				sum += optionalBenchmarkSchemaSum(value.Value)
			}
			index += stride
		}
		optionalBenchmarkSum = sum
		runtime.KeepAlive(values)
	})
	b.Run("payload=schema/repr=monomorphic-value", func(b *testing.B) {
		prepareOptionalControlBenchmark(b)
		values := make([]benchmarkPlainSchemaOptional, entries)
		for i := range values {
			if i%100 < percent {
				values[i] = benchmarkSomePlainSchema(schemas[i%len(schemas)])
			}
		}
		var sum uint64
		index := 0
		for b.Loop() {
			if value, ok := values[index%len(values)].Get(); ok {
				sum += optionalBenchmarkSchemaSum(value)
			}
			index += stride
		}
		optionalBenchmarkSum = sum
		runtime.KeepAlive(values)
	})
}

//go:noinline
func optionalBenchmarkLocalRawPairName(value domain.ResourceName) uint64 {
	pair := benchmarkPlainNameOptional{Value: value, Present: true}
	if pair.Present {
		return uint64(len(pair.Value))
	}
	return 0
}

//go:noinline
func optionalBenchmarkLocalRawPairSchema(value domain.ResourceQuerySchema) uint64 {
	pair := benchmarkPlainSchemaOptional{Value: value, Present: true}
	if pair.Present {
		return optionalBenchmarkSchemaSum(pair.Value)
	}
	return 0
}

//go:noinline
func optionalBenchmarkLocalMonomorphicSchema(value domain.ResourceQuerySchema) uint64 {
	pair := benchmarkSomePlainSchema(value)
	if value, ok := pair.Get(); ok {
		return optionalBenchmarkSchemaSum(value)
	}
	return 0
}
