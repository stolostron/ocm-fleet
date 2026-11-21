# Go lint tooling

We build golangci-lint in this isolated Go module to host custom domain and
architecture analyzers alongside the stock linters. The shared configuration
enables Go modernization checks, including the `mapcopy` plugin, and runs
`domainconstruct`, `domainzero`, and `domainserialization` in normal linting,
verification, and editor diagnostics. Domain types join these checks through
declarations as they are migrated. Broader domain field-access checks remain
follow-up work in the contributor effectiveness epic.

## VS Code diagnostics

The workspace settings configure the Go extension to invoke our
`golangci-lint-editor` companion on save. Build both executables with
`npx nx run ocm-fleet:setup:golangci-lint`. After changing the alternate tool path,
reload the VS Code window so the extension uses it.

Saving a Go file checks its containing module, including tests and dependent
packages within that module. Golangci-lint reuses cached package findings when
source and dependencies are unchanged; package loading and formatting checks
still have overhead. The first run needs to warm the analysis cache. Nx manages
the tool build, while the linter's package cache handles incremental analysis.

The adapter retains the last complete report for each checked module and returns
the combined findings to the Go extension. This compensates for the extension
clearing its lint diagnostics after every invocation. Findings disappear when
their module is checked successfully after a fix, or their source file is
deleted. A build or tool failure preserves the previous complete findings and
adds a diagnostic on the module's `go.mod`; package-loading errors also appear at
their source locations. Retained positions are bounded by the saved file's
current length if edits removed their original lines.

Use **Go: Lint Workspace** to check all registered Go modules and populate the
complete workspace report. On-save checks update only the containing module;
other modules retain their last checked findings until checked again. This also
means changes to dependencies shared across modules need a workspace check.
Standalone `poc/` experiments are excluded from workspace checks.

Reports live under `.nx/workspace-data/go-lint-editor/`, separately for each
VS Code process. Changes to the linter binary, configuration, flags, or analysis
environment invalidate the retained reports. Canceled runs leave the stored
reports unchanged, and OS file locks serialize concurrent report updates. To
discard retained diagnostics, remove that directory and run **Go: Lint Workspace**.

## Domain audits

To run only the server's domain audits, use this target from the repository root:

```sh
npx nx run server:lint:domain
```

