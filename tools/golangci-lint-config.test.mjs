import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { domainFinding, writeDomainReports } from "./domain-audit.mjs";

const run = promisify(execFile);
const binary = fileURLToPath(
  new URL("../bin/tools/golangci-lint", import.meta.url),
);
const config = fileURLToPath(new URL("../.golangci.yaml", import.meta.url));

test("identifies the exact custom executable in its version for analysis caching", async () => {
  const digest = createHash("sha256")
    .update(await readFile(binary))
    .digest("hex");
  const { stdout } = await run(binary, ["version"]);
  assert.match(
    stdout,
    new RegExp(`version v?2\\.\\d+\\.\\d+-fleet\\.${digest}\\b`),
  );
});

test("validates the shared config with our pinned linter", async () => {
  await assert.doesNotReject(
    run(binary, ["config", "verify", "--config", config]),
  );
});

test("accepts formatted Go code with checked errors", async (t) => {
  const { lint } = await fixture(t);
  await assert.doesNotReject(lint());
});

test("standard lint checks domain types as they are enrolled", async (t) => {
  const { directory, lint, issues, format } = await fixture(t);
  const path = join(directory, "main.go");
  const source = `package lintfixture

import "encoding/json"

type ID string

func NewID(raw string) ID { return ID(raw) }
func Check(raw string) ID { return ID(raw) }
func Missing() ID { var id ID; return id }
func Encode(id ID) ([]byte, error) { return json.Marshal(id) }
`;
  const expected = ["domainconstruct", "domainserialization", "domainzero"];
  const reportedLinters = async () =>
    [...new Set((await issues()).map((finding) => finding.FromLinter))].sort();

  await writeFile(path, source);
  await format();
  await assert.doesNotReject(lint());

  await writeFile(
    path,
    source.replace(
      "type ID string",
      "// ID is constructed explicitly.\n//domain:constructors NewID\ntype ID string",
    ),
  );
  await format();
  await assert.rejects(lint(), { code: 1 });
  assert.deepEqual(await reportedLinters(), expected);

  await assert.doesNotReject(
    lint({
      args: ["--disable=domainconstruct,domainzero,domainserialization"],
    }),
  );
  await assert.rejects(
    lint({
      args: ["--enable-only=domainconstruct,domainzero,domainserialization"],
    }),
    {
      code: 1,
    },
  );
  assert.deepEqual(await reportedLinters(), expected);
});

test("focused domain lint preserves same-line findings and provenance in JSON", async (t) => {
  const { directory, lint, issues } = await fixture(t);
  await mkdir(join(directory, "value"));
  await writeFile(
    join(directory, "value/id.go"),
    `package value

// ID is constructed explicitly.
//domain:constructors NewID
type ID string

func NewID(raw string) ID { return ID(raw) }
`,
  );
  await writeFile(
    join(directory, "main_test.go"),
    `package lintfixture

import "github.com/stolostron/ocm-fleet/lintfixture/value"

func uncheckedID(raw string) value.ID {
	return value.ID(raw)
}
`,
  );

  await assert.rejects(
    lint({
      args: ["--enable-only=domainconstruct,domainzero,domainserialization"],
    }),
    { code: 1 },
  );
  const raw = await issues();
  assert.equal(raw.length, 2, JSON.stringify(raw));
  const findings = raw.map(domainFinding);
  const construction = findings.find(
    (finding) => finding?.Rule === "domainconstruct.constructor-bypass",
  );
  const zero = findings.find(
    (finding) => finding?.Rule === "domainzero.invalid-zero",
  );
  assert.ok(construction, "expected a structured constructor finding");
  assert.ok(zero, "expected a structured zero-value finding");
  assert.match(construction.Pos.Filename, /[/\\]main_test\.go$/);
  assert.equal(zero.Pos.Filename, construction.Pos.Filename);
  assert.equal(zero.Pos.Line, construction.Pos.Line);
  assert.ok(
    zero.Provenance.some(
      ({ Kind, Pos }) =>
        Kind === "source" &&
        Pos.Filename.endsWith("main_test.go") &&
        Pos.Line > 0 &&
        Pos.Line < zero.Pos.Line,
    ),
    "expected the zero-value finding to retain its source location",
  );
});

