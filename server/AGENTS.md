Before designing or changing server code, read the linked guidance for every
row that applies.

| When the task touches                                                    | Read                                                                      |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------- |
| Values, entities, aggregates, transitions, or mutable ownership          | [Domain](docs/domain.md)                                                  |
| Valid zeros, absence, or result guards                                   | [Zero values and optionality](docs/domain.md#zero-values-and-optionality) |
| Constructors, parser signatures, options, commands, or parameter structs | [Constructors](docs/constructors.md)                                      |
| Raw DTOs, SQL scans, snapshots, codecs, or workflow payloads             | [Domain serialization](docs/serialization.md)                             |
| Durable execution, replay, or engine integration                         | [Durable workflows](docs/durable-workflows.md)                            |
| Domain directives, type enrollment, or lint findings                     | [Domain audits](../tools/golangci-lint/README.md#domain-audits)           |
| Layer boundaries or service responsibilities                             | [Internal architecture](docs/internal-architecture.md)                    |
| Package placement                                                        | [Package organization](docs/package-structure.md)                         |
| Tracing, logging, or metrics                                             | [Observer pattern](docs/observer-pattern.md)                              |
| Tests or fixtures                                                        | [Testing](docs/testing.md)                                                |

When changes affect integration-tested behavior, also run `npx nx run server:test-integration` (requires a container runtime).