Findings produce a nonzero exit status and a JSON report at
`.nx/reports/domainconstruct/server.json`, in addition to normal linter output.
The same run writes `server.md`, with separate main and test Markdown and JSON
reports. Test scope includes test files and recognized test support packages;
mixed-purpose ordinary files count as main code. Links target files, with line
and column numbers alongside them so they work in VS Code.
When type checking fails, the audit reports the failure and retains the last
complete report.
Each domain issue has a `Domain` object containing its rule, certainty, operation,
source expression, full type identity, member path, reason, primary location, and
ordered provenance. The [diagnostic format](#diagnostic-format) keeps these fields
independent of the English message.
The target builds the tool and checks its configuration first. It includes test
files and honors the shared generated-file policy. It selects `domainconstruct`,
`domainzero`, and `domainserialization` with `--enable-only`, independently of
their enablement in the shared config. Configured module plugins belong to
golangci-lint's `standard` group.

`domain:constructors` enrolls a type in construction and zero-value checks.
Serialization checks apply to enrolled values at recognized codec calls,
including inside unannotated envelopes. A `domain:boundary` declaration adds
a custom serialization entry point. A `domain:snapshot` marker independently
checks a snapshot for recursively plain data. The analyzers also validate
directive declarations.

Two checks can report with no directives. `domainzero` protects inferred
non-nil error sentinels against reassignment and address-taking by other
packages. `domainserialization` checks the shared `optional.Optional[T]` at
codec boundaries, including when `T` is a plain type.

### Enrolling domain types

Keep the linters enabled in normal verification. Declare applicable contracts
for new types and work through related existing types one at a time.

1. Identify the type's invariants, absence cases, and construction paths.
   Trace the callers and the boundaries where it is stored or sent. See
   [Domain](../../server/docs/domain.md) and
   [Domain serialization](../../server/docs/serialization.md).
2. Declare the construction, snapshot, presence, or codec contracts that
   apply. Run the [focused audit](#domain-audits) to find affected uses.
3. Fix producers, consumers, adapters, and
   [domain fixtures](../../server/docs/testing.md#domain-fixtures) in the
   enrollment change. For a confirmed analyzer error, add a regression case
   and fix the analyzer. Document narrow exceptions at the site that needs them.
4. Run `npx nx run server:verify` before moving on to the next type.

### Construction checks

Opt a type into construction checks in its declaration's doc comment:

```go
// ResourceID identifies one resource within its collection.
//domain:constructors NewResourceID
type ResourceID string
```

The space-separated names identify functions in the type's package that may
construct its representation. Methods use `Receiver.Method`, for example
`Alias.Ref`. Include a root hydration function or private compatibility helper
only when it deliberately restores this type under the root's hydration
contract. A function's name alone grants no permission, and permission applies
only to the listed type and function body, excluding nested closures. Tests
receive the same checks as production code. For fixture construction and
exceptions, see [Domain fixtures](../../server/docs/testing.md#domain-fixtures).

Defined scalars, arrays, and structs are supported. Arrays cover opaque values
such as UUIDs; element writes and mutable slices require representation ownership.
Aliases resolve to their original
type when checking callers, but cannot declare a separate policy. Put directives
on individual types in grouped declarations. Missing functions, unsupported
declarations, and misspelled directives are diagnostics, rather than silent
exemptions. Imported callers receive policies through Go analysis facts.
Generic containers propagate their concrete members' policies; generic type
declarations do not yet accept domain directives of their own.
The shared `server/pkg/optional.Optional[T]` has the explicit container contract
described under [shared optional storage](#shared-optional-storage).

The audit rejects unchecked conversions into a protected type, implicit constant
construction in assignments/arguments/returns/containers, operators that produce
new protected values, populated struct literals, field writes, and conversions
that expose a mutable pointer to the representation. Copying an existing value,
comparing values, and converting an immutable scalar out to its primitive are
allowed. Contextual comparison literals and switch cases observe a representation;
an explicit raw conversion inside a comparison still requires construction.
Reading an existing typed constant, local or imported, is a copy. Deriving a new
typed constant with an operator is construction. An untyped constant passed as a
domain value still needs construction.
Explicit range assignments are checked against the yielded types, including in
generic code when the constraint fixes those types. Integer ranges that can
iterate synthesize new values and require construction permission when the
iteration variable has a protected type. Map writes check implicit construction
of the key.
Channel sends and appended values follow ordinary transfer checks, including
multiple results passed to `append`. Constrained callbacks and composite
literals use their common signature or element types. Exact constraints
retain the construction policy of the constrained type.
Writing through an embedded field or exposing a pointer to it requires
permission from its protected owners. Replacing an existing nested value
requires permission from its enclosing owners. Directly returning a protected
map field, slice field, or slice view with accessible backing storage also
exposes the representation. Straight-line local bindings of a slice or map
retain that ownership within their block. Rebinding a local to fresh storage
clears its prior owner. The owner check covers slicing and index writes through
exact generic pointers to protected arrays. Converting a protected slice to a
nonempty array pointer exposes the same storage. Appending into reusable
capacity can modify that storage. A full slice expression forces allocation
when it leaves less spare capacity than the append needs. Read-only slice
views are allowed as inputs to `len` and as sources of `copy`.
Simple generic helpers that convert a raw argument into a protected
type are checked at their instantiated calls, including imported helpers.
Retained conversions use their instantiated targets even when the final result
has another type or is boxed into an interface. The checks include conversions
returned on a known failure or absence path.
Calls to approved constructors remain ordinary Go calls; the checker does not
prove their validation logic correct.

All equivalent zero storage forms are allowed by `domainconstruct`: `var value T`,
`new(T)`, `T{}`, and scalar zero conversions. `domainzero` checks their consumption,
including successful returns; there is no syntax-only exception for an error
return. Shared optionals, optional nil pointers, and type-only `reflect.TypeOf` observations are
allowed. `//domain:zero-valid` declares that zero supports ordinary domain use,
such as an empty `AliasSet`; other nonzero literals still require construction.

An approved constructor retains its construction permission when a `Must*`
helper calls it. A `nolint:domainconstruct` on a typed constant applies at
that declaration. Zero-value use and subsequent operations are checked
independently. See [Fixed values](../../server/docs/constructors.md#fixed-values).

### Serialization contracts

`domainserialization` checks nested protected values at recognized codec
calls and declared ports. It checks for an explicit codec declaration even
when a type implements an encoding interface or permits zero. See
[Domain serialization](../../server/docs/serialization.md) for choosing a
boundary representation.

```go
//domain:constructors NewResource ResourceFromSnapshot
//domain:serialization json
type Resource struct { /* private domain state */ }

//domain:snapshot
type ResourceSnapshot struct {
    Name string
    Children []ChildSnapshot
}
```

`domain:serialization` requires `domain:constructors` and accepts `json`,
`text`, and `sql` independently. The analyzer checks method signatures.
Use tests to establish the semantic contract for each codec. Construction,
mutation, and zero-value use remain subject to their own checks.
Declared decoding hooks receive construction permission for their receiver type
only, excluding nested closures. Child domain values still require their own
construction or restoration paths.

JSON requires pointer `UnmarshalJSON`. Composites also need a value encoding
hook (`MarshalJSON` or `MarshalText`). Text requires value `MarshalText` and
pointer `UnmarshalText`. SQL requires value `Value` and pointer `Scan`. The
checker uses the text contract for JSON map keys.

`domain:snapshot` opts a declaration into recursive shape checks using the
[plain-data snapshot rules](../../server/docs/serialization.md#snapshots-and-persistence).
Construction and zero-value checks use their own enrollment policy.

The analyzer recognizes the Go objects for JSON `Marshal`, `MarshalIndent`,
`Unmarshal`, `Encoder.Encode`, and `Decoder.Decode`, SQL `Row.Scan`/`Rows.Scan`,
SQL query/exec parameters and `Named`, and correctly shaped codec hooks on
protected receivers. An unrelated method called `Scan` is not a boundary.
Local SSA retains concrete interface payloads and variadic destinations when
their reaching definitions are known. Recursive type inspection finds domain
members of ordinary envelopes, including instantiated generic containers.

Declare other serialization APIs at their definitions:

```go
//domain:boundary json types=I,O
func RunActivity[I, O any](record Record, activity Activity[I, O], input I) (O, error)

type Execution[T any] interface {
    //domain:boundary json results=0
    AwaitResult(ctx context.Context) (T, error)
}

type scanner interface {
    //domain:boundary sql decode=dest
    Scan(dest ...any) error
}

//domain:boundary json encode=payload
func Send(payload any) error
```

Selectors are comma-separated lists. Use `types=` for function type parameters,
`encode=` and `decode=` for named parameters or a named method receiver, and
`results=` for zero-based payload result indices. Decoding requires exactly one
error result whose nil value means success. Declarations on functions and
interface methods travel across packages as analysis facts. Declare the
contract on each shared or forwarding port while concrete payload types are
still visible. Blank identifiers cannot select a parameter or receiver.

Encoding inputs are valid-value consumers, so encoding an uninitialized domain
value still produces a `domainzero` finding. Standard JSON field encoding skips
an exact `json:"-"` field in a plain envelope. A tag like `json:"-,"` names the
field `-`. Declared custom boundaries check the whole selected payload.
Types with custom JSON or text encoding hooks retain whole-value checks,
including when a pointer receiver can supply the hook. A field reached
through an embedded pointer is addressable even when the outer value is passed
by value. A concrete value boxed in an interface is unaddressable. A boxed
pointer still allows the encoder to use the pointee's pointer hook. A visible
field promoted from a protected embedded value also requires that owner's codec.
An explicit promoted method call checks the selected receiver. Default field
selection follows the encoder's breadth-first embedding and tag rules when the
traversal completes within its budget. Boxed payloads in package initializers
and their closures are checked alongside function bodies.

Decoder destinations accept zero output storage. A parameter selected for both
encoding and decoding must already be valid. After the error is checked,
the analyzer trusts a successfully decoded native root and its required
members when the runtime destination establishes the tracked type's policy and
other destinations cannot overwrite it. Merged destinations can establish that
proof when they agree on the storage and runtime pointer type. An overlapping
write through a merged pointer or a helper can invalidate the supplying slot's
earlier origin. The checker keeps tracking required fields in an ordinary
envelope. Default JSON decoding replaces a non-pointer interface payload and
can follow a pointer stored in that slot. The stored pointer is a possible
write target, since inputs such as `null` can replace the interface instead.
Default JSON decoding of a base64 string into a slice of protected byte values
can bypass the element decoder. Use a slice-level decoder that validates the
decoded values. A short JSON array also zeroes trailing slots in a Go array
without calling their element decoders. A checked array-level decoder can
validate that the input supplies every required slot.
Decoding through a raw representation or a different domain type's codec can
invalidate an earlier domain value even after the error check. Scanning a
primitive and converting it to a domain value still requires a constructor.

This is bounded static checking, not whole-program reflection or byte-buffer
provenance analysis. Opaque interface payloads, indirect function dispatch, and
unknown storage origins can hide types. A narrow, explained
`nolint:domainserialization` remains available for an exceptional representation;
do not grant a type an untested codec contract to suppress findings.

### Diagnostic format

All three analyzers use the shared [`diagnostic.Finding`](diagnostic/diagnostic.go)
model. A readable summary leads, followed by labeled records without column
padding. For example:

```text
return may produce an empty domain.ResourceID; empty values are invalid
  | rule: domainzero.invalid-zero; certainty: possible; reason: input-may-be-zero
  | operation: return; value: "s.Name"; type: "github.com/stolostron/ocm-fleet/server/internal/domain.ResourceID"; path: ""
  | source: s.Name enters this method as a plain string ("agent.go", line 101, column 7)
  | conversion: domain.ResourceID(s.Name) changes the type without validation ("agent.go", line 105, column 26)
  | use: return ("agent.go", line 105, column 2)
```

The `|` delimiter preserves record boundaries when editors collapse whitespace.
Metadata strings and filenames use JSON quoting without HTML escaping, so Go
expressions such as `&m` stay readable. Pipes within quoted strings remain
escaped. Provenance explanations escape backslashes, pipes, and newlines.
Human summaries use short package-qualified type names; metadata retains import
paths to distinguish identically named types.

The audit decodes these explicit fields rather than classifying English text:

| Field               | Meaning                                                                                                                                 |
| ------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `Rule`              | Stable namespaced identifier, such as `domainzero.invalid-zero`, `domainzero.zero-comparison`, or `domainconstruct.constructor-bypass`. |
| `Certainty`         | Confidence in the reported rule violation: `known` when established, `possible` when uncertain values or control flow may violate it.   |
| `Operation`         | Stable use or operation code, such as `return`, `argument`, `variadic-argument`, `receiver`, or `comparison`.                           |
| `Value`             | Source expression when known, such as `s.Name`.                                                                                         |
| `Type`              | Full identity of the affected domain type.                                                                                              |
| `Path`              | Member path through struct fields and collection elements, such as `Reports[0].ID`; empty at its root.                                  |
| `Reason`            | Stable source or contract code, independent of certainty and wording.                                                                   |
| `Pos`, `Provenance` | Primary location and ordered steps with stable kinds, locations, and explanations.                                                      |

Certainty applies to the reported rule across all three analyzers. Constructor bypasses,
zero comparisons, and policy violations are `known`; this does not claim that a
literal is invalid or that a compared operand is zero. Invalid-zero findings use
`known` when zero is established and `possible` when the available flow evidence
permits zero.

`input-may-be-zero` means an unvalidated input or raw result can contain zero;
giving it a domain type through conversion does not establish otherwise. Other
reasons distinguish `zero-value`, `source-permits-zero`,
`conversion-may-produce-zero`, `unchecked-error`, unchecked map/receive/assertion
results, `zero-initialized-storage`, and `cleared-element`.
The source expression and path within the consumed value are separate:
`s.Name` can produce a scalar ID with an empty path.

Source names come from declarations and expressions, never compiler temporary
names or allocation comments. Storage without a variable is described in
provenance, for example `assign to composite literal at path val.Deployment.name`.
Its `Value` can retain the source literal; descriptions are never substituted
for source expressions. If lowering loses the expression, `Value` stays empty.
An aggregate zero is not called `nil`, and a symbolic member index belongs in a
path description rather than an invented expression. Mutation steps use the
same source resolver for dynamic callees, with `anonymous function` or `call`
when a source name is unavailable.

Uncertainty preserves the reason for the zero concern. A branch or unknown
mutation can change certainty from `known` to `possible`; it does not replace
the source reason. Mutation provenance states what may change and which
guarantee remains unproven. For example:

```text
argument may contain an empty domain.ResourceName at path [0]; empty values are invalid
  | rule: domainzero.invalid-zero; certainty: possible; reason: zero-initialized-storage
  | operation: argument; value: "names"; type: "github.com/stolostron/ocm-fleet/server/internal/domain.ResourceName"; path: "[0]"
  | source: collection contains zero-initialized elements ("service.go", line 20, column 15)
  | mutation: ReplaceBatch may modify collection elements; nonzero initialization afterward is unproven ("service.go", line 25, column 2)
  | use: argument ("service.go", line 26, column 12)
```

This means the call may have initialized the elements, but the checker cannot
establish that it did. Unknown mutation alone does not create a zero concern
for a trusted domain value or an initialized collection. Raw output storage
still lacks that domain contract, and proven zero writes remain evidence of zero.

Use labels describe the source operation. Struct fields and collection elements
use the neutral phrase `at path`, for example `ID`, `[1]`, or `Reports[0].ID`.
The structured record uses `Path` (rendered as `path`) for both. An empty path
identifies the consumed value itself.

For an individual variadic argument, operation is `variadic-argument` and the
path is relative to that argument. The compiler's packing index is removed;
real indexes inside an argument remain. A slice passed with an ellipsis is a
container argument, so its element path remains intact. For example, given
`logValues(string, ...any)`:

| Source use                                                    | Operation           | Path     | Summary subject   |
| ------------------------------------------------------------- | ------------------- | -------- | ----------------- |
| `accept(id)`                                                  | `argument`          | empty    | argument          |
| `accept(request)` with an invalid `request.ID`                | `argument`          | `ID`     | argument          |
| `accept(ids)` with an invalid `ids[1]`                        | `argument`          | `[1]`    | argument          |
| `logValues("id", id)`                                         | `variadic-argument` | empty    | variadic argument |
| `logValues("items", reports)` with an invalid `reports[0].ID` | `variadic-argument` | `[0].ID` | variadic argument |
| `logValues("items", args...)` with an invalid `args[1]`       | `argument`          | `[1]`    | argument          |

The highlighted source argument identifies which variadic argument is affected.
Receiver labels apply to method receivers, including method expressions,
interface dispatch, and bound methods. Append arguments, copy sources, and
slice elements after clear have distinct labels. Return values, assignments,
map keys and values, operands, conversions, and channel sends retain their own
operation labels. The use provenance record carries the same operation code
as the diagnostic.

Source positions and labels are resolved together, only after deduplication.
At calls, the primary position identifies the source argument or receiver,
including individual arguments packed into a variadic slice. Calls without
matching source syntax retain their instruction position. The editor adapter
sends file, line, and column; the Go extension chooses the underline's length
because that protocol has no end position.

Native Go analysis retains the rule in `Diagnostic.Category` and provenance in
`Diagnostic.Related`. The golangci-lint adapter groups provenance into the same
message because upstream discards categories and emits related locations as
independent findings. Audit JSON retains the complete model, including the final
use step. Its `Category` field is a convenience derived from rule and certainty;
consumers should use the explicit fields for decisions.

### Zero-value flow

`domainzero` uses the construction type policies. It rejects a
`//domain:zero-valid` struct or nonempty array whose required values
recursively contain an invalid zero. Absent optionals, nil optional pointers, and empty
containers are valid in those checks. See
[Zero values and optionality](../../server/docs/domain.md#zero-values-and-optionality)
for deciding when to accept zero and how to represent absence.

Temporary storage such as `var value ResourceID` is allowed. The checker follows
local assignments, branches, copies, struct fields, and allocated pointers to
report invalid zeros reaching arguments, receivers (including bound methods),
successful results, conversions, comparisons, map keys and writes, and channel
sends and receives, including `select`. Boxing into an interface preserves the
value's origin until consumption; storing an inert error payload in an interface
is allowed. Inspecting its type with `reflect.TypeOf` is allowed. For example:

```go
var id ResourceID
consume(id) // Invalid: the required value was never initialized.

id, err := NewResourceID(raw)
if err != nil {
    return "", err // The payload is inert on this failure path.
}
consume(id) // Construction succeeded; no IsZero check is needed.
```

Findings distinguish a **known zero** from a **possible zero**, such as a raw
output that may or may not have been written. Related locations describe its
source, mutation, conversion, assignment, field projection, branch, and use as
available. Source names retain field paths, such as `s.Name` or
`input.Metadata.Name`. Traces are bounded and identify the supported evidence,
rather than every execution path. Construction and zero-flow findings remain independent,
including when they share a location. The shared configuration preserves multiple
findings on one line.

Comparing an invalid-zero domain type to zero is a separate convention finding,
even when the operand is trusted valid. This includes `id == ""`, named zero
constants, zero switch cases, and direct `string(id) == ""` or `len(id) == 0`
checks. Required values should establish validity through construction or checked
decoding. Remove redundant checks when that contract holds, repair a broken
construction or decoding boundary, or represent absence explicitly. Primitive DTO
checks, valid-zero types, nil pointer comparisons, and an approved constructor
inspecting its own representation are allowed. An absence guard does not replace
a constructor's full invariant checks. Narrowing conversions such as
`uint8(id) == 0` are ordinary representation tests: they can be true for a valid,
nonzero value and are not treated as checks for an absent domain value.

A successful scalar field guard still establishes a nonzero fact for zero-flow
analysis. That fact follows the exact field through aggregate returns and copies
when the storage is unchanged at the copy or use point. Writes to the field or
its containing value, possible alias writes, mutating calls, and deferred effects
prevent reuse unless the existing analysis proves they leave it unchanged. A
copy retains the state at its read point; checking a later version of its source
does not validate an earlier zero copy. This uses the same unchanged-storage
proof as repeated scalar reads. Construction, serialization, and zero-comparison
findings remain independent: nonzero does not establish all domain invariants.

Results paired with errors need a successful check of each error status before
use. Forwarding the result with its matching statuses is also allowed. This includes pointer results:
`nil` remains valid optional absence, but a failed result may instead contain a
non-nil pointer to an invalid value. Interface results retain concrete payload
types visible in the producing function, including across packages. A wrapper
that returns a new known error can carry a previously successful boxed type
into its failure result. Checking the error makes successful values usable
without defensive validation, subject to any declared presence guard.

An API returning `(T, bool)` or `(T, bool, error)` can declare boolean presence:

```go
type ResourceLookup interface {
    // Lookup returns the resource's ID, if present.
    //
    //domain:presence
    Lookup(name string) (ResourceID, bool, error)
}
```

`//domain:presence` takes no arguments and applies only to those two result
shapes. The checker recognizes established presence and error guards, or
forwarding the value with its matching indicators. Valid-zero types retain
their valid zeros. On an inactive path the analysis can preserve the payload
through boxing or pointer storage.

Facts follow the declaration across packages and statically resolved calls,
including method values and expressions. Each interface method and concrete
function or method supplies its own fact. Invalid signatures, duplicate markers,
and marker arguments produce `domainzero.invalid-presence-policy` findings,
including in packages without protected types. The contract applies to the
annotated results. Arbitrary parameters and separately stored fields use
ordinary flow analysis.

#### Shared optional storage

The analyzer explicitly recognizes
`github.com/stolostron/ocm-fleet/server/pkg/optional.Optional[T]`. Its absent zero
is valid storage, including in fields, arrays, slices, and map values. Type and
import aliases retain that identity. An unrelated type with the same name,
representation, or methods receives ordinary analysis; a separately defined
type does not acquire this contract merely by sharing Optional's underlying
representation.

`optional.Some(value)` consumes an already-valid T. The ordinary construction
and zero rules still check its argument, including known imported generic
factories that manufacture a present zero or convert raw input into T. This is
not a blanket exemption for the payload or its constructors.

`Get() (T, bool)` uses its declared `domain:presence` contract. Check its bool
before consuming a T whose zero is invalid, or forward the pair through another
declared presence API. Get returns exactly zero T on absence, so payloads with
valid zeros, such as nil pointers and empty slices, retain that validity. Error
guards on other APIs remain independent. The analyzer trusts this small shared
implementation's contract; tests exercise its actual source across packages.

The analyzer has limited visibility into later writes through pointers and
slice backing storage in generic wrappers. See
[Field access and getters](../../server/docs/domain.md#field-access-and-getters)
for ownership rules.

Optional has no native JSON, text, or SQL serialization contract. Encoding its
private fields would lose its value and presence; serialization boundaries
report this even when T itself is serializable. The snapshot checker also
checks T inside Optional.

No general optional-type registration directive, arbitrary field relationship,
or separate presence-method/unchecked-getter correlation is inferred.

Boolean comparisons retain their relationships, including repeated reads of
unchanged table fields and `wantErr` assertions followed by a terminating testing
call. A call through `testing.TB` establishes termination when it resolves to a
standard concrete testing type. Returns inside iterator range loops follow the same
result/error rules as ordinary returns.

Outside a declared absence path, a zero payload requires a provably non-nil error.
The checker recognizes branch
conditions, `errors.New`, `fmt.Errorf`, and `errors.Join` when local argument
contents establish its nilness. It infers non-nil package sentinels from their
initialization and checks for writes and address escapes; reassigning or exposing
an imported sentinel's address is a diagnostic. It also infers small error
wrappers from their bodies, including nil-preserving wrappers, and carries their
contracts across packages. A simple generic error fallback can retain exact
nilness at instantiation. Boxing the zero of a concrete pointer type produces a
non-nil error, while the zero of an interface type stays nil. Names such as
`ErrMissing` or `TerminalError` grant no exemption. Observing a context's
`Done` receive establishes the failure contract of its later `Err()` or
`context.Cause`, including a statically bound `Done` method value. The
checker follows small, statically ordered sequences of defers in reverse
registration order. Across blocks it can follow a single nonrepeating
registration or a definite registration followed by one provably later
registration. Each supported conditional defer is checked on paths where its
registration executes. Values, including keys passed to deferred `delete`, are
captured at registration. Referenced storage is observed during cleanup after
earlier cleanup effects. A reachable conditional write of a known zero can
make a value possibly zero. Map lookups, channel receives, and comma-ok type
assertions need their successful `ok` check when they can otherwise introduce
an invalid zero. An `ok` check establishes presence for map lookups and open
channel receives. Raw payloads still need their own domain construction.

A checked or forwarded error can retain its original result guard through local
branch merges. For example, `if err != nil { err = fmt.Errorf("lookup: %w", err) }`
preserves the implication that a nil forwarded error means the original error
was nil. The checker examines feasible incoming paths using existing non-nil
error contracts; it does not require an early return or a particular wrapper
name. Clearing an error, replacing it with an unrelated possibly-nil error, or
calling a wrapper that might swallow it establishes no such implication.
An independent error can make a boxed payload subject to the wrapper's
failure contract. Direct tuple forwarding and unchanged bindings from the
same call preserve the producer's payload and status relationship.
Presence checks remain independent. This proof has a bounded path budget and
does not infer relationships through loop-carried errors or mutable fields;
an incomplete proof retains the warning.

Arrays and slices retain element initialization state. The checker catches
omitted array elements, unfilled `make` slots, zero elements passed through
interfaces or variadic arguments, and invalid contents after `clear`, a partial
`copy`, or an append into the same backing array. It accepts initialized
individual slots, slice views, complete copies, append-based construction,
and complete sequential fills using `range` or an
index from zero to the length. A fill must assign every element on every back
edge; a `continue` that skips an assignment or a `break` does not establish that
contract. A complete fill also requires stable backing storage. Channel and
recognized synchronization operations can hand control to another owner after
a view has been published, including through a helper that stores it in a
global. The same sharing evidence can invalidate earlier writes to collection
slots, a guarded read, pre-handoff `errors.Join` contents, and an iterator flag's
earlier initialization. Later writes can reestablish validity in slots they
replace. Guarded lengths and exact offsets narrow the backing range affected by
a copy or clear.
A slice-to-array pointer exposes the converted prefix. An array value records
the elements at the copy, including when a later write changes the original
slice. A copied field is traced to its source at the copy. Storage reached
through references in copied or loop-filled elements is observed at the use.
Exact indexed reads check the selected field or converted type. Parsing each
element and checking its error before continuing is supported, including
assigning the result to its slot before checking the error.
Map writes check both keys and values; missing reads still need `ok` when zero
is invalid. Element tracking uses ranges rather than expanding large arrays.
Local and imported helper contracts preserve a returned slice's length when
its body establishes a stable parameter or constant length. Element writes and
unrelated cursor updates do not change that contract. Conditional allocation,
header reassignment, address escapes, captured headers, and deferred recovery
can defeat an exact length proof.

Generic helper summaries retain zero factories, `*new(T)`, fresh `new(T)` and
`make([]T, n)` results, simple struct construction, and forwarding until the call
provides concrete types. Instantiations with valid-zero types remain valid;
instantiations with required domain values must initialize them before use.
A known fractional argument can truncate to zero at a generic integer
conversion. Local and imported helpers receive the same checks. A constructor
may inspect its own representation while building a value. Successful returns
still require valid results.

The analysis is local, not a proof for every Go program. Domain-typed parameters
and successful opaque domain results are trusted according to their types;
raw parameters and results may still be zero when converted to a domain type.
A source domain type that permits zero does not establish a nonzero contract
when converted to a different type.
Checking a primitive for nonzero can remove that particular zero uncertainty,
but the construction rule still requires its other invariants to be established.
Supported helper summaries retain the known origins described above. Taking a
value's address can invalidate its inferred origin, including when calling a
promoted pointer method implicitly takes the address. The checker does not
prove arbitrary callee effects, global mutable storage, concurrent mutation,
arbitrary callback execution, complex loop correlations or generic transformations,
or reflection and unsafe writes. Dynamic slice offsets can require checking a
broader backing range than the program uses. Unknown interface payloads and
unsupported helper bodies can lose provenance; absence of a finding is not a
validity proof. A call that can reach a local through an output pointer, boxed
interface, stored callback, or pointers packaged in array/slice arguments changes
a previous known zero into a possible zero, retaining its source reason and
provenance. This is general mutation tracking, including unrecognized output
APIs; it does not prove a callback ran or initialized the value. Recognized
decoding boundaries add the checked native-root initialization contract
described above. Small body summaries establish read-only use or a definite
scalar write and are shared across packages. Unsupported bodies remain
possible mutations; the checker does not recursively analyze a whole call graph.
When two summarized writes reach the same scalar slot, the last write supplies
the state. A zero written through another direct scalar pointer parameter can
invalidate the earlier value when the two parameters may alias.
A protected struct's nonzero proof accounts for intervening field writes.
An overwrite can invalidate an earlier field or whole-value proof.
Compiler-generated iterator yields have a bounded local summary for paired
returns and initialization proved by a checked flag. The flag must describe the
current invocation when the iterator runs in a loop. The proof requires the
yield to control the flag and the iterator to have no independent path to the
target storage. Ordinary callbacks use
the general mutation rules. Copies made before mutation retain their state, and
a later explicit zero assignment remains invalid. Length relationships beyond the
simple helper contracts above are not inferred. Error factories whose non-nil
contract cannot be derived can produce uncertain findings.

For a justified case beyond these limits, use a narrow `nolint:domainzero` with
a local explanation of how initialization or the error contract is guaranteed.
Prefer teaching the checker a recurring ordinary Go pattern to adding repeated
suppressions. Do not mark a type `zero-valid` to silence an analysis limitation,
and do not add defensive `IsZero()` checks to work around it. Tests need the same
justification as production code.

### Analysis performance

Branch facts stay independent for each path. Alternative tracing stops once
later paths cannot change the selected finding, its certainty, or its provenance.
Contract inference limits branch work, recursive source paths, and convergence
rounds. Nested substitutions share a work budget and caps apply to the expanded
facts before export. Independent facts can settle while unresolved contracts
continue to converge. SSA queries share a work budget across paths, field
traversals, and related lookups. A direct scalar zero in a later call argument,
map value, or return result can still be checked after the instruction's budget
is exhausted.
An incomplete proof leaves unknown state and can miss cases beyond that limit.
Completed type, reference, and boxed-value traversals can reuse results at the
same observation point. A query with a different set of facts analyzes that
state independently.
Boolean refinement uses a per-function comparison index, and mutation
reachability is reused for the same call, storage field, range, and flow point;
neither shortcut caches assumptions from a different branch.
Storage read locations and positions are memoized from immutable SSA; their
truth and unchanged-storage proofs remain specific to each branch and observation.

Argument and receiver positions are resolved only after a finding survives
deduplication. They share the per-position expression cache used for diagnostic
text, so position mapping adds no syntax lookups to calls without findings.

For fields whose zero is valid, a store index can avoid tracing every path when
all assignments preserve that zero, including field copies. The proof includes
writes after the read and falls back to normal flow analysis for overlapping
collection copies or appends, cycles, unresolved origins, generic factory
summaries, and explicit payloads. Required domain fields and boxed invalid
values retain their usual checks.

The analyzer tests include real SSA fixtures, allocation regression checks, and
benchmarks for branch joins, optional fields, boolean refinement, and repeated
mutation queries. Compare fresh golangci-lint caches with a warm compiler cache
when measuring analyzer changes. Check both findings and provenance against a
baseline; an unchanged lint run mostly measures upstream cache and package
loading costs.

## Why a dedicated module

This keeps the complete linter dependency graph and source checksums reviewable
in `go.mod` and `go.sum`, without changing application dependencies. Nx selects
the exact compiler from the `toolchain` directive and builds the executable
locally. Public module downloads use Go's checksum database by default. Checksums
establish consistent source identity; they do not certify that code is safe or
provide signed build provenance.

The tooling currently uses Go 1.26.6, matching the application and CodeQL builds.
Custom analyzers do not require a Go upgrade. Keep compiler upgrades separate
from linter changes and check compatibility with CodeQL when raising the minimum.

At startup, the custom executable sets `GOTOOLCHAIN` to its build compiler's
version for its own Go subprocesses. This also covers config checks outside a
Go module. A newer host Go can otherwise select a newer standard library that
the linter cannot parse. The version comes from the executable, so there is no
second compiler pin to maintain. This overrides ambient `GOTOOLCHAIN` for the
linter only; application builds and tests keep their own Go environment. Modules
requiring a newer Go version still fail until the linter's compiler is upgraded
explicitly.

We considered these alternatives:

- **Upstream release binaries with our archive hashes:** fast setup and upstream's
  release build, but adding analyzers would require a custom executable. We would
  also keep maintaining a platform-specific installer and hash updater.
- **Versioned `go install`:** convenient for the upstream executable, but it
  ignores our local module files and does not compile in our analyzers.
- **`golangci-lint custom`:** the documented automatic module-plugin builder.
  It clones a release tag and resolves the combined dependencies in a temporary
  checkout. Our bootstrap binary hashes would not pin that checkout, and the
  combined dependency files would not be committed for review.
- **Go `.so` plugins:** require matching build environments and CGO. Upstream
  recommends the Module Plugin System instead.

Our small executable wrapper can host that recommended module-plugin API,
similar to Kubernetes. The cost is maintaining the wrapper, reviewing dependency
changes, and a slower first build. Go and Nx caches make subsequent builds cheap.
See [upstream plugin guidance](https://golangci-lint.run/docs/plugins/module-plugins/),
[the automatic builder](https://github.com/golangci/golangci-lint/blob/v2.15.0/pkg/commands/internal/builder.go),
and [Kubernetes' tools module](https://github.com/kubernetes/kubernetes/tree/master/hack/tools/golangci-lint).

## Working with the tool

Run these from the repository root:

```sh
npx nx run ocm-fleet:verify:go
npx nx run ocm-fleet:fmt:go
npx nx run ocm-fleet:fix:go
npx nx run ocm-fleet:lint:go

# Use the per-project targets and standard Nx project selection:
npx nx run server:fix
npx nx run-many --targets=fix --projects=server,deploy-aio
npx nx run go-lint-tool:fix

# Tool development and updates:
npx nx run ocm-fleet:setup:golangci-lint
npx nx run ocm-fleet:test:golangci-lint-module
npx nx run ocm-fleet:lint:golangci-lint
npx nx run ocm-fleet:test:golangci-lint-config
npx nx run ocm-fleet:test:go-quality
npx nx run ocm-fleet:update:golangci-lint --release=v2.15.0
```

Put detailed rule behavior and regression cases in the Go analyzer tests. Keep
the domain cases in `tools/golangci-lint-config.test.mjs` focused on plugin
activation, a representative CLI smoke test for exit status and JSON diagnostics,
and the audit's parser and report tests.

The quality integration tests run real Nx, Go, and linter commands in disposable
workspaces. They check source files on disk, per-module completion markers, lint
failure propagation, and Nx's exported task dependencies. These contracts keep
the tests independent of terminal colors and progress-message formatting. One
workspace lint-failure test exercises propagation; graphs exported from the actual
workspace commands check Go project selection, including newly added modules.
Individual failure cases use project targets or their relevant fix/check stages.
Nx caches these suites against the project definitions and configuration they read,
rather than application source changes. Fast fileset tests check selected input
inclusion and exclusion boundaries.

Run `fix:go` before staging changes across Go modules and review the diff.
It applies the shared formatters and supported linter fixes, then runs the same
lint and normal tests as `verify:go`. Use `verify:go` to format and check without
broader rewrites, or `fmt:go` to format and organize imports only. These workspace
targets select Nx projects tagged `type:go-module`. Use each project's `fmt`,
`verify`, or `fix` target to limit scope. Generated files and `testdata`
fixtures are excluded from rewrites. A fix pass can leave findings behind, so
verification still runs and fails if those findings or test failures remain.
`lint:go` remains a check without source edits for CI and other callers that need
that behavior.

The shared configuration enables Go's `modernize` suite and uses `mapcopy` to
limit `mapsloop` to copies between plain map variables. This avoids changing when
source and destination expressions are evaluated. The configuration excludes
`omitzero`, `slicessort`, `testingcontext`, and `waitgroupgo` because their edits
can change JSON output, NaN ordering, test cancellation timing, or goroutine
completion. It also excludes `slicescontains` because its edits can delete loop
comments.

`goimports` owns import layout and adds missing imports or removes unused ones;
repository imports form a local group after other dependencies. OCM imports use
the normal third-party group. We retain `gofmt` for its standard simplifications.
We dropped `gci` because it removes spacing before some import comments that
`goimports` requires, leaving formatted code that still fails lint. Reordering
the formatters cannot reconcile their independent checks.

Formatting uses `bin/tools/format-go`, a small Go command that reads the same
configuration and reuses golangci-lint's exclusion and simplification code. It
batches eligible files through the official `bin/tools/goimports` CLI, which
reuses its import resolver across files; golangci-lint's formatter creates one
per file. Generated files and excluded directories never reach goimports. The
tools module pins both programs' dependencies and compiler; no separate installer
or version list is needed. Nx still handles task ordering and failure propagation.

Source-rewriting tasks are uncached. They finish editing before starting a fresh
Nx invocation for lint and tests, ensuring checks hash the resulting source.
The tool build and successful checks are cached against their actual inputs.

Nx 23.3.1 rehashes files from the previous scan's modification-time window.
This lets checks after a rewrite reuse the task cache even when both invocations
see the same whole-second file timestamp.

The common task stages live in `nx.json`'s `targetDefaults`, scoped to projects
tagged `type:go-module`. Each module opts in with empty `fmt`, `verify`, `fix`,
`fix:lint`, `check`, and `lint` target objects, overriding
cache inputs where needed and keeping its own normal test target. Nx substitutes
the project root and name; commands use `NX_WORKSPACE_ROOT` for the common
executable and config. The tag filter keeps these defaults separate from
JavaScript lint tasks.

The shared `fix:lint` definition uses golangci-lint's `--issues-exit-code=0` so
unfixable findings do not prevent the final `check` target from running lint and
tests. That check still fails on remaining findings. Keep this override on the
fix stage; putting it in `.golangci.yaml` would also silence failures from normal
lint checks. Tool failures still stop the fix stage. Shared locking and diagnostic
path settings live in `.golangci.yaml` instead of repeated command arguments.

The retained JavaScript helper manages compiler selection, staged builds, and
dependency updates, while Nx handles task orchestration. The custom executable
remains at `bin/tools/golangci-lint`, alongside `goimports` and `format-go`.

Choose an explicit stable v2 release when updating. The update task uses Go's
module tooling in a disposable copy, then publishes `go.mod` and `go.sum` for
review. Review release notes, dependency changes, and compiler compatibility;
raise the compiler pin explicitly when needed. Run the tool module, setup, and
configuration tests and lint the tooling before committing. Ordinary builds use
read-only module resolution and staged dependency files, rejecting any checksum changes.

The executables go to `bin/tools/`. Nx caches them using tool source,
module files, the build helper, and the selected compiler and host settings.
Build inputs exclude documentation, project metadata, Go tests, and test fixtures;
module tests and lint still include test code. Keep any production embedded assets
outside those excluded paths, or include them explicitly in the build inputs.
Application lint cache keys include these inputs too. Building the tool disables
CGO and ambient Go flags, experiments, and workspace overrides. During analysis,
only the compiler selection is pinned; application Go flags and CGO settings
remain in effect.

The custom version includes the executable's SHA-256 hash, which also identifies
golangci-lint's analysis cache. Changes to our analyzers, dependencies, or compiler
therefore invalidate native cached findings as well as Nx results.

To add an analyzer, implement a standard `go/analysis.Analyzer` and cover accepted
and rejected examples with `analysistest`. Register it with `register.Plugin` from
`github.com/golangci/plugin-module-register/register` in an adapter package,
blank-import that package from `main.go`, and enable the module plugin in
`.golangci.yaml`. Run the tool module, setup, configuration, and workflow tests.

CI runs the read-only checks and tests the tool and its configuration. CodeQL
builds the tool module alongside application modules, excluding test fixtures
from standalone builds.