test("domain audit decodes explicit metadata independently of wording and whitespace", async () => {
  const issue = {
    FromLinter: "domainzero",
    Pos: { Filename: "agent.go", Line: 105, Column: 2 },
    Text: `Wording can change without changing the finding.
  | rule: domainzero.invalid-zero; certainty: possible; reason: input-may-be-zero
  | operation: return; value: "s.Name"; type: "example.com/fleet/domain.ResourceID"; path: ""
  | source: s.Name enters this method as a plain string ("agent.go", line 101, column 7)
  | conversion: ResourceID(s.Name) changes the type without validation ("agent.go", line 105, column 26)
  | use: return ("agent.go", line 105, column 2)`,
  };
  for (const text of [issue.Text, issue.Text.replace(/\s+/g, " ")]) {
    const finding = domainFinding({ ...issue, Text: text });
    assert.equal(finding.Rule, "domainzero.invalid-zero");
    assert.equal(finding.Certainty, "possible");
    assert.equal(finding.Reason, "input-may-be-zero");
    assert.equal(finding.Operation, "return");
    assert.equal(finding.Value, "s.Name");
    assert.equal(finding.Type, "example.com/fleet/domain.ResourceID");
    assert.equal(finding.Path, "");
    assert.deepEqual(finding.Pos, issue.Pos);
    assert.equal(finding.Category, "possible-zero");
    assert.deepEqual(
      finding.Provenance.map((step) => step.Kind),
      ["source", "conversion", "use"],
    );
    assert.equal(finding.Provenance[0].Pos.Line, 101);
  }
});

test("domain audit preserves quoted expressions, field paths, and escaped provenance", async () => {
  const value = 'NewID("x; | rule: y")\n';
  const filename = 'a | rule: "source".go';
  const quote = (text) => JSON.stringify(text).replaceAll("|", "\\u007c");
  const finding = domainFinding({
    FromLinter: "domainzero",
    Pos: { Filename: "use.go", Line: 12, Column: 2 },
    Text: `return may contain an empty domain.ID at path Reports[0].ID
  | rule: domainzero.invalid-zero; certainty: known; reason: zero-value
  | operation: return; value: ${quote(value)}; type: "example.com/domain.ID"; path: "Reports[0].ID"
  | source: a \\| b\\nuses \\\\ representation (${quote(filename)}, line 1, column 1)`,
  });
  assert.equal(finding.Value, value);
  assert.equal(finding.Path, "Reports[0].ID");
  assert.equal(finding.Provenance[0].Pos.Filename, filename);
  assert.equal(finding.Provenance[0].Message, "a | b\nuses \\ representation");
});

test("domain audit rejects invalid metadata without guessing from prose", async () => {
  for (const text of [
    "argument may use known invalid zero of domain.ID",
    `construct domain.ID through NewID
  | rule: domainconstruct.constructor-bypass; certainty: known; reason: constructor-required
  | operation: construction; value: "ID(raw)"; type: "example.com/domain.ID"; path: ""`,
    `return produces an empty domain.ID
  | rule: domainzero.invalid-zero; certainty: not-applicable; reason: zero-value
  | operation: return; value: "ID(0)"; type: "example.com/domain.ID"; path: ""`,
    `return produces an empty domain.ID
  | rule: domainzero.invalid-zero; certainty: known; reason: zero-value
  | operation: return; value: "invalid\\q"; type: "example.com/domain.ID"; path: ""`,
  ]) {
    assert.equal(
      domainFinding({ FromLinter: "domainzero", Text: text }),
      undefined,
    );
  }
});

