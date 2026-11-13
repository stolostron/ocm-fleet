# Domain

The domain package (see [internal-architecture.md](./internal-architecture.md)) is where the bulk of the business logic lives.

Logic should be pushed down as much as possible, without creating undue coupling. Domain objects are the lowest level with which to push down logic.

Logic about an object, like its state transitions, should be pushed to that struct. A struct's fields should almost never be manipulated directly, except by that struct's methods, if that struct lives in the domain model. Encode invariants in these methods to help reduce bugs and security issues in the code.

## Aggregates, entities, and value objects

Understand whether the type you are working with is a value, an entity, or an
aggregate. These describe ownership, identity, and consistency, rather than a
class hierarchy. Successful construction establishes the type's documented
invariants, following the "parse, don't validate" principle. Normalization is
also acceptable during construction.

Accepting a domain object means it is valid. You do not need to redundantly validate or re-parse it because the type communicates what "valid" means already.

Types are never "empty" unless "empty" has semantic meaning for the type.
Outside constructor options, optional domain values use pointers: `nil` means
absent, and a supplied value must be valid. Required arguments are positional;
see [constructors.md](./constructors.md).

**Value objects** are the simplest and most preferred. A value is defined
entirely by its data — two values with the same fields are equal. Values have
no lifecycle and are freely copied. Prefer values wherever possible. Use
structs with private fields for composite values and values with mutable
backing data. A simple immutable scalar can use a defined type, such as
`type ResourceID string`, with constructors and lint enforcement of their use.
Unchecked conversions, implicit constant assignments, and primitive operators
can all bypass construction. Private-field wrappers are also appropriate;
a single-field wrapper stores its value inline without inherently requiring
another allocation. Constructors enforce invariants; accessors must not expose
mutable backing data.

Use a `Must*` helper for fixed programmer-controlled values when a panic on
invalid input is appropriate. It calls the checked constructor and returns its
successful result; the name grants no permission to bypass validation. Go
constants cannot call constructors, including `Must*`. If an actual domain
constant is useful, explain a narrow `nolint:domainconstruct` at its declaration
and review the value's invariants. Reads of that typed constant are copies;
derived constants and untyped constants used as domain values still require
construction. The declaration exception does not exempt zero-value use.

For enums, ordinary defined primitive types with typed constants
are appropriate. Parse raw enum inputs against the allowed constants at the
boundary. Interfaces and function types describe behavior, rather than domain
values, and do not need struct wrappers.

**Entities** have identity and a lifecycle: the same entity may change state
without becoming a different entity. Child entities are owned by an aggregate
and accessed through it. For example, `ResourceRepresentation` is an entity
within the `PlatformResource` aggregate — it has identity (service +
collection_name + resource_id) and mutable state, but is never loaded or
persisted independently.

**Aggregates** are transactional consistency boundaries. The aggregate root is
an entity and the entry point for coordination; an aggregate may consist of
just its root. An aggregate owns its entities and values, and all mutations go
through the aggregate root's methods. External code never reaches into an
aggregate to mutate a child entity directly. The aggregate enforces invariants
that span multiple fields or child entities (e.g.
"a representation's collection must match the aggregate's collection"). The
repository loads and saves entire aggregates, not individual child entities.

Domain object fields are private and encapsulated with getters and methods
named for the relevant operation or behavior. Keep the patterns recognizable
to an experienced Go programmer; do not add frameworks or indirection merely
to make the model more formally pure.

### Zero values and optionality

Go permits zero values even for structs with private fields. Use constructors
for required domain values; do not pass an uninitialized value, omit it from a
struct literal, or treat it as optional. Document when zero is a meaningful,
valid value, such as an empty `AliasSet`. Do not add a universal `IsZero()` or
`Validate()` protocol that every receiver must call.

Temporary zero storage is fine if initialized before use, as are zero payloads
returned with a non-nil error. A successful required result must be valid.
For an optional result, return a pointer so absence is explicit.
The same rule applies to `var value T`, `new(T)`, empty struct literals, and
scalar zero conversions; changing storage syntax does not change validity.

For types enrolled in the domain lint, zero is invalid unless the declaration
includes `//domain:zero-valid`. Use that directive only when the zero state
satisfies the type's documented invariants and supports its normal operations.
It does not relax construction rules for other values. A valid-zero struct
cannot contain required fields whose zeros are invalid; nil optional pointers
and empty containers are fine.

