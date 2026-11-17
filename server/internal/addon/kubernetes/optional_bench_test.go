//go:build optionalbench

package kubernetes

import (
	"fmt"
	"runtime"
	"strconv"
	"testing"
	"unsafe"

	"google.golang.org/protobuf/types/known/structpb"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
	"github.com/stolostron/ocm-fleet/server/pkg/optional"
)

// The opt-in tag keeps the deliberate zero-sentinel controls outside ordinary
// domain lint. Inline storage uses the shared production Optional; pointer
// storage retains the earlier owning-copy trial for comparison.
type benchmarkValueOptional[T any] = optional.Optional[T]

type benchmarkValuePendingDelete = pendingDelete

type benchmarkPointerOptional[T any] struct {
	value *T
}

type benchmarkPointerOptionalPendingDelete struct {
	GVR  schema.GroupVersionResource
	Name benchmarkPointerOptional[domain.ResourceName]
}

// benchmarkSentinelPendingDelete preserves the writer's original representation
// as a baseline; its empty name represents absence only within this benchmark.
type benchmarkSentinelPendingDelete struct {
	GVR  schema.GroupVersionResource
	Name domain.ResourceName
}

// Typed sinks retain one slot or batch at a time. Retention makes the allocation
// boundary explicit without boxing values into interfaces or growing live data
// with the benchmark iteration count.
var (
	optionalBenchmarkSum            uint64
	optionalBenchmarkPointerName    benchmarkPointerOptional[domain.ResourceName]
	optionalBenchmarkValueName      benchmarkValueOptional[domain.ResourceName]
	optionalBenchmarkPointerSchema  benchmarkPointerOptional[domain.ResourceQuerySchema]
	optionalBenchmarkValueSchema    benchmarkValueOptional[domain.ResourceQuerySchema]
	optionalBenchmarkPointerNames   []benchmarkPointerOptional[domain.ResourceName]
	optionalBenchmarkValueNames     []benchmarkValueOptional[domain.ResourceName]
	optionalBenchmarkPointerSchemas []benchmarkPointerOptional[domain.ResourceQuerySchema]
	optionalBenchmarkValueSchemas   []benchmarkValueOptional[domain.ResourceQuerySchema]
)

func benchmarkSomeValue[T any](value T) benchmarkValueOptional[T] {
	return optional.Some(value)
}

// benchmarkSomePointer retains an independent payload-cell copy, matching the
// ownership contract of inline Some rather than borrowing an existing value.
func benchmarkSomePointer[T any](value T) benchmarkPointerOptional[T] {
	return benchmarkPointerOptional[T]{value: &value}
}

//domain:presence
func (o benchmarkPointerOptional[T]) Get() (T, bool) {
	if o.value == nil {
		var zero T
		return zero, false
	}
	return *o.value, true
}

