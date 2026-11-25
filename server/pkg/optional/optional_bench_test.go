package optional_test

import (
	"fmt"
	"runtime"
	"strconv"
	"testing"
	"unsafe"

	"github.com/stolostron/ocm-fleet/server/pkg/optional"
)

// BenchmarkOptionalLocal measures a present value that stays local to the
// construction and read operation.
func BenchmarkOptionalLocal(b *testing.B) {
	benchmarkLocalCases(b, "name", struct{}{})
	benchmarkLocalCases(b, "record", benchmarkRecordFields)
}

// BenchmarkOptionalBatch allocates and fills a 1,000-slot slice per operation.
// Retaining the slice lets owning pointer styles allocate their payload cells
// as they would in a long-lived collection.
func BenchmarkOptionalBatch(b *testing.B) {
	benchmarkBatchCases(b, "name", struct{}{}, &retainedNames)
	benchmarkBatchCases(b, "record", benchmarkRecordFields, &retainedRecords)
}

// BenchmarkOptionalRead reads one slot per operation from a prebuilt slice.
// The sequential and strided orders both visit every slot before repeating.
func BenchmarkOptionalRead(b *testing.B) {
	benchmarkReadCases(b, "name", struct{}{})
	benchmarkReadCases(b, "record", benchmarkRecordFields)
}

func benchmarkLocalCases[Fields any](b *testing.B, payload string, fields Fields) {
	values := benchmarkValues(128, fields)
	b.Run("payload="+payload, func(b *testing.B) {
		b.Run("repr=inline", func(b *testing.B) {
			b.ReportAllocs()
			sum, index := 0, 0
			for b.Loop() {
				sum += localInline(values[index%len(values)])
				index++
			}
			runtime.KeepAlive(sum)
		})
		b.Run("repr=boxed", func(b *testing.B) {
			b.ReportAllocs()
			sum, index := 0, 0
			for b.Loop() {
				sum += localBoxed(values[index%len(values)])
				index++
			}
			runtime.KeepAlive(sum)
		})
		b.Run("repr=copied-pointer", func(b *testing.B) {
			b.ReportAllocs()
			sum, index := 0, 0
			for b.Loop() {
				sum += localPointer(values[index%len(values)])
				index++
			}
			runtime.KeepAlive(sum)
		})
		b.Run("repr=value-bool", func(b *testing.B) {
			b.ReportAllocs()
			sum, index := 0, 0
			for b.Loop() {
				sum += localPair(values[index%len(values)])
				index++
			}
			runtime.KeepAlive(sum)
		})
		b.Run("repr=zero-sentinel", func(b *testing.B) {
			b.ReportAllocs()
			sum, index := 0, 0
			for b.Loop() {
				sum += localSentinel(values[index%len(values)])
				index++
			}
			runtime.KeepAlive(sum)
		})
	})
}

func benchmarkBatchCases[Fields any](b *testing.B, payload string, fields Fields, retained *retainedBatches[Fields]) {
	values := benchmarkValues(1_000, fields)
	b.Run("payload="+payload, func(b *testing.B) {
		for _, percent := range []int{0, 10, 100} {
			present := benchmarkPresentSlots(len(values), percent)
			b.Run(fmt.Sprintf("present=%d", percent), func(b *testing.B) {
				b.Run("repr=inline", func(b *testing.B) {
					benchmarkRetainedBatch(b, values, present, buildInline[Fields], &retained.inline)
				})
				b.Run("repr=boxed", func(b *testing.B) {
					benchmarkRetainedBatch(b, values, present, buildBoxed[Fields], &retained.boxed)
				})
				b.Run("repr=copied-pointer", func(b *testing.B) {
					benchmarkRetainedBatch(b, values, present, buildCopiedPointers[Fields], &retained.copied)
				})
				b.Run("repr=borrowed-pointer", func(b *testing.B) {
					benchmarkRetainedBatch(b, values, present, buildBorrowedPointers[Fields], &retained.borrowed)
				})
				b.Run("repr=value-bool", func(b *testing.B) {
					benchmarkRetainedBatch(b, values, present, buildPairs[Fields], &retained.pairs)
				})
				b.Run("repr=zero-sentinel", func(b *testing.B) {
					benchmarkRetainedBatch(b, values, present, buildSentinels[Fields], &retained.sentinels)
				})
			})
		}
	})
}

