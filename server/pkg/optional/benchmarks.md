# Optional representation benchmarks

The suite uses the shared `optional.Optional[T]` and compares it with five
ordinary Go representations using a name and a larger record with string
metadata. Normal tests compile the benchmarks. A `-bench` run executes them.

Run a quick check from the repository root with the workspace dependencies
installed.

```sh
GOWORK=off npx nx exec --projects=server --excludeTaskDependencies -- \
  go test -mod=readonly ./pkg/optional -run '^$' \
  -bench '^BenchmarkOptional' -benchmem -benchtime=1x -cpu=1
```

To compare Go versions, collect samples with the same checkout, machine, and
flags. Set `GOTOOLCHAIN` to each release you want to measure.

```sh
for toolchain in go1.26.10 go1.27.4; do
  GOWORK=off GOTOOLCHAIN="$toolchain" \
    npx nx exec --projects=server --excludeTaskDependencies -- \
    go test -mod=readonly ./pkg/optional -run '^$' \
    -bench '^BenchmarkOptional' -benchmem -benchtime=100ms -count=10 -cpu=1 \
    > "optional-$toolchain.txt"
done

benchstat optional-go1.26.10.txt optional-go1.27.4.txt
```

## What the cases measure

| Benchmark | One operation |
| --- | --- |
| `Local` | Construct and read a present value within one call. |
| `Batch` | Allocate and populate a retained slice of 1,000 slots at 0%, 10%, or 100% presence. |
| `Read` | Read one slot in a prebuilt slice of 128 or 65,536 slots at 10% or 100% presence. Both sequential and strided orders are included. |

Present slots are evenly spaced and the count rounds down to a whole slot.
Fixture construction and read setup run before the timer starts. Batch
measurements include building and retaining the slice. `B/slot` reports the
fixed size of one slice element. `B/op` and `allocs/op` include any additional
payload cells created during the operation.

| Representation | Presence and ownership |
| --- | --- |
| `inline` | The shared `Optional[T]` stores a shallow copy and a presence bit. |
| `boxed` | A test-only optional stores a pointer to a fresh shallow copy. `Get` returns the value and a presence flag. |
| `copied-pointer` | A nullable pointer to a fresh shallow copy, read directly. |
| `borrowed-pointer` | A nullable pointer into the fixture slice, used in the retained and read cases. |
| `value-bool` | A shallow copy and an explicit boolean, read directly. |
| `zero-sentinel` | A plain value with the empty name reserved for absence. |