// BenchmarkOptionalPendingDeletes measures bounded updates and reads of the
// writer's actual map entry shape. Keys, valid names, GVR metadata, and map
// capacity are prepared before timing. The construct variants also include the
// writer's ObjectResourceName construction; reporting remains excluded.
func BenchmarkOptionalPendingDeletes(b *testing.B) {
	names, _ := optionalBenchmarkFixtures(b)
	identities := optionalBenchmarkIdentities(b, len(names))
	keys := make([]string, 1024)
	for i := range keys {
		keys[i] = "uid-" + strconv.Itoa(i)
	}
	for _, construct := range []bool{false, true} {
		for _, percent := range []int{0, 50, 100} {
			b.Run(fmt.Sprintf("construct=%t/present=%d", construct, percent), func(b *testing.B) {
				b.Run("repr=pointer", func(b *testing.B) {
					prepareOptionalBenchmark(b)
					entries := make(map[string]benchmarkPointerOptionalPendingDelete, len(keys))
					for _, key := range keys {
						entries[key] = benchmarkPointerOptionalPendingDelete{GVR: testGVR}
					}
					var sum uint64
					index := 0
					for b.Loop() {
						entry := benchmarkPointerOptionalPendingDelete{GVR: testGVR}
						if index%100 < percent {
							name := names[index%len(names)]
							if construct {
								var err error
								name, err = ObjectResourceName(identities[index%len(identities)])
								if err != nil {
									b.Fatal(err)
								}
							}
							entry.Name = benchmarkSomePointer(name)
						}
						entries[keys[index%len(keys)]] = entry
						if name, ok := entries[keys[(index+257)%len(keys)]].Name.Get(); ok {
							sum += uint64(len(name))
						}
						index++
					}
					optionalBenchmarkSum = sum
					runtime.KeepAlive(entries)
				})
				b.Run("repr=value", func(b *testing.B) {
					prepareOptionalBenchmark(b)
					entries := make(map[string]benchmarkValuePendingDelete, len(keys))
					for _, key := range keys {
						entries[key] = benchmarkValuePendingDelete{GVR: testGVR}
					}
					var sum uint64
					index := 0
					for b.Loop() {
						entry := benchmarkValuePendingDelete{GVR: testGVR}
						if index%100 < percent {
							name := names[index%len(names)]
							if construct {
								var err error
								name, err = ObjectResourceName(identities[index%len(identities)])
								if err != nil {
									b.Fatal(err)
								}
							}
							entry.Name = benchmarkSomeValue(name)
						}
						entries[keys[index%len(keys)]] = entry
						if name, ok := entries[keys[(index+257)%len(keys)]].Name.Get(); ok {
							sum += uint64(len(name))
						}
						index++
					}
					optionalBenchmarkSum = sum
					runtime.KeepAlive(entries)
				})
				b.Run("repr=raw-value", func(b *testing.B) {
					prepareOptionalBenchmark(b)
					entries := make(map[string]benchmarkSentinelPendingDelete, len(keys))
					for _, key := range keys {
						entries[key] = benchmarkSentinelPendingDelete{GVR: testGVR}
					}
					var sum uint64
					index := 0
					for b.Loop() {
						entry := benchmarkSentinelPendingDelete{GVR: testGVR}
						if index%100 < percent {
							entry.Name = names[index%len(names)]
							if construct {
								var err error
								entry.Name, err = ObjectResourceName(identities[index%len(identities)])
								if err != nil {
									b.Fatal(err)
								}
							}
						}
						entries[keys[index%len(keys)]] = entry
						if name := entries[keys[(index+257)%len(keys)]].Name; name != "" {
							sum += uint64(len(name))
						}
						index++
					}
					optionalBenchmarkSum = sum
					runtime.KeepAlive(entries)
				})
				benchmarkOptionalPendingDeleteControls(b, names, identities, keys, construct, percent)
				benchmarkOptionalPairMapControl(b, names, identities, keys, construct, percent)
			})
		}
	}
}

// BenchmarkOptionalLocal lets construction and extraction remain local, so
// normal compiler inlining and escape analysis can eliminate temporary storage.
// Each variant has the same non-inlined pipeline boundary: b.Loop keeps only
// inputs/results alive, while optional intermediates inside the pipeline remain
// eligible for ordinary optimization.
func BenchmarkOptionalLocal(b *testing.B) {
	names, schemas := optionalBenchmarkFixtures(b)
	b.Run("payload=name/repr=pointer", func(b *testing.B) {
		prepareOptionalBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalPointerName(names[index%len(names)])
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=name/repr=value", func(b *testing.B) {
		prepareOptionalBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalValueName(names[index%len(names)])
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=schema/repr=pointer", func(b *testing.B) {
		prepareOptionalBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalPointerSchema(schemas[index%len(schemas)])
			index++
		}
		optionalBenchmarkSum = sum
	})
	b.Run("payload=schema/repr=value", func(b *testing.B) {
		prepareOptionalBenchmark(b)
		var sum uint64
		index := 0
		for b.Loop() {
			sum += optionalBenchmarkLocalValueSchema(schemas[index%len(schemas)])
			index++
		}
		optionalBenchmarkSum = sum
	})
	benchmarkOptionalLocalControls(b, names, schemas)
	benchmarkOptionalPairLocalControls(b, names, schemas)
}