test("domain audit separates application and test helpers and writes navigable provenance", async (t) => {
  const { directory } = await fixture(t);
  const reportPath = join(directory, ".nx/reports/domainconstruct/server.json");
  const issue = (filename, text) => ({
    FromLinter: "domainzero",
    Text: text,
    Pos: { Filename: join(directory, filename), Line: 12, Column: 3 },
  });
  const text = (
    message,
    certainty = "known",
    reason = "zero-value",
    rule = "domainzero.invalid-zero",
  ) => `${message}
  | rule: ${rule}; certainty: ${certainty}; reason: ${reason}
  | operation: argument; value: "raw"; type: "example.com/domain.ID"; path: ""`;
  await writeDomainReports({
    workspace: directory,
    moduleRoot: "server",
    reportPath,
    report: {
      Issues: [
        issue(
          "server/internal/application/service.go",
          text(
            "argument may be an empty domain.ID; empty values are invalid",
            "possible",
            "zero-initialized-storage",
          ) +
            `\n  | source: raw storage starts at zero ("internal/application/service.go", line 4, column 2)\n  | mutation: Scan may modify raw; nonzero initialization afterward is unproven (${JSON.stringify(join(directory, "server/internal/application/service.go"))}, line 8, column 2)`,
        ),
        issue(
          "server/internal/application/service_test.go",
          text("argument is an empty domain.ID; empty values are invalid"),
        ),
        issue(
          "server/internal/domain/queryrepotest/contract.go",
          text(
            "zero comparison for domain.ID",
            "known",
            "zero-check-on-required-value",
            "domainzero.zero-comparison",
          ),
        ),
        issue(
          "server/internal/infrastructure/postgres/testdb.go",
          text("argument is an empty domain.ID; empty values are invalid"),
        ),
        issue(
          "server/internal/testutil/fixtures.go",
          text("argument is an empty domain.ID; empty values are invalid"),
        ),
      ],
    },
  });
  const main = JSON.parse(
    await readFile(reportPath.replace(".json", "-main.json"), "utf8"),
  );
  const tests = JSON.parse(
    await readFile(reportPath.replace(".json", "-test.json"), "utf8"),
  );
  assert.equal(main.Issues.length, 1);
  assert.equal(tests.Issues.length, 4);
  assert.deepEqual(main.Issues[0].Domain.Pos, main.Issues[0].Pos);
  assert.equal(main.Issues[0].Domain.Provenance[1].Kind, "mutation");
  assert.equal(
    main.Issues[0].Domain.Provenance[0].Pos.Filename,
    join(directory, "server/internal/application/service.go"),
  );
  const markdown = await readFile(
    reportPath.replace(".json", "-main.md"),
    "utf8",
  );
  assert.match(
    markdown,
    /\]\(\.\.\/\.\.\/\.\.\/server\/internal\/application\/service\.go\)/,
  );
  assert.match(markdown, /line 8/);
  assert.doesNotMatch(markdown, /service\.go:\d+\)/);
});

test("formatting preserves registration comments and produces lint-clean, stable imports", async (t) => {
  const { directory, lint } = await fixture(t);
  // A local dependency reproduces third-party import grouping without downloads.
  await writeFile(
    join(directory, "go.mod"),
    "module github.com/stolostron/ocm-fleet/lintfixture\n\ngo 1.26.0\n\nrequire example.com/dependency v0.0.0\n\nreplace example.com/dependency => ./dependency\n",
  );
  await mkdir(join(directory, "dependency"));
  await writeFile(
    join(directory, "dependency/go.mod"),
    "module example.com/dependency\n\ngo 1.26.0\n",
  );
  for (const name of ["first", "parser", "second", "registration"]) {
    await mkdir(join(directory, "dependency", name));
    await writeFile(
      join(directory, "dependency", name, "value.go"),
      `package ${name}\n\nfunc Value(value int) int { return value }\n`,
    );
  }
  const path = join(directory, "main.go");
  // Two third-party imports before the registration comment trigger the conflict.
  await writeFile(
    path,
    `package lintfixture

import (
    "fmt"
    "os"

    "example.com/dependency/first"
    "example.com/dependency/parser"
    // Register custom handlers for their initialization side effects.
    _ "example.com/dependency/registration"
    "example.com/dependency/second"
)

func Check(value int) string {return fmt.Sprint(first.Value(value) + parser.Value(value) + second.Value(value))}

func Label(value int) string {return strconv.Itoa(value)}
`,
  );
  const format = () =>
    run(binary, ["fmt", "--config", config, "./..."], {
      cwd: directory,
      env: { ...process.env, GOWORK: "off" },
    });

  await format();
  const formatted = await readFile(path, "utf8");
  assert.match(formatted, /\/\/ Register custom handlers/);
  assert.match(formatted, /"strconv"/);
  assert.equal(formatted.includes('"os"'), false);
  await assert.doesNotReject(
    lint().catch((error) => {
      t.diagnostic(error.stdout + error.stderr);
      throw error;
    }),
  );
  await format();
  assert.equal(await readFile(path, "utf8"), formatted);
});

test("loads packages with its build toolchain despite an ambient override", async (t) => {
  const { lint } = await fixture(t);
  await assert.doesNotReject(
    lint({ env: { GOTOOLCHAIN: "invalid-ambient-toolchain" } }),
  );
});

test("still rejects modules requiring a newer Go version than the linter supports", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "go.mod"),
    "module lintfixture\n\ngo 1.99.0\n",
  );
  await assert.rejects(lint({ env: { GOTOOLCHAIN: "local" } }), (error) => {
    assert.match(
      error.stdout + error.stderr,
      /used to build golangci-lint is lower than the targeted Go version \(1\.99\.0\)/,
    );
    return true;
  });
});

test("reports unchecked production errors with a failing exit status and absolute paths", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(join(directory, "main.go"), uncheckedError);
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(
      error.stdout + error.stderr,
      /main\.go.*os\.Chdir.*not checked.*errcheck/,
    );
    assert.match(error.stdout + error.stderr, /^\/[^\n]*\/main\.go:\d+:\d+:/m);
    return true;
  });
});