func benchmarkRetainedBatch[Fields, Slot any](
	b *testing.B,
	values []benchmarkValue[Fields],
	present []int,
	build func([]benchmarkValue[Fields], []int) []Slot,
	retained *[]Slot,
) {
	b.ReportAllocs()
	b.Cleanup(func() { *retained = nil })
	for b.Loop() {
		*retained = build(values, present)
	}

	var slot Slot
	b.ReportMetric(float64(unsafe.Sizeof(slot)), "B/slot")
}

func benchmarkReadCases[Fields any](b *testing.B, payload string, fields Fields) {
	b.Run("payload="+payload, func(b *testing.B) {
		for _, count := range []int{128, 65_536} {
			values := benchmarkValues(count, fields)
			for _, percent := range []int{10, 100} {
				present := benchmarkPresentSlots(count, percent)
				b.Run(fmt.Sprintf("entries=%d/present=%d", count, percent), func(b *testing.B) {
					b.Run("order=sequential", func(b *testing.B) {
						benchmarkReadStyles(b, values, present, 1)
					})
					b.Run("order=strided", func(b *testing.B) {
						benchmarkReadStyles(b, values, present, 7_919)
					})
				})
			}
		}
	})
}

func benchmarkReadStyles[Fields any](b *testing.B, values []benchmarkValue[Fields], present []int, stride int) {
	// With a power-of-two length, each odd stride visits all indices.
	mask := len(values) - 1
	b.Run("repr=inline", func(b *testing.B) {
		slots := buildInline(values, present)
		b.ReportAllocs()
		sum, index := 0, 0
		for b.Loop() {
			if value, present := slots[index].Get(); present {
				sum += len(value.name)
			}
			index = (index + stride) & mask
		}
		runtime.KeepAlive(sum)
	})
	b.Run("repr=boxed", func(b *testing.B) {
		slots := buildBoxed(values, present)
		b.ReportAllocs()
		sum, index := 0, 0
		for b.Loop() {
			if value, present := slots[index].Get(); present {
				sum += len(value.name)
			}
			index = (index + stride) & mask
		}
		runtime.KeepAlive(sum)
	})
	b.Run("repr=copied-pointer", func(b *testing.B) {
		benchmarkReadPointers(b, buildCopiedPointers(values, present), stride)
	})
	b.Run("repr=borrowed-pointer", func(b *testing.B) {
		benchmarkReadPointers(b, buildBorrowedPointers(values, present), stride)
	})
	b.Run("repr=value-bool", func(b *testing.B) {
		slots := buildPairs(values, present)
		b.ReportAllocs()
		sum, index := 0, 0
		for b.Loop() {
			if slot := &slots[index]; slot.present {
				sum += len(slot.value.name)
			}
			index = (index + stride) & mask
		}
		runtime.KeepAlive(sum)
	})
	b.Run("repr=zero-sentinel", func(b *testing.B) {
		slots := buildSentinels(values, present)
		b.ReportAllocs()
		sum, index := 0, 0
		for b.Loop() {
			sum += len(slots[index].name)
			index = (index + stride) & mask
		}
		runtime.KeepAlive(sum)
	})
}

func benchmarkReadPointers[Fields any](b *testing.B, slots []*benchmarkValue[Fields], stride int) {
	mask := len(slots) - 1
	b.ReportAllocs()
	sum, index := 0, 0
	for b.Loop() {
		if value := slots[index]; value != nil {
			sum += len(value.name)
		}
		index = (index + stride) & mask
	}
	runtime.KeepAlive(sum)
}

// The local pipelines share a call boundary. The compiler can inline
// construction and reads inside each pipeline and eliminate temporary storage.
//
//go:noinline
func localInline[Fields any](value benchmarkValue[Fields]) int {
	result, _ := optional.Some(value).Get()
	return len(result.name)
}