Check a constructor's error before using its result, and a map lookup's or
channel receive's `ok` before using a required domain value. These checks
establish whether a value was produced; receivers still trust the value itself.
Do not add `IsZero()` checks to compensate for unchecked construction.
Comparisons to an invalid zero, including `id == ""`, indicate a redundant
check or missing optionality and receive a separate lint warning. Checking raw
input before construction and inspecting representation within its approved
constructor are appropriate.

Passing raw storage to an output API, such as SQL `Scan`, does not establish a
domain invariant. The linter follows reachable mutation and reports a possible
zero with its source and transformations when initialization is uncertain.
Construct from raw outputs after checking the operation's error. Domain-typed
parameters and successful constructed results carry their validity contract;
receivers do not need to revalidate them.

Avoid ordinary parameter bags containing required domain values. Required
arguments belong in the parameter list or in a constructed domain value or
command.

### Raw DTOs at boundaries

A raw DTO describes a serialized representation at an explicit protocol,
persistence, or durable workflow boundary. It makes no promise of domain
validity: fields may be missing, empty, or malformed. For example, a transport
adapter could decode a create request into this protocol DTO:

```go
type createPlatformResourceRequest struct {
    Name   string
    Labels map[string]string
}
```

The transport adapter decodes the request, calls `ParseResourceName` on the
raw name, and passes the resulting value positionally to
`PlatformResourceService.Create`, with labels supplied through a creation
option. An omitted or empty name is rejected during parsing. The service
receives the constructed domain value, not the request DTO. This input mapping
does not require a snapshot or serialization methods on `ResourceName`.