test("counts post-increments as reads while still reporting unused declarations", async (t) => {
  const { directory, lint } = await fixture(t);
  const path = join(directory, "main.go");
  const source =
    "package lintfixture\n\nfunc Check(limit int) {\n\tfor counter := 0; limit > 0; counter++ {\n\t\tlimit--\n\t}\n}\n";
  await writeFile(path, source);
  await assert.doesNotReject(lint());

  await writeFile(path, source + "\nvar unusedVariable int\n");
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /unusedVariable.*unused/);
    return true;
  });
});

test("reports unchecked operation errors in tests", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "main_test.go"),
    'package lintfixture\n\nimport (\n\t"os"\n\t"testing"\n)\n\nfunc TestCheck(t *testing.T) {\n\tos.Chdir(".")\n}\n',
  );
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(
      error.stdout + error.stderr,
      /main_test\.go.*os\.Chdir.*errcheck/,
    );
    return true;
  });
});

test("temporary cleanup exclusions require both an existing project and a matching operation", async (t) => {
  const existing = await fixture(t, { project: "server" });
  const close =
    'package lintfixture\n\nimport "io"\n\nfunc Close(resource io.Closer) {\n\tresource.Close()\n}\n';
  await writeFile(join(existing.directory, "main.go"), close);
  await assert.doesNotReject(existing.lint());

  await writeFile(
    join(existing.directory, "main.go"),
    close.replace('import "io"', 'import (\n\t"io"\n\t"os"\n)') +
      '\nfunc ChangeDirectory() {\n\tos.Chdir(".")\n}\n',
  );
  await assert.rejects(existing.lint(), { code: 1 });
  const issues = await existing.issues();
  assert.equal(issues.length, 1);
  assert.match(issues[0].Text, /os\.Chdir/);

  const added = await fixture(t, { project: "another-go-module" });
  await writeFile(join(added.directory, "main.go"), close);
  await assert.rejects(added.lint(), { code: 1 });
  assert.match((await added.issues())[0].Text, /Close.*not checked/);
});

test("temporary test exclusions leave bug checks active", async (t) => {
  const { directory, lint, issues } = await fixture(t, { project: "server" });
  const path = join(directory, "main_test.go");
  const source =
    'package lintfixture\n\nimport (\n\t"os"\n\t"testing"\n)\n\nfunc TestCheck(t *testing.T) {\n\tos.Chdir(".")\n}\n';
  await writeFile(path, source);
  await assert.doesNotReject(lint());

  await writeFile(
    path,
    source
      .replace('"os"', '"os"\n\t"regexp"')
      .replace('os.Chdir(".")', 'os.Chdir(".")\n\tregexp.MustCompile("[")'),
  );
  await assert.rejects(lint(), { code: 1 });
  const findings = await issues();
  assert.equal(findings.length, 1);
  assert.equal(findings[0].FromLinter, "staticcheck");
  assert.match(findings[0].Text, /SA1000/);
});

test("inventory decoding exclusions leave other errors in the same file active", async (t) => {
  const { directory, lint, issues } = await fixture(t, {
    project: "server/internal/infrastructure/sqlite",
  });
  const path = join(directory, "extension_resource_repo.go");
  const source =
    'package lintfixture\n\nimport "encoding/json"\n\nfunc Decode(data []byte) map[string]string {\n\tlabels := map[string]string{}\n\tjson.Unmarshal(data, &labels)\n\treturn labels\n}\n';
  await writeFile(path, source);
  await assert.doesNotReject(lint());

  await writeFile(
    path,
    source
      .replace(
        'import "encoding/json"',
        'import (\n\t"encoding/json"\n\t"os"\n)',
      )
      .replace("return labels", 'os.Chdir(".")\n\treturn labels'),
  );
  await assert.rejects(lint(), { code: 1 });
  const findings = await issues();
  assert.equal(findings.length, 1);
  assert.equal(findings[0].FromLinter, "errcheck");
  assert.match(findings[0].Text, /os\.Chdir/);
});

test("excludes generated code only with the standard header", async (t) => {
  const { directory, lint } = await fixture(t);
  const path = join(directory, "generated.go");
  const source = uncheckedError.replace("Check()", "Generated()");
  for (const header of ["", "// Code generated by lint fixture.\n\n"]) {
    await writeFile(path, header + source);
    await assert.rejects(lint(), (error) => {
      assert.equal(error.code, 1);
      assert.match(
        error.stdout + error.stderr,
        /generated\.go.*os\.Chdir.*errcheck/,
      );
      return true;
    });
  }
  await writeFile(
    path,
    "// Code generated by lint fixture. DO NOT EDIT.\n\n" + source,
  );
  await assert.doesNotReject(lint());
});