//go:noinline
func localBoxed[Fields any](value benchmarkValue[Fields]) int {
	result, _ := someBoxed(value).Get()
	return len(result.name)
}

//go:noinline
func localPointer[Fields any](value benchmarkValue[Fields]) int {
	pointer := &value
	return len(pointer.name)
}

//go:noinline
func localPair[Fields any](value benchmarkValue[Fields]) int {
	slot := valueBool[benchmarkValue[Fields]]{value: value, present: true}
	if slot.present {
		return len(slot.value.name)
	}
	return 0
}

//go:noinline
func localSentinel[Fields any](value benchmarkValue[Fields]) int {
	return len(value.name)
}

func buildInline[Fields any](values []benchmarkValue[Fields], present []int) []optional.Optional[benchmarkValue[Fields]] {
	slots := make([]optional.Optional[benchmarkValue[Fields]], len(values))
	for _, i := range present {
		slots[i] = optional.Some(values[i])
	}
	return slots
}

func buildBoxed[Fields any](values []benchmarkValue[Fields], present []int) []boxed[benchmarkValue[Fields]] {
	slots := make([]boxed[benchmarkValue[Fields]], len(values))
	for _, i := range present {
		slots[i] = someBoxed(values[i])
	}
	return slots
}

func buildCopiedPointers[Fields any](values []benchmarkValue[Fields], present []int) []*benchmarkValue[Fields] {
	slots := make([]*benchmarkValue[Fields], len(values))
	for _, i := range present {
		value := values[i]
		slots[i] = &value
	}
	return slots
}

func buildBorrowedPointers[Fields any](values []benchmarkValue[Fields], present []int) []*benchmarkValue[Fields] {
	slots := make([]*benchmarkValue[Fields], len(values))
	for _, i := range present {
		slots[i] = &values[i]
	}
	return slots
}

func buildPairs[Fields any](values []benchmarkValue[Fields], present []int) []valueBool[benchmarkValue[Fields]] {
	slots := make([]valueBool[benchmarkValue[Fields]], len(values))
	for _, i := range present {
		slots[i] = valueBool[benchmarkValue[Fields]]{value: values[i], present: true}
	}
	return slots
}

func buildSentinels[Fields any](values []benchmarkValue[Fields], present []int) []benchmarkValue[Fields] {
	slots := make([]benchmarkValue[Fields], len(values))
	for _, i := range present {
		slots[i] = values[i]
	}
	return slots
}

func benchmarkPresentSlots(count, percent int) []int {
	present := make([]int, count*percent/100)
	for i := range present {
		present[i] = i * count / len(present)
	}
	return present
}

func benchmarkValues[Fields any](count int, fields Fields) []benchmarkValue[Fields] {
	values := make([]benchmarkValue[Fields], count)
	for i := range values {
		values[i] = benchmarkValue[Fields]{fields: fields, name: "resource-" + strconv.Itoa(i+1)}
	}
	return values
}

// The name payload uses no extra fields. The larger payload models a record
// with string metadata that retains references to shared backing data.
type benchmarkValue[Fields any] struct {
	fields Fields
	name   string
}

var benchmarkRecordFields = [6]string{
	"kubernetes", "namespaces", "v1alpha1", "extension", "observed", "inventory",
}

type boxed[T any] struct {
	value *T
}

func someBoxed[T any](value T) boxed[T] { return boxed[T]{value: &value} }

func (o boxed[T]) Get() (T, bool) {
	if o.value == nil {
		var absent T
		return absent, false
	}
	return *o.value, true
}

type valueBool[T any] struct {
	value   T
	present bool
}

type retainedBatches[Fields any] struct {
	inline    []optional.Optional[benchmarkValue[Fields]]
	boxed     []boxed[benchmarkValue[Fields]]
	copied    []*benchmarkValue[Fields]
	borrowed  []*benchmarkValue[Fields]
	pairs     []valueBool[benchmarkValue[Fields]]
	sentinels []benchmarkValue[Fields]
}

var (
	retainedNames   retainedBatches[struct{}]
	retainedRecords retainedBatches[[6]string]
)
