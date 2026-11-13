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

test("domain checks can be explicitly disabled or selected for a focused audit", async (t) => {
  const { directory, lint, issues, format } = await fixture(t);
  await writeFile(
    join(directory, "main.go"),
    `package lintfixture

// ID is constructed explicitly.
//domain:constructors NewID
type ID string

func NewID(raw string) ID { return ID(raw) }
func Check(raw string) ID { return ID(raw) }
func Missing() ID { var id ID; return id }
`,
  );
  await format();
  await assert.doesNotReject(
    lint({}, ["--disable=domainconstruct,domainzero,domainserialization"]),
  );
  await assert.rejects(
    lint({}, ["--enable-only=domainconstruct,domainzero,domainserialization"]),
    {
      code: 1,
    },
  );
  assert.deepEqual(
    [...new Set((await issues()).map((finding) => finding.FromLinter))].sort(),
    ["domainconstruct", "domainzero"],
  );
});

test("domain audit loads construction policies across packages and includes tests", async (t) => {
  const { directory, lint, issues, format } = await fixture(t, {
    domain: true,
  });
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
  const source = `package lintfixture

import "github.com/stolostron/ocm-fleet/lintfixture/value"

func Check(raw string) value.ID { return value.ID(raw) }
`;
  await writeFile(join(directory, "main.go"), source);
  await writeFile(
    join(directory, "main_test.go"),
    `package lintfixture

import (
	"testing"

	"github.com/stolostron/ocm-fleet/lintfixture/value"
)

func TestCheck(t *testing.T) { _ = value.ID("fixture | rule: fake; quoted") }
`,
  );

  await format();
  const audit = () => lint();
  await assert.rejects(audit(), { code: 1 });
  const findings = await issues();
  const construction = findings.filter(
    (finding) => finding.FromLinter === "domainconstruct",
  );
  assert.equal(construction.length, 2);
  assert.equal(findings.length, 3);
  for (const finding of construction) {
    assert.equal(finding.FromLinter, "domainconstruct");
    assert.match(finding.Text, /construct value\.ID through NewID/);
    const structured = domainFinding(finding);
    assert.equal(structured.Rule, "domainconstruct.constructor-bypass");
    assert.equal(structured.Certainty, "known");
    assert.equal(structured.Category, "construction");
    assert.equal(structured.Reason, "constructor-required");
    assert.equal(structured.Operation, "construction");
    assert.equal(
      structured.Type,
      "github.com/stolostron/ocm-fleet/lintfixture/value.ID",
    );
    if (finding.Pos.Filename.endsWith("main_test.go")) {
      assert.equal(
        structured.Value,
        'value.ID("fixture | rule: fake; quoted")',
      );
    }
  }

  await writeFile(
    join(directory, "main.go"),
    source.replace("value.ID(raw)", "value.NewID(raw)"),
  );
  await writeFile(
    join(directory, "main_test.go"),
    'package lintfixture\n\nimport "testing"\n\nfunc TestCheck(t *testing.T) { _ = Check("fixture") }\n',
  );
  await assert.doesNotReject(audit());
});

