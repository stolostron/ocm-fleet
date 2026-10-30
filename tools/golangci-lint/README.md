# Go lint tooling

We build golangci-lint in this isolated Go module so we can add custom analyzers
for domain and architecture rules later. Only stock linters are enabled now;
the domain field-access analyzer is deferred to follow-up work in the contributor
effectiveness epic.

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
