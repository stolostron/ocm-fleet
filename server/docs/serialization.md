# Domain serialization

Use explicit representations at protocol, persistence, and durable workflow
boundaries. Parse external input under the current input contract. Restore
trusted persisted state under its compatibility contract.

## Raw DTOs at boundaries

Use raw DTOs at serialization boundaries. Decode external input into raw
fields and construct checked values before calling domain behavior. Check
required fields when parsing the containing object. If a field is omitted,
its decoder may never run.

For example, a transport adapter can decode a create request into this DTO.

```go
type createPlatformResourceRequest struct {
    Name   string
    Labels map[string]string
}
```

Parse `Name` through `ParseResourceName` and supply labels through a creation
option. See [Constructors](./constructors.md) for choosing the signature.

## Snapshots and persistence

Each persisted aggregate has one public snapshot and restoration contract at
its root. Define these pieces together in `snapshot.go`.

| Piece                | Purpose                                                                        |
| -------------------- | ------------------------------------------------------------------------------ |
| `FooSnapshot`        | Exposes plain data in exported fields. Mark the type with `//domain:snapshot`. |
| `Snapshot()`         | Captures aggregate state, including pending writes, without mutating it.       |
| `FooFromSnapshot(s)` | Restores a valid aggregate from supported stored state.                        |

Use concrete, recursively plain data in snapshot fields. Primitives, enums,
nested DTOs, collections, pointers for optional data, and standard library
values such as `time.Time` are suitable. Unwrap domain values even if they
support a codec. A nested DTO can expose components that the repository uses
directly, such as a resource name's collection and ID. Use private conversion
helpers for nested values that share the root's restoration contract.

Repositories write snapshot fields to their storage layout. On reads, they
scan or decode into a snapshot and pass it to the restoration function.
Scalar fields can go straight to SQL. Repositories choose the encoding for
blobs or documents.

Restore the invariants that operations on the aggregate rely on. This can
require compatibility rules from older formats, including support for states
that current creation rules reject. Return an error if the stored state cannot
be restored. Derive loaded baselines such as `loadedGeneration` from persisted
state and start with empty pending write buffers. New objects leave persistence
baselines at zero.

## Native encoding contracts

Prefer snapshots at persistence and workflow boundaries. An ordinary
serialization envelope can also carry a domain value that declares a checked
native round trip for the codec being used. Verify each codec independently.

Encode already-valid values. Successful native decoding must restore valid
state, and unsupported or invalid inputs must return an error. Delegate an
aggregate codec to its snapshot and restoration contract. A scalar can reuse
a checked parser where its rules fit the stored format. When a codec delegates
to hydration, use it on trusted serialized state.

Use value receivers for encoding methods so values in interfaces and maps can
be encoded. Check for missing required data on decode. JSON `null` can bypass
a text decoder, and a SQL scanner can succeed without assigning a value.
Translate Go optional values, including `Optional[T]`, into raw nullable
fields or optional pointers in the serialized representation.

Establish codec behavior with the [boundary tests](./testing.md#domain-fixtures)
before declaring support.
The [lint tooling guide](../../tools/golangci-lint/README.md#serialization-contracts)
covers codec declarations and how to enroll custom serialization ports.