test("domain audit includes serialization policy and plain snapshot findings", async (t) => {
  const { directory, lint, issues, format } = await fixture(t, {
    domain: true,
  });
  await writeFile(
    join(directory, "main.go"),
    `package lintfixture

import "encoding/json"

//domain:constructors NewID
type ID string

func NewID(raw string) ID { return ID(raw) }
func Encode(id ID) ([]byte, error) { return json.Marshal(id) }
func Decode(raw []byte) error {
	var m struct { ID ID }
	return json.Unmarshal(raw, &m)
}

//domain:snapshot
type Snapshot struct { ID ID }
`,
  );
  await format();
  await assert.rejects(lint(), { code: 1 });
  const raw = await issues();
  const findings = raw.map(domainFinding);
  assert.equal(findings.length, 3);
  assert.deepEqual(findings.map((finding) => finding.Rule).sort(), [
    "domainserialization.snapshot-domain-value",
    "domainserialization.unsupported-codec",
    "domainserialization.unsupported-codec",
  ]);
  for (const finding of findings) {
    assert.equal(finding.Certainty, "known");
    assert.equal(finding.Category, "serialization");
  }
  const decode = findings.find((finding) => finding.Operation === "decode");
  assert.equal(decode.Value, "&m");
  assert.equal(decode.Path, "ID");
  assert.ok(raw.some((issue) => issue.Text.includes('; value: "&m"; type:')));
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

test("domain audit retains mutation provenance, observations, and imported constants", async (t) => {
  const { directory, lint, issues, format } = await fixture(t, {
    domain: true,
  });
  await mkdir(join(directory, "value"));
  await writeFile(
    join(directory, "value/id.go"),
    `package value

import "errors"

// ID must be nonempty.
//domain:constructors NewID
type ID string

func NewID(raw string) (ID, error) {
  if raw == "" { return "", errors.New("empty ID") }
  return ID(raw), nil
}

// Fixed is a reviewed domain constant.
const Fixed ID = "fixed" //nolint:domainconstruct // This fixed declaration is reviewed explicitly.
`,
  );
  await writeFile(
    join(directory, "main.go"),
    `package lintfixture

import "github.com/stolostron/ocm-fleet/lintfixture/value"

type Scanner interface { Scan(...any) error }

func Check(scanner Scanner) (value.ID, error) {
  var raw string
  if err := scanner.Scan(&raw); err != nil { return "", err }
  return value.ID(raw), nil
}

func Compare(id value.ID) bool { return id == "" }
func Observe(id value.ID) bool { return id == "fixed" }
func Copy() value.ID { return value.Fixed }
`,
  );
  await format();
  await assert.rejects(lint(), { code: 1 });
  const findings = await issues();
  assert.equal(findings.length, 3, JSON.stringify(findings));
  assert.equal(
    findings.filter((finding) => finding.FromLinter === "domainconstruct")
      .length,
    1,
  );
  const comparison = findings
    .map(domainFinding)
    .filter((finding) => finding?.Rule === "domainzero.zero-comparison");
  assert.equal(comparison.length, 1);
  assert.equal(comparison[0].Certainty, "known");
  assert.equal(comparison[0].Category, "zero-comparison");
  assert.equal(comparison[0].Reason, "zero-check-on-required-value");
  assert.equal(comparison[0].Value, "id");
  const mutation = findings.find(
    (finding) => domainFinding(finding)?.Rule === "domainzero.invalid-zero",
  );
  assert.ok(mutation, JSON.stringify(findings));
  const structured = domainFinding(mutation);
  assert.equal(structured.Category, "possible-zero");
  assert.equal(structured.Rule, "domainzero.invalid-zero");
  assert.equal(structured.Certainty, "possible");
  assert.equal(structured.Reason, "zero-initialized-storage");
  assert.equal(structured.Operation, "return");
  assert.equal(structured.Value, "raw");
  assert.equal(structured.Path, "");
  assert.equal(
    structured.Type,
    "github.com/stolostron/ocm-fleet/lintfixture/value.ID",
  );
  for (const kind of ["source", "mutation", "conversion", "use"]) {
    assert.ok(
      structured.Provenance.some(
        (step) => step.Kind === kind && step.Pos.Line > 0,
      ),
      kind,
    );
  }
  assert.equal(
    structured.Provenance.find((step) => step.Kind === "mutation").Message,
    "Scan may modify raw; nonzero initialization afterward is unproven",
  );
  assert.ok(
    !findings.some((finding) => finding.Text.includes("(related information)")),
  );

  const source = await readFile(join(directory, "main.go"), "utf8");
  await writeFile(
    join(directory, "main.go"),
    source
      .replace("return value.ID(raw), nil", "return value.NewID(raw)")
      .replace('return id == ""', "return id == value.Fixed"),
  );
  await format();
  await assert.doesNotReject(lint());
});

test("domain audit distinguishes variadic uses from real member paths", async (t) => {
  const { directory, lint, issues, format } = await fixture(t, {
    domain: true,
  });
  await writeFile(
    join(directory, "main.go"),
    `package lintfixture

import "fmt"

// ID is constructed explicitly.
//domain:constructors NewID
type ID string

func NewID(raw string) ID { return ID(raw) }

func Check() {
  var bad ID
  _ = fmt.Sprint("id: ", bad)
  _ = fmt.Sprint("request: ", struct{ ID ID }{ID: bad})
  _ = fmt.Sprint("ids: ", []ID{bad})
  _ = fmt.Sprint([]any{bad}...)
}
`,
  );
  await format();
  await assert.rejects(lint(), { code: 1 });
  const findings = (await issues()).map(domainFinding);
  assert.equal(findings.length, 4);
  assert.ok(
    findings.every((finding) => finding?.Rule === "domainzero.invalid-zero"),
  );
  assert.deepEqual(
    findings
      .sort((a, b) => a.Pos.Line - b.Pos.Line)
      .map(({ Operation, Path, Message, Certainty }) => ({
        Operation,
        Path,
        Message,
        Certainty,
      })),
    [
      {
        Operation: "variadic-argument",
        Path: "",
        Message:
          "variadic argument is an empty lintfixture.ID; empty values are invalid",
        Certainty: "known",
      },
      {
        Operation: "variadic-argument",
        Path: "ID",
        Message:
          "variadic argument contains an empty lintfixture.ID at path ID; empty values are invalid",
        Certainty: "known",
      },
      {
        Operation: "variadic-argument",
        Path: "[0]",
        Message:
          "variadic argument contains an empty lintfixture.ID at path [0]; empty values are invalid",
        Certainty: "known",
      },
      {
        Operation: "argument",
        Path: "[0]",
        Message:
          "argument contains an empty lintfixture.ID at path [0]; empty values are invalid",
        Certainty: "known",
      },
    ],
  );
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

test("domain audit distinguishes valid zeros and checked error payloads across packages", async (t) => {
  const { directory, lint, issues, format } = await fixture(t, {
    domain: true,
  });
  await mkdir(join(directory, "value"));
  await writeFile(
    join(directory, "value/id.go"),
    `package value

import "errors"

// ID must be nonempty.
//domain:constructors NewID
type ID string

func NewID(raw string) (ID, error) {
	if raw == "" { return "", errors.New("empty ID") }
	return ID(raw), nil
}

// Total includes zero.
//domain:constructors NewTotal
//domain:zero-valid
type Total int

func NewTotal(raw int) Total { return Total(raw) }
`,
  );
  const source = `package lintfixture

import "github.com/stolostron/ocm-fleet/lintfixture/value"

func Check() value.ID {
	var id value.ID
	return id
}

func Checked(raw string) (value.ID, error) {
	id, err := value.NewID(raw)
	if err != nil { return "", err }
	return id, nil
}

func Total() value.Total {
	var total value.Total
	return total
}
`;
  await writeFile(join(directory, "main.go"), source);
  await writeFile(
    join(directory, "main_test.go"),
    `package lintfixture

import (
	"testing"

	"github.com/stolostron/ocm-fleet/lintfixture/value"
)

func TestCheck(t *testing.T) {
	var id value.ID
	t.Log(id)
}
`,
  );
  await format();
  const audit = () => lint();
  await assert.rejects(audit(), { code: 1 });
  const findings = await issues();
  assert.equal(findings.length, 2, JSON.stringify(findings));
  for (const finding of findings) {
    assert.equal(finding.FromLinter, "domainzero");
    assert.equal(domainFinding(finding).Rule, "domainzero.invalid-zero");
    assert.equal(
      domainFinding(finding).Type,
      "github.com/stolostron/ocm-fleet/lintfixture/value.ID",
    );
  }

  await writeFile(
    join(directory, "main.go"),
    source
      .replace("var id value.ID\n\treturn id", 'return value.NewID("valid")')
      .replace("func Check() value.ID", "func Check() (value.ID, error)"),
  );
  await writeFile(
    join(directory, "main_test.go"),
    'package lintfixture\n\nimport "testing"\n\nfunc TestCheck(t *testing.T) {\n\tid, err := Check()\n\tif err != nil { t.Fatal(err) }\n\tt.Log(id)\n}\n',
  );
  await format();
  await assert.doesNotReject(audit());
});

test("domain audit covers zero operations, boxed failures, collections, and generic factories", async (t) => {
  const { directory, lint, issues, format } = await fixture(t, {
    domain: true,
  });
  await mkdir(join(directory, "value"));
  await writeFile(
    join(directory, "value/id.go"),
    `package value

import "errors"

// ID must be nonempty.
//domain:constructors NewID
type ID string

func NewID(raw string) (ID, error) {
  if raw == "" { return "", errors.New("empty ID") }
  return ID(raw), nil
}

var ErrMissing = errors.New("missing")

func Terminal(err error) error {
  if err == nil { return nil }
  return errors.Join(err)
}

func Zero[T any]() (value T) { return }

func PointerFailure() (*ID, error) { return new(ID), ErrMissing }

func BoxedFailure() (any, error) {
  var id ID
  var payload any = id
  return payload, ErrMissing
}
`,
  );
  const valid = `package lintfixture

import "github.com/stolostron/ocm-fleet/lintfixture/value"

func Filled(raw []string) ([]value.ID, error) {
  ids := make([]value.ID, len(raw))
  for index, input := range raw {
    var err error
    ids[index], err = value.NewID(input)
    if err != nil { return nil, err }
  }
  return ids, nil
}

func Failure() (value.ID, error) { return "", value.Terminal(value.ErrMissing) }

func Optional() *value.ID { return value.Zero[*value.ID]() }
`;
  await writeFile(
    join(directory, "main.go"),
    `${valid}

func Generic() value.ID { return value.Zero[value.ID]() }

func Collection() []value.ID { return make([]value.ID, 2) }

func Pointer() *value.ID {
  id, _ := value.PointerFailure()
  return id
}

func Boxed() any {
  payload, _ := value.BoxedFailure()
  return payload
}

func Send(ch chan<- value.ID) {
  var id value.ID
  select { case ch <- id: default: }
}

func Compare(valid value.ID) bool {
  var id value.ID
  return id == valid
}
`,
  );
  await format();
  const audit = () => lint();
  await assert.rejects(audit(), { code: 1 });
  const findings = await issues();
  assert.equal(findings.length, 6, JSON.stringify(findings));
  for (const finding of findings) {
    assert.equal(finding.FromLinter, "domainzero");
    assert.equal(domainFinding(finding).Rule, "domainzero.invalid-zero");
    assert.equal(
      domainFinding(finding).Type,
      "github.com/stolostron/ocm-fleet/lintfixture/value.ID",
    );
  }

  await writeFile(join(directory, "main.go"), valid);
  await format();
  await assert.doesNotReject(audit());
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
    lint({ GOTOOLCHAIN: "invalid-ambient-toolchain" }),
  );
});

test("still rejects modules requiring a newer Go version than the linter supports", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "go.mod"),
    "module lintfixture\n\ngo 1.99.0\n",
  );
  await assert.rejects(lint({ GOTOOLCHAIN: "local" }), (error) => {
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

// Real modules exercise config paths, formatter checks, exclusions, and exit
// statuses without third-party dependencies or running the fixture's Go code.
async function fixture(t, { project = "", domain = false } = {}) {
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
    lint: (env = {}, args = []) =>
      run(
        binary,
        [
          "run",
          "--config",
          configuration,
          "--output.text.path=stdout",
          `--output.json.path=${report}`,
          ...(domain
            ? ["--enable-only=domainconstruct,domainzero,domainserialization"]
            : []),
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