test("reports formatting issues without modifying sources", async (t) => {
  const { directory, lint } = await fixture(t);
  const path = join(directory, "main.go");
  const source = "package lintfixture\n\nfunc Check() int {return 1}\n";
  await writeFile(path, source);
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(
      error.stdout + error.stderr,
      /File is not properly formatted \((?:gofmt|goimports)\)/,
    );
    return true;
  });
  assert.equal(await readFile(path, "utf8"), source);
});

test("reports map copy loops and replaces them only when fixes are requested", async (t) => {
  const { directory, lint, issues } = await fixture(t);
  const path = join(directory, "main.go");
  const source = `package lintfixture

func Merge(dst, src map[string]int) {
	for k, v := range src {
		// Source entries take precedence over existing values.
		dst[k] = v
	}
}
`;
  await writeFile(path, source);

  await assert.rejects(lint(), { code: 1 });
  const findings = await issues();
  assert.equal(findings.length, 1);
  assert.equal(findings[0].FromLinter, "mapcopy");
  assert.match(findings[0].Text, /maps\.Copy/);
  assert.equal(await readFile(path, "utf8"), source);

  await lint({ args: ["--fix", "--issues-exit-code=0"] });
  const fixed = await readFile(path, "utf8");
  assert.match(fixed, /import "maps"/);
  assert.match(fixed, /Source entries take precedence over existing values/);
  assert.match(fixed, /maps\.Copy\(dst, src\)/);
  assert.doesNotMatch(fixed, /for .*range/);
  await lint();
});

test("leaves map copy loops with potentially observable evaluation order alone", async (t) => {
  const { directory, lint } = await fixture(t);
  const path = join(directory, "main.go");
  const source = `package lintfixture

import "iter"

func WithDestinationCall(dst func() map[string]int, src map[string]int) {
	for k, v := range src {
		dst()[k] = v
	}
}

func WithSourceCall(dst map[string]int, src func() map[string]int) {
	for k, v := range src() {
		dst[k] = v
	}
}

func WithIterator(dst map[string]int, src iter.Seq2[string, int]) {
	for k, v := range src {
		dst[k] = v
	}
}
`;
  await writeFile(path, source);
  await lint({ args: ["--fix", "--issues-exit-code=0"] });
  assert.equal(await readFile(path, "utf8"), source);
  await lint();
});

test("preserves the ordering policy of a floating-point sort", async (t) => {
  const { directory, lint } = await fixture(t);
  const path = join(directory, "main.go");
  const source = `package lintfixture

import "sort"

func Order(values []float64) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}
`;
  await writeFile(path, source);
  await lint({ args: ["--fix", "--issues-exit-code=0"] });
  assert.equal(await readFile(path, "utf8"), source);
  await lint();
});

// Real modules exercise config paths, formatter checks, exclusions, and exit
// statuses without third-party dependencies or running the fixture's Go code.
async function fixture(t, { project = "" } = {}) {
  const root = await mkdtemp(join(tmpdir(), "ocm-fleet-lint-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const directory = join(root, project);
  await mkdir(directory, { recursive: true });
  const configuration = join(root, ".golangci.yaml");
  const report = join(root, "issues.json");
  await cp(config, configuration);
  await writeFile(
    join(directory, "go.mod"),
    "module github.com/stolostron/ocm-fleet/lintfixture\n\ngo 1.26.0\n",
  );
  await writeFile(
    join(directory, "main.go"),
    'package lintfixture\n\nimport "os"\n\nfunc Check() error {\n\treturn os.Chdir(".")\n}\n',
  );
  return {
    directory,
    format: () =>
      run(binary, ["fmt", "--config", configuration, "./..."], {
        cwd: directory,
        env: { ...process.env, GOWORK: "off" },
      }),
    issues: async () => JSON.parse(await readFile(report, "utf8")).Issues ?? [],
    lint: ({ env = {}, args = [] } = {}) =>
      run(
        binary,
        [
          "run",
          "--config",
          configuration,
          "--output.text.path=stdout",
          `--output.json.path=${report}`,
          ...args,
          "./...",
        ],
        {
          cwd: directory,
          env: { ...process.env, ...env, GOWORK: "off" },
        },
      ),
  };
}

const uncheckedError =
  'package lintfixture\n\nimport "os"\n\nfunc Check() {\n\tos.Chdir(".")\n}\n';