// BenchmarkOptionalCacheStore constructs, retains, and reads one cache slot.
// Unlike Local, a newly present pointer payload must survive the current call.
func BenchmarkOptionalCacheStore(b *testing.B) {
	names, schemas := optionalBenchmarkFixtures(b)
	for _, percent := range []int{0, 50, 100} {
		b.Run(fmt.Sprintf("present=%d", percent), func(b *testing.B) {
			b.Run("payload=name/repr=pointer", func(b *testing.B) {
				prepareOptionalBenchmark(b)
				var sum uint64
				index := 0
				for b.Loop() {
					if index%100 < percent {
						optionalBenchmarkPointerName = benchmarkSomePointer(names[index%len(names)])
					} else {
						optionalBenchmarkPointerName = benchmarkPointerOptional[domain.ResourceName]{}
					}
					if name, ok := optionalBenchmarkPointerName.Get(); ok {
						sum += uint64(len(name))
					}
					index++
				}
				optionalBenchmarkSum = sum
			})
			b.Run("payload=name/repr=value", func(b *testing.B) {
				prepareOptionalBenchmark(b)
				var sum uint64
				index := 0
				for b.Loop() {
					if index%100 < percent {
						optionalBenchmarkValueName = benchmarkSomeValue(names[index%len(names)])
					} else {
						optionalBenchmarkValueName = benchmarkValueOptional[domain.ResourceName]{}
					}
					if name, ok := optionalBenchmarkValueName.Get(); ok {
						sum += uint64(len(name))
					}
					index++
				}
				optionalBenchmarkSum = sum
			})
			b.Run("payload=schema/repr=pointer", func(b *testing.B) {
				prepareOptionalBenchmark(b)
				var sum uint64
				index := 0
				for b.Loop() {
					if index%100 < percent {
						optionalBenchmarkPointerSchema = benchmarkSomePointer(schemas[index%len(schemas)])
					} else {
						optionalBenchmarkPointerSchema = benchmarkPointerOptional[domain.ResourceQuerySchema]{}
					}
					if schema, ok := optionalBenchmarkPointerSchema.Get(); ok {
						sum += optionalBenchmarkSchemaSum(schema)
					}
					index++
				}
				optionalBenchmarkSum = sum
			})
			b.Run("payload=schema/repr=value", func(b *testing.B) {
				prepareOptionalBenchmark(b)
				var sum uint64
				index := 0
				for b.Loop() {
					if index%100 < percent {
						optionalBenchmarkValueSchema = benchmarkSomeValue(schemas[index%len(schemas)])
					} else {
						optionalBenchmarkValueSchema = benchmarkValueOptional[domain.ResourceQuerySchema]{}
					}
					if schema, ok := optionalBenchmarkValueSchema.Get(); ok {
						sum += optionalBenchmarkSchemaSum(schema)
					}
					index++
				}
				optionalBenchmarkSum = sum
			})
			benchmarkOptionalCacheControls(b, names, schemas, percent)
			benchmarkOptionalPairCacheControl(b, schemas, percent)
		})
	}
}

// BenchmarkOptionalBatch measures allocating and filling a retained 1,000-slot
// slice. One operation is one whole batch, including the slice allocation.
func BenchmarkOptionalBatch(b *testing.B) {
	names, schemas := optionalBenchmarkFixtures(b)
	for _, percent := range []int{0, 10, 50, 100} {
		b.Run(fmt.Sprintf("present=%d", percent), func(b *testing.B) {
			b.Run("payload=name/repr=pointer", func(b *testing.B) {
				prepareOptionalBenchmark(b)
				for b.Loop() {
					batch := make([]benchmarkPointerOptional[domain.ResourceName], 1000)
					for i := range batch {
						if i%100 < percent {
							batch[i] = benchmarkSomePointer(names[i%len(names)])
						}
					}
					optionalBenchmarkPointerNames = batch
				}
			})
			b.Run("payload=name/repr=value", func(b *testing.B) {
				prepareOptionalBenchmark(b)
				for b.Loop() {
					batch := make([]benchmarkValueOptional[domain.ResourceName], 1000)
					for i := range batch {
						if i%100 < percent {
							batch[i] = benchmarkSomeValue(names[i%len(names)])
						}
					}
					optionalBenchmarkValueNames = batch
				}
			})
			b.Run("payload=schema/repr=pointer", func(b *testing.B) {
				prepareOptionalBenchmark(b)
				for b.Loop() {
					batch := make([]benchmarkPointerOptional[domain.ResourceQuerySchema], 1000)
					for i := range batch {
						if i%100 < percent {
							batch[i] = benchmarkSomePointer(schemas[i%len(schemas)])
						}
					}
					optionalBenchmarkPointerSchemas = batch
				}
			})
			b.Run("payload=schema/repr=value", func(b *testing.B) {
				prepareOptionalBenchmark(b)
				for b.Loop() {
					batch := make([]benchmarkValueOptional[domain.ResourceQuerySchema], 1000)
					for i := range batch {
						if i%100 < percent {
							batch[i] = benchmarkSomeValue(schemas[i%len(schemas)])
						}
					}
					optionalBenchmarkValueSchemas = batch
				}
			})
			benchmarkOptionalBatchControls(b, names, schemas, percent)
		})
	}
}

