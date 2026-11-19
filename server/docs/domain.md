# Domain

The domain package owns the business rules. Push each rule down to the value
or aggregate that has the context to enforce it. Accepting a checked domain
value means trusting its documented invariants without revalidating.

## Aggregates, entities, and value objects

**Value objects** are defined by their data and have no lifecycle. Prefer
values wherever possible. A simple immutable scalar can use a defined type,
such as `ResourceID`, with checked construction. Use private-field structs for
composite values and values with mutable backing data. For closed enums, use
typed constants and check raw inputs against the allowed values. Use
interfaces and function types directly for behavior.

**Entities** have identity and a lifecycle. They can change state while
remaining the same entity. Child entities belong to an aggregate and are
accessed through its root.

**Aggregates** are transactional consistency boundaries. The root owns its
values and child entities and enforces the rules that span them. Change
aggregate state through the root's methods. For example,
`PlatformResource` owns its `ResourceRepresentation` entities and checks that
a representation's collection matches its own. Load and save the whole
aggregate as a unit. Discard the aggregate outside of its transaction, 
whether commit or rolled back.

For rules that span aggregates or require external coordination, see
[Internal architecture](./internal-architecture.md#when-to-introduce-a-service).

## Field access and getters

Keep fields private. Within a type's own methods, use direct field access.
Elsewhere, including tests in the same package, use accessors and methods
named for the behavior. Constructors, snapshot and codec implementations,
and ETag computation may access the representation directly.

Own mutable data. Copy maps, slices, or pointed-to values when retaining them
would let callers change the object's state. Apply the same rule when returning
data from accessors and snapshots.

## Zero values and optionality

Enrolled types have invalid zeros by default. Use `//domain:zero-valid` only
when zero satisfies the type's invariants, including the requirements of nested
fields. Document the meaning of that zero, such as an empty `AliasSet`.

Construct required values with invalid zeros before use. Temporary zero storage
is fine during initialization. A failed result can carry a typed zero such as
`Foo{}` or `var zero Foo` with a known non-nil error. Check constructor errors
and the `ok` results of map lookups or channel receives before using a required
value.

Use `optional.Optional[T]` for values that may be absent outside constructor
options. Its zero is absent. `Some` takes an already-valid value, including a
valid zero or nil, and copies it with Go's ordinary shallow-copy semantics.
Check `Get`'s presence result before consuming the payload.

```go
name := optional.Some(validName)
if value, ok := name.Get(); ok {
    consume(value)
}
```

Use nil pointers to express absence where reference semantics are appropriate.
Use `//domain:presence` for an optional `(T, bool)` or `(T, bool, error)`
result. Declare it on both the interface method and its implementation when
applicable. Use the payload once presence is true and any error is nil.
An inactive payload may be zero. Booleans such as `created` or `changed`
keep their ordinary meaning. See
[zero-value flow checks](../../tools/golangci-lint/README.md#zero-value-flow).

See the [optional representation benchmarks](../pkg/optional/benchmarks.md) for
the storage tradeoffs.

## Adding a new aggregate

Apply these rules to new domain types. For a new root, follow the
[constructor guide](./constructors.md) for creation, the
[snapshot contract](./serialization.md#snapshots-and-persistence) for persisted
state, and the [fixture guide](./testing.md#domain-fixtures) for tests. Use
[domain lint enrollment](../../tools/golangci-lint/README.md#enrolling-domain-types)
to check the model and its boundaries.