Parse untrusted data into valid domain objects; hydrate directly only from
trusted persisted state. A DTO's name or location does not establish that
trust. Raw data does not carry domain guarantees merely because a field has a
domain type. Missing fields must be handled when parsing the containing
object, even when individual fields have custom unmarshaling methods. Do not
use raw DTOs as ordinary internal argument bags. Restoring persisted state uses
the serialization root's snapshot and its separate
[hydration compatibility contract](#constructors-vs-snapshots).

### Where logic belongs

- **Value object constructors** enforce the invariant of that single value
  (format, non-emptiness, allowed characters). Callers must use the
  constructor. Private fields prevent directly supplying unchecked contents;
  defined scalars rely on lint enforcement of construction.
  Zero-value misuse remains a caller bug, rather than a reason for every
  receiver to revalidate.
- **Aggregate methods** enforce invariants that require looking across multiple
  fields or entities within the aggregate. For example, role exclusivity rules,
  cross-entity uniqueness within the aggregate, or collection matching.
- **Repositories** enforce storage-level constraints: cross-aggregate
  uniqueness (e.g. alias ownership across different platform resources),
  referential integrity, and transactional consistency. Repository logic should
  be storage-oriented — translating aggregate state into SQL, handling
  conflicts, reconciling — not business logic.
- **Application services** coordinate work that spans an aggregate and
  something else, or orchestrate several composable aggregate methods into a
  higher-level use case (e.g. claim-or-get with alias attachment). Services
  should not duplicate logic that the aggregate already enforces.

If logic can live in a value constructor, it should. If it requires aggregate
context, it belongs on the aggregate. Only when it truly spans aggregates or
requires external coordination does it belong in a service.

### Self-enforcing invariants at each layer

Each layer trusts that layers below it have already enforced their invariants.
A value object, once constructed, is known to be valid. An aggregate method
does not re-validate value objects passed to it — it only checks cross-field
invariants that the values cannot enforce alone. An application service does
not re-validate what the aggregate already checks — it only enforces cross-
aggregate or coordination-level invariants. The transport layer constructs
value objects and commands from raw input, and those constructors are the first
line of validation.

## Snapshots and persistence

Every domain aggregate that participates in a repository has a corresponding
**snapshot** type — a DTO with exported data fields used as the explicit
serialization boundary between the domain and persistence layers. Other
serialization roots can use the same pattern. The domain owns this state
representation; repositories own its mapping to physical storage. The pattern
eliminates two problems: (1) domain types no longer need exported fields,
preserving encapsulation; (2) hydration is coordinated at the serialization
root rather than scattered across repository scan functions.

Use one public snapshot and restoration contract per serialization root.
Unwrap simple domain values to their raw contents. Introduce nested snapshot
types when their structure is useful or shared; a value object does not need
its own public `Snapshot` / `FromSnapshot` pair merely because it is nested.
Private conversion helpers can localize the work without exposing another
unchecked construction path.

Repositories lose access to domain methods when fields become plain data. If
consumers need the components of a value, expose that structure in a nested
DTO. For example, a `ResourceNameSnapshot` can expose `Collection` and `ID`
strings so a repository need not parse a name again. Keep that representation
about domain state; column layouts, joins, and database-specific details belong
to the repository.

### Anatomy of the pattern

Each aggregate `Foo` has:

| Piece                          | Location               | Purpose                                                                                                                                                        |
| ------------------------------ | ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `FooSnapshot`                  | `snapshot.go`          | Struct with exported plain-data fields, including nested DTOs where useful. No domain behavior.                                                                |
| `(f *Foo) Snapshot()`          | `snapshot.go`          | Captures current state (including pending write buffers) into a snapshot.                                                                                      |
| `FooFromSnapshot(s)`           | `snapshot.go`          | Factory: hydrates a domain object from a snapshot. Pending buffers start empty; internal baselines (e.g. `loadedGeneration`) are derived from persisted state. |
| Encoding adapters, when needed | Serialization boundary | Use the snapshot and restoration contract. Current `MarshalJSON` / `UnmarshalJSON` methods support the memworkflow JSON fidelity pass.                         |
| Accessor methods               | aggregate source file  | Read-only access to private fields for external callers.                                                                                                       |

### Rules

- **Snapshot fields are plain data, recursively.** Primitives, enums, nested
  DTOs, arrays, slices, maps, and pointers for optional data. Conventional
  standard-library values such as `time.Time` are appropriate too. Represent
  domain values with raw contents or nested DTOs, even when those values have
  encoding methods. Do not embed domain objects, services, or runtime handles.
- **Pending buffers are write-path only.** `Snapshot()` captures pending
  objects non-mutatively. `FromSnapshot()` always initializes them symmetrically.
  Of course, pending state is not likely to be hydrated on read, so this state is typically
  never provided to FromSnapshot in the first place. In other words, **an aggregate is only valid within its transaction boundary.** After commit, it should be discarded. Trying to use an aggregate from one transaction into another is an error with undefined behavior.
- **Repositories use snapshots for both reads and writes.** Write paths call
  `obj.Snapshot()` and read individual fields. Read paths assemble a snapshot
  from stored data and call `FooFromSnapshot(s)` to produce the domain object.
  SQL repositories can pass scalar fields to database APIs directly; no byte
  serialization round-trip is required. Repositories choose encodings for
  fields stored as blobs or documents.
- **External callers use constructors and accessors.** Code outside the `domain`
  package must never construct aggregates via struct literals. Use `NewFoo(...)`
  for creation and `FooFromSnapshot(s)` only at trusted reconstitution
  boundaries; use `obj.Field()` for reads.
- **Encoding belongs at the boundary.** Snapshot composition does not depend
  on JSON or on every included type implementing a serialization interface.
  Current aggregate JSON adapters use snapshots for the memworkflow engine's
  JSON fidelity pass; other encoders can use the same data representation.
  Delegating decoding to hydration assumes trusted serialized state; parsing
  untrusted input must establish invariants through constructors instead. Use
  value receivers for `MarshalJSON` so non-addressable struct values (e.g.
  fields inside a view passed by value) can still be marshaled.
- **SQL hooks are selective conveniences.** `sql.Scanner` and `driver.Valuer`
  can be useful for scalar value types such as identifiers. Use them
  judiciously when the mapping is useful and stable; they are not a requirement
  for domain values or a replacement for the root's snapshot contract.

## Field access and getters

Within a struct's own methods, direct private field access is natural and fine.
Outside of that — even within the same package — prefer the public getter
methods. This keeps the call sites honest about encapsulation and makes it
straightforward to add derived calculations or caching to a
getter later without auditing every caller. The rule applies to production code
and tests equally.

Returning a map, slice, or pointer must not let callers mutate private state.
Return a copy where necessary, including from snapshots. A pointer expressing
an optional value does not grant permission to mutate the owner's state.

Intentional exceptions:

- **Snapshot / FromSnapshot / JSON marshaling** in `snapshot.go` — these are
  the explicit serialization boundary and must touch raw fields.
- **Constructors** (`NewFoo(...)`) — struct-literal initialization is the
  whole point.
- **Etag computation** (`etag.go`) — these functions hash raw aggregate state
  to produce opaque tokens and are tightly coupled to the serialization shape
  by design.

## Constructors vs snapshots

Domain aggregates have **two** construction paths:

| Path                 | Function                                                  | When                                                |
| -------------------- | --------------------------------------------------------- | --------------------------------------------------- |
| `NewFoo(...)`        | Creates a brand-new object that has never been persisted. | Creation workflows, factory methods.                |
| `FooFromSnapshot(s)` | Reconstitutes an existing object from persisted state.    | Repository reads, trusted workflow deserialization. |

The `New*` constructors establish creation-time invariants (e.g. initial state,
required fields). Positional arguments make required inputs explicit; parsing
raw inputs and checking relationships between arguments happen at runtime.
Constructors leave internal persistence baselines (like `loadedGeneration`)
at zero since the object has never been loaded from storage. After
construction, callers use domain methods
(e.g. `AdvanceManifestStrategy`) for subsequent valid transitions.

`FromSnapshot` is hydration, reserved for trusted persisted state. It restores
state and internal baselines without necessarily applying today's creation
rules or calling each value's current constructor. The restoration contract
must account for previous snapshot formats and rules. The compatibility
mechanism belongs to the serialization boundary and is not prescribed here.

Hydrated objects must still satisfy the assumptions their operations rely on;
supported historical state may differ from what new-object creation permits.
Restoration can return an error when a stored representation cannot be
supported. Parsing external requests or configuration instead establishes the
current input contract. A snapshot factory's availability is not permission
to bypass construction in application code or ordinary test setup.

### Zero-value returns

Error-path returns that need a typed zero value should use the Go zero literal
directly (`AuthMethod{}`, `SignerEnrollment{}`), rather than
`FooFromSnapshot(FooSnapshot{})`. This is legal outside the domain package too:
private fields prevent supplying field contents, not empty literals. A named
return or `var zero Foo` is also fine. The accompanying error must be non-nil;
writing `return Foo{}, err` alone does not establish that.

## Adding a new aggregate

1. Define the private-field struct in its source file with accessor methods.
2. Add a `NewFoo(...)` constructor for the creation path.
3. Add the plain-data `FooSnapshot` to `snapshot.go`, with nested DTOs where useful.
4. Add `Snapshot()` and `FooFromSnapshot()` at the root. Add encoding adapters
   when a boundary requires them, such as the current memworkflow JSON pass.
5. Add a round-trip test in `snapshot_test.go`.
6. Have repositories scan into the snapshot and call `FooFromSnapshot`.

## Test helpers

When test helpers need to seed a domain object, reach the desired state through
constructors and known valid transitions, as the application would. Helpers
can encapsulate those steps, but must not manufacture state by assigning
private fields or inventing snapshots. For example, to test an object at a
later generation N, perform the corresponding read, modify, write cycles.

Round-trip tests should snapshot a valid object and hydrate that snapshot.
Malformed input belongs in tests of parsing and rejection boundaries.
Persistence compatibility tests may need explicit stored fixtures; any other
workaround must explain locally why normal construction and transitions are
unsuitable and why the manufactured state is legitimate. Tests are not a
blanket exception to encapsulation or hydration rules.

The migration is incremental. `RelationshipType`, `AliasRef`,
`NewPlatformResource`, and `AliasSet.Get` in
[resource_identity.go](../internal/domain/resource_identity.go)
illustrate the intended representation, construction, and optional results.
The `domainconstruct` and `domainzero` audits check explicitly annotated types,
including the defined scalars in that file. They check construction and follow
local zero-value flow, respectively; see the [lint tooling guide](../../tools/golangci-lint/README.md#domain-construction-audit)
for their scope and current limits.
Unannotated primitive definitions and older parameter bags still need review
before being used as templates for new code.