// BenchmarkOptionalRead measures extraction from already-built small and large
// collections. Setup allocations are excluded, so this isolates read locality
// and returning T by value. Strides 1 and 7,919 visit every element exactly once
// per cycle at both sizes, providing sequential and permuted access without RNG.
func BenchmarkOptionalRead(b *testing.B) {
	names, schemas := optionalBenchmarkFixtures(b)
	for _, entries := range []int{100, 100000} {
		for _, percent := range []int{10, 100} {
			for _, stride := range []int{1, 7919} {
				b.Run(fmt.Sprintf("entries=%d/present=%d/stride=%d", entries, percent, stride), func(b *testing.B) {
					b.Run("payload=name/repr=pointer", func(b *testing.B) {
						prepareOptionalBenchmark(b)
						values := make([]benchmarkPointerOptional[domain.ResourceName], entries)
						for i := range values {
							if i%100 < percent {
								values[i] = benchmarkSomePointer(names[i%len(names)])
							}
						}
						var sum uint64
						index := 0
						for b.Loop() {
							if name, ok := values[index%len(values)].Get(); ok {
								sum += uint64(len(name))
							}
							index += stride
						}
						optionalBenchmarkSum = sum
						runtime.KeepAlive(values)
					})
					b.Run("payload=name/repr=value", func(b *testing.B) {
						prepareOptionalBenchmark(b)
						values := make([]benchmarkValueOptional[domain.ResourceName], entries)
						for i := range values {
							if i%100 < percent {
								values[i] = benchmarkSomeValue(names[i%len(names)])
							}
						}
						var sum uint64
						index := 0
						for b.Loop() {
							if name, ok := values[index%len(values)].Get(); ok {
								sum += uint64(len(name))
							}
							index += stride
						}
						optionalBenchmarkSum = sum
						runtime.KeepAlive(values)
					})
					b.Run("payload=schema/repr=pointer", func(b *testing.B) {
						prepareOptionalBenchmark(b)
						values := make([]benchmarkPointerOptional[domain.ResourceQuerySchema], entries)
						for i := range values {
							if i%100 < percent {
								values[i] = benchmarkSomePointer(schemas[i%len(schemas)])
							}
						}
						var sum uint64
						index := 0
						for b.Loop() {
							if schema, ok := values[index%len(values)].Get(); ok {
								sum += optionalBenchmarkSchemaSum(schema)
							}
							index += stride
						}
						optionalBenchmarkSum = sum
						runtime.KeepAlive(values)
					})
					b.Run("payload=schema/repr=value", func(b *testing.B) {
						prepareOptionalBenchmark(b)
						values := make([]benchmarkValueOptional[domain.ResourceQuerySchema], entries)
						for i := range values {
							if i%100 < percent {
								values[i] = benchmarkSomeValue(schemas[i%len(schemas)])
							}
						}
						var sum uint64
						index := 0
						for b.Loop() {
							if schema, ok := values[index%len(values)].Get(); ok {
								sum += optionalBenchmarkSchemaSum(schema)
							}
							index += stride
						}
						optionalBenchmarkSum = sum
						runtime.KeepAlive(values)
					})
					benchmarkOptionalReadControls(b, names, schemas, entries, percent, stride)
					benchmarkOptionalPairReadControls(b, schemas, entries, percent, stride)
				})
			}
		}
	}
}

