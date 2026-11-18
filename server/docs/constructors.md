# Constructors

- Pass required arguments positionally. Prefer domain values that already
  describe their invariants over primitives that every caller must validate.
- Use the "Option" function pattern for optional constructor arguments and
  creation factory methods, with exported `With...` functions. Start with valid
  defaults, apply options to private configuration, and check relationships
  between arguments before returning. Options accept supplied domain values
  by value; omitting the option selects the default.
- Ensure the object is valid after successful construction. Lazy assignment to
  public fields must not be required. If there are circular references, this
  is a smell. Later lifecycle transitions may change an object, but must not be
  needed to make its initial state valid.
- Return an error when construction can fail. A constructor that composes
  already-valid values need not return an error or revalidate those values.
- Simple immutable scalars may use defined types with constructors and lint
  enforcement. Callers must not manufacture values through conversions,
  implicit constant assignments, or primitive operators. Composite values use
  private-field structs; see [domain.md](./domain.md).
- A `Must*` helper may call a checked constructor and panic on invalid fixed
  programmer input. Go constants cannot call it. Retaining an actual domain
  constant needs a local, explained construction-lint exception and review;
  ordinary reads of that typed constant are copies.
- Outside constructor options, use `optional.Optional[T]` from `pkg/optional`
  for a value that may be absent. It stores T inline; its zero value is absent,
  `optional.Some(value)` requires a valid T, and `Get()` returns `(T, bool)`.
  Use pointers where reference semantics are appropriate: `nil` can express
  absence, and a supplied pointer must refer to a valid value. Optional results
  can also use a declared boolean presence contract; see [domain.md](./domain.md).
  Do not use an invalid T zero as an absence sentinel or require callers to
  discover missing arguments with `IsZero()`.
- Avoid exported parameter structs containing required domain values. They
  allow required fields to be silently omitted. Use positional arguments or a
  constructed domain value or command instead.
- Use raw DTOs to describe serialized representations at explicit protocol,
  persistence, or durable workflow boundaries. Parse untrusted data into valid
  domain objects before invoking domain behavior. Restore trusted persisted
  state through the serialization root's snapshot contract, which must account
  for previous formats and may bypass current creation rules. Do not use raw
  DTOs as ordinary internal argument bags; see
  [domain.md](./domain.md#raw-dtos-at-boundaries) for an example.
- Own mutable inputs. Copy maps, slices, or pointed-to values when retaining
  them would let callers change an object's state outside its methods.
- Native JSON, text, and SQL hydration methods are construction boundaries.
  Declare supported codecs explicitly and return an error if restoration cannot
  produce valid state. They may reuse a stable scalar `Parse` constructor or a
  root's compatibility-aware `FromSnapshot`; no separate scalar snapshot API is
  mandatory. These declarations do not permit encoding an invalid zero value.

For example, an extension resource type requires its type, API version, collection ID, and
creation time. `WithInventory` enables optional inventory reporting.

```go
extensionType := domain.NewExtensionResourceType(resourceType, version, collectionID, now,
    domain.WithInventory(),
)
```

See [domain.md](./domain.md) for value, entity, and aggregate invariants, zero
values, and the distinction between parsing and hydration.
