Read the relevant docs below **before** writing or modifying code in this module. They contain design rules that are easy to violate without context. Paths are relative to this folder (server/).

For how to...

- decide what logic to put where (layering, service boundaries, method signatures), see docs/internal-architecture.md
- implement instrumentation (observability: tracing, logging, metrics), see docs/observer-pattern.md
- decide what package to use, see docs/package-structure.md
- write tests, see docs/testing.md
- write durable workflows and integrate with durable computing libraries, see docs/durable-workflows.md
- write or modify constructors, see docs/constructors.md
- write domain objects (aggregates, entities, values, where invariants belong, snapshot persistence), see docs/domain.md

When changes affect integration-tested behavior, also run `npx nx run server:test-integration` (requires a container runtime).
