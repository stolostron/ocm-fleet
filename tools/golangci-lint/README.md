# Go lint tooling

We build golangci-lint in this isolated Go module to host custom domain and
architecture analyzers alongside the stock linters. The opt-in domain audit runs
`domainconstruct` and `domainzero` for explicitly annotated domain types. Shared
linting and normal verification explicitly disable them while their existing
findings are migrated. Broader domain field-access checks remain
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
Each domain issue has a `Domain` object containing its rule, certainty, operation,
source expression, full type identity, field path, reason, primary location, and
ordered provenance. The [diagnostic format](#diagnostic-format) keeps these fields
independent of the English message.
The target builds the tool and checks its configuration first. It includes test
files and honors the shared generated-file policy. It selects `domainconstruct`,
`domainzero`, and `domainserialization` with
`--enable-only`, overriding their temporary disablement in the shared config.
Normal `lint`, `check`, `verify`, and VS Code diagnostics use the stock linters and
formatting checks. Configured module plugins belong to golangci-lint's `standard`
group, so the shared config must explicitly disable them; removing an `enable`
entry alone leaves them active.

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
receive the same checks as production code. Use a local, explained `nolint`
only for a justified fixture that cannot use normal construction.

Defined scalars, arrays, and structs are supported. Arrays cover opaque values
such as UUIDs; element writes and mutable slices require representation ownership.
Aliases resolve to their original
type when checking callers, but cannot declare a separate policy. Put directives
on individual types in grouped declarations. Missing functions, unsupported
declarations, and misspelled directives are diagnostics, rather than silent
exemptions. Imported callers receive policies through Go analysis facts.
Generic containers propagate their concrete members' policies; generic type
declarations do not yet accept domain directives of their own.

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
Simple generic helpers that convert a raw argument into a protected
type are checked at their instantiated calls, including imported helpers.
Calls to approved constructors remain ordinary Go calls; the checker does not
prove their validation logic correct.

All equivalent zero storage forms are allowed by `domainconstruct`: `var value T`,
`new(T)`, `T{}`, and scalar zero conversions. `domainzero` checks their consumption,
including successful returns; there is no syntax-only exception for an error
return. Optional nil pointers and type-only `reflect.TypeOf` observations are
allowed. `//domain:zero-valid` declares that zero supports ordinary domain use,
such as an empty `AliasSet`; other nonzero literals still require construction.

For programmer-controlled fixed values, a `Must*` helper may call a checked
constructor and panic on error. It needs no special lint permission when it
uses the normal constructor. Such a call initializes a variable, not a Go
constant. Retaining an actual domain constant requires a narrow, explained
`nolint:domainconstruct` on its declaration and review of its invariants. The
exception does not make zero valid or exempt subsequent operations. Ordinary
closed enums retain their typed constant pattern.

### Serialization contracts

`domainserialization` checks serialization of constructor-protected types at
known boundaries. An ordinary envelope may contain domain values recursively,
but each domain value must declare support for the codec. A protected value
without that support must first become raw representation data, usually through
its serialization root's snapshot. Implementing an encoding interface alone is
not an opt-in, and `domain:zero-valid` does not imply encoding support.

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

`domain:serialization` accepts `json`, `text`, and `sql`, independently. Each is
a reviewed contract: encoding valid state and decoding it successfully restores
valid domain state under the type's restoration rules. Unsupported or invalid
state must return an error. Compatibility defaults, migrations, and historical
creation rules belong to restoration, which need not call today's `New` function.
The analyzer checks the standard method signatures; tests must establish the
implementation's semantic round trip and rejection behavior. Declaring a codec
does not exempt construction, mutation, or zero-value use.
Declared decoding hooks receive construction permission for their receiver type
only, excluding nested closures. Child domain values still require their own
construction or restoration paths.

JSON requires pointer `UnmarshalJSON`; composites also need a value encoding
hook (`MarshalJSON` or `MarshalText`). Text requires value `MarshalText` and
pointer `UnmarshalText`; SQL requires value `Value` and pointer `Scan`. Encoding
hooks must work for values in interfaces and maps, where pointer methods alone
are insufficient. Text support does not imply JSON support: JSON can skip a text
decoder for `null`. JSON map keys use the text contract. SQL scanners must not
silently succeed on NULL or empty data while leaving a required value invalid.
Use an optional representation for absence. A scalar may reuse its checked
`Parse` constructor; a separate scalar snapshot API is not required.

`domain:snapshot` identifies the stronger representation rule. Snapshots contain
plain data recursively, including primitives, enums, raw nested DTOs, collections,
optional pointers, and `time.Time`. Protected domain values remain prohibited
even when they support a codec. Interface payloads, runtime handles, and behavior
are not plain data. The marker does not promise valid scalar contents and does
not enroll the snapshot in construction or zero-value rules. Names alone do not
identify snapshots. Existing snapshot types are being migrated incrementally.

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

Selectors are comma-separated lists: `types=` names function type parameters,
`encode=` and `decode=` name parameters (or a named method receiver), and
`results=` gives zero-based payload result indices. Selectors are checked
against the signature; decoding requires exactly one error result whose nil
value means success. Declarations on functions and interface methods travel
across packages as analysis facts. They are kept separate from inferred body
effects. Use them on shared ports before their payload types are erased, rather
than teaching the analyzer application-specific function names.

Encoding inputs are valid-value consumers, so encoding an uninitialized domain
value still produces a `domainzero` finding. Decoder destinations accept zero
output storage; a parameter selected for both encoding and decoding must already
be valid. After the error is checked, decoding a declared native domain
root establishes its validity, including its required members. Decoding an
ordinary envelope does not establish required-field presence: omitted fields
can bypass nested decoders. Failure and unchecked errors establish no validity.
Scanning a primitive and converting it to a domain value still requires a
constructor.

This is bounded static checking, not whole-program reflection or byte-buffer
provenance analysis. Opaque interface payloads, indirect function dispatch, and
unknown storage origins can hide types. New generic forwarding abstractions
need their own boundary declaration. A narrow, explained
`nolint:domainserialization` remains available for an exceptional representation;
do not grant a type an untested codec contract to suppress findings.

### Diagnostic format

All three analyzers use the shared [`diagnostic.Finding`](diagnostic/diagnostic.go)
model. A readable summary leads, followed by labeled records without column
padding. For example:

```text
return may produce an empty domain.ResourceID; empty values are invalid
  | rule: domainzero.invalid-zero; certainty: possible; reason: input-may-be-zero
  | operation: return; value: "s.Name"; type: "github.com/stolostron/ocm-fleet/server/internal/domain.ResourceID"; field-path: ""
  | source: s.Name enters this method as a plain string ("agent.go", line 101, column 7)
  | conversion: domain.ResourceID(s.Name) changes the type without validation ("agent.go", line 105, column 26)
  | use: return ("agent.go", line 105, column 2)
```

The `|` delimiter preserves record boundaries when editors collapse whitespace.
Metadata strings and filenames use JSON quoting; provenance explanations escape
backslashes, pipes, and newlines. Human summaries use short package-qualified
type names; metadata retains import paths to distinguish identically named types.

The audit decodes these explicit fields rather than classifying English text:

| Field               | Meaning                                                                                                                                 |
| ------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `Rule`              | Stable namespaced identifier, such as `domainzero.invalid-zero`, `domainzero.zero-comparison`, or `domainconstruct.constructor-bypass`. |
| `Certainty`         | Confidence in the reported rule violation: `known` when established, `possible` when uncertain values or control flow may violate it.   |
| `Operation`         | Stable operation code, such as `return`, `argument`, `assignment`, or `comparison`.                                                     |
| `Value`             | Source expression when known, such as `s.Name`.                                                                                         |
| `Type`              | Full identity of the affected domain type.                                                                                              |
| `FieldPath`         | Affected field within the consumed value, such as `Reports[0].ID`; empty for a scalar result.                                           |
| `Reason`            | Stable evidence code, independent of wording.                                                                                           |
| `Pos`, `Provenance` | Primary location and ordered steps with stable kinds, locations, and explanations.                                                      |

Certainty applies to the reported rule across all three analyzers. Constructor bypasses,
zero comparisons, and policy violations are `known`; this does not claim that a
literal is invalid or that a compared operand is zero. Invalid-zero findings use
`known` when zero is established and `possible` when the available flow evidence
permits zero.

`input-may-be-zero` means an unvalidated input or raw result can contain zero;
giving it a domain type through conversion does not establish otherwise. Other
reasons distinguish `zero-value`, `source-permits-zero`,
`conversion-may-produce-zero`, `possible-mutation`, `branch-may-be-zero`,
`unchecked-error`, unchecked map/receive/assertion results, and uninitialized or
cleared elements. The source expression and result field path are separate:
`s.Name` can produce a scalar ID whose field path is empty.

For `domainzero` value-use findings at calls, the primary position identifies the
source argument or receiver, including individual arguments packed into a
variadic slice. Calls without matching source syntax retain their instruction
position. The editor adapter sends file, line, and column; the Go extension
chooses the underline's length because that protocol has no end position.

Native Go analysis retains the rule in `Diagnostic.Category` and provenance in
`Diagnostic.Related`. The golangci-lint adapter groups provenance into the same
message because upstream discards categories and emits related locations as
independent findings. Audit JSON retains the complete model, including the final
use step. Its `Category` field is a convenience derived from rule and certainty;
consumers should use the explicit fields for decisions.

### Zero-value flow

`domainzero` uses the same type policies. An enrolled type's zero is invalid
unless its declaration includes `//domain:zero-valid`. This is a statement of
domain meaning, not something the checker can infer from a representation.
The checker rejects a valid-zero struct whose required fields recursively
contain an invalid-zero value. Optional nil pointers and empty containers do
not contradict that declaration.

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
checks. Remove redundant checks or represent absence explicitly. Primitive DTO
checks, valid-zero types, nil pointer comparisons, and an approved constructor
inspecting its own representation are allowed. An absence guard does not replace
a constructor's full invariant checks. Narrowing conversions such as
`uint8(id) == 0` are ordinary representation tests: they can be true for a valid,
nonzero value and are not treated as checks for an absent domain value.

Results paired with an error need a successful error check before use; forwarding
the result and its matching error is also allowed. This includes pointer results:
`nil` remains valid optional absence, but a failed result may instead contain a
non-nil pointer to an invalid value. Interface results retain concrete payload
types visible in the producing function, including across packages. Checking
the error makes those successful values usable without defensive validation.
Boolean comparisons retain their relationships, including repeated reads of
unchanged table fields and `wantErr` assertions followed by a terminating testing call. The standard
`testing.TB` interface has the same termination contract; a nonfatal assertion
does not establish success. Returns inside iterator range loops follow the same
result/error rules as ordinary returns.

A zero payload requires a provably non-nil error. The checker recognizes branch
conditions, `errors.New`, `fmt.Errorf`, and `errors.Join` when local argument
contents establish its nilness. It infers non-nil package sentinels from their
initialization and checks for writes and address escapes; reassigning or exposing
an imported sentinel's address is a diagnostic. It also infers small error
wrappers from their bodies, including nil-preserving wrappers, and carries their
contracts across packages. Names such as `ErrMissing` or `TerminalError` grant no
exemption. Observing a context's `Done` receive establishes its later `Err()` or
`context.Cause` failure contract for that context. Ordinary deferred cleanup
preserves these rules. Map lookups, channel receives, and comma-ok type assertions
need their successful `ok` check when they can otherwise introduce an invalid zero.

Arrays and slices retain element initialization state. The checker catches
omitted array elements, unfilled `make` slots, zero elements passed through
interfaces or variadic arguments, and invalid contents after `clear` or a partial
`copy`. It accepts initialized individual slots, slice views, complete copies,
append-based construction, and complete sequential fills using `range` or an
index from zero to the length. A fill must assign every element on every back
edge; a `continue` that skips an assignment or a `break` does not establish that
contract. Parsing each element and checking its error before continuing is
supported, including assigning the result to its slot before checking the error.
Map writes check both keys and values; missing reads still need `ok` when zero
is invalid. Element tracking uses ranges rather than expanding large arrays.
Local and imported helper contracts preserve a returned slice's length when
its body establishes a stable parameter or constant length. Element writes and
unrelated cursor updates do not change that contract. Conditional allocation,
header reassignment, address escapes, and captured headers are not assumed to
preserve the length.

Generic helper summaries retain zero factories, `*new(T)`, fresh `new(T)` and
`make([]T, n)` results, simple struct construction, and forwarding until the call
provides concrete types. Instantiations with valid-zero types remain valid;
instantiations with required domain values must initialize them before use.
Local and imported helpers receive the same checks. A constructor may inspect
its own representation while building a value; successful zero results remain
diagnostics.

The analysis is local, not a proof for every Go program. Domain-typed parameters
and successful opaque domain results are trusted according to their types;
raw parameters and results may still be zero when converted to a domain type.
A source domain type that permits zero does not establish a nonzero contract
when converted to a different type.
Checking a primitive for nonzero can remove that particular zero uncertainty,
but the construction rule still requires its other invariants to be established.
Supported helper summaries
retain the known origins described above. The checker does not
prove arbitrary callee effects, global mutable storage, concurrent mutation,
arbitrary callback execution, complex loop correlations or generic transformations,
or reflection and unsafe writes. Dynamic slice offsets can require checking a
broader backing range than the program uses. Unknown interface payloads and
unsupported helper bodies can lose provenance; absence of a finding is not a
validity proof. A call that can reach a local through an output pointer, boxed
interface, stored callback, or pointers packaged in array/slice arguments changes
a previous known zero into a possible zero, retaining its provenance. This is
general mutation tracking, including unrecognized output APIs; it does not prove
a callback ran or initialized the value. Recognized decoding boundaries add the
checked native-root initialization contract described above. Small body summaries establish read-only use or a
definite scalar write and are shared across packages. Unsupported bodies remain
possible mutations; the checker does not recursively analyze a whole call graph.
Compiler-generated iterator yields have a bounded local summary for paired
returns and initialization proved by a checked flag; ordinary callbacks do not
inherit that contract. Copies made before mutation retain their state, and a
later explicit zero assignment remains invalid. Length relationships beyond the
simple helper contracts above are not inferred. Error factories whose non-nil
contract cannot be derived can produce uncertain findings.
A returned boolean is not automatically treated as an absence marker: use
optional pointers for ordinary domain results.

For a justified case beyond these limits, use a narrow `nolint:domainzero` with
a local explanation of how initialization or the error contract is guaranteed.
Prefer teaching the checker a recurring ordinary Go pattern to adding repeated
suppressions. Do not mark a type `zero-valid` to silence an analysis limitation,
and do not add defensive `IsZero()` checks to work around it. Tests need the same
justification as production code.

Decode boundary data into plain DTOs and then parse or hydrate through the
serialization root, as described in the [domain guidance](../../server/docs/domain.md).
Decoding into a struct with domain fields does not establish their validity.

The initial annotations cover values in the resource identity slice. Broader
type enrollment and application cleanup follow review of the audit findings.

### Analysis performance

Branch facts stay independent for each path. Alternative tracing stops once
later paths cannot change the selected finding, its certainty, or its provenance.
Boolean refinement uses a per-function comparison index, and mutation
reachability is reused for the same call, storage field, range, and flow point;
neither shortcut caches assumptions from a different branch.

Argument and receiver positions are resolved only after a finding survives
deduplication. They share the per-position expression cache used for diagnostic
text, so position mapping adds no syntax lookups to calls without findings.

For fields whose zero is valid, a store index can avoid tracing every path when
all assignments preserve that zero, including field copies. The proof includes
writes after the read and falls back to normal flow analysis for cycles,
unresolved origins, generic factory summaries, and explicit payloads. Required
domain fields and boxed invalid values retain their usual checks.

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
npx nx run server:verify
npx nx run-many --targets=verify --projects=server,deploy-aio
npx nx run go-lint-tool:verify

# Tool development and updates:
npx nx run ocm-fleet:setup:golangci-lint
npx nx run ocm-fleet:test:golangci-lint-module
npx nx run ocm-fleet:lint:golangci-lint
npx nx run ocm-fleet:test:golangci-lint-config
npx nx run ocm-fleet:test:go-quality
npx nx run ocm-fleet:update:golangci-lint --release=v2.15.0
```

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

`fmt:go` applies the shared formatters, including import organization. `verify:go`
does that first, then runs lint and the normal tests. `fix:go` additionally applies
supported linter fixes before rechecking; review those changes. These workspace
targets select Nx projects tagged `type:go-module`. Use each project's `fmt`,
`verify`, or `fix` target to limit scope. Generated files and `testdata`
fixtures are excluded from rewrites. A fix pass can leave findings behind, so
verification still runs and fails if those findings or test failures remain.
`lint:go` remains a check without source edits for CI and other callers that need
that behavior.

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

After successful rewrites, `refresh:inputs` discards only Nx's `nx_files.nxt`
file-hash metadata, preserving task cache records and outputs. Nx 23.1.2
[compares whole-second modification times](https://github.com/nrwl/nx/blob/23.1.2/packages/nx/src/native/workspace/files_hashing.rs),
so a fast rewrite can otherwise reuse a stale hash even in a new process.
This workaround belongs to the shared task stages and can be revisited when Nx
is upgraded.

The common task stages live in `nx.json`'s `targetDefaults`, scoped to projects
tagged `type:go-module`. Each module opts in with empty `fmt`, `verify`, `fix`,
`fix:lint`, `refresh:inputs`, `check`, and `lint` target objects, overriding
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

Application cleanup and CI lint enforcement remain deferred; CI tests the tool
and its configuration. CodeQL builds the tool module alongside application modules,
excluding test fixtures from standalone builds.