func TestOptionalBenchmarkLayout(t *testing.T) {
	t.Logf("ResourceName: payload=%d pointer=%d value=%d", unsafe.Sizeof(domain.ResourceName("")),
		unsafe.Sizeof(benchmarkPointerOptional[domain.ResourceName]{}), unsafe.Sizeof(benchmarkValueOptional[domain.ResourceName]{}))
	t.Logf("ResourceQuerySchema: payload=%d pointer=%d value=%d", unsafe.Sizeof(domain.ResourceQuerySchema{}),
		unsafe.Sizeof(benchmarkPointerOptional[domain.ResourceQuerySchema]{}), unsafe.Sizeof(benchmarkValueOptional[domain.ResourceQuerySchema]{}))
	t.Logf("pendingDelete: pointer=%d value=%d sentinel=%d", unsafe.Sizeof(benchmarkPointerOptionalPendingDelete{}),
		unsafe.Sizeof(benchmarkValuePendingDelete{}), unsafe.Sizeof(benchmarkSentinelPendingDelete{}))
}

func optionalBenchmarkFixtures(tb testing.TB) ([]domain.ResourceName, []domain.ResourceQuerySchema) {
	tb.Helper()
	service, err := domain.NewServiceName("benchmark.example.io")
	if err != nil {
		tb.Fatal(err)
	}
	version, err := domain.NewAPIVersion("v1")
	if err != nil {
		tb.Fatal(err)
	}
	collectionID, err := domain.NewCollectionID("widgets")
	if err != nil {
		tb.Fatal(err)
	}
	collection := domain.NewCollectionName(collectionID)
	names := make([]domain.ResourceName, 128)
	schemas := make([]domain.ResourceQuerySchema, len(names))
	for i := range names {
		id, err := domain.NewResourceID("widget-" + strconv.Itoa(i))
		if err != nil {
			tb.Fatal(err)
		}
		names[i], err = domain.NewResourceName(collection, id)
		if err != nil {
			tb.Fatal(err)
		}
		typeName := "Widget" + strconv.Itoa(i)
		resourceType, err := domain.NewResourceType(service, typeName)
		if err != nil {
			tb.Fatal(err)
		}
		schemas[i] = domain.ResourceQuerySchema{
			ResourceType:                   resourceType,
			ServiceName:                    service,
			TypeName:                       typeName,
			APIVersion:                     version,
			CollectionName:                 collection,
			SpecDescriptor:                 structpb.File_google_protobuf_struct_proto.Messages().ByName("Struct"),
			InventoryObservationDescriptor: structpb.File_google_protobuf_struct_proto.Messages().ByName("Value"),
		}
	}
	return names, schemas
}

func optionalBenchmarkIdentities(tb testing.TB, count int) []KubernetesObjectIdentity {
	tb.Helper()
	scopeNamespace, err := NewScopeNamespace(ObjectScopeNamespaced, "default")
	if err != nil {
		tb.Fatal(err)
	}
	identities := make([]KubernetesObjectIdentity, count)
	for i := range identities {
		identities[i] = KubernetesObjectIdentity{
			ClusterResourceName: testClusterResourceName("target-1"),
			GVR:                 testGVR,
			ScopeNamespace:      scopeNamespace,
			UID:                 "uid-" + strconv.Itoa(i),
		}
	}
	return identities
}

func optionalBenchmarkSchemaSum(schema domain.ResourceQuerySchema) uint64 {
	sum := len(schema.ResourceType) + len(schema.ServiceName) + len(schema.TypeName) +
		len(schema.APIVersion) + len(schema.CollectionName)
	if schema.SpecDescriptor != nil {
		sum++
	}
	if schema.InventoryObservationDescriptor != nil {
		sum++
	}
	return uint64(sum)
}

func prepareOptionalBenchmark(b *testing.B) {
	b.Helper()
	b.ReportAllocs()
	b.Cleanup(func() {
		optionalBenchmarkPointerName = benchmarkPointerOptional[domain.ResourceName]{}
		optionalBenchmarkValueName = benchmarkValueOptional[domain.ResourceName]{}
		optionalBenchmarkPointerSchema = benchmarkPointerOptional[domain.ResourceQuerySchema]{}
		optionalBenchmarkValueSchema = benchmarkValueOptional[domain.ResourceQuerySchema]{}
		optionalBenchmarkPointerNames = nil
		optionalBenchmarkValueNames = nil
		optionalBenchmarkPointerSchemas = nil
		optionalBenchmarkValueSchemas = nil
	})
}
