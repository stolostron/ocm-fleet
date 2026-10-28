# Go lint tooling

We build golangci-lint in this isolated Go module so we can add analyzers for our
domain and architecture rules. The first, `fleetdomain`, checks private domain
field access outside the owning type's methods, following
[the domain guide](../../server/docs/domain.md#field-access-and-getters).

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

We considered these alternatives:

- **Upstream release binaries with our archive hashes:** fast setup and upstream's
  release build, but our analyzers need a custom executable. We would also keep
  maintaining a platform-specific installer and hash updater.
- **Versioned `go install`:** convenient for the upstream executable, but it
  ignores our local module files and does not compile in our analyzers.
- **`golangci-lint custom`:** the documented automatic module-plugin builder.
  It clones a release tag and resolves the combined dependencies in a temporary
  checkout. Our bootstrap binary hashes would not pin that checkout, and the
  combined dependency files would not be committed for review.
- **Go `.so` plugins:** require matching build environments and CGO. Upstream
  recommends the Module Plugin System instead.

We use that recommended module-plugin API with a small executable wrapper,
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

# Select projects by their Nx names; ocm-fleet selects the lint tooling.
npx nx run ocm-fleet:verify:go --projects=server

# Tool development and updates:
npx nx run ocm-fleet:setup:golangci-lint
npx nx run ocm-fleet:test:golangci-lint-analyzers
npx nx run ocm-fleet:lint:golangci-lint
npx nx run ocm-fleet:test:golangci-lint-config
npx nx run ocm-fleet:test:go-workflow
npx nx run ocm-fleet:update:golangci-lint --release=v2.15.0
```

`fmt:go` applies the shared formatters, including import organization. `verify:go`
does that first, then runs lint and the normal tests. `fix:go` additionally applies
supported linter fixes before rechecking; review those changes. All three accept
comma-separated `--projects` and otherwise cover the application Go projects and
this module. Generated files and analyzer `testdata` fixtures are excluded from
rewrites. A fix pass can leave findings behind, so verification still runs and
fails if those findings or test failures remain. `lint:go` remains a check without
source edits for CI and other callers that need that behavior.

Source-rewriting tasks are uncached. They finish editing before starting a fresh
Nx invocation for lint and tests, ensuring checks hash the resulting source.
The tool build and individual checks retain their normal cache settings.

Choose an explicit stable v2 release when updating. The update task uses Go's
module tooling in a disposable copy, then publishes `go.mod` and `go.sum` for
review. Review release notes, dependency changes, and compiler compatibility;
raise the compiler pin explicitly when needed. Run the analyzer, setup, and
configuration tests and lint the tooling before committing. Ordinary builds use read-only module
resolution and staged dependency files, rejecting any checksum changes.

The executable goes to `bin/tools/golangci-lint`. Nx caches it using tool source,
module files, the build helper, and the selected compiler and host settings.
Application lint cache keys include these inputs too. Compiler selection is
isolated from application linting; building the tool disables CGO and ambient
Go flags, experiments, and workspace overrides.

The custom version includes the executable's SHA-256 hash, which also identifies
golangci-lint's analysis cache. Changes to our analyzers, dependencies, or compiler
therefore invalidate native cached findings as well as Nx results.

To add an analyzer, implement a standard `go/analysis.Analyzer`, cover accepted
and rejected examples with `analysistest`, and register it through `plugin`.
The current field-access check covers private fields with a matching exported,
zero-argument getter of the same result type. It respects constructors and the
documented `snapshot.go` / `etag.go` exceptions, including in tests. Fields without
such getters and mutation through returned references remain outside its scope.
Application cleanup and CI lint enforcement remain deferred; CI tests the tool
and its configuration. CodeQL builds the tool module alongside the application
modules, excluding analyzer `testdata` fixtures.
