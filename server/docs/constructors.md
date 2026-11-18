# Constructors

A successful constructor establishes valid initial state, including required
fields and relationships between inputs. Use `New*` for fresh instances.
A checked `Parse*` factory can validate or normalize a raw representation
as it constructs a value. Return an error when construction can fail.
When composing checked inputs guarantees the invariants, return the value
directly.

Revisit ownership if construction needs circular references or assignment
after the constructor returns. Follow the
[ownership rules](./domain.md#field-access-and-getters) when retaining mutable
inputs.

## Parameters and options

- Prefer positional arguments for required inputs when the call reads clearly.
- An exported parameter struct can group independent inputs when field names
  improve readability. Use an enrolled domain value type with an invalid zero
  for each required field. `domainzero` can flag an omitted field at a Go call
  site using the field type's policy.
- Use positional arguments when a caller must explicitly supply a value whose
  zero is valid. Use a checked command to enforce invariants across fields.
- Use the option function pattern for optional constructor and factory
  arguments. Start with valid defaults, apply exported `With...` functions to
  private configuration, and check the resulting combination before returning.
  Options take supplied domain values by value.

`WithInventory` enables inventory reporting for an extension resource type.

```go
extensionType := domain.NewExtensionResourceType(resourceType, version, collectionID, now,
    domain.WithInventory(),
)
```

For absence outside constructor options, see
[Zero values and optionality](./domain.md#zero-values-and-optionality).
For construction at a wire or storage boundary, see
[Domain serialization](./serialization.md).

## Fixed values

Use a `Must*` helper for fixed programmer-controlled inputs when a panic on
invalid input is appropriate. The helper calls the checked constructor.
For a required Go constant of a constructor-protected type, review its
invariants and document a narrow `nolint:domainconstruct` on its declaration.
